package api

import (
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
	"strings"
	"time"

	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/netproxy"
	"github.com/2ag/2ag/internal/supervisor"
)

type Server struct {
	stateMachine *core.StateMachine
	bus          *core.EventBus
	mux          *http.ServeMux
	// handler 是 mux 外面包了 CORS 中间件的那一层，实际交给 http.Server。
	// mux 保留裸的（内部路由与单元测试直接跑它），两者不可混用。
	handler http.Handler
	server  *http.Server
}

func NewServer(sm *core.StateMachine, bus *core.EventBus) *Server {
	s := &Server{
		stateMachine: sm,
		bus:          bus,
		mux:          http.NewServeMux(),
	}
	s.routes()
	// CORS 必须包在 mux 外层，而不是逐个 handler 手写响应头。
	// 注入补丁（internal/patcher/injected_hub.js）跑在宿主页面 https://127.0.0.1:<hostPort> 上，
	// 它要 fetch 的是本服务 http://127.0.0.1:<apiPort> —— 端口不同即跨源。
	// 历史实现全文件没有任何 Access-Control-* 响应头，浏览器在预检阶段就把
	// /api/v1/accounts 与舱内账号切换全部拒掉，于是面板只能常年显示「网关未连接」。
	s.handler = corsMiddleware(s.mux)
	return s
}

