//go:build windows

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/2ag/2ag/internal/patcher"
)

// ============================================================================
// Upstream Compatibility Guardian
//
// 与 Update Guardian（update_guardian.go）的分工，必须先说清楚，否则一定会被合并：
//
//	Update Guardian 答「官方安装是几点几、2Ag 冻结宿主是几点几」——版本事实。
//	本文件      答「跑着的这个宿主，还支撑得起哪些能力」——能力事实。
//
// 两者不能互相替代，因为**版本号不是兼容性的判据**。上游完全可能在同一版本号里
// 改掉一个 aria-label（补丁静默下沉到兜底层），也可能跨一个大版本却没动我们依赖的
// 任何东西。如果兼容性写成 `if version == "2.17.0" { supported = true }`，
// 那么第一次上游改 DOM 而没改版本号时，这个判断会以最自信的口吻给出错误答案。
//
// 所以本文件只做四件事：
//  ① Host Fingerprint —— 把「我们是拿哪个宿主、以哪种形态、在探测什么」钉住；
//  ② Capability Probe —— 逐项真去宿主 DOM 上试（探针在补丁侧，见 probeCapabilities）；
//  ③ Graceful Degradation —— 某项失败不影响其它项，逐项如实上报；
//  ④ Sync Readiness —— 只有关键能力有足够证据才说 READY FOR MANUAL SYNC。
//
// 明确不做：自动同步宿主、修改官方安装、拉取上游版本清单、把「全绿」当成兼容结论。
// ============================================================================

// CapabilityState 是单项能力的四态。
//
// 刻意复用任务书里的四个词而不是另造一套：这套词汇已经出现在本文件的其它地方
// （注入链路的 gate、官方面板的 level），再造同义词只会让两处读起来像两件事。
const (
	CapOK          = "OK"          // 探针命中，功能可用
	CapDegraded    = "DEGRADED"    // 能用，但走的是兜底层 —— 上游大概改了 DOM
	CapUnsupported = "UNSUPPORTED" // 宿主结构里根本没有这个东西（不是坏了，是不适用）
	CapFailed      = "FAILED"      // 本该有，实测出错
	CapUnknown     = "UNKNOWN"     // 探不到（没有常驻会话 / 官方形态 / 探针不可达）
)

// CapabilityItem 是一项能力的实测结论。
type CapabilityItem struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// HostFingerprint 是「我们在探测哪个宿主」的完整身份。
//
// 之所以把这么多字段一起带出来而不是只放一个版本号：能力探测的结果必须能被
// 回溯到具体的一次实测。用户下次问「为什么上次显示 OK 这次 DEGRADED」，
// 需要的是「上次探的是 2.17.0 的冻结宿主、这次探的是官方 2.18.1」这种能对上的事实，
// 而不是一个孤零零的版本字符串。
type HostFingerprint struct {
	Mode string `json:"mode"` // enhanced / official

	OfficialVersion string `json:"official_version"`
	OfficialExe     string `json:"official_exe"`

	FrozenVersion string `json:"frozen_version"`
	FrozenExe     string `json:"frozen_exe"`

	// RunningExe 是**此刻真的在跑**的那个宿主可执行文件（由探针读回进程路径）。
	// 它可能与 FrozenExe 不同（用户拿别的副本起的宿主），也可能为空（没在跑）。
	RunningExe string `json:"running_exe"`
	RunningPID int    `json:"running_pid"`
	// RunningVersion 直接读运行中那个 exe 的版本资源，而不是拿 FrozenVersion 顶替 ——
	// 本机实测两者确实不同（沙箱目录经 junction 解析后与 FrozenExe 字面不等），
	// 而「这份读数到底来自哪个版本」是它唯一需要能被回溯的东西。
	RunningVersion string `json:"running_version"`

	// CDPPort / HostPage 是探测所经由的通道。留空表示「这次探测没能连上」，
	// 而空值与 0 必须能被前端区分 —— 所以端口用 int 且只在连上时填。
	CDPPort  int    `json:"cdp_port"`
	HostPage string `json:"host_page"`

	// InjectionSizeBytes 是本次跑在宿主上的补丁字节数。
	// 它与「能力探测」同源，能回答「是不是旧补丁在报这些状态」。
	InjectionSizeBytes int `json:"injection_size_bytes"`

	ProbedAt string `json:"probed_at"`
}

