//go:build windows

package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// Official Host Read-only Compatibility Probe
//
// 这一节回答 Compatibility Guardian 唯一无法自答的问题：
//
//	「官方 2.18.1 上，2Ag 依赖的那些语义锚点还在不在？」
//
// 在增强形态里我们探的是**冻结宿主**（2.17.0），所以那时候得出的任何绿色
// 都不构成对新版的证据 —— 这一点在 assessSyncReadiness 里已经写死了。
// 要拿到真正的证据，只有一个办法：去官方版本上实测一次。
//
// ★ 关键事实（本机实测，决定了本节可行）：
//
//	官方 Antigravity 自己就会开一个 CDP 端口，不需要任何 --remote-debugging-port。
//	证据：切到官方形态后（LaunchOfficialHost 传的是零参数），
//	%APPDATA%\Antigravity\DevToolsActivePort 立刻被官方自己写成 60550，
//	并且 60550 上确实有 /json 可读。这不是 2Ag 加的参数。
//
// 因此本节**不需要给官方加任何调试开关**也能取到读数 —— 而这正是任务书
// 要求守住的那条边界（「如果官方启动环境根本无法安全 attach，就如实报告」）。
//
// ★ 只读契约（本节的全部代码都受它约束）：
//
//	✓ 只发 Runtime.evaluate，且表达式里只有查询（querySelector / getComputedStyle）
//	✓ 不注册 Page.addScriptToEvaluateOnNewDocument
//	✓ 不写 DOM、不写 localStorage、不碰凭据、不改配置
//	✗ 绝不评估会改状态的表达式
//
// 表达式本身写成**一次性的纯查询**，没有任何副作用 —— 这一点在代码里靠
// officialAnchorProbeExpr 的构成来保证（它只读，不写）。
// ============================================================================

// OfficialAnchorReport 是「在官方版本上，这些锚点还在不在」的实测结论。
type OfficialAnchorReport struct {
	// Available 为 false 表示这次没探到（官方没在跑 / 端口没监听 / 连不上），
	// 此时下面的字段全部是零值，界面据此显示 UNKNOWN 而不是「全都好」。
	Available bool   `json:"available"`
	Reason    string `json:"reason"`

	Version   string `json:"version"`
	Exe       string `json:"exe"`
	CDPPort   int    `json:"cdp_port"`
	HostPage  string `json:"host_page"`
	ProbedAt  string `json:"probed_at"`
	ProbeExpr string `json:"-"`

	// Anchors 逐项列出 2Ag 依赖的语义锚点在官方版本上的存在性。
	// 与 capability 的四态同一套词汇：这里只可能是 OK / UNSUPPORTED / FAILED，
	// 因为官方上跑的是宿主自己，没有 2Ag 补丁可探（RUNTIME_INJECTION 之类不适用）。
	Anchors []CapabilityItem `json:"anchors"`

	OK          int `json:"ok"`
	Unsupported int `json:"unsupported"`
	Failed      int `json:"failed"`

	Headline string `json:"headline"`
}

// officialAnchorProbeExpr 是**纯只读**的锚点查询表达式。
//
// 它刻意不碰 __2ag / 不注册任何东西 / 不写任何属性，只回答「这些选择器命中吗」。
// 之所以把每一项都包在独立的 try 里：官方页面上任何一条查询抛错都不该让整份
// 报告变成「探测失败」—— 一条查询出错恰恰是我们要知道的信息。
const officialAnchorProbeExpr = `(function () {
  function q(sel) { try { return !!document.querySelector(sel); } catch (e) { return 'ERR:' + e.name; } }
  function qn(sel) { try { return document.querySelectorAll(sel).length; } catch (e) { return -1; } }
  var out = {
    href: String(location.href),
    lang: document.documentElement ? document.documentElement.getAttribute('lang') : null,
    conversationView: q('[data-testid="conversation-view"]'),
    sidebarToggle: q('[data-testid="sidebar-toggle"]'),
    sendByAriaEn: q('button[aria-label*="Send" i]'),
    sendByAriaZh: q('button[aria-label*="发送"]'),
    sendByTestid: q('button[data-testid*="send" i]'),
    sendBySubmit: q('button[type="submit"]'),
    modelSelector: q('[data-testid*="model" i]') || q('[aria-label*="model" i]') || q('[aria-label*="模型"]'),
    sidebarClassToken: qn('[class~="bg-sidebar"]'),
    cardBorderToken: qn('[class~="bg-card-border"]'),
    insetZeroCount: qn('[class~="inset-0"]'),
    bodyChildren: document.body ? document.body.children.length : -1,
    // 官方页面上不该存在的东西（如果存在，说明有别的注入在跑）
    hasShadowHost: !!document.getElementById('twoag-injected-root'),
    hasNativeStyle: !!document.getElementById('2ag-native-core-style'),
    hasBgLayer: !!document.getElementById('2ag-dream-skin-bg'),
    has2agGlobal: typeof window.__2ag !== 'undefined'
  };
  return JSON.stringify(out);
})()`