// corsMiddleware 给所有响应补上 CORS 头，并对 OPTIONS 预检直接短路 204。
//
// 为什么需要它：注入补丁运行在宿主 Webview 的页面源上（形如 https://127.0.0.1:<port>），
// 而 2Ag 的 API 服务监听另一个端口。浏览器按同源策略判定为跨源，凡「非简单请求」
// 或带自定义头的请求都要先发预检；服务端不回 Access-Control-Allow-* 时预检失败，
// fetch 直接抛 TypeError，请求根本到不了业务 handler。
//
// 为什么不是 "*" + 无脑放行：本服务绑定在 127.0.0.1 上，只服务于本机宿主页面；
// 判据取 Origin 的 host，只认 127.0.0.1 / localhost / ::1 三种本机形态，
// 避免任意远端页面靠跨源读走账号与配额。无 Origin 头的请求（curl / 同源导航）不涉及 CORS，直接放行。
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && isTrustedLocalOrigin(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", "*")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-2Ag-Source")
			// 预检结果缓存 10 分钟，减少轮询期间无谓的 OPTIONS 往返。
			h.Set("Access-Control-Max-Age", "600")
		}
		// 预检请求没有 body 也不该进业务逻辑，直接 204。
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isTrustedLocalOrigin 判定 Origin 是否属于本机形态。
// 只解析 host（丢弃端口与 scheme），因为宿主与 API 的端口必然不同，
// 按完整 Origin 比对会把唯一的合法来源也一起挡掉。
func isTrustedLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch strings.ToLower(host) {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/context", s.handleContext)
	s.mux.HandleFunc("/api/v1/context/view.js", s.handleContextView)
	s.mux.HandleFunc("/api/v1/runtime", s.handleRuntime)
	s.mux.HandleFunc("/api/v1/doctor", s.handleDoctor)
	s.mux.HandleFunc("/api/v1/environment", s.handleEnvironment)
	s.mux.HandleFunc("/api/v1/quota/history", s.handleQuotaHistory)
	s.mux.HandleFunc("/api/v1/skills", s.handleSkills)
	s.mux.HandleFunc("/api/v1/skills/read", s.handleSkillRead)
	s.mux.HandleFunc("/api/v1/state", s.handleGetState)
	s.mux.HandleFunc("/api/v1/action", s.handlePostAction)
	s.mux.HandleFunc("/api/v1/events", s.handleEvents)
	s.mux.HandleFunc("/api/v1/dashboard", s.handleGetDashboard)
	s.mux.HandleFunc("/api/v1/host/status", s.handleGetHostStatus)
	s.mux.HandleFunc("/api/v1/host/launch", s.handleHostLaunch)
	s.mux.HandleFunc("/api/v1/host/takeover", s.handleHostTakeover)
	// 舱内账号轮转：G-Cockpit 不切回 2Ag 主窗口就能换账号并重启宿主沙箱。
	s.mux.HandleFunc("/api/v1/host/switch-and-restart", s.handleHostSwitchAndRestart)
	s.mux.HandleFunc("/api/v1/sessions", s.handleSessionsRoute)
	s.mux.HandleFunc("/api/v1/sessions/preview", s.handleSessionPreview)
	s.mux.HandleFunc("/api/v1/sessions/export", s.handleExportSession)
	s.mux.HandleFunc("/api/v1/sessions/delete", s.handleDeleteSession)
	s.mux.HandleFunc("/api/v1/accounts", s.handleGetAccounts)
	s.mux.HandleFunc("/api/v1/accounts/active", s.handleGetActiveAccount)
	s.mux.HandleFunc("/api/v1/accounts/primary", s.handleSetPrimaryAccount)
	s.mux.HandleFunc("/api/v1/accounts/refresh", s.handleRefreshAccounts)
	// 添加账号 = Official Antigravity Login Broker。
	//
	// 为什么不是一个 /oauth/login 端点：2Ag 不再是任何人的 OAuth 客户端。
	// Google 授权由本机官方 Antigravity 原生完成，2Ag 只负责「起官方 → 等结果 →
	// 捕获 → 加密入库 → 恢复原账号」。因此这里的语义是「开始一个流程 + 轮询它的阶段」，
	// 而不是「拿到一个授权 URL」。0.1.1 之前的 login-browser / oauth/status 已删除。
	s.mux.HandleFunc("/api/v1/accounts/broker/start", s.handleBrokerStart)
	s.mux.HandleFunc("/api/v1/accounts/broker/status", s.handleBrokerStatus)
	s.mux.HandleFunc("/api/v1/accounts/broker/cancel", s.handleBrokerCancel)
	// Account Vault：DPAPI 加密的账号保险库。列表只回元数据（邮箱/显示名/时间/字节数），
	// 永远不回 token —— 前端不需要它，能拿到它就是泄漏面。
	s.mux.HandleFunc("/api/v1/accounts/vault", s.handleVaultList)
	s.mux.HandleFunc("/api/v1/accounts/vault/delete", s.handleVaultDelete)
	s.mux.HandleFunc("/api/v1/accounts/import-json", s.handleImportAccountsJSON)
	// 凭据快照归档：把 Windows 凭据管理器里当前真实登录 blob 存到 ~/.2ag/vault/，
	// 防止后续换号覆盖掉宿主自己刷新过的 token（那份 token 别处没有副本）。
	s.mux.HandleFunc("/api/v1/accounts/snapshot", s.handleSnapshotCredential)
	s.mux.HandleFunc("/api/v1/hot/reload", s.handleHotReload)
	// 自定义壁纸入口：界面上的「Skin Studio」用它把用户手输的图片路径送进状态机。
	//
	// 为什么不复用 /api/v1/action 直接发 SET_THEME：那条路对 payload 不做任何实测，
	// 路径写错也照样落盘并回 200。壁纸是唯一一个「用户提供外部文件」的输入，
	// 必须当场查实文件在不在、是不是图片，否则失败形态就是安静的假成功
	// （配置里有路径，宿主背景还是旧图）。
	s.mux.HandleFunc("/api/v1/wallpaper", s.handleWallpaper)
	// 壁纸选取器：弹 Windows 原生「打开文件」对话框，返回用户选定的图片绝对路径。
	//
	// 为什么不能在前端用 <input type="file">：2Ag Manager 是 WebView2 视窗，
	// 网页选出的 File 对象拿不到可用的本地绝对路径（浏览器安全模型如此），
	// 而状态机需要的正是一个绝对路径（宿主侧要靠它把图读出来转 base64）。
	// 所以「选择」这件事必须在 Go 侧发生。
	//
	// 这个处理器是 POST 且会阻塞到用户做出选择或取消为止 —— 对话框自己跑
	// 模态消息循环，不占 Manager 的 UI 线程。阻塞时长由用户决定，
	// 因此 handler 里任何地方都不能假设它「很快返回」。
	s.mux.HandleFunc("/api/v1/wallpaper/pick", s.handleWallpaperPick)
	s.mux.HandleFunc("/api/v1/wallpaper/preview", s.handleWallpaperPreview)
	s.mux.HandleFunc("/api/v1/wallpaper/clear", s.handleWallpaperClear)
	// Official Compatibility：回答「退出 2Ag 后直接启动官方 Antigravity 会不会继承脏状态」。
	//
	// 读接口和写接口刻意分开：GET 可以随便轮询（面板就挂在那儿），
	// POST 会真的改写系统凭据管理器，必须是一次明确的用户动作。
	s.mux.HandleFunc("/api/v1/compat", s.handleGetCompat)
	s.mux.HandleFunc("/api/v1/compat/restore-credential", s.handleRestoreCredential)
	// Update Guardian：只报事实（官方版本 / 冻结宿主版本 / 是否落后），不做任何同步动作。
	// 本轮明确不实现自动更新、不拦截官方 updater、不改官方安装目录。
	s.mux.HandleFunc("/api/v1/update-guardian", s.handleGetUpdateGuardian)
	// Upstream Compatibility Guardian：逐项真去宿主 DOM 上试一次，回答
	// 「跑着的这个宿主还支撑得起哪些能力」。与 update-guardian 的分工必须写清楚 ——
	// 前者答版本事实，这个答能力事实，两者不能互相替代，因为版本号不是兼容性判据。
	s.mux.HandleFunc("/api/v1/capability", s.handleGetCapability)
	s.mux.HandleFunc("/json", s.handleProxyCDP)
	s.mux.HandleFunc("/json/list", s.handleProxyCDP)
}