// CapabilityReport 是 Compatibility Guardian 的完整读数。
type CapabilityReport struct {
	Fingerprint  HostFingerprint  `json:"fingerprint"`
	Capabilities []CapabilityItem `json:"capabilities"`

	OK          int `json:"ok"`
	Degraded    int `json:"degraded"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`
	Unknown     int `json:"unknown"`

	// Headline 是一句话结论。刻意不在「全绿」与「有 DEGRADED」之间做二值化：
	// UNSUPPORTED 不算坏（宿主就是没这个东西），FAILED 才是。
	Headline string `json:"headline"`

	// Sync 是「能否据此认为可以手工同步冻结宿主」的判据。
	Sync SyncReadiness `json:"sync"`
}

// SyncReadiness 回答「现在有没有足够证据去做一次手工同步」。
//
// 注意它的默认答案是 NOT_READY —— 这不是保守，是本轮唯一诚实的答案：
// 三态，刻意不是二值 —— 证据是分层的，压成一个布尔值必然丢掉信息：
//
//	NOT_READY              没有目标版本上的任何证据
//	PARTIAL_EVIDENCE       官方只读探测确认了 DOM 层锚点，但注入路径仍未被验证
//	READY_FOR_MANUAL_SYNC  已在目标版本上以增强形态跑过一次，关键能力全部 OK
//
// 为什么要中间那一档：官方宿主自己会开一个调试端口（本机实测，非 2Ag 所加 ——
// LaunchOfficialHost 传的是零参数），所以「不注入也能读到官方版上那些 aria-label /
// Tailwind 令牌还在不在」是可行的。但那**只覆盖 DOM 层**：补丁能不能挂上去、
// Shadow 树能不能建、中枢能不能活，这些必须在增强形态下才验得到。
// 把 DOM 层的绿说成「可以同步了」，就是把「发现新版本」谎报成「验证过新版能用」。
type SyncReadiness struct {
	State  string `json:"state"` // NOT_READY / PARTIAL_EVIDENCE / READY_FOR_MANUAL_SYNC
	Reason string `json:"reason"`

	// CoveredOnOfficial 列出**在目标版本上**已经拿到证据的能力。
	// 由官方只读探测映射而来（见 officialAnchorEvidence），因此每一项的 Detail
	// 都带「由官方只读探测推断」的前缀 —— 推断与实测在界面上必须能分开。
	CoveredOnOfficial []CapabilityItem `json:"covered_on_official"`

	// Uncovered 是「仍然没有证据」的能力，也就是**同步之后要重点盯什么**。
	// 这一栏是本轮真正的交付物：它把「升级后大概会坏什么」从猜测变成一份待验清单。
	Uncovered []CapabilityItem `json:"uncovered"`
}

// capabilityProbeExpr 让宿主的补丁实例回报能力自检结果。
//
// 表达式刻意保持极薄：真正的探测逻辑（八项、四态）在补丁里，因为只有那里能访问
// DOM 与补丁的内部状态。Go 侧只负责「取回来 + 解析 + 失败时如实说失败」。
//
// 与 PushHubState 的表达式同一风格：先问消费者在不在，再调用，最后校验返回形状 ——
// 旧版补丁会「成功地什么都不做」，把那种情况报成成功就等于制造假读数。
const capabilityProbeExpr = `(function () {
  if (!window.__2ag || typeof window.__2ag.probeCapabilities !== 'function') return 'no-consumer';
  var r = window.__2ag.probeCapabilities();
  if (!r || typeof r !== 'object' || !r.cap) return 'no-report';
  return JSON.stringify({
    cap: r.cap, ok: r.ok, degraded: r.degraded,
    unsupported: r.unsupported, failed: r.failed,
    version: (window.__2ag && window.__2ag.version) || '',
    href: location.href
  });
})()`

