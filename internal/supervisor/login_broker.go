//go:build windows

package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// brokerWaitTimeout 是一次「添加账号」的等待上限。
//
// 取 10 分钟：用户要在官方窗口里完成一次真正的 Google 登录（可能还要选账号、
// 过二次验证、甚至先处理网络问题）。超时太短会把「慢」误判成「失败」，
// 而误判的代价是把他刚输完的登录流程掐掉。
const brokerWaitTimeout = 10 * time.Minute

var (
	loginBrokerMu      sync.Mutex
	loginBrokerStatus  = LoginBrokerStatus{Stage: brokerStageIdle, Message: "尚未开始"}
	loginBrokerCancel  bool
	loginBrokerRunning bool
)

// LoginBrokerStatusNow 返回当前状态的快照（并发安全）。
func LoginBrokerStatusNow() LoginBrokerStatus {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	out := loginBrokerStatus
	out.Steps = append([]string(nil), loginBrokerStatus.Steps...)
	return out
}

// StartLoginBroker 启动一次「通过官方 Antigravity 原生登录添加账号」。
//
// 单飞：已经在跑就直接拒绝，而不是排队 —— 两个并发流程会各自删/写同一条凭据，
// 那是能直接把用户的登录态弄丢的组合。
func StartLoginBroker(configuredMode string, networkModes ...string) error {
	mode := "AUTO"
	if len(networkModes) > 0 {
		mode = networkModes[0]
	}
	network, err := buildLoginNetwork(mode, os.Environ())
	if err != nil {
		return err
	}
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		release()
		return fmt.Errorf("存在未完成的凭据恢复，请重启 2Ag 先恢复原账号")
	}
	loginBrokerMu.Lock()
	if loginBrokerRunning {
		loginBrokerMu.Unlock()
		release()
		return fmt.Errorf("已有一个「添加账号」流程在进行中（阶段：%s）", loginBrokerStatus.Stage)
	}
	loginBrokerRunning = true
	loginBrokerCancel = false
	loginBrokerStatus = LoginBrokerStatus{
		Running:     true,
		Stage:       brokerStageBackingUp,
		Message:     "正在备份当前登录凭据…",
		Steps:       nil,
		NetworkMode: network.Mode,
		StartedAt:   time.Now().Format(time.RFC3339),
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}
	loginBrokerMu.Unlock()

	go func() { defer release(); runLoginBroker(configuredMode, network) }()
	return nil
}

// CancelLoginBroker 请求取消当前流程（协作式：由流程自己在安全检查点上退出并回滚）。
func CancelLoginBroker() error {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	if !loginBrokerRunning {
		return fmt.Errorf("当前没有正在进行的「添加账号」流程")
	}
	loginBrokerCancel = true
	loginBrokerStatus.Stage = brokerStageCancelled
	loginBrokerStatus.Message = "正在取消并恢复原账号…"
	loginBrokerStatus.UpdatedAt = time.Now().Format(time.RFC3339)
	return nil
}

func brokerCancelled() bool {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	return loginBrokerCancel
}

// brokerStage 推进阶段并记录一条人类可读的步骤。
func brokerStage(stage, message string) {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	loginBrokerStatus.Stage = stage
	loginBrokerStatus.Message = message
	if stage == brokerStageWaiting {
		loginBrokerStatus.FailureCode = classifyAccountFailure(message)
	}
	loginBrokerStatus.UpdatedAt = time.Now().Format(time.RFC3339)
	loginBrokerStatus.Steps = append(loginBrokerStatus.Steps, "["+time.Now().Format("15:04:05")+"] "+message)
	log.Printf("[2ag] Login Broker [%s] %s", stage, message)
}

// brokerMutate 在锁内改状态（用于写 email/结果类字段）。
func brokerMutate(fn func(*LoginBrokerStatus)) {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	fn(&loginBrokerStatus)
	loginBrokerStatus.UpdatedAt = time.Now().Format(time.RFC3339)
}

func brokerFinish(stage, message string, failure string) {
	loginBrokerMu.Lock()
	defer loginBrokerMu.Unlock()
	loginBrokerStatus.Stage = stage
	loginBrokerStatus.Message = message
	loginBrokerStatus.FailureReason = failure
	if failure != "" {
		loginBrokerStatus.FailureCode = classifyAccountFailure(message + " " + failure)
	} else {
		loginBrokerStatus.FailureCode = ""
	}
	loginBrokerStatus.Running = false
	loginBrokerStatus.UpdatedAt = time.Now().Format(time.RFC3339)
	loginBrokerStatus.FinishedAt = loginBrokerStatus.UpdatedAt
	loginBrokerStatus.Steps = append(loginBrokerStatus.Steps, "["+time.Now().Format("15:04:05")+"] "+message)
	loginBrokerRunning = false
	log.Printf("[2ag] Login Broker [%s] %s", stage, message)
}