// officialDevToolsActivePort 读官方 profile 的 DevToolsActivePort。
//
// 只读该文件本身，不创建、不修改。官方 host 每次启动都会重写它，
// 所以它同时是「官方在跑」的一个信号（配合端口是否真的在监听）。
func officialDevToolsActivePort() (int, string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return 0, "", errors.New("APPDATA 未设置")
	}
	candidates := []string{
		filepath.Join(appData, "Antigravity", "DevToolsActivePort"),
		filepath.Join(appData, "antigravity", "DevToolsActivePort"),
	}
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) == 0 {
			continue
		}
		port := atoiOrNeg(strings.TrimSpace(lines[0]))
		if port <= 0 {
			continue
		}
		path := ""
		if len(lines) > 1 {
			path = strings.TrimSpace(lines[1])
		}
		return port, path, nil
	}
	return 0, "", errors.New("官方 profile 下没有 DevToolsActivePort（官方未以增强调试端口启动过）")
}

// portListening 判断回环端口是否真的在监听。
//
// 必须查这一项：DevToolsActivePort 是**上一次**官方启动写下的，官方退出后文件仍在，
// 端口却已经没了。拿一个陈旧文件去连，会得到「连接被拒」这种看起来像 2Ag 出错的
// 现象，而事实是「官方根本没在跑」。
func portListening(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 400*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// ProbeOfficialHostReadOnly 在官方宿主上做一次纯只读的锚点探测。
//
// 返回 Available=false 的三种情形（都如实说明，绝不猜）：
//   - 官方没在跑（端口没监听）
//   - 端口在监听但 /json 读不到工作台视窗
//   - 连上但表达式求值失败
//
// 任何情形下都不注入、不改状态。这是本函数存在的全部意义 ——
// 它让「升级后大概会坏什么」从猜测变成实测。
func ProbeOfficialHostReadOnly() OfficialAnchorReport {
	rep := OfficialAnchorReport{ProbedAt: time.Now().Format(time.RFC3339)}

	rep.Exe = FindOfficialAntigravity()
	if rep.Exe != "" {
		rep.Version = fileProductVersion(rep.Exe)
	}

	port, _, err := officialDevToolsActivePort()
	if err != nil {
		rep.Reason = err.Error()
		rep.Headline = "未取得官方宿主的只读读数：" + rep.Reason
		return rep
	}
	rep.CDPPort = port

	if !portListening(port) {
		rep.Reason = fmt.Sprintf("官方 profile 记着端口 %d，但该端口没有监听 —— 官方宿主当前没在运行。", port)
		rep.Headline = rep.Reason
		return rep
	}

	wsURL, page, err := officialWorkbenchTarget(port)
	if err != nil {
		rep.Reason = err.Error()
		rep.Headline = "官方宿主在运行（端口 " + itoa(uint32(port)) + "），但没能取到工作台视窗：" + rep.Reason
		return rep
	}
	rep.HostPage = page

	// 一次性 WebSocket 往返。刻意不复用常驻会话：那是增强形态的通道，
	// 官方形态下不该有任何「常驻」的东西。
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		rep.Reason = "连接官方工作台调试端点失败：" + err.Error()
		rep.Headline = rep.Reason
		return rep
	}
	defer conn.Close()

	raw, err := evaluateCDPString(conn, reader, cdpSessionProbeID, officialAnchorProbeExpr)
	if err != nil {
		rep.Reason = "在官方宿主上求值只读锚点查询失败：" + err.Error()
		rep.Headline = rep.Reason
		return rep
	}
	rep.Available = true
	rep.Anchors, rep.OK, rep.Unsupported, rep.Failed = classifyOfficialAnchors(raw)
	rep.Headline = officialAnchorHeadline(rep)
	return rep
}