func (s *Server) HandleStatic(prefix string, fs http.FileSystem) {
	if prefix == "/" {
		s.mux.Handle("/", http.FileServer(fs))
		return
	}
	s.mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(fs)))
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := s.stateMachine.GetState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handlePostAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action core.StateAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 这里严禁直接执行 HOST_CTRL 的物理动作（拉起/重启/停止宿主）。
	//
	// ApplyAction 对 HOST_CTRL 的处理是「发布 CommandEvent，不改状态」（见 internal/core/state.go
	// 的命令型 action 分支），命令由订阅者统一消费：图形模式下是 cmd/2ag/manager.go 的
	// CommandEvent 订阅者，CLI 模式下是 cmd/2ag/main.go 的 cmdCh 订阅者。
	// 历史实现在 ApplyAction 之前又直接调了一次 supervisor，于是同一次点击
	// 会先被 api 层执行一遍、再被订阅者执行一遍 —— start 会拉起两个宿主，
	// restart 会互相踩掉对方刚建好的沙箱，stop 会在宿主已死后对空 PID 再查杀一次。
	// 命令型 action 的唯一执行点是 CommandEvent 订阅者。
	if err := s.stateMachine.ApplyAction(action); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.bus.Subscribe(core.StateChangedEvent)
	defer s.bus.Unsubscribe(core.StateChangedEvent, ch)

	// Send initial state
	state := s.stateMachine.GetState()
	data, _ := json.Marshal(state)
	fmt.Fprintf(w, "event: state_changed\ndata: %s\n\n", data)
	flusher.Flush()

	for {
		select {
		case event := <-ch:
			data, err := json.Marshal(event.Payload)
			if err == nil {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

// Mux 返回裸路由表（不含 CORS 包装）。
// 供单元测试用 httptest 直接驱动路由，不经过中间件。
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) Serve(l net.Listener) error {
	// 用 s.handler（mux + CORS）而非 s.mux：注入补丁的所有 fetch 都靠这层带响应头。
	s.server = &http.Server{Handler: s.handler}
	return s.server.Serve(l)
}

func (s *Server) Start(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.handler,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

type AccountQuotaDTO struct {
	ID                string                 `json:"id"`
	Email             string                 `json:"email"`
	Name              string                 `json:"name"`
	Role              string                 `json:"role"`
	IsPrimary         bool                   `json:"is_primary"`
	IsActive          bool                   `json:"is_active"`
	Status            string                 `json:"status"`
	Weight            int                    `json:"weight"`
	GeminiPool        supervisor.QuotaWindow `json:"gemini_pool"`
	ClaudePool        supervisor.QuotaWindow `json:"claude_pool"`
	Gemini5hPercent   int                    `json:"gemini_5h_percent"`
	Gemini5hReset     string                 `json:"gemini_5h_reset"`
	GeminiWeekPercent int                    `json:"gemini_week_percent"`
	GeminiWeekReset   string                 `json:"gemini_week_reset"`
	Claude5hPercent   int                    `json:"claude_5h_percent"`
	Claude5hReset     string                 `json:"claude_5h_reset"`
	ClaudeWeekPercent int                    `json:"claude_week_percent"`
	ClaudeWeekReset   string                 `json:"claude_week_reset"`
	Models            []string               `json:"models"`
}

func toAccountQuotaDTO(acc supervisor.AccountInstance) AccountQuotaDTO {
	return AccountQuotaDTO{
		ID:                acc.ID,
		Email:             acc.Email,
		Name:              acc.Name,
		Role:              acc.Role,
		IsPrimary:         acc.IsPrimary,
		IsActive:          acc.IsActive,
		Status:            acc.Status,
		Weight:            acc.Weight,
		GeminiPool:        acc.GeminiPool,
		ClaudePool:        acc.ClaudePool,
		Gemini5hPercent:   acc.GeminiPool.FiveHourPercent,
		Gemini5hReset:     acc.GeminiPool.FiveHourReset,
		GeminiWeekPercent: acc.GeminiPool.WeeklyPercent,
		GeminiWeekReset:   acc.GeminiPool.WeeklyReset,
		Claude5hPercent:   acc.ClaudePool.FiveHourPercent,
		Claude5hReset:     acc.ClaudePool.FiveHourReset,
		ClaudeWeekPercent: acc.ClaudePool.WeeklyPercent,
		ClaudeWeekReset:   acc.ClaudePool.WeeklyReset,
		Models:            acc.Models,
	}
}

// resolveActiveHostAccount 返回「宿主当前真实在跑的账号」，并让 2Ag 的记录与之一致。
//
// 为什么需要它（真机实证，见 scripts/audit/probe_real_identity.js）：
//
//	Antigravity 2.x 的登录身份存在 Windows 凭据管理器的 gemini:antigravity 里
//	（CRED_PERSIST_LOCAL_MACHINE ⇒ 机器级，所有 --user-data-dir 共享），
//	而 ~/.2ag/active_account.txt 只是 2Ag 自己记的「上次让谁上」的便签。
//	历史实现单向信任这张便签，于是出现「宿主跑着 A 账号的沙箱，
//	面板却显示 arukas 的邮箱与配额」—— 用户看到的每个数字都属于另一个账号。
//
// 只在宿主存活时采信凭据：宿主不在时凭据是上一轮登录留下的旧值，此时面板显示的
// 应当是 2Ag 的「预设主控」（live_status 也正是这么标的），而不是一个没有活体
// 在维护的历史身份。hostLive 由调用方按 ProbeRealHost() 的结果传入。
func resolveActiveHostAccount(hostLive bool) (supervisor.AccountInstance, bool) {
	if !hostLive {
		return supervisor.GetActiveAccountInstance(), false
	}
	if email, err := supervisor.ReadHostLoginEmailCached(); err == nil && email != "" {
		if !strings.EqualFold(email, supervisor.GetActiveAccountEmail()) {
			log.Printf("[2ag] 宿主真实登录身份为 %s，与本地记录不一致，已按系统凭据修正主控账号", email)
			supervisor.SetActiveAccount(email)
		}
		return supervisor.GetActiveAccountInstance(), true
	}
	// 宿主活着却读不出身份：这是「未知」，不是「已确认」。第二个返回值为 false，
	// 调用方必须把它如实报给界面 —— 否则界面会把 active_account.txt 里的旧值
	// 当成宿主当前身份展示，这正是历史缺陷（显示的是另一个账号）。
	return supervisor.GetActiveAccountInstance(), false
}

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostStatus := supervisor.ProbeRealHost()
	isLive := hostStatus.ProcessFound && hostStatus.CDPConnected

	activeAcc, identityVerified := resolveActiveHostAccount(isLive)

	liveText := "● 预设主控 (待挂钩)"
	if isLive {
		liveText = "● 实时在线 (已挂钩)"
	}

	// 协议网关真实状态。
	//
	// 历史上这里是写死的一块假数据：地址恒为 127.0.0.1:8045、延迟恒为 28ms、
	// 状态恒为 READY —— 而 8045 从来没有被任何进程监听（netproxy 用的是
	// 127.0.0.1:0 动态端口，Manager 图形模式更是完全不启动代理）。
	// 现在改为读取进程内真实登记的代理实例；未运行时如实报告 offline。
	gatewayStatus := BuildGatewayStatus()

	payload := map[string]any{
		"host_status":       hostStatus,
		"active_account":    toAccountQuotaDTO(activeAcc),
		"is_live":           isLive,
		"live_status":       liveText,
		"identity_verified": identityVerified,
		"gateway_status":    gatewayStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

// BuildGatewayStatus 返回协议网关的真实运行状态。
//
// 探测方式：取进程内真实登记的 netproxy 实例 → 向它自己的 /status 端点发一次
// 真实 HTTP 请求并计时。这既给出真实监听地址，也给出真实往返延迟，
// 而不是任何形式的估算或常量。
func BuildGatewayStatus() map[string]any {
	proxy := netproxy.Active()
	if proxy == nil {
		return map[string]any{
			"active":     false,
			"address":    "",
			"mode":       "未启用（本地协议代理未启动）",
			"latency_ms": 0,
			"status":     "OFFLINE",
			"protocol":   "Gemini v1internal -> OpenAI Compatible",
		}
	}
	address := proxy.Address()
	status := "READY"
	latency := int64(0)
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	started := time.Now()
	if resp, err := client.Get("http://" + address + "/status"); err != nil {
		status = "UNREACHABLE"
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		latency = time.Since(started).Milliseconds()
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			status = "DEGRADED"
		}
	}
	return map[string]any{
		"active":     true,
		"address":    address,
		"mode":       "本地透明转发 · 隐私屏蔽名单生效",
		"latency_ms": latency,
		"status":     status,
		"protocol":   "Gemini v1internal -> OpenAI Compatible",
	}
}

func (s *Server) handleGetHostStatus(w http.ResponseWriter, r *http.Request) {
	status := supervisor.ProbeRealHost()
	isLive := status.ProcessFound && status.CDPConnected

	activeAcc, identityVerified := resolveActiveHostAccount(isLive)
	liveText := "● 预设主控 (待挂钩)"
	if isLive {
		liveText = "● 实时在线 (已挂钩)"
	}
	// 形态读数与宿主读数同源返回：界面上「现在是 OFFICIAL CLEAN 还是 2Ag ENHANCED」
	// 和「宿主在不在」是同一屏里的同一件事，分两个请求拿会让它们短暂不一致，
	// 而这一屏恰恰是用户用来判断「我现在到底是什么状态」的。
	modeFacts := supervisor.RuntimeModeFactsNow(s.currentRuntimeMode())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"process_found":     status.ProcessFound,
		"pid":               status.PID,
		"memory_mb":         status.MemoryMB,
		"cdp_connected":     status.CDPConnected,
		"cdp_port":          status.CDPPort,
		"active_account":    toAccountQuotaDTO(activeAcc),
		"is_live":           isLive,
		"live_status":       liveText,
		"identity_verified": identityVerified,
		"runtime_mode":      modeFacts,
	})
}

// currentRuntimeMode 读「配置里说要跑哪种形态」。
//
// 刻意读状态机而不是读盘：状态机持有的是用户刚刚在界面上做出的选择，
// 磁盘上的那份还要等 300ms 落盘定时器。刚点完切换就读盘，
// 会有一瞬间界面显示的还是旧形态 —— 而这种闪烁正好出现在用户最需要确认的时刻。
func (s *Server) currentRuntimeMode() string {
	if s.stateMachine == nil {
		return ""
	}
	return s.stateMachine.GetState().RuntimeMode
}

// handleGetActiveAccount 返回 2Ag 的「预设主控账号」（即用户的选择意图，落盘于
// ~/.2ag/active_account.txt），而**不是**宿主当前的登录身份。
//
// 与 host/status、dashboard 的分工是刻意的：
//   - host/status 与 dashboard 描述「宿主现在是什么」，必须由系统凭据反哺真身；
//   - 本接口描述「2Ag 选的是谁」，是配置读取，不能因为宿主此刻登录着别的账号就
//     改写用户的设置（那会让一次 GET 悄悄改掉配置）。
//
// 两者语义不同、允许短暂不一致；switch-and-restart 成功时会把两者对齐。
func (s *Server) handleGetActiveAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	activeAcc := supervisor.GetActiveAccountInstance()
	dto := toAccountQuotaDTO(activeAcc)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"account":             dto,
		"active_account":      dto,
		"email":               activeAcc.Email,
		"name":                dto.Name,
		"role":                dto.Role,
		"status":              dto.Status,
		"gemini_5h_percent":   dto.Gemini5hPercent,
		"gemini_5h_reset":     dto.Gemini5hReset,
		"gemini_week_percent": dto.GeminiWeekPercent,
		"gemini_week_reset":   dto.GeminiWeekReset,
		"claude_5h_percent":   dto.Claude5hPercent,
		"claude_5h_reset":     dto.Claude5hReset,
		"claude_week_percent": dto.ClaudeWeekPercent,
		"claude_week_reset":   dto.ClaudeWeekReset,
		"gemini_pool":         dto.GeminiPool,
		"claude_pool":         dto.ClaudePool,
	})
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ScanLocalAccounts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"accounts": accounts,
	})
}