// capabilityIDs 是报告的行顺序。
//
// 固定顺序是刻意的：能力列表若随探测结果重排，用户两次看到的面板就没法逐行对比，
// 而「上次 OK 这次 DEGRADED」正是这张面板唯一要传达的信息。
var capabilityIDs = []string{
	"CDP_AVAILABLE",
	"WORKSPACE_TARGET",
	"RUNTIME_INJECTION",
	"SKIN_ROOT",
	"SKIN_BACKGROUND",
	"SIDEBAR_HOOK",
	"LOCALIZATION_ROOT",
	"G_HUB",
	"PROMPT_HOOK",
	"MODEL_SELECTOR",
}

// ProbeCapabilityReport 汇总一次兼容性探测。只读，不注入、不改配置。
//
// 官方形态下不发探测：那个形态的契约就是「不挂 CDP、不注入」，
// 这里若为了拿到读数去连一次 CDP，就把刚建立的边界破坏掉了。
// 如实回 UNKNOWN 并说明原因，比偷偷越界去取一个好数字重要。
func ProbeCapabilityReport() CapabilityReport {
	rep := CapabilityReport{
		Fingerprint:  buildHostFingerprint(),
		Capabilities: []CapabilityItem{},
	}

	if IsOfficialRuntime() {
		for _, id := range capabilityIDs {
			rep.Capabilities = append(rep.Capabilities, CapabilityItem{
				ID: id, State: CapUnknown,
				Detail: "官方形态不挂 CDP、不注入，因此无法（也不应该）探测宿主侧能力",
			})
		}
		rep.Unknown = len(capabilityIDs)
		rep.Headline = fmt.Sprintf("官方形态（%s）：本形态下 2Ag 不注入任何补丁，能力探测按设计不可用。",
			orUnknown(rep.Fingerprint.FrozenVersion))
		// 但**官方只读探测仍然照做** —— 它不注入、不写任何状态，用的是官方自己开的
		// 那个调试端口（官方每次启动都会开，不是 2Ag 加的）。
		// 这一点很关键：把官方形态直接判成「什么都测不了」，会浪费掉那份本来能拿到的、
		// 关于目标版本 DOM 锚点的实测证据 —— 而那正是 SyncReadiness 最需要的东西。
		covered, uncovered := splitByOfficialEvidence(rep)
		if len(covered) > 0 {
			rep.Sync = SyncReadiness{
				State:             "PARTIAL_EVIDENCE",
				CoveredOnOfficial: covered,
				Uncovered:         uncovered,
				Reason: fmt.Sprintf("官方形态下 2Ag 不注入，因此没有增强形态的能力读数；"+
					"但对官方 %s 的**只读探测**确认了 %d 项 DOM 层锚点仍然成立（见下表，均标注来源）。"+
					"注入路径那几项只能等真升级一次才知道。",
					orUnknown(rep.Fingerprint.OfficialVersion), len(covered)),
			}
		} else {
			rep.Sync = SyncReadiness{
				State: "NOT_READY", Uncovered: uncovered,
				Reason: "官方形态下不注入，且本次没有取到官方宿主的只读锚点读数（官方可能没在运行）。",
			}
		}
		return rep
	}

	raw, page, err := queryHostCapabilities()
	if err != nil {
		// 探测通道本身失败：这是 FAILED 而不是 UNSUPPORTED —— 前者是「本该有但出错」，
		// 后者是「宿主没这个东西」。把连不上说成「不支持」会让用户以为上游改版了。
		for _, id := range capabilityIDs {
			rep.Capabilities = append(rep.Capabilities, CapabilityItem{
				ID: id, State: CapFailed, Detail: "探测通道失败：" + err.Error(),
			})
		}
		rep.Failed = len(capabilityIDs)
		rep.Headline = "无法连接宿主 CDP 通道，本次兼容性探测失败：" + err.Error()
		rep.Sync = SyncReadiness{State: "NOT_READY", Reason: "探测通道失败，没有可用证据。"}
		return rep
	}
	rep.Fingerprint.HostPage = page

	parsed, perr := parseCapabilityPayload(raw)
	if perr != nil {
		for _, id := range capabilityIDs {
			rep.Capabilities = append(rep.Capabilities, CapabilityItem{
				ID: id, State: CapFailed, Detail: "宿主补丁的回执无法解析：" + perr.Error(),
			})
		}
		rep.Failed = len(capabilityIDs)
		rep.Headline = "宿主上的补丁实例无法完成能力探测（" + perr.Error() + "）。"
		rep.Sync = SyncReadiness{State: "NOT_READY", Reason: "宿主未回报可用读数。"}
		return rep
	}

	for _, id := range capabilityIDs {
		item, ok := parsed.cap[id]
		if !ok {
			// 补丁没报这一项（可能是旧版补丁）：如实标 UNKNOWN，
			// 绝不用「没报 = 好」或「没报 = 坏」填补空白。
			rep.Capabilities = append(rep.Capabilities, CapabilityItem{
				ID: id, State: CapUnknown, Detail: "宿主上的补丁实例未回报这一项（可能是旧版补丁）",
			})
			continue
		}
		rep.Capabilities = append(rep.Capabilities, CapabilityItem{
			ID: id, State: item.State, Detail: item.Detail,
		})
	}

	// 补丁没在预期列表里的项也带出来：将来补丁加了新能力、Go 侧忘了加 ID 时，
	// 用户至少能在面板上看到它，而不是它被静默丢掉。
	var extra []string
	for id := range parsed.cap {
		found := false
		for _, known := range capabilityIDs {
			if known == id {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, id)
		}
	}
	sort.Strings(extra)
	for _, id := range extra {
		rep.Capabilities = append(rep.Capabilities, CapabilityItem{
			ID: id, State: parsed.cap[id].State, Detail: parsed.cap[id].Detail,
		})
	}

	for _, c := range rep.Capabilities {
		switch c.State {
		case CapOK:
			rep.OK++
		case CapDegraded:
			rep.Degraded++
		case CapUnsupported:
			rep.Unsupported++
		case CapFailed:
			rep.Failed++
		default:
			rep.Unknown++
		}
	}

	rep.Headline = capabilityHeadline(rep)
	rep.Sync = assessSyncReadiness(rep)
	return rep
}

