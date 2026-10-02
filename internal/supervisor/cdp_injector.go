package supervisor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/patcher"
)

type cdpTarget struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// IsRealWorkbenchTarget 判断 Target 是否为真实工作台页面，严格只抓取真实的渲染视窗
func IsRealWorkbenchTarget(t cdpTarget) bool {
	// 1. 严格只抓取 page 类型的渲染目标
	if t.Type != "page" || t.WebSocketDebuggerURL == "" {
		return false
	}

	// 2. 严格拦截无效协议与开发工具
	if strings.HasPrefix(t.URL, "devtools://") ||
		strings.HasPrefix(t.URL, "chrome://") ||
		t.URL == "about:blank" {
		return false
	}

	// 3. 拦截 1 秒即销毁的临时启动遮罩
	if strings.HasPrefix(t.URL, "data:text/html") ||
		strings.Contains(t.URL, "Loading%20Antigravity") ||
		strings.Contains(strings.ToLower(t.Title), "loading antigravity") {
		return false
	}

	// 4. 黄金特征判定：
	// Antigravity 的真实主工作台 URL 必为本地高位端口 https://127.0.0.1:xxxxx/ 或 vscode-file://
	// 此时绝对不能检查 Title 是否为空！Title 为空也必须判定为真实工作台！
	if strings.HasPrefix(t.URL, "https://127.0.0.1:") ||
		strings.HasPrefix(t.URL, "http://127.0.0.1:") ||
		strings.Contains(t.URL, "workbench") ||
		strings.HasPrefix(t.URL, "vscode-file://") {
		return true
	}

	// 5. 兜底：如果 URL 或 Title 显式包含 antigravity 关键字
	lowerURL := strings.ToLower(t.URL)
	lowerTitle := strings.ToLower(t.Title)
	if strings.Contains(lowerURL, "antigravity") || strings.Contains(lowerTitle, "antigravity") {
		return true
	}

	return false
}

func targetScore(t cdpTarget) int {
	score := 50
	lowerTitle := strings.ToLower(t.Title)
	lowerURL := strings.ToLower(t.URL)
	if strings.Contains(lowerTitle, "anti-antigravity") {
		score = 120
	} else if strings.Contains(lowerTitle, "antigravity") {
		score = 110
	} else if strings.Contains(lowerURL, "workbench") || strings.Contains(lowerURL, "workbench.html") {
		score = 100
	} else if strings.Contains(lowerURL, "/c/") || strings.Contains(lowerURL, "section=") {
		score = 95
	} else if strings.HasPrefix(lowerURL, "https://127.0.0.1:") || strings.HasPrefix(lowerURL, "http://127.0.0.1:") {
		score = 90
	} else if strings.HasPrefix(lowerURL, "vscode-file://") {
		score = 80
	}
	return score
}

// tryEnableBrowserAutoAttach 尝试连接 Browser 目标并激活 Target.setAutoAttach
func tryEnableBrowserAutoAttach(ctx context.Context, targetAddr string) {
	client := &http.Client{Timeout: 1 * time.Second}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+targetAddr+"/json/version", nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				continue
			}

			var verInfo struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			if err := json.Unmarshal(data, &verInfo); err != nil || verInfo.WebSocketDebuggerURL == "" {
				continue
			}

			conn, reader, err := openCDPWebSocket(ctx, verInfo.WebSocketDebuggerURL)
			if err != nil {
				continue
			}
			defer conn.Close()

			autoAttachReq := map[string]any{
				"id":     1001,
				"method": "Target.setAutoAttach",
				"params": map[string]any{
					"autoAttach":             true,
					"waitForDebuggerOnStart": false,
					"flatten":                true,
				},
			}
			if err := sendCDPCommand(conn, reader, 1001, autoAttachReq); err == nil {
				log.Printf("[2ag] 已成功向 Browser 目标发送 Target.setAutoAttach")
			}
			return
		}
	}
}