func (s *Server) handleSetPrimaryAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		http.Error(w, "email 不能为空", http.StatusBadRequest)
		return
	}
	// 存在性校验：只接受本地账号文件里真实存在的邮箱。
	//
	// 没有这道闸时，任意字符串（例如 a@b.c 这种幽灵地址）都会被写进主控标记、
	// 落盘到 ~/.2ag/active_account.txt，并回 success:true —— 界面随即顶着这个
	// 不存在的账号渲染「主控活跃」，而它的配额当然是空的。主控身份必须对应一个
	// 真实存在的账号，否则「主控」这个词没有任何意义。
	// 校验依据是 ScanLocalAccounts() 的真实扫描结果，不是调用方传入的任何值。
	known := false
	for _, acc := range supervisor.ScanLocalAccounts() {
		if strings.EqualFold(acc.Email, req.Email) {
			known = true
			break
		}
	}
	if !known {
		http.Error(w, "该账号不在本地账号文件中: "+req.Email, http.StatusNotFound)
		return
	}
	supervisor.SetActiveAccount(req.Email)
	activeAcc := supervisor.GetActiveAccountInstance()
	dto := toAccountQuotaDTO(activeAcc)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":        true,
		"email":          supervisor.GetActiveAccountEmail(),
		"active_account": dto,
	})
}

