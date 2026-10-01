//go:build windows

package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// Update Guardian（最小实现）
//
// 这个模块存在的唯一理由，是纠正一句此前被反复说错的话。
//
// 事实是：2Ag 至今没被上游更新破坏，靠的是「跑的是另一份冻结副本」——
// 2Ag 拉的是 D:\Anti-antigravity\app\Antigravity.exe（钉在 2.17.0），
// 而官方 updater 更新的是 %LOCALAPPDATA%\Programs\Antigravity（现在是 2.18.1）。
// 这是物理隔离，不是兼容层。把物理隔离说成「update compatible」是错的：
// 一旦用户决定去用官方新版，那份冻结副本不会自己跟上，增强层也就停留在旧版。
//
// 所以这里做五件事，一件都不多做：
//   ① 发现官方安装目录  ② 读官方版本  ③ 读 2Ag 冻结宿主版本
//   ④ 发现 pending updater  ⑤ 比较版本
//
// 明确不做（本轮范围）：updater MITM / 拦截官方 updater / DLL hook /
// 自动下载 / 自动替换宿主文件。任何「同步」动作都必须由用户显式发起，
// 而且不在这个模块里发生 —— 它只提供事实与成本提示。
// ============================================================================

// UpdateGuardianReport 是 Update Guardian 的完整读数。
//
// 每一个字符串字段在「读不到」时都是空串而不是占位符：
// 界面必须能区分「版本是 2.18.1」与「我们读不出来」，
// 否则一次 P/Invoke 失败会被渲染成「版本不明」这种软性假象。
type UpdateGuardianReport struct {
	OfficialExe     string `json:"official_exe"`
	OfficialVersion string `json:"official_version"`
	OfficialSizeMB  string `json:"official_size_mb"`

	FrozenHostExe     string `json:"frozen_host_exe"`
	FrozenHostVersion string `json:"frozen_host_version"`

	// Status 取值：UP_TO_DATE / HOST_BEHIND / HOST_AHEAD / UNKNOWN
	//   UP_TO_DATE  —— 两者版本字符串一致
	//   HOST_BEHIND —— 官方更新，2Ag 冻结宿主落后（需要用户决策的状态）
	//   HOST_AHEAD  —— 冻结副本比官方安装还新（少见；通常意味着官方被降级或卸载）
	//   UNKNOWN     —— 至少一侧版本读不出来。绝不猜成 UP_TO_DATE。
	Status  string `json:"status"`
	Summary string `json:"summary"`

	PendingPresent bool   `json:"pending_present"`
	PendingExe     string `json:"pending_exe"`
	PendingInfo    string `json:"pending_info"`
	PendingTarget  string `json:"pending_target"`

	UpdaterConfigURL string `json:"updater_config_url"`

	// CanSync 恒为 false，且不是「暂时不行」而是「本模块不提供」。
	// 界面上那句 [同步新版宿主] 是在陈述代价（约 557 MB），不是可点的承诺。
	CanSync      bool   `json:"can_sync"`
	SyncNote     string `json:"sync_note"`
	SyncEstimate string `json:"sync_estimate"`
}

// antigravityUpdaterDir 是官方 updater 的下载缓存目录。
// 依据：官方 resources\app-update.yml 里的 updaterCacheDirName: antigravity-updater。
func antigravityUpdaterDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return ""
	}
	return filepath.Join(local, "antigravity-updater")
}

// frozenHostExePath 给出 2Ag 自己的冻结宿主副本路径（副本不在位时为空串）。
//
// 这里**不能**再用 detectDefaultAntigravityPath()：那个函数在冻结副本不存在时会
// 回退到官方安装目录里的 exe，于是更新面板会把官方 exe 显示成「2Ag 冻结宿主」，
// 两边版本当然永远相同（自己跟自己比），「冻结宿主是否落后于官方」这件事就永远
// 查不出来。0.1.1 起安装包不再携带冻结副本 —— 它是用户第一次启动增强形态时
// 在**他本机**建立出来的（见 frozen_host.go），所以「还没有副本」是一个必须被
// 如实报出来的正常状态，而不是可以拿官方 exe 顶上的空白。
func frozenHostExePath() string {
	return FrozenHostExePath()
}

// officialUpdateConfigURL 从官方 app-update.yml 里读出更新源 URL。
//
// 只读不写。读它的理由是：用户看到「官方 2.18.1」时，下一个问题必然是
// 「它从哪儿更新」—— 把 URL 摆出来比解释「有个 updater」更能说明问题。
// yml 是简单的 key: value，不引入 YAML 依赖，手工取一行即可。
func officialUpdateConfigURL() string {
	officialExe := FindOfficialAntigravity()
	if officialExe == "" {
		return ""
	}
	yml := filepath.Join(filepath.Dir(officialExe), "resources", "app-update.yml")
	data, err := os.ReadFile(yml)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "url:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "url:"))
		}
	}
	return ""
}

