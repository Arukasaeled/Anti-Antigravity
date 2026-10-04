package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/patcher"
)

// 本文件承载「主窗口 → 宿主补丁」的**增量状态推送**通道。
//
// 存在的理由（不是锦上添花，是消除一条用户感知到的延迟）：
// 图形模式此前只有一条生效路径 —— StateChangedEvent → 700ms 去抖 → HotReloadCDP
// → 重新读盘 → 重新组装整段补丁源码 → 建立新 CDP 连接 → Runtime.evaluate 重跑整个 IIFE。
// 每改一个滑块要走完整条链，用户看到的就是那「约一秒」。
//
// 而补丁从 2.1 起就自带 window.__2ag_onStateUpdate 这个消费者（收 config.Config 形态的 JSON），
// 只是一直只有 CLI 模式在调用它。这里把它接到图形模式上：
// 复用宿主那个**已经建立好的常驻 CDP 会话**（persistentCDPSession），
// 一次 Runtime.evaluate 就把新值落到正在运行的补丁实例上 —— 不重连、不重跑补丁。
//
// 但「不读盘」这个说法只对推送本身成立。推送路径还必须顺手刷新
// Page.addScriptToEvaluateOnNewDocument 注册的那份启动脚本（见 PushHubState 内注释），
// 而那件事必须从磁盘重建表达式 —— 否则会把注册倒退回旧补丁源码。
// 代价是每次推送读约 300KB（补丁源码 + 壁纸），且这条路只在状态真的变化时才会走到，
// 换来的是「跳转之后配置不回退」这个正确性，值得。
//
// 边界（必须诚实）：这条通道只能改「补丁已经认识的那些值」。补丁源码本身的更新
// 仍然只能靠 HotReloadCDP。因此推送失败/无处可落时，调用方必须回落到整段重注入。

const cdpSessionPushID = 9002

// errNoLiveSession 表示宿主视窗上还没有补丁的常驻会话（补丁尚未挂上，或会话已失效注销）。
var errNoLiveSession = errors.New("宿主视窗上还没有补丁的常驻会话")

// hubStatePush 是推送载荷。
//
// 为什么不是直接把 config.Config 推过去：
// 宿主工作台页面是 https://127.0.0.1:xxxxx，Chromium 的同源策略会把
// url("D:/Pictures/x.jpg") 这类本地路径直接拦掉（这正是 ResolveWallpaperDataURL
// 存在的全部理由）。只推 wallpaper_path 的话，宿主背景层不会有任何变化 ——
// 那就成了「推了但没变」的假功能。所以在 config.Config 的全部字段之外，
// 额外带一个已解析成 data:image/...;base64 的 wallpaper。
//
// 内嵌 Config 让外层 JSON 保持扁平（blur / opacity / modal_opacity / language /
// gravity_boost / wallpaper_path ...），与 cmd/2ag/main.go 里 CLI 模式的推送口径逐字兼容。
//
// Wallpaper 必须是 *string 而不是 string。这条指针是「清除壁纸」这条路的承重件：
//
//	nil   → 本次推送不带壁纸字段（滑块拖动等高频推送走这条，省掉上兆的 base64）
//	指向 "" → 本次推送明确要求**回到 Native**（拆除背景层）
//	指向 data:… → Custom，贴这张图
//
// 用 string + omitempty 表达不了第一种与第二种的区别 —— 空串会被 omitempty 吞掉，
// 于是「清除壁纸」这条指令根本到不了宿主，用户看到的就是「点了 Clear 没反应」。
type hubStatePush struct {
	config.Config
	Language           string  `json:"language"`
	LanguagePreference string  `json:"language_preference"`
	Wallpaper          *string `json:"wallpaper,omitempty"`
}

// 壁纸的 base64 载荷是整条推送里唯一可能上兆的部分，而滑块拖动会以每秒十来次的频率触发推送。
// 因此只在壁纸路径真的变了的那一次把图带上；其余推送留空，补丁侧对缺字段是安全的
// （__2ag_onStateUpdate 只采信真实存在的字段，不会把 undefined 写成空）。
var (
	pushStateMu              sync.Mutex
	lastPushedWallpaperPath  string
	lastPushedWallpaperReady bool
)

// BuildHubStatePush 组装一次推送的 JSON 载荷。
// includeWallpaper 为 false 时不带图片，只带配置字段。
//
// includeWallpaper 为 true 时**总是**带上 wallpaper 字段，哪怕它的值是空串 ——
// 空串是「回到 Native」这条指令本身，不是「没有内容」。见 hubStatePush 的注释。
func BuildHubStatePush(cfg config.Config, includeWallpaper bool) (string, error) {
	payload := hubStatePush{Config: cfg, Language: cfg.UILanguage(), LanguagePreference: cfg.Language}
	if includeWallpaper {
		resolved := ResolveWallpaperDataURL(cfg.WallpaperPath)
		payload.Wallpaper = &resolved
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("序列化宿主状态失败: %w", err)
	}
	return string(data), nil
}