func (s *Server) handleRefreshAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ScanLocalAccounts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"accounts":  accounts,
		"refreshed": true,
		"timestamp": time.Now().Unix(),
	})
}

// handleBrokerStart 开始一次「通过官方 Antigravity 原生登录添加账号」。
//
// 这个动作会短暂地改动机器上唯一那份登录凭据（删掉 → 官方登录 → 写回原账号），
// 因此它必须由用户显式点击触发，而且进度必须可轮询、可取消：
// 用户要能看到「我的原账号回来了没有」。
func (s *Server) handleBrokerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := supervisor.StartLoginBroker(s.currentRuntimeMode()); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": err.Error(),
			"status":  supervisor.LoginBrokerStatusNow(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "已开始「添加账号」：将启动官方 Antigravity，请在弹出的官方窗口里用 Google 登录",
		"status":  supervisor.LoginBrokerStatusNow(),
	})
}

// handleBrokerStatus 轮询添加账号流程的阶段。
func (s *Server) handleBrokerStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"status":  supervisor.LoginBrokerStatusNow(),
	})
}

// handleBrokerCancel 请求取消（流程会在安全检查点上回滚并恢复原账号）。
func (s *Server) handleBrokerCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := supervisor.CancelLoginBroker(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "已请求取消，正在恢复原账号",
		"status":  supervisor.LoginBrokerStatusNow(),
	})
}

// handleVaultList 列出保险库里的账号（只有元数据，没有任何 token）。
func (s *Server) handleVaultList(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ListVaultAccountEntries()
	// 当前系统登录身份：面板要能区分「保险库里有这个账号」与「现在就是它」。
	current := ""
	if v, err := supervisor.ReadHostLoginEmailCached(); err == nil {
		current = strings.TrimSpace(v)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"current":  current,
		"accounts": accounts,
		"count":    len(accounts),
	})
}

// handleVaultDelete 删除保险库里的一个账号（只删 2Ag 自己的副本，不动系统当前凭据）。
func (s *Server) handleVaultDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		http.Error(w, "缺少 email", http.StatusBadRequest)
		return
	}
	if err := supervisor.DeleteVaultAccount(email); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "已从保险库删除 " + email,
	})
}

func (s *Server) handleImportAccountsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	accounts, err := supervisor.ImportAccountsJSON(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"accounts": accounts,
	})
}

// handleSnapshotCredential 归档当前系统凭据（Windows 凭据管理器 target=gemini:antigravity）
// 到 ~/.2ag/vault/<email_hash>.bin。
//
// 为什么不做「静默成功」：归档失败的三种成因（没有凭据 / 读不出归属邮箱 / 磁盘写失败）
// 对使用者意味着完全不同的后续动作，必须原样透出。前端 toast 显示的就是这里的 message。
func (s *Server) handleSnapshotCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshot, err := supervisor.SnapshotAntigravityCredential()
	if err != nil {
		// 404 用于「没有可归档的东西」（凭据缺失 / 归属不明），500 用于真实故障。
		// 前端据此区分「宿主还没登录」与「归档功能坏了」。
		status := http.StatusInternalServerError
		if !snapshotBytesWritten(snapshot) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"email":   snapshot.Email,
		"path":    snapshot.Path,
		"bytes":   snapshot.Bytes,
		"message": "已成功归档 " + snapshot.Email + " 凭据快照",
	})
}

// snapshotBytesWritten 区分「归档动作没做成」与「归档做到了但后续步骤失败」：
// 只要一个字节都没写进去，就属于前者 → 404，而不是 500。
func snapshotBytesWritten(s supervisor.CredentialSnapshot) bool {
	return s.Bytes > 0
}