// stopAllAntigravityProcesses 停掉当前机器上所有 antigravity.exe（含子进程）。
//
// 为什么必须这么彻底，而不是只杀 2Ag 托管的那一个：
//   - gemini:antigravity 是 machine 级凭据，所有实例共享；只要还有一个实例活着，
//     它随时可能把手里的旧 token 再写回去，把刚建立的登录态覆盖掉。
//   - Electron 的单实例锁：旧实例不退，新实例只会把旧窗口唤到前台然后退出，
//     「重新启动后校验身份」就永远等不到真正的重启。
//
// 刻意用 /T 连子进程一起结束：GPU/渲染/网络子进程不退出会继续持有文件锁。
//
// 返回值是「调用后仍然活着的进程数」（0 = 真的清干净了）。刻意不报「杀掉了几个」：
// taskkill 对「父进程已死、自己随后消失」的子进程会返回非零，于是「杀成功数」会
// 莫名其妙地小于进程总数（真机见过 1/6），读日志的人会以为有 5 个没杀掉 ——
// 而真正要回答的问题只有一个：**现在还剩几个**。
func stopAllAntigravityProcesses(reason string) int {
	procs := scanAntigravityProcesses()
	for _, p := range procs {
		cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(p.PID), "/T")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		_ = cmd.Run()
	}
	// 等它们真的从进程表里消失：写的凭据要在「没有实例能覆盖它」的时刻落地。
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if len(scanAntigravityProcesses()) == 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	SetManagedHostPID(0)
	left := len(scanAntigravityProcesses())
	if len(procs) > 0 {
		log.Printf("[2ag] Login Broker：已请求停止 %d 个 Antigravity 进程（%s），现在剩余 %d 个",
			len(procs), reason, left)
	}
	return left
}

// officialLoginPageState 只读探测官方窗口当前停在哪一页。
//
// 返回 "onboarding"（停在登录页）/ "app"（已离开登录页）/ ""（读不到：官方没跑、
// 端口没了、或 /json 拿不到东西）。**只读**：不注入、不留常驻会话 ——
// 官方形态的契约就是不碰宿主。
func officialLoginPageState() string {
	port, _, err := officialDevToolsActivePort()
	if err != nil || port <= 0 || !portListening(port) {
		return ""
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var targets []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(body, &targets) != nil {
		return ""
	}
	sawPage := false
	for _, t := range targets {
		if t.Type != "page" {
			continue
		}
		sawPage = true
		if strings.Contains(t.URL, "/onboarding") {
			return "onboarding"
		}
	}
	if sawPage {
		return "app"
	}
	return ""
}

// classifyLoginPageText 从官方登录页的可见文本里识别「网络不通」这一类故障，
// 返回一句可诊断的中文提示；识别不出返回空串。
//
// 隔离成纯函数是为了可测：真实故障文本只出现在用户机器上，但判定规则必须
// 在仓库里被钉住。
//
// 措辞纪律：只陈述官方页面自己报了什么，并把排查方向指向用户的网络/代理。
// 排查方向结合本次选择的网络模式；不把上游故障掩盖成「请重试」。
func classifyLoginPageText(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "proxyconnect") || strings.Contains(lower, "err_proxy_connection_failed"):
		return "官方窗口报告代理连接失败（proxyconnect）。请检查代理是否运行，或选择直连重试。"
	case strings.Contains(lower, "err_connection_refused"):
		return "官方窗口报告连接被拒绝（ERR_CONNECTION_REFUSED）：目标地址没有服务在监听，通常是代理端口填错或代理已退出。"
	case strings.Contains(lower, "err_name_not_resolved"):
		return "官方窗口报告域名解析失败（ERR_NAME_NOT_RESOLVED）：当前网络无法解析 Google 的域名，请检查 DNS 或代理。"
	case strings.Contains(lower, "err_internet_disconnected"):
		return "官方窗口报告本机网络已断开（ERR_INTERNET_DISCONNECTED）。"
	case strings.Contains(lower, "err_tunnel_connection_failed"):
		return "官方窗口报告无法建立到代理的隧道（ERR_TUNNEL_CONNECTION_FAILED），请检查代理是否放行 Google 域名。"
	case strings.Contains(lower, "err_connection_timed_out") || strings.Contains(lower, "err_timed_out"):
		return "官方窗口报告连接超时：当前网络到 Google 的连接被阻断或极慢，请检查代理是否可用。"
	case strings.Contains(lower, "eof") &&
		(strings.Contains(lower, "oauth2.googleapis.com/token") ||
			strings.Contains(lower, "loadcodeassist") ||
			strings.Contains(lower, "cloudcode-pa.googleapis.com")):
		// EOF（而不是 dial/tls 错误）= 连接**已经建立**、请求发出去了，然后在
		// 半途被对端或中间设备单方面切断。这跟「连不上」是不同的故障：直连
		// 同一个端点通常能正常应答，所以它指向代理/中间网络在丢连接，而不是
		// Google 侧不可达。因此只如实转述，并建议直连或检查代理。
		return "官方窗口报告与 Google 的连接被中途切断（EOF）。请尝试直连或更换代理后重试。"
	case strings.Contains(lower, "connection reset") || strings.Contains(lower, "err_connection_reset"):
		return "官方窗口报告连接被重置（connection reset）。请尝试直连或更换代理。"
	case strings.Contains(lower, "oauth2.googleapis.com/token"):
		return "官方窗口报告 token endpoint 错误（oauth2.googleapis.com/token）。请检查网络后重新登录。"
	case strings.Contains(lower, "there was an unexpected issue setting up your account"):
		return "官方窗口报告「设置账号时发生意外问题」，页面上的原始报错见官方窗口本身。"
	}
	return ""
}