// PushHubStateResult 是这次推送的真实回执。字段全部是实际发生的计数，不含推测。
type PushHubStateResult struct {
	Targets        int    // CDP 上识别出的真实工作台视窗数
	Pushed         int    // 成功落到补丁实例上的视窗数
	Skipped        int    // 没有常驻会话（补丁未挂上）而跳过的视窗数
	Registered     int    // 成功把「跳转自愈注册」同步到本次配置的视窗数
	WallpaperSet   bool   // 本次是否携带了壁纸图片
	Consumed       string // 补丁侧的原始回执：applied / unchanged / no-consumer / no-report
	NeedFullReload bool   // 本次推送带壁纸但宿主没能落实 ⇒ 调用方应回落到整段重注入
	Message        string // 人话回执
}

// PushHubState 把配置增量推给宿主里正在运行的补丁实例。
//
// 与 HotReloadCDP 的分工：
//   - 本函数：复用已建立的常驻会话，一次 evaluate，毫秒级，用于「值变了」；
//   - HotReloadCDP：重建会话并重跑整段补丁，秒级，用于「补丁源码变了」或本函数无法落地时兜底。
func PushHubState(cfg config.Config) (PushHubStateResult, error) {
	result := PushHubStateResult{}

	// 官方形态闸。注意这里返回的是 NeedFullReload=false 的「无消费者」结果而非错误：
	// 调用方 manager.go 的 applyHostVisibleChange 在快路径失败时会回落到整段重注入，
	// 若在这里报错，官方形态下拖一次滑块就会触发一次「失败 → 重注入」的连锁，
	// 而重注入同样被拒 ⇒ 用户看到一条红色的假故障。如实回执「没有消费者」才是对的。
	if IsOfficialRuntime() || isOfficialCDPTarget(ResolveCDPAddr()) {
		result.Consumed = "no-consumer"
		result.Message = "当前为官方形态（OFFICIAL CLEAN）：配置已保存，但不会推送到任何宿主"
		return result, nil
	}

	pushStateMu.Lock()
	needWallpaper := !lastPushedWallpaperReady || cfg.WallpaperPath != lastPushedWallpaperPath
	pushStateMu.Unlock()

	payload, err := BuildHubStatePush(cfg, needWallpaper)
	if err != nil {
		return result, err
	}
	result.WallpaperSet = needWallpaper

	targetAddr := ResolveCDPAddr()

	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://" + targetAddr + "/json")
	if err != nil {
		return result, fmt.Errorf("无法连接 CDP 端口 %s: %w", targetAddr, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return result, fmt.Errorf("读取 CDP 目标列表失败: %w", err)
	}
	var targets []cdpTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return result, fmt.Errorf("解析 CDP 目标列表失败: %w", err)
	}

	// 表达式是自解释的：它返回一个字符串，说明这次推送到底落成了什么。
	//
	// 为什么不让补丁静默吞掉：宿主上正在跑的那个实例可能是更旧的补丁版本
	// （比如用户刚更新了源码但还没热重载）。旧实例的 __2ag_onStateUpdate 不认识
	// wallpaper 字段 —— 它会「成功返回」却什么都不做。若不区分这两种情况，
	// 换壁纸就会表现为「点了没反应且毫无提示」，也就是我们反复要根除的假功能。
	// 故：补丁如实回报栽了没栽，Go 侧据此决定是否回落到整段重注入。
	expression := fmt.Sprintf(`(function () {
		if (typeof window.__2ag_onStateUpdate !== 'function') return 'no-consumer';
		var r = window.__2ag_onStateUpdate(%s);
		if (!r || typeof r !== 'object' || typeof r.wallpaper !== 'string') return 'no-report';
		return r.wallpaper;
	})()`, payload)

	// 「跳转自愈注册」也要跟上本次配置。
	//
	// 这是本通道引入的一个真实缺陷，不是防御性代码：Page.addScriptToEvaluateOnNewDocument
	// 注册的启动脚本，是宿主每次 SPA 跳转 / 刷新时新文档唯一会执行的那份补丁。此前只有
	// HotReloadCDP 会更新它（performCDPInject → acquirePersistentCDPSession → ensureScript）。
	// 而本通道把 HotReloadCDP 降级为「仅在推不动时才走」之后，改任何宿主可见配置都不再
	// 触碰那份注册 —— 于是：改完配置立刻看一切正常，可用户一切换会话 / 刷新页面，
	// 新文档执行的还是**上一次注册时的旧快照**，设置静默回退。
	//
	// 这里用**内存中的 cfg** 重建表达式，而不是调 BuildHubConfig() 重新读盘：
	// 状态机落盘有 schedulePersistLocked 的 300ms 定时器，而本通道的去抖窗口只有 120ms，
	// 重新读盘拿到的会是上一个值。
	//
	// 重建失败（读不到补丁源 / 壁纸读不到等）不算致命：增量推送本身仍然有效，
	// 只是这份注册会停在旧配置上 —— 如实计入 Diagnostic，不假装成功。
	scriptExpression, scriptExprErr := patcher.BuildHubExpression(hubConfigFromConfig(cfg))
	if scriptExprErr != nil {
		log.Printf("[2ag] 无法为跳转自愈注册重建补丁表达式（本次注册不更新）: %v", scriptExprErr)
	}

	var firstErr error
	needFullReload := false
	reloadReason := ""
	// wallpaperAccepted 与「evaluate 成功」是两件事，必须分开记。
	//
	// evaluate 成功只证明调用抵达了补丁函数；而旧版实例会**成功地什么都不做**。
	// 若把「抵达」当成「落实」，记账就会写下一个宿主根本没接收的壁纸路径，
	// 此后所有推送都按「路径没变」跳过壁纸 —— 表现为「换壁纸必须重启才生效」。
	wallpaperAccepted := false
	for _, t := range targets {
		if !IsRealWorkbenchTarget(t) {
			continue
		}
		result.Targets++
		session := lookupLiveSession(t.WebSocketDebuggerURL)
		if session == nil {
			result.Skipped++
			continue
		}
		raw, err := session.evalRaw(cdpSessionPushID, expression)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result.Pushed++
		status, _ := raw.(string)
		result.Consumed = status

		// 把「跳转自愈注册」更新成本次的配置。
		//
		// 必须放在推送成功之后：推送失败说明这个视窗的会话有问题，此时去动注册
		// 只会把事情弄得更糟（注册成功但补丁根本没跑起来 = 下一跳必现残缺态）。
		// ensureScript 内部按 expression 比较，配置没变时是一次纯内存判断，不发命令。
		if scriptExprErr == nil {
			if err := session.ensureScript(scriptExpression); err != nil {
				log.Printf("[2ag] 跳转自愈注册更新失败（该视窗下次跳转可能回退到旧配置）: %v", err)
			} else {
				result.Registered++
			}
		}

		// 只有当这次推送确实带了壁纸指令时，才关心它落没落下去。
		if !result.WallpaperSet {
			continue
		}
		// 'cleared' 与 'applied' 一样是「落实」：它表示宿主已按指令拆除了背景层。
		// 漏掉它会让「清除壁纸」被判为未落实 ⇒ 调用方回落到整段重注入，
		// 而重注入读的是同一份空配置，结果相同 —— 用户白等一次秒级重载。
		if status == "applied" || status == "unchanged" || status == "cleared" {
			wallpaperAccepted = true
			continue
		}
		needFullReload = true
		if reloadReason == "" {
			reloadReason = status
		}
	}
	result.NeedFullReload = needFullReload

	if result.Pushed == 0 {
		if result.Targets == 0 {
			return result, fmt.Errorf("CDP 端口 %s 可达，但没有找到 Antigravity 工作台视窗", targetAddr)
		}
		if result.Skipped == result.Targets {
			return result, errNoLiveSession
		}
		if firstErr != nil {
			return result, fmt.Errorf("状态推送全部失败，最后一个错误: %w", firstErr)
		}
		return result, fmt.Errorf("状态推送未落到任何视窗（目标数 %d）", result.Targets)
	}

	// 只有宿主真的收下这张图才记账。
	//
	// 两个反例都必须挡住：
	//   - 补丁实例是旧版（回执 no-consumer / no-report）⇒ 图根本没送达；
	//   - 全部视窗都没有常驻会话（Pushed == 0）⇒ 上一条 return 已经拦住了。
	// 记错账的后果不是丢一次更新，而是**此后每一次**壁纸变更都被误判为「路径没变」
	// 而跳过壁纸，用户必须重启宿主才能看到新图。
	if needWallpaper && wallpaperAccepted {
		pushStateMu.Lock()
		lastPushedWallpaperPath = cfg.WallpaperPath
		lastPushedWallpaperReady = true
		pushStateMu.Unlock()
	}

	result.Message = fmt.Sprintf("已向 %d/%d 个工作台视窗推送增量状态", result.Pushed, result.Targets)
	if result.Registered > 0 {
		result.Message += fmt.Sprintf("（%d 个视窗的跳转自愈注册已同步）", result.Registered)
	} else if scriptExprErr == nil && result.Pushed > 0 {
		result.Message += "（跳转自愈注册同步失败，下次跳转可能回退，见日志）"
	}
	if result.WallpaperSet {
		result.Message += "（含壁纸）"
	}
	if needFullReload {
		result.Message += fmt.Sprintf("；宿主上的补丁实例未落实本次壁纸（回执: %s），需要整段重注入", reloadReason)
	}
	return result, nil
}

// ResetPushedWallpaperHint 让下一次推送重新携带壁纸。
// 整段重注入之后调用：那次重注入已经用 INITIAL_CONFIG 把壁纸写进新实例，
// 但「上次推过什么」这个记账与实例无关，留着会让之后的壁纸变更被误判为「没变」。
func ResetPushedWallpaperHint() {
	pushStateMu.Lock()
	lastPushedWallpaperPath = ""
	lastPushedWallpaperReady = false
	pushStateMu.Unlock()
}