// capabilityHeadline 一句话概括，按「坏消息优先」排列。
//
// 顺序不是随便定的：FAILED 是「坏了」，DEGRADED 是「在退化」，UNSUPPORTED 是
// 「本来就没有」。把 UNSUPPORTED 排在前面会让一条无害的信息盖住真正的故障。
func capabilityHeadline(rep CapabilityReport) string {
	ver := rep.Fingerprint.runnerVersionLabel()
	switch {
	case rep.Failed > 0:
		return fmt.Sprintf("%s：%d 项能力实测失败（%s）。其余 %d 项仍可工作 —— 这是逐项降级，不是整体失效。",
			ver, rep.Failed, firstIDsWithState(rep, CapFailed), rep.OK)
	case rep.Degraded > 0:
		return fmt.Sprintf("%s：%d 项能力正在走兜底层（%s），语义层可能已被上游改动。其余 %d 项正常。",
			ver, rep.Degraded, firstIDsWithState(rep, CapDegraded), rep.OK)
	case rep.Unknown > 0 && rep.OK == 0:
		return ver + "：本次没有取得任何能力读数。"
	case rep.Unsupported > 0:
		return fmt.Sprintf("%s：%d 项能力在当前宿主上不存在（%s），%d 项正常 —— 「不存在」不等于「坏了」。",
			ver, rep.Unsupported, firstIDsWithState(rep, CapUnsupported), rep.OK)
	default:
		return fmt.Sprintf("%s：%d 项能力全部命中。", ver, rep.OK)
	}
}

// firstIDsWithState 取前几个处于某状态的能力 id，供一句话结论点名。
func firstIDsWithState(rep CapabilityReport, state string) string {
	var ids []string
	for _, c := range rep.Capabilities {
		if c.State == state {
			ids = append(ids, c.ID)
		}
	}
	if len(ids) > 3 {
		return strings.Join(ids[:3], "、") + " 等"
	}
	return strings.Join(ids, "、")
}

// runnerVersionLabel 描述「这次探的是哪个宿主」。
func (f HostFingerprint) runnerVersionLabel() string {
	if f.RunningExe != "" {
		base := filepath.Base(f.RunningExe)
		// 用 RunningVersion（实读自那个进程的 exe），而不是拿 FrozenVersion / OfficialVersion
		// 去顶替 —— 前者是本机实测出来的差异（沙箱路径经 junction 解析后字面不等于 FrozenExe），
		// 顶替会在「用户拿别的副本起宿主」时给出一句自信的错话。
		if f.RunningVersion != "" {
			return base + " " + f.RunningVersion
		}
		return base
	}
	if f.FrozenVersion != "" {
		return "冻结宿主 " + f.FrozenVersion
	}
	return "宿主"
}

func isUnderDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}

// officialInstallDir 返回官方安装目录（找不到则空串）。
func officialInstallDir() string {
	if exe := FindOfficialAntigravity(); exe != "" {
		return filepath.Dir(exe)
	}
	return ""
}

// assessSyncReadiness 回答「现在能不能据此手工同步冻结宿主」。
//
// 判据只有一条，且必须是**证据**而不是**推断**：
//
//	我们手上得有「目标版本上这些能力仍然成立」的实测读数。
//
// 当前唯一能产出这种读数的路径是「用户已经手工把冻结宿主换成新版并跑起来」。
// 在那之前，无论官方版本号是多少、无论 2.17.0 上探得多绿，都不构成对新版的证据。
// 所以默认 NOT_READY，而且要把「缺的到底是什么证据」讲清楚。
func assessSyncReadiness(rep CapabilityReport) SyncReadiness {
	sync := SyncReadiness{State: "NOT_READY"}
	frozen := rep.Fingerprint.FrozenVersion
	official := rep.Fingerprint.OfficialVersion

	// 注入路径四项：它们在官方形态下**按契约**永远拿不到读数，
	// 因此只能靠「增强形态 + 目标版本」来验。这是本判断里最硬的那条约束。
	injectionCritical := []string{"RUNTIME_INJECTION", "SKIN_ROOT", "G_HUB", "SKIN_BACKGROUND"}

	covered, uncovered := splitByOfficialEvidence(rep)

	if frozen == "" || official == "" {
		sync.Reason = "无法比较版本（官方或冻结宿主版本读不出来），没有可依据的事实。"
		sync.Uncovered = uncovered
		return sync
	}
	if frozen == official {
		sync.State = "READY_FOR_MANUAL_SYNC"
		sync.Reason = fmt.Sprintf("两侧同为 %s —— 此时「同步」没有内容，冻结宿主已是最新。", frozen)
		return sync
	}

	// 路径 A：本次读数就在官方版本上（用户已经把冻结宿主换成新版并以增强形态跑起来了）。
	runningOfficial := rep.Fingerprint.RunningExe != "" &&
		isUnderDir(rep.Fingerprint.RunningExe, officialInstallDir())
	runningVersion := rep.Fingerprint.RunningVersion
	if runningOfficial && runningVersion == official {
		var bad []string
		for _, id := range injectionCritical {
			for _, c := range rep.Capabilities {
				if c.ID == id && (c.State == CapFailed || c.State == CapUnknown) {
					bad = append(bad, id+"="+c.State)
				}
			}
		}
		if len(bad) > 0 {
			sync.Reason = fmt.Sprintf("已在官方 %s 上以增强形态取得读数，但关键能力未通过：%s。"+
				"这不是「可以同步」，而是「同步后会坏这些」。", official, strings.Join(bad, "、"))
			sync.Uncovered = uncovered
			return sync
		}
		sync.State = "READY_FOR_MANUAL_SYNC"
		sync.CoveredOnOfficial = rep.Capabilities
		sync.Reason = fmt.Sprintf("已在官方 %s 上以增强形态取得完整读数：注入 / 皮肤根 / 中枢 / 背景层全部 OK。"+
			"这构成「可以手工同步」的证据 —— 但本轮仍不自动同步，动作必须由你显式发起。", official)
		return sync
	}

	// 路径 B：本次读数取自旧版冻结宿主，但官方只读探测可能已经确认了 DOM 层。
	if len(covered) > 0 {
		sync.State = "PARTIAL_EVIDENCE"
		sync.CoveredOnOfficial = covered
		sync.Uncovered = uncovered
		sync.Reason = fmt.Sprintf("已在官方 %s 上通过**只读探测**确认 %d 项 DOM 层锚点仍然成立（见下表，均标注来源）；"+
			"但注入路径的 %d 项（%s）在官方形态下按契约无法验证 —— 补丁能不能挂上去只有真升级一次才知道。"+
			"因此这是「半份证据」，不构成可以同步的结论。",
			official, len(covered), len(injectionCritical), strings.Join(injectionCritical, "、"))
		return sync
	}

	sync.Reason = fmt.Sprintf("本次能力读数取自 %s，而目标是升级到官方 %s —— 在旧版上探得再绿也不构成对新版的证据。"+
		"官方宿主若正在运行（它会自己开一个调试端口，不需要 2Ag 加参数），这里会自动补上只读锚点探测，"+
		"从而至少确认 DOM 层那一半。", orUnknown(frozen), official)
	sync.Uncovered = uncovered
	return sync
}