// ResolveWallpaperDataURL 将本地图片文件解析并编码为 data:image/...;base64,... 格式
// 彻底解决 Chromium/Electron 对 https:// 页面加载 file:/// 协议的同源安全拦截
//
// 返回值语义（这是壁纸链路里最容易出错的一处，故写明）：
//
//	""           输入为空 ⇒ Native 形态。调用方据此**不得**画背景层。
//	data:image/… 成功读到的图片
//	原始路径串    读不到文件时的最后手段（宿主会拒载，但不至于让整条链路崩掉）
//
// 历史实现把空输入改写成一张写死的图片路径，于是「清除壁纸」永远回不到原生背景。
// 这里不再有任何兜底图片：空就是空，读到空串的人必须自己决定怎么表达「没有壁纸」。
func ResolveWallpaperDataURL(wallpaperPath string) string {
	if strings.TrimSpace(wallpaperPath) == "" {
		return ""
	}
	cleanPath := strings.TrimPrefix(wallpaperPath, "file:///")
	cleanPath = filepath.Clean(cleanPath)

	data, err := os.ReadFile(cleanPath)
	if err == nil && len(data) > 0 {
		ext := strings.ToLower(filepath.Ext(cleanPath))
		mimeType := "image/jpeg"
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		case ".gif":
			mimeType = "image/gif"
		}
		return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	}
	// 读不到就如实返回原路径，让调用方/界面看到真实原因（文件被删、路径写错）。
	// 绝不悄悄换成另一张图 —— 那正是「点了清除却回到某张旧图」的成因。
	return wallpaperPath
}

// WatchAndInjectCDP 监听 CDP 端口并在目标页面就绪时物理注入 anti-Antigravity 补丁
// BuildHubConfig 依据磁盘上的真实配置组装宿主补丁载荷。
//
// 抽成独立函数的原因：初次注入与「热重载补丁」必须走同一条组装路径，
// 否则两项功能会各自演化出不同的默认值（历史上前者写死三个 GravityBoost 键、
// 后者完全不透传），用户在界面上看到的永远不是配置文件的真实状态。
func BuildHubConfig() patcher.HubConfig {
	cfgPath, err := config.DefaultPath()
	var cfg config.Config
	if err == nil {
		cfg, _ = config.Load(cfgPath)
	}
	return hubConfigFromConfig(cfg)
}

// hubConfigFromConfig 是把落盘配置翻译成补丁配置的纯映射（除壁纸读取外无 I/O）。
//
// 抽出来是为了让「不许夹带默认值」这条不变量可以被测试钉住：BuildHubConfig 自己要读
// 磁盘配置，无法在测试里喂入 blur = 0 这种边界值，而正是那个边界值被历史实现的
// `if blur <= 0 { blur = 28 }` 悄悄改掉，导致用户选 Default 后界面仍显示中等模糊。
func hubConfigFromConfig(cfg config.Config) patcher.HubConfig {
	// wpPath 直接透传，**不兜底**。
	//
	// 空串是合法且有意义的：它表示 Native 形态（使用宿主原生背景）。
	// 历史实现写 `if wpPath == "" { wpPath = config.DefaultWallpaperPath }`，
	// 于是在这条链路上「清除壁纸」又一次被改写成了某张具体图片。
	wpPath := cfg.WallpaperPath
	wpDataURL := ResolveWallpaperDataURL(wpPath)

	// blur / opacity 直接透传，不做「<= 0 就回填默认」的兜底。
	//
	// 为什么必须去掉这个兜底：blur = 0 与 opacity = 0.1 是「恢复默认外观」（native 预设）
	// 的合法取值，config.Validate() 的区间也是 [0,40] / [0.1,0.9]。历史实现写成
	// `if blur <= 0 { blur = 28 }`、`if opacity <= 0 { opacity = 0.6 }`，于是用户选了
	// Default 之后：磁盘上 blur=0 → 注入时被改成 28 → 宿主实际渲染 blur(28px)，
	// 而 28/0.1 这个组合不匹配任何预设 ⇒ 舱内主题卡反推选中项时全灭（(none)），
	// 面板显示的外观与用户选择永久不一致。
	// injected_hub.js:1122 早已用同一句话禁止过前端版本的这个写法
	// （「严禁用 state.blur || 28 这种写法」），Go 侧漏改，这里补齐。
	// cfg 缺失时 config.Load 返回的 Default() 已经给了 20 / 0.55，无需在此兜底。
	blur := cfg.Blur
	opacity := cfg.Opacity

	return patcher.HubConfig{
		Language:      "zh-CN",
		Preset:        "Dark Dream",
		Blur:          blur,
		Opacity:       opacity,
		Wallpaper:     wpDataURL,
		WallpaperPath: wpPath,
		// ModalOpacity 必须一起透传。历史实现漏了这一项，而 Normalize() 也不补它
		// （Normalize 只拦越界值，从不会把 0 改成别的），于是整段注入与热重载送出的
		// INITIAL_CONFIG 里 modal_opacity 恒为 0 —— 用户在界面拖动「模态弹窗遮蔽度」
		// 后宿主纹丝不动。
		// 真机上「看起来是对的」纯属巧合：用户自己的配置值恰好等于补丁内置默认值 0.9；
		// 一旦改成 0.8 / 0.95 就会立刻暴露（宿主遮罩毫无变化）。
		// 注意这一项要与 injected_hub.js 的 snake_case 读取配套：HubConfig 的 json tag
		// 是 modal_opacity，而补丁内部一律用 modalOpacity，只补一边仍然断链。
		ModalOpacity: cfg.ModalOpacity,

		// GravityBoost 真实透传：此前这里写死三个键且恒为 true，导致前端六个开关
		// 与 2ag.json 的真实配置完全无法到达宿主。现在直接透传落盘配置，
		// 由 injected_hub.js 的 boost 对象逐项消费。
		GravityBoost: cfg.GravityBoost,
		// 语言锁定：把配置里的语言一并带进宿主，避免注入脚本用硬编码默认值。
		Network: cfg.Network,
	}
}