// handleGetCompat 返回 Official Compatibility / Clean State 的完整读数。
//
// 这里刻意不缓存：面板每次展开都真读一遍文件系统与凭据管理器。
// 这一屏的全部价值就在于「此刻是否干净」，一个来自十分钟前的缓存快照
// 会把用户刚做的恢复动作显示成没生效。
func (s *Server) handleGetCompat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(supervisor.ProbeOfficialCompatibility())
}

// handleRestoreCredential 把 2Ag 归档的凭据快照写回 Windows 凭据管理器。
//
// 为什么是独立端点而不是复用 /api/v1/action：这条动作会改写机器级共享状态，
// 且用户必须显式指定「恢复成哪个账号」。走通用 action 通道会让它看起来
// 和切换一个主题一样轻。
func (s *Server) handleRestoreCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		http.Error(w, "缺少 email：必须明确指定要恢复成哪个账号，不做「恢复成上一个」这种猜测", http.StatusBadRequest)
		return
	}

	result, err := supervisor.RestoreAntigravityCredential(email)
	if err != nil {
		// 归还 result 而不是只报错：即使复核失败，用户也需要看到「当前归属是谁」，
		// 否则他不知道自己是回到了官方账号还是停在一个中间态。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"message": err.Error(),
			"result":  result,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": result.Message,
		"result":  result,
	})
}

// handleGetUpdateGuardian 报告官方 Antigravity 与 2Ag 冻结宿主的版本关系。
//
// 只读。不下载、不替换文件、不碰官方 updater —— 本轮实现的是「让用户至少知道
// 自己落后了」，不是自动更新。
func (s *Server) handleGetUpdateGuardian(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(supervisor.ProbeUpdateGuardian())
}

// handleGetCapability 报告宿主能力实测结论（Compatibility Guardian 的数据源）。
//
// 与 /api/v1/update-guardian 并列而不是合并进它：版本是静态事实（读 exe 版本资源，
// 快且无副作用），能力是动态事实（要走一次 CDP 往返到宿主）。把两者塞进一个端点，
// 会让「只想看版本」的轮询顺带把 CDP 也打一遍。
func (s *Server) handleGetCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	rep := supervisor.ProbeCapabilityReport()
	// 共享数据树的 schema 指纹一并带出：它是「两形态共读一棵树」这个已知风险的
	// 唯一可量化信号，而它与能力探测读的是同一批事实（都来自只读磁盘/DOM）。
	//
	// 官方只读探测同样带出来 —— 它是「升级后大概会坏什么」唯一能给出实测证据的来源，
	// 而它只在官方宿主恰好开着、且自己开了调试端口时才有读数（Available=false 时
	// 前端会显示 UNKNOWN，不会被当成「全都好」）。
	json.NewEncoder(w).Encode(map[string]any{
		"guardian":       rep,
		"shared_data":    supervisor.ProbeSharedDataSchema(),
		"official_probe": supervisor.ProbeOfficialHostReadOnly(),
	})
}

func (s *Server) handleSessionsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.handleDeleteSession(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := supervisor.ScanAISessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id == "" {
		http.Error(w, "缺少会话 ID", http.StatusBadRequest)
		return
	}
	if err := supervisor.DeleteSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"id":      id,
	})
}

func (s *Server) handleExportSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "缺少会话 ID", http.StatusBadRequest)
		return
	}
	md, err := supervisor.ExportSessionMarkdown(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"session-%s.md\"", shortID))
	w.Write([]byte(md))
}

func (s *Server) handleHostLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomPath string `json:"custom_path"`
		Email      string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Email != "" {
		supervisor.SetActiveAccount(req.Email)
	}
	if err := supervisor.LaunchEnhancedHost(req.CustomPath, req.Email); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "宿主已成功拉起并附加 CDP 沙箱",
	})
}

func (s *Server) handleHostTakeover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomPath string `json:"custom_path"`
		Email      string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Email != "" {
		supervisor.SetActiveAccount(req.Email)
	}
	if err := supervisor.TakeoverHost(req.CustomPath, req.Email); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "宿主已成功热接管，CDP 调试端口与账号沙箱已就绪",
	})
}

// handleHostSwitchAndRestart 舱内账号轮转：切主控 + 换真实登录凭据 + 重启宿主沙箱。
//
// 与 /api/v1/accounts/primary 的区别在于「物理动作」：那条路由只改内存里的主控标记，
// 宿主进程仍挂着旧账号的沙箱目录；这条会清掉旧宿主整棵进程树、把目标账号的凭据
// 写进 Windows 凭据管理器，再用新账号专属的 profile 目录重新拉起 —— 三者齐备，
// 「换号」才在磁盘与系统凭据层面真正发生。
//
// 诚实性要求：过去这条路由只改 active_account.txt 与 --user-data-dir，宿主原生
// Settings 里显示的仍是旧账号（凭据在机器级凭据管理器里，profile 隔离改不到它），
// 而接口却回 success:true —— 用户看到的就是「虚假切换」。现在切换后必须回读系统
// 凭据确认身份，读不到就如实报错，绝不用请求里的 email 冒充结果。
func (s *Server) handleHostSwitchAndRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		http.Error(w, "email 不能为空", http.StatusBadRequest)
		return
	}

	// 切换是事务：停宿主 → 写凭据 → 按原形态启动 → 校验实际身份 → 失败自动回滚。
	//
	// 与 0.1.1 之前那版的区别有两点，都是被真机证伪过的：
	//  1. 凭据来源不再是 cockpit 账号库，而是 2Ag 自己的 DPAPI 保险库 ——
	//     账号是用户通过官方原生登录加进来的，不再依赖第三方工具的数据目录。
	//  2. 不再「先设主控标记、依赖 LaunchEnhancedHost 顺手写凭据」：那条路径里
	//     ApplyAntigravityCredential 失败只记日志，于是「沙箱换了、身份没换」会
	//     以 success:true 收场。现在写凭据与校验都是显式的，且必须回读一致。
	result, err := supervisor.SwitchAccountTransactional(email, s.currentRuntimeMode())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"email":   email,
			"message": err.Error(),
			"result":  result,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":             true,
		"email":               result.VerifiedOwner,
		"credential_switched": true,
		"message":             result.Message,
		"result":              result,
	})
}