// splitByOfficialEvidence 按「官方只读探测覆盖了没有」把能力分成两栏。
//
// 判据是**实测的锚点结论**，不是推断：covered 里的每一项都来自 officialAnchorEvidence
// 对官方只读探测结果的映射，而官方只读探测在拿不到读数时会返回 Available=false
// （此时 covered 为空，报告退回 NOT_READY）。
//
// uncovered 保留的是 rep.Capabilities 里没有被覆盖的项 —— 它的用途不是「报错」，
// 而是「升级之后第一件事该看哪几行」。因此它包含的是增强形态下的实测状态，
// 让用户对比「升级前是 OK，升级后变了吗」。
func splitByOfficialEvidence(rep CapabilityReport) (covered, uncovered []CapabilityItem) {
	official := ProbeOfficialHostReadOnly()
	covered = officialAnchorEvidence(official)
	if len(covered) == 0 {
		// 没拿到官方读数时，uncovered 就是全部能力（一份完整的待验清单）。
		return nil, append([]CapabilityItem{}, rep.Capabilities...)
	}
	byID := map[string]bool{}
	for _, c := range covered {
		byID[c.ID] = true
	}
	for _, c := range rep.Capabilities {
		if !byID[c.ID] {
			uncovered = append(uncovered, c)
		}
	}
	return covered, uncovered
}

// buildHostFingerprint 采集宿主身份。只读。
func buildHostFingerprint() HostFingerprint {
	fp := HostFingerprint{ProbedAt: time.Now().Format(time.RFC3339)}
	if IsOfficialRuntime() {
		fp.Mode = "official"
	} else {
		fp.Mode = "enhanced"
	}

	fp.OfficialExe = FindOfficialAntigravity()
	if fp.OfficialExe != "" {
		fp.OfficialVersion = fileProductVersion(fp.OfficialExe)
	}
	fp.FrozenExe = frozenHostExePath()
	if fp.FrozenExe != "" {
		fp.FrozenVersion = fileProductVersion(fp.FrozenExe)
	}

	// 运行中的宿主：探测返回的是「2Ag 托管的那个」，它才是能力读数的来源。
	status := ProbeRealHost()
	fp.RunningPID = status.PID
	fp.RunningExe = getProcessExePath(status.PID)
	if fp.RunningExe != "" {
		fp.RunningVersion = fileProductVersion(fp.RunningExe)
	}
	fp.CDPPort = status.CDPPort

	fp.InjectionSizeBytes = hubSourceSize()
	return fp
}

// hubSourceSize 取补丁源码的字节数（用于回答「是不是旧补丁在报这些状态」）。
// 读不到就返回 0 —— 那一列在界面上会显示为空，而不是显示一个看起来像真值的数。
func hubSourceSize() int {
	_, size := patcher.HubSourceInfo()
	return size
}

// capabilityPayload 是宿主补丁回执的形状。
type capabilityPayload struct {
	cap map[string]CapabilityItem
}