// HotReloadReport 是热重载的真实执行回执。
type HotReloadReport struct {
	Injected   int
	Targets    int
	SourceKind string
	Source     string
	Size       int
	Message    string
}

// HotReloadCDP 用磁盘上最新的补丁源，通过当前活跃的 CDP 连接重新注入宿主。
//
// 三个「不再」：
//   - 不再复用编译期 embed 快照（patcher.BuildHubExpression 已改为优先读磁盘）；
//   - 不再只发一个事件就宣称成功（这里真的建立 CDP 连接并 evaluate）；
//   - 不再把异常静默吞掉（失败原因原样返回给 API 层，由界面展示）。
func HotReloadCDP(targetAddr string) (HotReloadReport, error) {
	report := HotReloadReport{}
	if targetAddr == "" {
		targetAddr = ResolveCDPAddr()
	}
	if isOfficialCDPTarget(targetAddr) {
		return report, fmt.Errorf("当前目标是官方客户端；请启动增强宿主后再注入")
	}

	// 官方形态闸：注入链路共三个入口（HotReloadCDP / WatchAndInjectCDP / PushHubState），
	// 三处都要各自早退。只在 LaunchEnhancedHost 里拦是不够的 —— 用户完全可能先手动
	// 启动官方 Antigravity，再在 Manager 里点「热重载补丁」，那条路径根本不经过启动函数。
	// 这里返回错误而不是静默成功：界面必须能说出「官方形态下不会注入」，
	// 否则用户看到的是一次「成功」的空转。
	if IsOfficialRuntime() {
		return report, fmt.Errorf("当前为官方形态（OFFICIAL CLEAN）：不会向任何宿主注入补丁")
	}

	if targetAddr == "" {
		targetAddr = ResolveCDPAddr()
	}

	// 1. 先确认补丁源的真实来源，让用户在界面上看到到底注入的是哪个文件。
	origin, size := patcher.HubSourceInfo()
	report.Source = origin
	report.Size = size
	if strings.HasPrefix(origin, "embedded:") {
		report.SourceKind = "embedded"
	} else {
		report.SourceKind = "disk"
	}

	// 2. 用与初次注入完全相同的组装路径构建表达式（含真实 GravityBoost）。
	expression, origin2, err := patcher.BuildHubExpressionWithSource(BuildHubConfig())
	if err != nil {
		return report, fmt.Errorf("构建注入脚本失败: %w", err)
	}
	if origin2 != "" {
		report.Source = origin2
	}

	// 3. 拉取活跃目标。
	client := &http.Client{Timeout: 1 * time.Second}
	resp, err := client.Get("http://" + targetAddr + "/json")
	if err != nil {
		return report, fmt.Errorf("无法连接 CDP 端口 %s（宿主未启动或未开启调试端口）: %w", targetAddr, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return report, fmt.Errorf("读取 CDP 目标列表失败: %w", err)
	}
	var targets []cdpTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return report, fmt.Errorf("解析 CDP 目标列表失败: %w", err)
	}

	var valid []cdpTarget
	for _, t := range targets {
		if IsRealWorkbenchTarget(t) {
			valid = append(valid, t)
		}
	}
	report.Targets = len(valid)
	if len(valid) == 0 {
		return report, fmt.Errorf("CDP 端口 %s 可达，但没有找到 Antigravity 工作台视窗", targetAddr)
	}

	// 4. 逐个对视窗执行真实注入。只要有一个失败就如实上报，不粉饰。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var firstErr error
	for _, t := range valid {
		if err := performCDPInject(ctx, t.WebSocketDebuggerURL, expression); err != nil {
			log.Printf("[2ag] 热重载注入视窗 %q 失败: %v", t.Title, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		report.Injected++
	}

	if report.Injected == 0 {
		if firstErr != nil {
			return report, fmt.Errorf("全部 %d 个视窗注入失败，最后一个错误: %w", report.Targets, firstErr)
		}
		return report, fmt.Errorf("未注入任何视窗（目标数 %d）", report.Targets)
	}

	report.Message = fmt.Sprintf("已对 %d/%d 个工作台视窗重新注入补丁（来源: %s, %d 字节）",
		report.Injected, report.Targets, report.Source, report.Size)
	log.Printf("[2ag] 热重载完成: %s", report.Message)
	return report, nil
}

// buildHubConfig 保留内部调用点的小写别名，避免与既有调用风格冲突。
func buildHubConfig() patcher.HubConfig { return BuildHubConfig() }

func WatchAndInjectCDP(targetAddr string, timeout time.Duration) error {
	if targetAddr == "" {
		targetAddr = ResolveCDPAddr()
	}
	if isOfficialCDPTarget(targetAddr) {
		log.Println("[2ag] 跳过官方客户端 CDP 目标，不注入")
		return nil
	}
	// 官方形态闸（见 HotReloadCDP 的同名闸门注释）。这个入口由 Manager 启动后
	// 无条件调用（manager.go 启动 600ms 后就会跑到这里），不拦的话官方形态下
	// 仍会建立一条常驻 CDP 会话并持续巡检注入。
	if IsOfficialRuntime() {
		log.Printf("[2ag] 官方形态：跳过 WatchAndInjectCDP（不建立 CDP 会话、不注入）")
		return nil
	}

	if targetAddr == "" {
		targetAddr = ResolveCDPAddr()
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("[2ag] 正在监听 CDP 端口 %s 准备物理注入 anti-Antigravity (超时: %v)...", targetAddr, timeout)

	hubConfig := buildHubConfig()
	expression, err := patcher.BuildHubExpression(hubConfig)
	if err != nil {
		return fmt.Errorf("构建注入脚本失败: %w", err)
	}

	// 异步向 Browser 目标发送 Target.setAutoAttach，确保主窗口打开及后续窗口能被监听
	go tryEnableBrowserAutoAttach(ctx, targetAddr)

	client := &http.Client{Timeout: 1 * time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	injected := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[2ag] CDP 自动注入监听超时 (%s)", targetAddr)
			return ctx.Err()
		case <-ticker.C:
			if IsOfficialRuntime() || isOfficialCDPTarget(targetAddr) {
				return nil
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+targetAddr+"/json", nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				continue
			}

			var targets []cdpTarget
			if err := json.Unmarshal(data, &targets); err != nil {
				continue
			}

			var validTargets []cdpTarget
			for _, t := range targets {
				if IsRealWorkbenchTarget(t) {
					validTargets = append(validTargets, t)
				}
			}

			if len(validTargets) == 0 {
				continue
			}

			// 优先匹配 Title 包含 Anti-antigravity、Antigravity 的主渲染视窗
			sort.Slice(validTargets, func(i, j int) bool {
				return targetScore(validTargets[i]) > targetScore(validTargets[j])
			})

			injectedCount := 0
			for _, t := range validTargets {
				key := t.ID
				if key == "" {
					key = t.WebSocketDebuggerURL
				}
				if injected[key] {
					continue
				}

				log.Printf("[2ag] 捕获到 Antigravity 真实工作台页面 (Title: %q, URL: %s, Score: %d)，执行全量广播注入...", t.Title, t.URL, targetScore(t))

				if err := performCDPInject(ctx, t.WebSocketDebuggerURL, expression); err != nil {
					log.Printf("[2ag] 针对页面 %q 的 CDP 注入尝试暂时失败: %v; 持续重试...", t.Title, err)
					continue
				}

				injected[key] = true
				injectedCount++
			}

			if injectedCount > 0 {
				log.Printf("[2ag] 宿主物理变身 anti-Antigravity 成功！(已对全部 %d 个真实可视视窗完成全量广播注入，880px 黄金居中与 Dream-Skin 壁纸引擎已生效)", injectedCount)
				go maintainCDPInjection(targetAddr, expression)
				return nil
			}
		}
	}
}

func watchAndInjectCDP(targetAddr string, timeout time.Duration) {
	_ = WatchAndInjectCDP(targetAddr, timeout)
}

// maintainCDPInjection 启动常驻保活巡检协程：当宿主发生 SPA 路由跳转或页面重绘导致补丁丢失时，自动重新激活
//
// 时序要点：本协程的生命周期锚定在 cdpDaemonContext 上，而不是调用方传入的 ctx。
// 历史实现由 WatchAndInjectCDP 传入一个 15s 超时的 ctx，保活巡检只跑 15 秒就整体退出，
// 之后页面再跳转就再也没有任何补救通道了。
func maintainCDPInjection(targetAddr string, expression string) {
	// 多个入口（初次注入 / 热重载 / 主机接管）都可能拉起巡检，必须去重，
	// 否则同一目标会并存多条巡检协程，重复注入。
	maintainMu.Lock()
	if _, running := maintainLoops[targetAddr]; running {
		maintainMu.Unlock()
		return
	}
	maintainLoops[targetAddr] = struct{}{}
	maintainMu.Unlock()

	go func() {
		defer func() {
			maintainMu.Lock()
			delete(maintainLoops, targetAddr)
			maintainMu.Unlock()
		}()
		runMaintainLoop(targetAddr, expression)
	}()
}

func runMaintainLoop(targetAddr string, expression string) {
	ctx := cdpDaemonContext()
	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// 存活探针。历史实现检测的是 document.getElementById('2ag-hud-root')，
	// 而补丁真实挂载的宿主 id 是 'twoag-injected-root' —— 该 id 永远不存在，
	// 于是 needsInject 恒为 true，每 3 秒无脑重注入一次（既浪费又掩盖真实状态）。
	// 这里改为检测真实宿主并且要求它仍在文档树内，才能真正判断补丁是否存活。
	probeExpr := `Boolean((function(){var h=document.getElementById('twoag-injected-root');return !!h && h.isConnected && !!h.shadowRoot;})() || document.getElementById('2ag-dream-skin-bg'))`

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if IsOfficialRuntime() || isOfficialCDPTarget(targetAddr) {
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+targetAddr+"/json", nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		var targets []cdpTarget
		if err := json.Unmarshal(data, &targets); err != nil {
			continue
		}

		for _, t := range targets {
			if !IsRealWorkbenchTarget(t) {
				continue
			}

			// 优先复用该目标的持久会话：它既是探针通道，也是补丁注册的载体。
			session := lookupLiveSession(t.WebSocketDebuggerURL)
			needsInject := true
			if session != nil {
				if value, err := session.evalRaw(9999, probeExpr); err == nil {
					if alive, ok := value.(bool); ok && alive {
						needsInject = false
					}
				}
			}

			if needsInject {
				log.Printf("[2ag] 检测到工作台视窗 (Title: %q, URL: %s) 需重新激活 2Ag 补丁，执行热注入...", t.Title, t.URL)
				// 重注入前重新取一次当前配置，而不是用启动时捕获的那份快照。
				//
				// 两个理由，第二个是硬伤：①配置与补丁源码都可能已经变了；
				// ②performCDPInject 会把这个 expression 喂给 ensureScript，也就是
				// **覆盖注册** —— 拿旧 expression 去重激活，等于把「跳转自愈契约」
				// 倒退回启动那一刻的值，用户此后每次页面跳转都会看到设置悄悄回退。
				// 巡检本就是罕见路径（只在补丁真的不见了时才走），这里读一次盘完全付得起，
				// 换来的是「重激活之后的补丁一定是当前配置」。
				expr := expression
				if fresh, ferr := patcher.BuildHubExpression(buildHubConfig()); ferr == nil {
					expr = fresh
				} else {
					log.Printf("[2ag] 重新激活前重建补丁表达式失败，沿用启动时快照: %v", ferr)
				}
				if err := performCDPInject(ctx, t.WebSocketDebuggerURL, expr); err != nil {
					log.Printf("[2ag] 重新激活失败 (Title: %q): %v", t.Title, err)
				}
			}
		}
	}
}

func performCDPInject(ctx context.Context, wsURL string, expression string) error {
	// 1. 取得（或建立）该目标的持久会话，并把补丁注册为「跳转自愈契约」。
	//
	// 这里是「补丁被冲刷消失」的修复点。历史实现在同一函数里先注册
	// Page.addScriptToEvaluateOnNewDocument，紧接着 defer conn.Close() —— 而该注册是
	// 会话绑定的（probe_persist.js 四组对照实测：同会话导航 mark=1，关掉连接再导航 mark=0）。
	// 于是每次宿主内部路由跳转/刷新都会回到无补丁状态，界面最终呈现的是原版。
	// 现在注册挂在一个被保活维护的常驻会话上，注册一旦成功就持续有效。
	session, err := acquirePersistentCDPSession(ctx, wsURL, expression)
	if err != nil {
		return fmt.Errorf("建立 CDP 持久会话失败: %w", err)
	}

	// 2. 区域语言锁定（Force Locale zh-CN）的网络层与运行时双落地。
	// 顺序要点：Network.setExtraHTTPHeaders 必须先经 Network.enable 打开域，
	// 否则 CDP 会直接回报 “Network.enable wasn't called” 而静默失效。
	// 语言在服务端协商阶段就会被参考，等页面加载完再改已来不及 —— 所以放在注入最早期。
	if err := session.exchange(7, map[string]any{"id": 7, "method": "Network.enable"}); err != nil {
		log.Printf("[2ag] Network.enable: %v", err)
	}
	langHeaderReq := map[string]any{
		"id":     8,
		"method": "Network.setExtraHTTPHeaders",
		"params": map[string]any{
			"headers": map[string]any{
				"Accept-Language": "zh-CN,zh;q=0.9",
			},
		},
	}
	if err := session.exchange(8, langHeaderReq); err != nil {
		// 语言头是体验增强项，失败不应中断整个注入流程
		log.Printf("[2ag] Network.setExtraHTTPHeaders (Accept-Language): %v", err)
	}

	// Emulation.setLocaleOverride 是锁定 navigator.language / navigator.languages /
	// Intl 默认区域的权威通道，且不受页面内部逻辑改写，作为 Accept-Language 之外的第二道锁。
	localeOverrideReq := map[string]any{
		"id":     9,
		"method": "Emulation.setLocaleOverride",
		"params": map[string]any{
			"locale": "zh-CN",
		},
	}
	if err := session.exchange(9, localeOverrideReq); err != nil {
		log.Printf("[2ag] Emulation.setLocaleOverride (zh-CN): %v", err)
	}

	// 3. 对当前已存在的文档即时生效，让 880px 居中排版与 HUD 立刻呈现。
	//
	// 历史实现在这里之后还起了一个协程，每 800ms 重连并重跑三遍 evaluate，
	// 理由是「抗击 SPA 挂载重刷与状态水合」。那是在没有持久会话时对上述注册失效的
	// 手工补偿：它每次都要新建一条 WebSocket，且注定只能覆盖到当时那一份文档。
	// 登记在持久会话上的注册已经能让每一个新文档在 document-start 拿到补丁，
	// 这条补偿路径连同它的三次额外连接一起移除。
	if err := session.evaluate(3, expression); err != nil {
		return fmt.Errorf("Runtime.evaluate 失败: %w", err)
	}

	// 4. 确认宿主真实登录身份。
	//
	// 权威来源是 Windows 凭据管理器里的 gemini:antigravity —— 宿主每次登录/刷新
	// 都会改写它，所以它永远比任何缓存或配置文件新。
	// 历史实现走 HostAccountProbeExpr（在宿主页面里扫 localStorage / sessionStorage / DOM
	// 找邮箱）。真机实测该探针长期返回空：宿主工作台的 localStorage 里只有
	// 2ag.subsystems.config.v1 一个键，DOM 里也没有任何邮箱痕迹 —— 登录身份根本不在
	// 渲染进程里。于是「自动认领主控」从未生效，界面显示的一直是 active_account.txt
	// 里的旧值，与宿主实际身份脱节。
	identifyHostOwner := func() {
		if realEmail, err := ReadHostLoginEmail(); err == nil && realEmail != "" {
			log.Printf("[2ag] 已确认宿主真实登录身份（Windows 凭据管理器 target=%s）: %s", antigravityCredTarget, realEmail)
			SetActiveAccount(realEmail)
			return
		}
		// 凭据不可用时才退回 CDP 探针 —— 它在某些宿主版本里仍可能读到东西，
		// 但绝不能反过来让探针结果覆盖凭据（凭据才是宿主登录的唯一物理依据）。
		if realEmail, err := session.evalString(10, HostAccountProbeExpr); err == nil && realEmail != "" && strings.Contains(realEmail, "@") {
			log.Printf("[2ag] 凭据管理器未给出身份，退回 CDP 探针识别到: %s", realEmail)
			SetActiveAccount(realEmail)
		}
	}
	identifyHostOwner()

	return nil
}

func openCDPWebSocket(ctx context.Context, rawURL string) (net.Conn, *bufio.Reader, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 CDP URL 失败: %w", err)
	}
	if u.Host == "" || u.Path == "" {
		return nil, nil, errors.New("CDP URL 不完整")
	}

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 CDP 端口失败: %w", err)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("生成 WebSocket Key 失败: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.RequestURI(), u.Host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("发送 WebSocket 握手请求失败: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("读取 WebSocket 握手响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("WebSocket 握手状态码异常: %s", resp.Status)
	}
	return conn, reader, nil
}

func sendCDPCommand(conn net.Conn, reader *bufio.Reader, id int, command map[string]any) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	if err := writeCDPFrame(conn, 1, payload); err != nil {
		return err
	}
	for {
		frame, opcode, err := readCDPFrame(reader)
		if err != nil {
			return err
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(conn, 10, frame) // Pong
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("CDP 错误: %s", response.Error.Message)
		}
		return nil
	}
}