// readPendingUpdateInfo 读 pending\update-info.json 里的文件名。
//
// 只取 fileName 一个字段：sha512 与 isAdminRightsRequired 对「用户是否需要在意」
// 没有增量信息，多读一个字段就多一处会随上游格式变化而失效的解析。
func readPendingUpdateInfo(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var info struct {
		FileName string `json:"fileName"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		return ""
	}
	return info.FileName
}

// ProbeUpdateGuardian 汇总一次 Update Guardian 读数。纯只读。
func ProbeUpdateGuardian() UpdateGuardianReport {
	var rep UpdateGuardianReport

	rep.OfficialExe = FindOfficialAntigravity()
	if rep.OfficialExe != "" {
		rep.OfficialVersion = fileProductVersion(rep.OfficialExe)
		if fi, err := os.Stat(rep.OfficialExe); err == nil {
			rep.OfficialSizeMB = mbString(float64(fi.Size()) / (1024 * 1024))
		}
	}

	rep.FrozenHostExe = frozenHostExePath()
	if rep.FrozenHostExe != "" {
		rep.FrozenHostVersion = fileProductVersion(rep.FrozenHostExe)
	}

	rep.UpdaterConfigURL = officialUpdateConfigURL()

	if dir := antigravityUpdaterDir(); dir != "" {
		pendingDir := filepath.Join(dir, "pending")
		infoPath := filepath.Join(pendingDir, "update-info.json")
		if fi, err := os.Stat(pendingDir); err == nil && fi.IsDir() {
			rep.PendingPresent = true
			rep.PendingInfo = infoPath
			rep.PendingTarget = readPendingUpdateInfo(infoPath)
			if entries, err := os.ReadDir(pendingDir); err == nil {
				for _, e := range entries {
					if strings.HasSuffix(strings.ToLower(e.Name()), ".exe") {
						rep.PendingExe = filepath.Join(pendingDir, e.Name())
						break
					}
				}
			}
		}
	}

	rep.Status, rep.Summary = classifyVersions(rep)
	rep.CanSync = false
	rep.SyncEstimate = "约 557 MB（官方 resources\\ 整体，含 unpacked 运行时）"
	rep.SyncNote = "同步需要把官方 resources\\ 拷成 2Ag 冻结副本的 resources\\app\\" +
		"（unpacked 优先级高于 asar，这正是冻结副本能钉在 2.17.0 的机制）。" +
		"本轮不提供自动同步：它会替换你正在使用的宿主，必须由你显式发起。"
	return rep
}

// classifyVersions 比较两个版本字符串。
//
// 刻意只做「相等 / 不等」的粗判，不做语义化版本比较：本机两个版本分别是
// "2.18.1" 与 "2.17.0"，字符串相等已经足以回答「2Ag 是否落后」这个唯一的问题。
// 真去做 semver 解析，就等于承诺能正确处理 "2.18.1-beta.3" 这类上游可能
// 给出的形式 —— 那是我们没有证据支持的承诺。
func classifyVersions(rep UpdateGuardianReport) (string, string) {
	switch {
	case rep.OfficialVersion == "" && rep.FrozenHostVersion == "":
		return "UNKNOWN", "两侧版本都读不出来：官方安装目录未定位到，2Ag 冻结宿主的版本资源也无法解析。"
	case rep.OfficialVersion == "":
		return "UNKNOWN", "未定位到官方 Antigravity 安装，无法比较版本。2Ag 冻结宿主为 " + rep.FrozenHostVersion + "。"
	case rep.FrozenHostVersion == "":
		return "UNKNOWN", "官方为 " + rep.OfficialVersion + "，但读不出 2Ag 冻结宿主的版本。"
	case rep.OfficialVersion == rep.FrozenHostVersion:
		return "UP_TO_DATE", "官方与 2Ag 冻结宿主同为 " + rep.OfficialVersion + "。"
	default:
		msg := "官方为 " + rep.OfficialVersion + "，2Ag 冻结宿主为 " + rep.FrozenHostVersion +
			"。2Ag 靠的是物理隔离（跑另一份冻结副本），不是兼容层 —— 增强层停留在旧版，直至你决定同步。"
		if rep.PendingPresent {
			msg += " 另外发现官方 updater 已下载待安装包。"
		}
		return "HOST_BEHIND", msg
	}
}

// mbString 把 MB 数值渲染成一位小数。
//
// 不用 fmt.Sprintf("%.1f") 是为了让这个文件不引入 fmt —— 它已经是
// 本包里最不需要格式化能力的一个模块。
func mbString(mb float64) string {
	whole := int64(mb)
	frac := int64((mb - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(uint32(whole)) + "." + string(rune('0'+frac))
}

// updateGuardianCheckedAt 便于日志/排查的时间戳（不是报告字段）。
func updateGuardianCheckedAt() string {
	return time.Now().UTC().Format(time.RFC3339)
}