// parseCapabilityPayload 解析补丁的 JSON 回执或哨兵字符串。
func parseCapabilityPayload(raw string) (capabilityPayload, error) {
	out := capabilityPayload{cap: map[string]CapabilityItem{}}
	trimmed := strings.TrimSpace(raw)
	switch trimmed {
	case "", "no-consumer":
		return out, errors.New("宿主上没有 2Ag 补丁实例（window.__2ag.probeCapabilities 不存在）")
	case "no-report":
		return out, errors.New("补丁实例存在但没有回报能力读数（可能是旧版补丁）")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return out, fmt.Errorf("回执不是 JSON：%s", truncateForMessage(trimmed, 80))
	}
	var shape struct {
		Cap map[string]struct {
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"cap"`
	}
	if err := json.Unmarshal([]byte(trimmed), &shape); err != nil {
		return out, fmt.Errorf("回执解析失败：%v", err)
	}
	for id, v := range shape.Cap {
		out.cap[id] = CapabilityItem{ID: id, State: v.State, Detail: v.Detail}
	}
	if len(out.cap) == 0 {
		return out, errors.New("回执里没有任何能力项")
	}
	return out, nil
}

// truncateForMessage 截断可能很长的诊断串，避免一句话结论被撑爆。
func truncateForMessage(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// queryHostCapabilities 走一次极短的 CDP 往返把能力读数取回来。
//
// 复用常驻会话而不是新建连接：拖滑块那条通道已经证明「同一条会话里做一次
// Runtime.evaluate 是毫秒级」的，而新建连接要重走握手。会话不存在时如实失败，
// 由调用方报 FAILED，不去偷偷重注入一份补丁来「凑」出读数。
func queryHostCapabilities() (string, string, error) {
	targets, err := listCDPTargets()
	if err != nil {
		return "", "", err
	}
	var wsURL string
	best := -1
	for _, t := range targets {
		if !IsRealWorkbenchTarget(t) {
			continue
		}
		if s := targetScore(t); s > best {
			best = s
			wsURL = t.WebSocketDebuggerURL
		}
	}
	if wsURL == "" {
		return "", "", errors.New("CDP 上未找到工作台视窗")
	}
	page := ""
	for _, t := range targets {
		if t.WebSocketDebuggerURL == wsURL {
			page = t.URL
			break
		}
	}
	session := lookupLiveSession(wsURL)
	if session == nil {
		return "", page, errors.New("工作台视窗上没有补丁的常驻会话（补丁可能还没挂上）")
	}
	raw, err := session.evalString(cdpSessionProbeID, capabilityProbeExpr)
	if err != nil {
		return "", page, err
	}
	return raw, page, nil
}

// listCDPTargets 取一次 CDP 目标列表。
func listCDPTargets() ([]cdpTarget, error) {
	addr := ResolveCDPAddr()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/json")
	if err != nil {
		return nil, fmt.Errorf("无法连接 CDP 端口 %s: %w", addr, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取 CDP 目标列表失败: %w", err)
	}
	var targets []cdpTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return nil, fmt.Errorf("解析 CDP 目标列表失败: %w", err)
	}
	return targets, nil
}

// 探测通道用的 CDP 命令 id。选一个与既有几条通道（9002 推送 / 99 邮箱探针）不冲突的值。
const cdpSessionProbeID = 9003

// sharedDataSchemaFacts 是 .gemini\antigravity 的格式指纹。
//
// 这一节回答的是比 UI hook 更隐蔽的一类兼容风险：两个形态共读同一棵数据树，
// 官方新版一旦升级了 DB 结构，旧版冻结宿主再去读就可能踩到不认识的格式。
//
// 做法刻意保守：只读 SQLite 文件头里**规范固定**的那几个字段，不解析 schema 内容、
// 不引入 sqlite 驱动、不跑迁移。SQLite 的文件头布局是稳定的公开格式：
//
//	offset 16..17  page size (大端)
//	offset 18      file format write version
//	offset 19      file format read version
//	offset 60..63  user_version (大端)
//
// 如果这几个字段在两侧一样，说明两边的 SQLite 层面是同一代格式；
// 不一样则是一个**明确的**信号。读不出就报 UNKNOWN，绝不自己发明一个 schema 版本号。
type sharedDataSchemaFacts struct {
	Available        bool   `json:"available"`
	ConversationDBs  int    `json:"conversation_dbs"`
	UserVersions     []int  `json:"user_versions"`
	PageSizes        []int  `json:"page_sizes"`
	FileFormatWrite  []int  `json:"file_format_write"`
	SummariesVersion int    `json:"summaries_user_version"`
	SummariesPresent bool   `json:"summaries_present"`
	MigrationHighKey int    `json:"migration_high_key"`
	MigrationMarkers int    `json:"migration_markers"`
	Note             string `json:"note"`
}

// ProbeSharedDataSchema 只读地读出数据树的格式指纹。
func ProbeSharedDataSchema() sharedDataSchemaFacts {
	var facts sharedDataSchemaFacts
	dir := sharedDataTreeDir()
	if dir == "" {
		facts.Note = "无法定位用户主目录"
		return facts
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		facts.Note = "共享数据树不存在"
		return facts
	}
	facts.Available = true

	convDir := filepath.Join(dir, "conversations")
	if entries, err := os.ReadDir(convDir); err == nil {
		uv := map[int]bool{}
		ps := map[int]bool{}
		fw := map[int]bool{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".db") {
				continue
			}
			h, ok := readSQLiteHeader(filepath.Join(convDir, e.Name()))
			if !ok {
				continue
			}
			facts.ConversationDBs++
			uv[h.userVersion] = true
			ps[h.pageSize] = true
			fw[h.formatWrite] = true
		}
		facts.UserVersions = sortedIntKeys(uv)
		facts.PageSizes = sortedIntKeys(ps)
		facts.FileFormatWrite = sortedIntKeys(fw)
	}

	sumPath := filepath.Join(dir, "conversation_summaries.db")
	if h, ok := readSQLiteHeader(sumPath); ok {
		facts.SummariesPresent = true
		facts.SummariesVersion = h.userVersion
	}

	facts.MigrationHighKey, facts.MigrationMarkers = readMigrationMarkers(filepath.Join(dir, "antigravity_state.pbtxt"))

	facts.Note = fmt.Sprintf("conversations 下 %d 个 .db；user_version=%v；"+
		"conversation_summaries.db=%v；迁移标记最高 key=%d（共 %d 条）",
		facts.ConversationDBs, facts.UserVersions,
		facts.SummariesVersion, facts.MigrationHighKey, facts.MigrationMarkers)
	return facts
}

// sqliteHeaderFacts 是文件头里那四个规范字段。
type sqliteHeaderFacts struct {
	pageSize    int
	formatWrite int
	formatRead  int
	userVersion int
}

// readSQLiteHeader 读 100 字节文件头。任何失败都返回 false（不猜）。
func readSQLiteHeader(path string) (sqliteHeaderFacts, bool) {
	f, err := os.Open(path)
	if err != nil {
		return sqliteHeaderFacts{}, false
	}
	defer f.Close()
	h := make([]byte, 100)
	if _, err := f.Read(h); err != nil {
		return sqliteHeaderFacts{}, false
	}
	if string(h[0:15]) != "SQLite format 3" {
		return sqliteHeaderFacts{}, false
	}
	ps := int(h[16])<<8 | int(h[17])
	if ps == 1 {
		ps = 65536 // SQLite 规范：页大小 1 表示 65536
	}
	return sqliteHeaderFacts{
		pageSize:    ps,
		formatWrite: int(h[18]),
		formatRead:  int(h[19]),
		userVersion: int(h[60])<<24 | int(h[61])<<16 | int(h[62])<<8 | int(h[63]),
	}, true
}

// readMigrationMarkers 从 antigravity_state.pbtxt 里读迁移标记。
//
// 只认两样东西：`migrations: { key: N ... }` 里的 N，以及出现过的迁移相关行数。
// 这是个 pbtxt 文本文件，格式由官方自己决定 —— 所以解析刻意写得极松：
// 认不出就返回 0,0，而 0 与「真的没有迁移」在面板上会被区分（Note 里写明）。
func readMigrationMarkers(path string) (highKey int, markerCount int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	keyIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "migrations:") {
			markerCount++
			keyIdx = i
		}
		if strings.HasPrefix(trimmed, "key:") && keyIdx >= 0 && i-keyIdx <= 2 {
			v := strings.TrimSpace(strings.TrimPrefix(trimmed, "key:"))
			if n := atoiOrNeg(v); n > highKey {
				highKey = n
			}
			keyIdx = -1
		}
	}
	return highKey, markerCount
}

// atoiOrNeg 是极小的整数解析（避免为几行代码引入 strconv 的依赖面）。
func atoiOrNeg(s string) int {
	if s == "" {
		return -1
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
		if n > 1<<20 {
			return -1
		}
	}
	return n
}

func sortedIntKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