func (s *Server) handleProxyCDP(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + supervisor.ResolveCDPAddr() + r.URL.RequestURI())
	if err != nil {
		http.Error(w, "CDP 端口未就绪或未运行: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// HotReloadResult 是「热重载补丁」的真实回执。
//
// 历史上该按钮只往状态机发一个 HOT_RELOAD 事件、前端不读响应体，
// 于是无论后台是成功、失败还是根本没找到宿主，界面都只会打一行「动作派发成功」——
// 这正是「假热重载」能长期存活的原因。现在把每一步的真实结果都摊开给调用方。
type HotReloadResult struct {
	Success    bool   `json:"success"`
	Injected   int    `json:"injected"`
	Targets    int    `json:"targets"`
	SourceKind string `json:"source_kind"` // disk | embedded
	Source     string `json:"source"`
	Size       int    `json:"size"`
	Message    string `json:"message"`
}

func (s *Server) handleHotReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	report, err := supervisor.HotReloadCDP(supervisor.ResolveCDPAddr())
	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		// 404 而不是 200：前端 logMsg 只有在 !r.ok 时才会把失败显示成错误，
		// 静默返回 200 会让「注入失败」看起来像「注入成功」。
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":     false,
			"source_kind": report.SourceKind,
			"source":      report.Source,
			"size":        report.Size,
			"injected":    report.Injected,
			"targets":     report.Targets,
			"message":     err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"injected":    report.Injected,
		"targets":     report.Targets,
		"source_kind": report.SourceKind,
		"source":      report.Source,
		"size":        report.Size,
		"message":     report.Message,
	})
}

// handleWallpaper 实现「Skin Studio → 自定义壁纸」这条入口的落点。
//
// 三件事，顺序不能换：先**查实**用户给的文件，再把规范化后的路径交给状态机，
// 最后回一份带实测信息的回执。整体走的是既有链路（ApplyAction → StateChangedEvent
// → 订阅者 → CDP 增量推送 / 整段重注入），没有任何绕过状态机的旁路 ——
// 这一点是刻意的：此前换壁纸只有 CLI `2ag skin set-wallpaper`，它直写 2ag.json
// 而不经状态机，于是界面与宿主都不会更新，用户改完只看得到「配置文件变了」。
//
// 为什么不由这条路由自己去做注入：ApplyAction 之后会发布 StateChangedEvent，
// 图形模式的订阅者已经在监听它（并把变更推给宿主）。若这里再推一次，
// 同一次操作会推两遍，而两遍之间用户的意图可能已经变了。
func (s *Server) handleWallpaper(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// GET：回报当前配置里的壁纸与它此刻在磁盘上的实测状态。
	// 界面用它来做「加载时回显」——只显示一个路径字符串是不够的，
	// 用户需要知道那个文件现在还在不在（换盘符、删文件都会让它失效）。
	if r.Method == http.MethodGet {
		s.writeWallpaperState(w, "")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "请求体解析失败: " + err.Error()})
		return
	}

	// 查实：文件存在、不是目录、体积合理、文件头是可识别的图片。
	info, err := supervisor.ValidateWallpaperFile(req.Path)
	if err != nil {
		// 400 + success:false：让界面能把「为什么不行」原样显示出来，
		// 而不是给一个沉默的失败。
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
		return
	}

	if err := s.stateMachine.ApplyAction(core.StateAction{
		Type:    core.SetThemeAction,
		Payload: map[string]any{"wallpaper_path": info.Path},
	}); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "状态机拒绝了这次变更: " + err.Error()})
		return
	}

	s.writeWallpaperState(w, "已应用："+info.Name+"（"+info.MimeType+"）")
}