func writeCDPFrame(conn net.Conn, opcode byte, payload []byte) error {
	var header bytes.Buffer
	header.WriteByte(0x80 | opcode)
	length := len(payload)
	if length < 126 {
		header.WriteByte(byte(length) | 0x80)
	} else if length <= 65535 {
		header.WriteByte(126 | 0x80)
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(length))
		header.Write(size[:])
	} else {
		header.WriteByte(127 | 0x80)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(length))
		header.Write(size[:])
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header.Write(mask)
	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := conn.Write(header.Bytes()); err != nil {
		return err
	}
	_, err := conn.Write(masked)
	return err
}

func readCDPFrame(reader *bufio.Reader) ([]byte, byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return nil, 0, err
	}
	if first&0x0f == 8 {
		return nil, 0, errors.New("WebSocket 连接已关闭")
	}
	length := uint64(second & 0x7f)
	if length == 126 {
		var size [2]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return nil, 0, err
		}
		length = uint64(binary.BigEndian.Uint16(size[:]))
	} else if length == 127 {
		var size [8]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return nil, 0, err
		}
		length = binary.BigEndian.Uint64(size[:])
	}
	if length > 16<<20 {
		return nil, 0, errors.New("CDP Frame 过大")
	}
	masked := second&0x80 != 0
	mask := make([]byte, 4)
	if masked {
		if _, err := io.ReadFull(reader, mask); err != nil {
			return nil, 0, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, 0, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return payload, first & 0x0f, nil
}

// HostAccountProbeExpr 探测宿主内存与 DOM 中的真实登录邮箱
const HostAccountProbeExpr = `(() => {
	try {
		for (let i = 0; i < localStorage.length; i++) {
			const k = localStorage.key(i);
			const v = localStorage.getItem(k);
			if (v && v.includes('@')) {
				const m = v.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
				if (m) return m[0];
			}
		}
	} catch(e) {}
	try {
		for (let i = 0; i < sessionStorage.length; i++) {
			const k = sessionStorage.key(i);
			const v = sessionStorage.getItem(k);
			if (v && v.includes('@')) {
				const m = v.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
				if (m) return m[0];
			}
		}
	} catch(e) {}
	try {
		const nodes = document.querySelectorAll('[aria-label*="@"], [title*="@"], [data-email], [class*="account"], [class*="profile"], [class*="user"]');
		for (const n of nodes) {
			const text = (n.getAttribute('aria-label') || '') + ' ' + (n.getAttribute('title') || '') + ' ' + (n.getAttribute('data-email') || '') + ' ' + (n.innerText || '');
			const m = text.match(/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/);
			if (m) return m[0];
		}
	} catch(e) {}
	return "";
})()`

// QueryHostEmailViaCDP 自动通过 CDP Runtime.evaluate 探针读取宿主内存中的真实登录邮箱
func QueryHostEmailViaCDP(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get("http://" + ResolveCDPAddr() + "/json")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var targets []cdpTarget
	if err := json.Unmarshal(data, &targets); err != nil {
		return "", err
	}
	var wsURL string
	bestScore := -1
	for _, t := range targets {
		if IsRealWorkbenchTarget(t) {
			score := targetScore(t)
			if score > bestScore {
				bestScore = score
				wsURL = t.WebSocketDebuggerURL
			}
		}
	}
	if wsURL == "" {
		for _, t := range targets {
			if t.Type == "page" && t.WebSocketDebuggerURL != "" && !strings.HasPrefix(strings.ToLower(t.URL), "devtools://") {
				wsURL = t.WebSocketDebuggerURL
				break
			}
		}
	}
	if wsURL == "" {
		return "", errors.New("未找到宿主页面调试端点")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	val, err := evaluateCDPString(conn, reader, 99, HostAccountProbeExpr)
	if err != nil {
		return "", err
	}
	val = strings.TrimSpace(val)
	if strings.Contains(val, "@") {
		return val, nil
	}
	return "", errors.New("宿主内存中未探测到有效登录邮箱")
}

func evaluateCDPString(conn net.Conn, reader *bufio.Reader, id int, expr string) (string, error) {
	command := map[string]any{
		"id":     id,
		"method": "Runtime.evaluate",
		"params": map[string]any{
			"expression":    expr,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	if err := writeCDPFrame(conn, 1, payload); err != nil {
		return "", err
	}
	for {
		frame, opcode, err := readCDPFrame(reader)
		if err != nil {
			return "", err
		}
		if opcode == 9 { // Ping
			_ = writeCDPFrame(conn, 10, frame) // Pong
			continue
		}
		if opcode != 1 {
			continue
		}
		var response struct {
			ID    int `json:"id"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
			Result struct {
				Result struct {
					Type  string `json:"type"`
					Value any    `json:"value"`
				} `json:"result"`
			} `json:"result"`
		}
		if err := json.Unmarshal(frame, &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return "", fmt.Errorf("CDP 错误: %s", response.Error.Message)
		}
		if str, ok := response.Result.Result.Value.(string); ok {
			return str, nil
		}
		return "", nil
	}
}