// officialAnchorEvidence 把官方只读探测的锚点结论翻译成「哪些 capability 拿到了证据」。
//
// 这是本文件对 SyncReadiness 的实质贡献：升级判断需要的是「新版本上这些能力还成立吗」，
// 而锚点探测恰好能在**不注入**的前提下回答其中的 DOM 层那一半。
//
// 映射刻意保守 —— 只映射那些「锚点在场 ⟹ 能力大概率成立」的项：
//
//	SIDEBAR_HOOK / SKIN_BACKGROUND  依赖的是 Tailwind 令牌与全屏遮罩族，锚点在就成立
//	PROMPT_HOOK / MODEL_SELECTOR    依赖的是 aria-label 与 testid，锚点在就成立
//	LOCALIZATION_ROOT               依赖 lang 属性与可翻译文本节点
//	WORKSPACE_TARGET                依赖会话区与侧栏
//
// 不映射的（**必须**留白，否则就成了「没验证却给结论」）：
//
//	RUNTIME_INJECTION / SKIN_ROOT / G_HUB / CDP_AVAILABLE
//	  这四项要的是「补丁能挂上去并且活着」，而官方形态的契约就是不注入 ——
//	  在官方上永远拿不到它们的证据。它们只能在「增强形态 + 新版冻结宿主」下验证。
func officialAnchorEvidence(rep OfficialAnchorReport) []CapabilityItem {
	if !rep.Available {
		return nil
	}
	const prefix = "由官方 " + "只读探测" + "推断"
	out := make([]CapabilityItem, 0, len(rep.Anchors))
	for _, a := range rep.Anchors {
		switch a.ID {
		case "OFFICIAL_CLEANLINESS":
			continue // 它是反面检查，不是能力
		case "WORKSPACE_TARGET", "PROMPT_HOOK", "MODEL_SELECTOR",
			"SIDEBAR_HOOK", "SKIN_BACKGROUND", "LOCALIZATION_ROOT":
			out = append(out, CapabilityItem{
				ID:     a.ID,
				State:  a.State,
				Detail: "（" + prefix + "）" + a.Detail,
			})
		}
	}
	return out
}