// officialLoginPageDiagnostic 只读读取官方登录页的可见文本并给出诊断提示。
//
// 返回 "" 表示「拿不到 / 没有可识别的故障」——调用方据此保持原有提示，
// 绝不会因为读不到页面就谎报故障。
//
// **只读**：只在登录流程进行中调用，只发一次 Runtime.evaluate 取 innerText，
// 不注入任何东西，不建立常驻会话。这与官方形态探针
// （official_probe.go 的「只发 Runtime.evaluate，且表达式里只有查询」）同一纪律。
func officialLoginPageDiagnostic() string {
	port, _, err := officialDevToolsActivePort()
	if err != nil || port <= 0 || !portListening(port) {
		return ""
	}
	if native := readNativeAuthAt(CDPAddrForPort(port)); native.Available && native.Failure != "" {
		brokerMutate(func(s *LoginBrokerStatus) { s.NativeAuth = &native })
		return "原生 Antigravity 登录服务报告（" + native.Failure + "）：" + native.Message + "。来源：LanguageServer/GetAuthStatus；2Ag 未执行地区判断。"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var targets []struct {
		Type              string `json:"type"`
		URL               string `json:"url"`
		WebSocketDebugger string `json:"webSocketDebuggerUrl"`
	}
	if json.Unmarshal(body, &targets) != nil {
		return ""
	}
	// 登录期间优先看 /onboarding 页；没有就退到第一个可用的 page。
	wsURL := ""
	for _, t := range targets {
		if t.Type != "page" || t.WebSocketDebugger == "" {
			continue
		}
		if strings.Contains(t.URL, "/onboarding") {
			wsURL = t.WebSocketDebugger
			break
		}
		if wsURL == "" {
			wsURL = t.WebSocketDebugger
		}
	}
	if wsURL == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, reader, err := openCDPWebSocket(ctx, wsURL)
	if err != nil {
		return ""
	}
	defer conn.Close()
	text, err := evaluateCDPString(conn, reader, 1,
		"document.body && document.body.innerText ? document.body.innerText.slice(0, 4000) : ''")
	if err != nil {
		return ""
	}
	return classifyLoginPageText(text)
}

// runLoginBroker 是流程本体。
func runLoginBroker(configuredMode string, network loginNetwork) {
	// 流程期间把进程内形态钉成「官方」：2Ag 的注入链路（HotReload / WatchAndInjectCDP /
	// PushHubState）统一以 IsOfficialRuntime() 为闸门，钉住它才能保证
	// 「添加账号期间 2Ag 绝不往官方宿主里打补丁」。
	//
	// 收尾必须**显式**解除，绝不能只靠 defer：
	// defer 会一直挂到函数返回，而「恢复原账号 → 按原形态重启宿主」正好发生在
	// 函数末尾。ShapePin 没解开时 restartHostInMode("enhanced", …) → LaunchEnhancedHost
	// 会读到 IsOfficialRuntime()==true，于是把「增强形态」重定向成再拉起一个官方实例 ——
	// 界面说增强、跑着官方，正是本功能最不能出的错。（真机复现过。）
	wasOfficial := IsOfficialRuntime()
	SetRuntimeMode(RuntimeModeOfficialValue)
	defer func() {
		// 兜底：panic 或任何提前 return 的路径都要把形态放回去。
		SetRuntimeMode(restoreRuntimeModeValue(wasOfficial))
	}()

	defer func() {
		if r := recover(); r != nil {
			_, err := restoreBrokerRecovery(func() error {
				if stopAllAntigravityProcesses("异常恢复") != 0 {
					return fmt.Errorf("宿主未停止")
				}
				return nil
			}, writeAntigravityCredentialRaw, readAntigravityCredentialRaw)
			if err != nil {
				brokerFinish(brokerStageFailed, "原账号恢复失败，请重启 2Ag 重试恢复", "恢复原账号失败")
			} else {
				brokerFinish(brokerStageFailed, "流程内部错误，原凭据已恢复", "内部错误")
			}
		}
	}()

	origRaw, err := readAntigravityCredentialRaw()
	if err != nil {
		brokerFinish(brokerStageFailed, "读取当前凭据失败，未做任何改动", "读取当前凭据失败: "+err.Error())
		return
	}
	origEmail := ""
	if v, e := ReadHostLoginEmail(); e == nil {
		origEmail = strings.TrimSpace(v)
	}
	prior := RuntimeModeFactsNow(configuredMode)
	hadHost := prior.Effective != "none"
	brokerMutate(func(s *LoginBrokerStatus) {
		s.OriginalEmail = origEmail
		s.OriginalMode = prior.Effective
	})

	// ① 备份当前凭据。内存里的 origRaw 是回滚来源，同时归档进 DPAPI 保险库 ——
	// 这样即使 2Ag 自己在流程中途被杀掉，用户的原始凭据也还在保险库里，不会只剩
	// 「已被删除」这一种可能。
	brokerStage(brokerStageBackingUp, "正在备份当前登录凭据…")
	if origEmail != "" && len(origRaw) > 0 {
		if _, err := StoreVaultCredential(origEmail, displayNameFromCredentialPayload(origRaw), origRaw); err != nil {
			// 备份失败就绝不往下走：后面要删掉这条凭据，没有备份的删除等于把
			// 用户唯一一份 refresh_token 置于险境。
			brokerFinish(brokerStageFailed, "备份当前凭据失败，出于安全考虑已中止（未做任何改动）",
				"备份当前凭据到保险库失败: "+err.Error())
			return
		}
		brokerStage(brokerStageBackingUp, "已备份当前凭据 "+origEmail+" 到 DPAPI 保险库")
	} else {
		if len(origRaw) > 0 {
			brokerFinish(brokerStageFailed, "无法识别当前凭据账号，请先在官方客户端确认登录", "identity mismatch：无法识别当前凭据")
			return
		}
		brokerStage(brokerStageBackingUp, "当前没有登录凭据，无需备份")
	}
	if err := saveBrokerRecovery(origRaw, origEmail); err != nil {
		brokerFinish(brokerStageFailed, "保存恢复记录失败，当前登录态未改动", "保存恢复记录失败")
		return
	}

	// ② 停掉所有 Antigravity 与 ③ 临时删除当前凭据
	brokerStage(brokerStageStopping, "正在停止运行中的 Antigravity…")
	if stopAllAntigravityProcesses("准备官方登录") != 0 {
		brokerFinish(brokerStageFailed, "官方客户端仍在运行，已中止；重启 2Ag 恢复", "宿主停止失败")
		return
	}
	if err := deleteAntigravityCredentialRaw(); err != nil {
		brokerRollbackAndFinish("删除当前凭据失败", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	if raw, _ := readAntigravityCredentialRaw(); len(raw) > 0 {
		brokerRollbackAndFinish("当前凭据未被清空", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	brokerStage(brokerStageStopping, "已清空当前登录凭据（原凭据已备份，流程结束会恢复）")

	// ④ 以本次网络模式启动官方 Antigravity，仅开放只读诊断端口
	brokerStage(brokerStageLaunching, "正在启动官方 Antigravity（登录网络："+network.Mode+"）…")
	if err := launchOfficialLoginHost(network); err != nil {
		brokerRollbackAndFinish("启动官方 Antigravity 失败: "+err.Error(), origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	brokerStage(brokerStageLaunching, "官方 Antigravity 已启动：请在它的窗口里点击「Continue with Google」")

	// ⑤ 轮询：直到凭据**成为一个完整登录结果**
	deadline := time.Now().Add(brokerWaitTimeout)
	launchedAt := time.Now()
	var goneSince time.Time
	var lastDiagnosticAt time.Time
	lastHint := ""
	lastDiagnostic := ""
	addedEmail := ""
	for {
		if brokerCancelled() {
			brokerRollbackAndFinish("已取消（原账号会恢复）", origRaw, origEmail, prior, hadHost, wasOfficial)
			return
		}
		if time.Now().After(deadline) {
			brokerRollbackAndFinish("等待登录超时（10 分钟）", origRaw, origEmail, prior, hadHost, wasOfficial)
			return
		}

		raw, err := readAntigravityCredentialRaw()
		if err == nil && len(raw) > 0 {
			if email, ok := credentialUsableEmail(raw); ok {
				port, _, _ := officialDevToolsActivePort()
				native := readNativeAuthAt(CDPAddrForPort(port))
				brokerMutate(func(s *LoginBrokerStatus) { s.NativeAuth = &native })
				if nativeCredentialReady(raw, native, email) {
					addedEmail = email
					break
				}
				if native.Failure != "" {
					brokerStage(brokerStageWaiting, "原生 Antigravity 拒绝登录（"+native.Failure+"）："+native.Message)
				}
				time.Sleep(time.Second)
				continue
			}
			// 中间态（官方先写 {"token":null}）。这里**绝不入库**，
			// 只是如实告诉用户「看到了但还不算完成」。
			if lastHint != "placeholder" {
				lastHint = "placeholder"
				brokerStage(brokerStageWaiting, "已检测到中间态凭据（尚未完成登录），继续等待…")
			}
		} else {
			switch officialLoginPageState() {
			case "onboarding":
				// 先看官方页面上有没有它自己报出的网络故障。官方登录页会把
				// 「proxyconnect tcp: dial tcp 127.0.0.1:1」这类原始错误直接显示
				// 在页面上，而 2Ag 原先完全看不见 —— 用户只能等到 10 分钟超时，
				// 拿不到「是网络/代理不通」这个结论。这里把官方自己的报错读出来
				// 转述给用户；读不到就保持原提示，绝不凭空断言故障。
				//
				// 每 5 秒探一次，且启动后先给 15 秒宽限：诊断要开一个 CDP
				// 连接，正常登录不该被打扰，只有「停着不动」才值得去看页面。
				diagnostic := lastDiagnostic
				if time.Since(launchedAt) >= 15*time.Second && time.Since(lastDiagnosticAt) >= 5*time.Second {
					lastDiagnosticAt = time.Now()
					diagnostic = officialLoginPageDiagnostic()
					lastDiagnostic = diagnostic
				}
				if diagnostic != "" {
					if lastHint != "network:"+diagnostic {
						lastHint = "network:" + diagnostic
						brokerStage(brokerStageWaiting, diagnostic)
					}
					break
				}
				if lastHint != "onboarding" {
					lastHint = "onboarding"
					brokerStage(brokerStageWaiting, "等待 Google 登录：请在官方 Antigravity 窗口点击「Continue with Google」")
				}
			case "app":
				if lastHint != "app" {
					lastHint = "app"
					brokerStage(brokerStageWaiting, "官方窗口已离开登录页，正在确认凭据…")
				}
			default:
				if lastHint != "nolink" {
					lastHint = "nolink"
					brokerStage(brokerStageWaiting, "正在等待官方 Antigravity 打开登录页…")
				}
			}
		}

		// 官方窗口被用户关掉了：不再有无穷等待，如实失败。
		if len(scanAntigravityProcesses()) == 0 {
			if goneSince.IsZero() {
				goneSince = time.Now()
			} else if time.Since(goneSince) > 8*time.Second {
				brokerRollbackAndFinish("官方 Antigravity 已退出，登录未完成", origRaw, origEmail, prior, hadHost, wasOfficial)
				return
			}
		} else {
			goneSince = time.Time{}
		}
		time.Sleep(1 * time.Second)
	}

	// ⑥ 保存到 DPAPI 保险库
	brokerStage(brokerStageDetected, "检测到账号 "+addedEmail)
	brokerMutate(func(s *LoginBrokerStatus) { s.AccountEmail = addedEmail })
	brokerStage(brokerStageSaving, "正在把账号 "+addedEmail+" 加密写入保险库…")
	rawNow, err := readAntigravityCredentialRaw()
	if err != nil || len(rawNow) == 0 {
		brokerRollbackAndFinish("重新读取凭据失败，未归档", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	if owner, ok := credentialUsableEmail(rawNow); !ok || !strings.EqualFold(owner, addedEmail) {
		brokerRollbackAndFinish("identity mismatch：登录凭据归属发生变化，未归档", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	port, _, _ := officialDevToolsActivePort()
	if !nativeCredentialReady(rawNow, readNativeAuthAt(CDPAddrForPort(port)), addedEmail) {
		brokerRollbackAndFinish("归档前原生登录身份未确认，未归档", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	if _, err := StoreVaultCredential(addedEmail, displayNameFromCredentialPayload(rawNow), rawNow); err != nil {
		brokerRollbackAndFinish("写入保险库失败: "+err.Error(), origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	if err := SaveOwnedAccountEntry(addedEmail, displayNameFromCredentialPayload(rawNow)); err != nil {
		brokerRollbackAndFinish("账号已存入保险库，但账号登记失败", origRaw, origEmail, prior, hadHost, wasOfficial)
		return
	}
	brokerMutate(func(s *LoginBrokerStatus) { s.VaultSaved = true })
	brokerStage(brokerStageSaving, "账号 "+addedEmail+" 已加密保存到保险库（index.json 只写元数据）")

	// ⑦ 恢复原账号（原本没有账号则保留新账号）
	restored, _, restoreErr := brokerRestore(origRaw, origEmail, prior, hadHost, wasOfficial, true)
	if restoreErr != nil {
		brokerFinish(brokerStageFailed,
			"账号 "+addedEmail+" 已保存，但恢复未完成，请重启 2Ag",
			restoreErr.Error())
		return
	}

	msg := "账号 " + addedEmail + " 已添加"
	if restored {
		msg += "；已恢复原账号 " + origEmail
	} else {
		msg += "，并已成为当前账号"
	}
	if !hadHost {
		msg += "；流程开始时没有宿主在运行，未自动启动宿主"
	}
	brokerFinish(brokerStageDone, msg, "")
}

// restoreRuntimeModeValue 把「是否官方」翻译回 SetRuntimeMode 需要的形态值。
func restoreRuntimeModeValue(wasOfficial bool) string {
	if wasOfficial {
		return RuntimeModeOfficialValue
	}
	return "enhanced"
}

// Only a successful first login may retain a new account when originally signed
// out. Failure/cancellation restores signed out, including after process loss.
func brokerRestore(origRaw []byte, origEmail string, prior RuntimeModeFacts, hadHost bool, wasOfficial bool, keepNew bool) (bool, string, error) {
	// ★ 进入恢复阶段前先解除形态钉住。
	//
	// 这一步必须在**重启宿主之前**做：恢复阶段要按「流程开始前的形态」重启宿主，
	// 而 LaunchEnhancedHost 会先问 IsOfficialRuntime()；钉住没解开的话，
	// 「增强形态」会被静默重定向成再拉起一个官方实例（真机复现过）。
	// 这里恢复的是「流程开始前那个进程内形态」，不是 prior.Effective ——
	// 后者是「当时机器上跑着什么」，可能是 none，不能当形态值用。
	SetRuntimeMode(restoreRuntimeModeValue(wasOfficial))

	brokerStage(brokerStageRestoring, "正在恢复原账号…")
	if stopAllAntigravityProcesses("登录结束，准备恢复原账号") != 0 {
		return false, "", fmt.Errorf("宿主停止失败，恢复记录已保留")
	}

	restored := false
	if origEmail != "" && len(origRaw) > 0 {
		if err := writeAntigravityCredentialRaw(origRaw); err != nil {
			return false, "", fmt.Errorf("写回原凭据失败，恢复记录已保留: %w", err)
		}
		back, err := readAntigravityCredentialRaw()
		if err != nil || !bytes.Equal(back, origRaw) {
			return false, "", fmt.Errorf("原凭据回读不一致，恢复记录已保留")
		}
		restored = true
		SetActiveAccount(origEmail)
		SetPersistedActiveAccount(origEmail)
	} else if !keepNew {
		if err := deleteAntigravityCredentialRaw(); err != nil {
			return false, "", err
		}
		if err := ClearActiveAccount(); err != nil {
			return false, "", err
		}
		back, err := readAntigravityCredentialRaw()
		if err != nil || len(back) != 0 {
			return false, "", fmt.Errorf("退出登录状态未恢复，恢复记录已保留")
		}
	}

	currentEmail := ""
	if got, err := ReadHostLoginEmail(); err != nil {
		return false, "", err
	} else {
		currentEmail = strings.TrimSpace(got)
	}

	if hadHost && (len(origRaw) == 0 || restored) {
		target := currentEmail
		if target == "" {
			target = origEmail
		}
		if err := restartHostInMode(prior.Effective, target); err != nil {
			return false, currentEmail, fmt.Errorf("凭据已恢复，但宿主重启失败: %w", err)
		} else {
			brokerMutate(func(s *LoginBrokerStatus) { s.HostRestarted = true })
			if currentEmail != "" {
				if _, err := waitForNativeAccount(currentEmail, 20*time.Second); err != nil {
					return false, currentEmail, fmt.Errorf("凭据已恢复，但原生登录尚未确认: %w", err)
				}
			}
		}
	}
	if err := clearBrokerRecovery(); err != nil {
		return false, currentEmail, fmt.Errorf("恢复完成，但恢复记录清理失败: %w", err)
	}
	brokerMutate(func(s *LoginBrokerStatus) { s.Restored = restored; s.CurrentEmail = currentEmail })
	brokerStage(brokerStageRestoring, "已恢复登录状态"+orNoAccount(currentEmail))
	return restored, currentEmail, nil
}

// brokerRollbackAndFinish 是失败/取消路径的统一出口：先把机器恢复原状，再如实收尾。
func brokerRollbackAndFinish(reason string, origRaw []byte, origEmail string, prior RuntimeModeFacts, hadHost bool, wasOfficial bool) {
	restored, currentEmail, restoreErr := brokerRestore(origRaw, origEmail, prior, hadHost, wasOfficial, false)
	detail := reason
	switch {
	case origEmail == "":
		detail += "；流程开始时没有登录账号，当前身份：" + orNoAccount(currentEmail)
	case restored:
		detail += "；原账号 " + origEmail + " 已恢复"
	default:
		detail += "；原账号 " + origEmail + " **未能确认恢复**（当前身份：" + orNoAccount(currentEmail) + "）"
	}
	stage := brokerStageFailed
	if brokerCancelled() && restoreErr == nil {
		stage = brokerStageCancelled
	}
	if restoreErr != nil {
		reason = "恢复未完成，请重启 2Ag 重试恢复"
		detail += "；" + restoreErr.Error()
	}
	brokerFinish(stage, reason, detail)
}

func orNoAccount(v string) string {
	if strings.TrimSpace(v) == "" {
		return "无（凭据管理器里没有 gemini:antigravity）"
	}
	return v
}

// restartHostInMode 按指定形态重启宿主（停 → 起），供切换与恢复共用。
func restartHostInMode(mode, accountEmail string, workspacePaths ...string) error {
	if mode == RuntimeModeOfficialValue {
		return LaunchOfficialHost("")
	}
	if strings.TrimSpace(accountEmail) != "" {
		SetActiveAccount(accountEmail)
	}
	return launchEnhancedHost("", accountEmail, false, workspacePaths...)
}

// SwitchAccountTransactional 事务化切换登录账号：停宿主 → 写凭据 → 启动 → 校验 → 提交/回滚。
//
// 为什么不「运行中直接改凭据」：宿主只在进程启动时读取凭据，运行期间还会自己刷新并
// 回写。在它活着的时候改，等于让两个写入者抢同一条记录 —— 结果既不确定，也无法校验。
// 事务化的价值全在最后那步校验上：**验证失败就回滚，并且把回滚结果也验证一遍**，
// 于是「切换失败」不再等于「用户不知道自己在哪个账号上」。
func SwitchAccountTransactional(email, configuredMode string, options ...AccountSwitchOptions) (result AccountSwitchResult, resultErr error) {
	release, err := lockCredentialOperation()
	if err != nil {
		return AccountSwitchResult{}, err
	}
	defer release()
	transactionStage("BackingUp", strings.TrimSpace(email))
	defer func() {
		if resultErr != nil {
			transactionStage("Failed", "")
		} else {
			transactionStage("Committed", "")
		}
	}()
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		return AccountSwitchResult{}, fmt.Errorf("请重启 2Ag 先恢复原账号")
	}
	var out AccountSwitchResult
	out.Email = strings.TrimSpace(email)
	if out.Email == "" {
		return out, fmt.Errorf("切换账号需要明确的目标邮箱")
	}
	var option AccountSwitchOptions
	if len(options) > 0 {
		option = options[0]
	}

	prevRaw, err := readAntigravityCredentialRaw()
	if err != nil {
		return out, fmt.Errorf("读取原凭据失败，未切换: %w", err)
	}
	out.PreviousOwner = emailFromCredentialPayload(prevRaw)
	if len(prevRaw) > 0 && out.PreviousOwner == "" {
		return out, fmt.Errorf("identity mismatch：无法识别原凭据，未切换")
	}
	if option.ExpectedOwner != "" && !strings.EqualFold(option.ExpectedOwner, out.PreviousOwner) {
		return out, fmt.Errorf("当前账号已改变，本次接力已取消")
	}
	prior := RuntimeModeFactsNow(configuredMode)
	out.Mode = prior.Effective
	hadHost := prior.Effective != "none"
	if hadHost && prior.Effective != prior.Configured {
		return out, fmt.Errorf("运行形态仍待切换，请先完成形态切换后再换号；未停止宿主")
	}
	if option.ExpectedOwner != "" && prior.Effective != "enhanced" {
		return out, fmt.Errorf("自动接力仅支持运行中的增强宿主")
	}
	if option.ExpectedOwner != "" {
		for _, process := range scanAntigravityProcesses() {
			if !IsFrozenHostPath(process.Exe) {
				return out, fmt.Errorf("其他 Antigravity 实例正在运行，自动接力已暂缓")
			}
		}
	}
	sameOwner := strings.EqualFold(out.PreviousOwner, out.Email)
	if sameOwner && hadHost {
		transactionStage("VerifyingNativeAuth", "")
		fresh, err := waitForNativeAccount(out.Email, 5*time.Second)
		if err != nil {
			return out, fmt.Errorf("当前凭据属于目标账号，但原生登录未确认；现有宿主未停止: %w", err)
		}
		if _, err := StoreVaultCredential(out.Email, displayNameFromCredentialPayload(fresh), fresh); err != nil {
			return out, fmt.Errorf("当前登录有效，但保存最新登录失败: %w", err)
		}
		out.AlreadyActive, out.Verified = true, true
		out.VerifiedOwner = out.PreviousOwner
		out.Message = "当前已是目标账号，原生身份已核验，保留现有宿主和最新凭据"
		return out, nil
	}
	// With no host, the same email still needs a native bootstrap and verification.
	// Use its current refreshed blob rather than overwriting it with an older archive.
	target := prevRaw
	if !sameOwner {
		target, err = ReadVaultCredential(out.Email)
		if err != nil {
			return out, err
		}
	}
	if owner, ok := credentialRestorableEmail(target); !ok || !strings.EqualFold(owner, out.Email) {
		return out, fmt.Errorf("目标账号凭据不完整、已失效或归属不一致，未切换")
	}
	if option.WorkspacePath != "" {
		info, err := os.Stat(option.WorkspacePath)
		if err != nil || !info.IsDir() {
			return out, fmt.Errorf("接力工作目录不可用，未切换")
		}
	}

	// 切换是破坏性的：当前凭据里可能有宿主刚刷新过的 refresh_token，
	// 那是全世界唯一的一份。先归档再动手。
	if out.PreviousOwner != "" && len(prevRaw) > 0 {
		if _, err := StoreVaultCredential(out.PreviousOwner, displayNameFromCredentialPayload(prevRaw), prevRaw); err != nil {
			return out, fmt.Errorf("备份原凭据失败，未切换: %w", err)
		}
	}
	if option.BeforeStop != nil {
		if err := option.BeforeStop(); err != nil {
			return out, err
		}
	}
	rollback := func(reason string) (AccountSwitchResult, error) {
		out.Verified = false
		out.RolledBack = true
		transactionStage("RollingBack", "")
		ok, _ := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost, option.WorkspacePath)
		out.RollbackVerified = ok
		out.Message = reason
		if ok {
			out.VerifiedOwner = out.PreviousOwner
			SetActiveAccount(out.PreviousOwner)
			SetPersistedActiveAccount(out.PreviousOwner)
			if err := clearBrokerRecovery(); err != nil {
				out.Message += "；原账号已恢复，但恢复记录清理失败，请重启 2Ag"
			} else {
				out.Message += "；原账号及宿主已恢复"
			}
		} else {
			out.Message += "；恢复未完成，请重启 2Ag 读取恢复记录"
		}
		return out, fmt.Errorf("%s", out.Message)
	}

	// Persist before stopping: process loss or partial stop must remain recoverable.
	if err := saveBrokerRecovery(prevRaw, out.PreviousOwner); err != nil {
		return out, fmt.Errorf("无法保存切号恢复记录，未停止宿主: %w", err)
	}
	transactionStage("StoppingHost", "")
	if stopAllAntigravityProcesses("账号切换：先停宿主") != 0 {
		return rollback("宿主未能完全停止，未写入目标凭据")
	}
	transactionStage("SavingCurrentCredential", "")
	// Stop may flush a refreshed token. Preserve the final blob.
	if latest, err := readAntigravityCredentialRaw(); err != nil {
		return rollback("停止后读取原凭据失败")
	} else if !strings.EqualFold(emailFromCredentialPayload(latest), out.PreviousOwner) {
		return rollback("停止后账号归属发生变化")
	} else {
		prevRaw = latest
	}
	if out.PreviousOwner != "" {
		if _, err := StoreVaultCredential(out.PreviousOwner, displayNameFromCredentialPayload(prevRaw), prevRaw); err != nil {
			return rollback("保存原账号最新凭据失败")
		}
	}
	if err := updateBrokerRecovery(prevRaw, out.PreviousOwner); err != nil {
		return rollback("更新切号恢复记录失败")
	}
	transactionStage("WritingTargetCredential", "")
	if err := writeAntigravityCredentialRaw(target); err != nil {
		return rollback("写入目标凭据失败")
	}
	log.Printf("[2ag] 账号切换：已写入目标凭据 %s（%d B）", out.Email, len(target))

	mode := prior.Effective
	if mode == "" || mode == "none" {
		mode = configuredMode
	}
	if strings.TrimSpace(mode) == "" {
		mode = "enhanced"
	}
	transactionStage("StartingHost", "")
	if err := restartHostInMode(mode, out.Email, option.WorkspacePath); err != nil {
		// 启动失败也算切换失败：宿主没起来就无从校验，必须回滚。
		return rollback("启动目标宿主失败：" + err.Error())
	}
	out.HostRestarted = true

	transactionStage("VerifyingNativeAuth", "")
	fresh, authErr := waitForNativeAccount(out.Email, 25*time.Second)
	if authErr != nil {
		return rollback(authErr.Error())
	}
	got := emailFromCredentialPayload(fresh)
	out.VerifiedOwner = got
	out.Verified = strings.EqualFold(got, out.Email)
	if out.Verified {
		if _, err := StoreVaultCredential(out.Email, displayNameFromCredentialPayload(fresh), fresh); err != nil {
			return rollback("保存原生刷新后的登录失败")
		}
		SetActiveAccount(out.Email)
		if mode != RuntimeModeOfficialValue {
			SetPersistedActiveAccount(out.Email)
		}
		out.Message = "已切换到 " + out.Email + "（宿主已按「" + modeLabel(mode) + "」重启，实际登录身份已核对一致）"
		if err := clearBrokerRecovery(); err != nil {
			return rollback("清理切号恢复记录失败")
		}
		return out, nil
	}

	// 回滚：写回旧凭据 → 重启 → 再校验一次
	return rollback("目标登录身份核对失败：" + orNoAccount(got))
}

// switchRollback 写回旧凭据并按原形态重启，然后**再校验一次**回滚结果。
func switchRollback(prevRaw []byte, prevEmail string, prior RuntimeModeFacts, hadHost bool, workspacePaths ...string) (bool, string) {
	if stopAllAntigravityProcesses("账号切换回滚：先停宿主") != 0 {
		return false, ""
	}
	if prevEmail == "" || len(prevRaw) == 0 {
		if err := deleteAntigravityCredentialRaw(); err != nil {
			log.Printf("[2ag] 账号切换回滚：清空凭据失败: %v", err)
			return false, ""
		}
	} else if err := writeAntigravityCredentialRaw(prevRaw); err != nil {
		log.Printf("[2ag] 账号切换回滚：写回旧凭据失败: %v", err)
		return false, ""
	}
	got, err := readAntigravityCredentialRaw()
	if err != nil || !bytes.Equal(got, prevRaw) {
		return false, ""
	}
	if prevEmail == "" {
		if err := ClearActiveAccount(); err != nil {
			return false, ""
		}
	} else {
		SetActiveAccount(prevEmail)
	}
	if hadHost {
		mode := prior.Effective
		if mode == "" || mode == "none" {
			mode = "enhanced"
		}
		if err := restartHostInMode(mode, prevEmail, workspacePaths...); err != nil {
			log.Printf("[2ag] 账号切换回滚：重启宿主失败: %v", err)
			return false, prevEmail
		}
		if prevEmail != "" {
			transactionStage("RollbackVerifying", "")
			if _, err := waitForNativeAccount(prevEmail, 20*time.Second); err != nil {
				log.Printf("[2ag] 原凭据已恢复，但原生登录尚未确认: %v", err)
				return false, prevEmail
			}
		}
	}
	owner := waitForHostLoginEmail(prevEmail, 8)
	ok := strings.EqualFold(owner, prevEmail)
	return ok, owner
}

// waitForHostLoginEmail 轮询等待宿主登录身份变成期望值（读的是凭据管理器的真实事实）。
func waitForHostLoginEmail(want string, tries int) string {
	got := ""
	for i := 0; i < tries; i++ {
		invalidateHostLoginCache()
		if v, err := ReadHostLoginEmail(); err == nil {
			got = strings.TrimSpace(v)
			if want == "" || strings.EqualFold(got, want) {
				return got
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return got
}