// writeWallpaperState 组装壁纸回执。message 为空时是纯读取（GET 用）。
func (s *Server) writeWallpaperState(w http.ResponseWriter, note string) {
	state := s.stateMachine.GetState()
	info, err := supervisor.ValidateWallpaperFile(state.WallpaperPath)

	payload := map[string]any{
		"success": true,
		// 配置里的路径是「用户选择」，files 里的 info 是「此刻磁盘上的实测」。
		// 两者分开返回：路径存在但文件已被删除是完全可能的，界面必须能区分，
		// 否则用户会盯着一个不存在的路径以为一切正常。
		"path": state.WallpaperPath,
	}
	if err != nil {
		payload["file_ok"] = false
		payload["file_message"] = err.Error()
	} else {
		payload["file_ok"] = true
		payload["file"] = info
	}
	if note != "" {
		payload["message"] = note
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// handleWallpaperPick 弹一次系统原生文件选择器，并把用户选中的图片当真应用掉。
//
// 它把「选择」和「应用」合成一次请求，而不是让前端分两步（先选、再 POST 路径）：
// 分两步的话，用户选完到前端发出第二个请求之间有一个可观测的中间态 ——
// 对话框关了，界面还是旧壁纸，中间如果前端崩了或被刷新，用户的选择就凭空消失。
// 合成一步后，只要这个请求返回 success，配置与宿主就已经是新图了。
//
// 三种结局分别对应三种 HTTP 语义，前端必须区别对待：
//   - 200 {cancelled:true}    用户取消。不是错误，不要弹红字。
//   - 200 {success:true,...}  已应用，附带落盘后的实测信息。
//   - 4xx/5xx {success:false} 真的失败（选了非图片、状态机拒绝、对话框起不来）。
func (s *Server) handleWallpaperPick(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "请用 POST 调用"})
		return
	}

	picked, err := supervisor.PickImageFile("选择壁纸图片")
	switch {
	case errors.Is(err, supervisor.ErrPickCancelled):
		// 取消是正常操作，不是失败。前端据此把按钮恢复成不忙的状态即可。
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":   true,
			"cancelled": true,
			"message":   "已取消选择，壁纸未改动",
		})
		return
	case errors.Is(err, supervisor.ErrPickBusy):
		// 409：已经有一个对话框开着了。前端不该把它显示成「操作失败」，
		// 而是提示「请先在已打开的对话框里完成选择」。
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
		return
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": err.Error()})
		return
	}

	// 选出来的东西必须先过同一套校验：扩展名过滤只是收窄了浏览范围，
	// 用户完全可以在「所有文件」里挑一个 .txt，而 ValidateWallpaperFile
	// 是按文件头字节判类型的 —— 这一步才是真正的门禁。
	info, err := supervisor.ValidateWallpaperFile(picked)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			// 把用户选了什么也带回去：否则界面只能显示「这不是图片」，
			// 而用户刚在对话框里看到的那一长串路径已经关掉看不见了。
			"path":    picked,
			"message": "选中的文件无法作为壁纸：" + err.Error(),
		})
		return
	}

	if err := s.stateMachine.ApplyAction(core.StateAction{
		Type:    core.SetThemeAction,
		Payload: map[string]any{"wallpaper_path": info.Path},
	}); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "状态机拒绝了这次变更: " + err.Error()})
		return
	}

	s.writeWallpaperState(w, "已应用："+info.Name+"（"+info.MimeType+"）")
}

// handleWallpaperPreview 把「当前配置里那张壁纸」的原始字节回给界面，用于缩略图。
//
// 为什么必须由后端来喂这张图：Manager 跑在 WebView2 里，页面自身是
// http://127.0.0.1:<managerPort>，它加载不了用户磁盘上的 file:// 路径
// （跨协议，且 WebView2 默认禁止本地文件访问）。宿主那边同理 —— 这也正是
// ResolveWallpaperDataURL 存在的理由。所以缩略图只能走后端。
//
// 安全边界（重要）：**只服务状态机里当前记录的那一个路径**，不接受调用方传路径。
// 本服务监听在本机回环上，而注入补丁跑在宿主页面里、能 fetch 本服务；
// 一旦允许任意路径，宿主里任何一段脚本都能把用户磁盘上的任意文件读出来。
// 要预览别的东西，先让它成为「当前壁纸」—— 而那一步必然经过校验与状态机。
func (s *Server) handleWallpaperPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info, err := supervisor.ValidateWallpaperFile(s.stateMachine.GetState().WallpaperPath)
	if err != nil {
		// 404 而不是 500：这里最常见的成因是「配置里那个文件已经不在磁盘上了」，
		// 那是用户环境的正常变化，不是服务端故障。界面据此显示占位图。
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	file, err := os.Open(filepath.FromSlash(info.Path))
	if err != nil {
		http.Error(w, "无法打开该文件", http.StatusNotFound)
		return
	}
	defer file.Close()

	// ETag 由「路径长度 + 大小 + 文件名」拼成：内容变了它必然变，内容没变就不必重传。
	// 界面每轮状态刷新都会请求这张图，没有它就得反复推几十 MB。
	etag := fmt.Sprintf(`"%x-%x-%s"`, len(info.Path), info.SizeBytes, info.Name)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", info.MimeType)
	// 壁纸可能很大（上限 32 MiB），交给内核直接送，不在用户态再拷一遍。
	_, _ = io.Copy(w, file)
}

// handleWallpaperClear 清空壁纸配置，回到「没有自定义壁纸」的默认外观。
//
// 它走的是同一个 SetThemeAction —— 换壁纸与清除壁纸不是两条路径，
// 而是同一条路径上的两个值（空串 = 没有自定义壁纸）。
// 注入补丁的 ensureDreamSkin() 对空值会移除皮肤层，所以清空是立刻可见的。
func (s *Server) handleWallpaperClear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "请用 POST 调用"})
		return
	}
	if err := s.stateMachine.ApplyAction(core.StateAction{
		Type:    core.SetThemeAction,
		Payload: map[string]any{"wallpaper_path": ""},
	}); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": "状态机拒绝了这次变更: " + err.Error()})
		return
	}
	s.writeWallpaperState(w, "已清除自定义壁纸，外观回到默认")
}