// officialWorkbenchTarget 在官方 CDP 上找那个工作台页面。
//
// 复用 IsRealWorkbenchTarget / targetScore：官方版与冻结版是同一个产品，
// 「哪个 target 才是工作台」的判据不该分家 —— 否则会出现「增强形态认得出、
// 官方形态认不出」这种纯粹由实现分叉造成的假故障。
func officialWorkbenchTarget(port int) (string, string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json", port))
	if err != nil {
		return "", "", fmt.Errorf("读取官方 CDP 目标列表失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("读取官方 CDP 目标列表失败: %w", err)
	}
	var targets []cdpTarget
	if err := json.Unmarshal(body, &targets); err != nil {
		return "", "", fmt.Errorf("解析官方 CDP 目标列表失败: %w", err)
	}
	best := -1
	wsURL, page := "", ""
	for _, t := range targets {
		if !IsRealWorkbenchTarget(t) {
			continue
		}
		if s := targetScore(t); s > best {
			best = s
			wsURL = t.WebSocketDebuggerURL
			page = t.URL
		}
	}
	if wsURL == "" {
		return "", "", errors.New("官方 CDP 上没有可识别的 Antigravity 工作台视窗")
	}
	return wsURL, page, nil
}

// classifyOfficialAnchors 把只读查询的 JSON 变成逐项结论。
//
// 映射规则有意与补丁侧的 probeCapabilities 对齐：同一批锚点、同一套四态，
// 这样「2.17 的读数」与「2.18.1 的读数」才能逐行对比 —— 而逐行对比正是
// 「升级后大概会坏什么」这个问题的答案形态。
func classifyOfficialAnchors(raw string) ([]CapabilityItem, int, int, int) {
	var payload struct {
		Href              string      `json:"href"`
		Lang              string      `json:"lang"`
		ConversationView  interface{} `json:"conversationView"`
		SidebarToggle     interface{} `json:"sidebarToggle"`
		SendByAriaEn      interface{} `json:"sendByAriaEn"`
		SendByAriaZh      interface{} `json:"sendByAriaZh"`
		SendByTestid      interface{} `json:"sendByTestid"`
		SendBySubmit      interface{} `json:"sendBySubmit"`
		ModelSelector     interface{} `json:"modelSelector"`
		SidebarClassToken int         `json:"sidebarClassToken"`
		CardBorderToken   int         `json:"cardBorderToken"`
		InsetZeroCount    int         `json:"insetZeroCount"`
		HasShadowHost     bool        `json:"hasShadowHost"`
		HasNativeStyle    bool        `json:"hasNativeStyle"`
		HasBgLayer        bool        `json:"hasBgLayer"`
		Has2agGlobal      bool        `json:"has2agGlobal"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return []CapabilityItem{{
			ID: "OFFICIAL_ANCHOR_PAYLOAD", State: CapFailed,
			Detail: "官方宿主返回的锚点 JSON 无法解析：" + err.Error(),
		}}, 0, 0, 1
	}

	truthy := func(v interface{}) bool {
		b, ok := v.(bool)
		return ok && b
	}
	errored := func(v interface{}) (string, bool) {
		s, ok := v.(string)
		if ok && strings.HasPrefix(s, "ERR:") {
			return s, true
		}
		return "", false
	}

	var items []CapabilityItem
	ok, unsupported, failed := 0, 0, 0
	add := func(id string, present bool, detail string) {
		state := CapUnsupported
		if present {
			state = CapOK
			ok++
		} else {
			unsupported++
		}
		items = append(items, CapabilityItem{ID: id, State: state, Detail: detail})
	}
	addFail := func(id, detail string) {
		items = append(items, CapabilityItem{ID: id, State: CapFailed, Detail: detail})
		failed++
	}

	// WORKSPACE_TARGET：会话区 + 侧栏开关。
	if e, bad := errored(payload.ConversationView); bad {
		addFail("WORKSPACE_TARGET", "conversation-view 查询报错："+e)
	} else {
		add("WORKSPACE_TARGET", truthy(payload.ConversationView) || truthy(payload.SidebarToggle),
			fmt.Sprintf("conversation-view=%v sidebar-toggle=%v", payload.ConversationView, payload.SidebarToggle))
	}

	// PROMPT_HOOK：三层发送按钮选择器里有没有命中的。
	sendHit := truthy(payload.SendByAriaEn) || truthy(payload.SendByAriaZh) ||
		truthy(payload.SendByTestid) || truthy(payload.SendBySubmit)
	add("PROMPT_HOOK", sendHit,
		fmt.Sprintf("aria(En)=%v aria(Zh)=%v testid=%v submit=%v",
			payload.SendByAriaEn, payload.SendByAriaZh, payload.SendByTestid, payload.SendBySubmit))

	// MODEL_SELECTOR。
	add("MODEL_SELECTOR", truthy(payload.ModelSelector), fmt.Sprintf("命中=%v", payload.ModelSelector))

	// SIDEBAR_HOOK：透光级联依赖的 Tailwind 令牌。
	add("SIDEBAR_HOOK", payload.SidebarClassToken > 0,
		fmt.Sprintf("[class~=\"bg-sidebar\"] 命中 %d 个", payload.SidebarClassToken))

	// SKIN_BACKGROUND：输入框令牌 + 全屏遮罩族（Modal Glass 的落点）。
	add("SKIN_BACKGROUND", payload.CardBorderToken > 0,
		fmt.Sprintf("[class~=\"bg-card-border\"] 命中 %d 个；[class~=\"inset-0\"] 命中 %d 个",
			payload.CardBorderToken, payload.InsetZeroCount))

	// LOCALIZATION_ROOT：lang 属性（词典层的挂载前提）。
	add("LOCALIZATION_ROOT", payload.Lang != "",
		fmt.Sprintf("html[lang]=%v（空值表示官方没写 lang，词典层的属性侧前提不成立）", payload.Lang))

	// CLEANLINESS：官方页面上有没有别人的注入残留。
	// 这一项是**反向**的：命中才是坏消息，所以单独处理，不并入 ok/unsupported 计数。
	dirty := payload.HasShadowHost || payload.HasNativeStyle || payload.HasBgLayer || payload.Has2agGlobal
	if dirty {
		items = append(items, CapabilityItem{
			ID: "OFFICIAL_CLEANLINESS", State: CapFailed,
			Detail: fmt.Sprintf("官方页面上发现了 2Ag 的残留物（shadowHost=%v nativeStyle=%v bgLayer=%v __2ag=%v）——"+
				"官方形态本应零注入，这说明有别的注入在跑。",
				payload.HasShadowHost, payload.HasNativeStyle, payload.HasBgLayer, payload.Has2agGlobal),
		})
		failed++
	} else {
		items = append(items, CapabilityItem{
			ID: "OFFICIAL_CLEANLINESS", State: CapOK,
			Detail: "官方页面上没有 Shadow 宿主 / native style / 背景层 / window.__2ag —— 零注入符合预期",
		})
		ok++
	}

	return items, ok, unsupported, failed
}

// officialAnchorHeadline 一句话概括官方侧的锚点读数。
func officialAnchorHeadline(rep OfficialAnchorReport) string {
	ver := orUnknown(rep.Version)
	switch {
	case rep.Failed > 0:
		return fmt.Sprintf("官方 %s（只读探测）：%d 项未通过，其中包含异常项 —— 见下表。",
			ver, rep.Failed)
	case rep.Unsupported > 0:
		return fmt.Sprintf("官方 %s（只读探测）：%d 项锚点已不在，%d 项仍在。这些「不在」项对应的 2Ag 能力在升级后会失效。",
			ver, rep.Unsupported, rep.OK)
	default:
		return fmt.Sprintf("官方 %s（只读探测）：%d 项锚点全部命中。", ver, rep.OK)
	}
}
