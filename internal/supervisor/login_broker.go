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
	"strings"
	"sync"
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
	if err := RequireManagedHosts(); err != nil {
		return err
	}
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

// Refuse external instances before stopping any owned/adopted host. Every
// broker, recovery and rollback call shares this ownership gate.
func stopAllAntigravityProcesses(reason string) int {
	if err := stopManagedHosts(); err != nil {
		log.Printf("[2ag] %s: %v", reason, err)
		remaining := len(scanAntigravityProcesses())
		if remaining == 0 {
			return 1
		}
		return remaining
	}
	return 0
}

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
			if email, ok := BrokerFreshCredentialValidation(raw); ok {
				addedEmail = email
				break
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
	if owner, ok := BrokerFreshCredentialValidation(rawNow); !ok || !strings.EqualFold(owner, addedEmail) {
		brokerRollbackAndFinish("identity mismatch：登录凭据归属发生变化，未归档", origRaw, origEmail, prior, hadHost, wasOfficial)
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
	restored, currentEmail := brokerRestore(origRaw, origEmail, prior, hadHost, wasOfficial)
	if !restored && origEmail != "" && len(origRaw) > 0 {
		brokerFinish(brokerStageFailed,
			"账号 "+addedEmail+" 已保存到保险库，但恢复原账号 "+origEmail+" 失败",
			"恢复原凭据失败：系统凭据归属为 "+currentEmail)
		return
	}
	if hadHost && !LoginBrokerStatusNow().HostRestarted {
		brokerFinish(brokerStageFailed, "账号已归档，但原宿主未恢复运行，请查看流程记录", "宿主恢复启动失败")
		return
	}

	msg := "账号 " + addedEmail + " 已添加"
	if restored {
		msg += "；已恢复原凭据 " + origEmail
	} else {
		msg += "；系统凭据已保留，宿主内部身份未确认"
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

// 返回 (restored, currentEmail)：restored 表示「原本那个账号确实回来了」；
// 原本没有账号时 restored 为 false，currentEmail 即新账号（这是期望结果，不是失败）。
func brokerRestore(origRaw []byte, origEmail string, prior RuntimeModeFacts, hadHost bool, wasOfficial bool) (bool, string) {
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
		return false, ""
	}

	restored := false
	if origEmail != "" && len(origRaw) > 0 {
		if err := writeAntigravityCredentialRaw(origRaw); err != nil {
			log.Printf("[2ag] Login Broker：写回原凭据失败: %v", err)
		} else if got, _ := ReadHostLoginEmail(); strings.EqualFold(strings.TrimSpace(got), origEmail) {
			back, readErr := readAntigravityCredentialRaw()
			if readErr != nil || !bytes.Equal(back, origRaw) {
				return false, got
			}
			restored = true
			if err := clearBrokerRecovery(); err != nil {
				restored = false
			}
			brokerStage(brokerStageRestoring, "已恢复原凭据 "+origEmail)
		} else {
			log.Printf("[2ag] Login Broker：原凭据写回后校验不一致（期望 %s，实际 %s）", origEmail, strings.TrimSpace(got))
		}
	}

	currentEmail := ""
	if got, err := ReadHostLoginEmail(); err == nil {
		currentEmail = strings.TrimSpace(got)
	}
	brokerMutate(func(s *LoginBrokerStatus) {
		s.Restored = restored
		s.CurrentEmail = currentEmail
	})

	if hadHost && (len(origRaw) == 0 || restored) {
		target := currentEmail
		if target == "" {
			target = origEmail
		}
		if err := restartHostInMode(prior.Effective, target); err != nil {
			log.Printf("[2ag] Login Broker：重启宿主失败（形态 %s）: %v", prior.Effective, err)
			brokerStage(brokerStageRestoring, "宿主重启失败："+err.Error()+"（系统凭据已恢复）")
		} else {
			brokerMutate(func(s *LoginBrokerStatus) { s.HostRestarted = true })
			brokerStage(brokerStageRestoring, "已按「"+modeLabel(prior.Effective)+"」重启宿主")
		}
	}
	return restored, currentEmail
}

// brokerRollbackAndFinish 是失败/取消路径的统一出口：先把机器恢复原状，再如实收尾。
func brokerRollbackAndFinish(reason string, origRaw []byte, origEmail string, prior RuntimeModeFacts, hadHost bool, wasOfficial bool) {
	restored, currentEmail := brokerRestore(origRaw, origEmail, prior, hadHost, wasOfficial)
	detail := reason
	switch {
	case origEmail == "":
		detail += "；流程开始时没有系统凭据，当前凭据归属：" + orNoAccount(currentEmail)
	case restored:
		detail += "；原凭据 " + origEmail + " 已恢复（宿主内部身份未确认）"
	default:
		detail += "；原凭据 " + origEmail + " **未能确认恢复**（当前凭据归属：" + orNoAccount(currentEmail) + "）"
	}
	stage := brokerStageFailed
	if brokerCancelled() && (origEmail == "" || restored) && (!hadHost || LoginBrokerStatusNow().HostRestarted) {
		stage = brokerStageCancelled
	}
	if origEmail != "" && !restored {
		reason = "恢复原账号失败，请重启 2Ag 重试恢复"
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
func restartHostInMode(mode, accountEmail string) error {
	return launchHostInMode(mode, "", accountEmail)
}

// SwitchAccountTransactional 事务化切换登录账号：停宿主 → 写凭据 → 启动 → 校验 → 提交/回滚。
//
// 为什么不「运行中直接改凭据」：宿主只在进程启动时读取凭据，运行期间还会自己刷新并
// 回写。在它活着的时候改，等于让两个写入者抢同一条记录 —— 结果既不确定，也无法校验。
// 事务化的价值全在最后那步校验上：**验证失败就回滚，并且把回滚结果也验证一遍**，
// 于是「切换失败」不再等于「用户不知道自己在哪个账号上」。
func SwitchAccountTransactional(email, configuredMode string) (AccountSwitchResult, error) {
	release, err := lockCredentialOperation()
	if err != nil {
		return AccountSwitchResult{}, err
	}
	defer release()
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		return AccountSwitchResult{}, fmt.Errorf("请重启 2Ag 先恢复原账号")
	}
	var out AccountSwitchResult
	out.Email = strings.TrimSpace(email)
	if err := ValidateLaunchAccount(out.Email); err != nil {
		return out, err
	}
	if err := RequireManagedHosts(); err != nil {
		return out, err
	}
	if out.Email == "" {
		return out, fmt.Errorf("切换账号需要明确的目标邮箱")
	}

	target, err := ReadVaultCredential(out.Email)
	if err != nil {
		return out, err
	}
	credential, err := StoredCredentialValidation(target, out.Email)
	if err != nil {
		return out, err
	}
	out.CredentialState, out.CredentialRecovery = credential.State, credential.Recovery

	prevRaw, err := readAntigravityCredentialRaw()
	if err != nil {
		return out, fmt.Errorf("读取原凭据失败，未切换: %w", err)
	}
	if prev, _ := ReadHostLoginEmail(); strings.TrimSpace(prev) != "" {
		out.PreviousOwner = strings.TrimSpace(prev)
	}
	if len(prevRaw) > 0 && out.PreviousOwner == "" {
		return out, fmt.Errorf("identity mismatch：无法识别原凭据，未切换")
	}
	prior := RuntimeModeFactsNow(configuredMode)
	out.Mode = configuredMode
	hadHost := prior.Effective != "none"

	// 切换是破坏性的：当前凭据里可能有宿主刚刷新过的 refresh_token，
	// 那是全世界唯一的一份。先归档再动手。
	if out.PreviousOwner != "" && len(prevRaw) > 0 {
		if _, err := StoreVaultCredential(out.PreviousOwner, displayNameFromCredentialPayload(prevRaw), prevRaw); err != nil {
			return out, fmt.Errorf("备份原凭据失败，未切换: %w", err)
		}
	}

	if stopAllAntigravityProcesses("账号切换：先停宿主") != 0 {
		return out, fmt.Errorf("宿主仍在运行，未写入目标凭据")
	}
	if err := writeAntigravityCredentialRaw(target); err != nil {
		ok, owner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
		out.RolledBack, out.RollbackCredentialVerified = ok, ok
		out.Message = fmt.Sprintf("写入失败: %v；原凭据恢复: %t（%s）", err, ok, owner)
		return out, fmt.Errorf("%s", out.Message)
	}
	log.Printf("[2ag] 账号切换：已写入目标凭据 %s（%d B）", out.Email, len(target))
	// Verify the written material before starting, with the same stored policy.
	// Expiration must not turn a successful credential write into a rollback.
	back, verifyErr := readAntigravityCredentialRaw()
	if verifyErr == nil {
		credential, verifyErr = StoredCredentialValidation(back, out.Email)
	}
	if verifyErr != nil {
		ok, owner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
		out.RolledBack, out.RollbackCredentialVerified = ok, ok
		out.Message = fmt.Sprintf("目标凭据回读失败；原凭据恢复: %t（%s）", ok, owner)
		return out, fmt.Errorf("%s: %w", out.Message, verifyErr)
	}
	out.CredentialState, out.CredentialRecovery = credential.State, credential.Recovery

	mode := configuredMode
	if strings.TrimSpace(mode) == "" {
		mode = "enhanced"
	}
	if err := restartHostInMode(mode, out.Email); err != nil {
		// 启动失败也算切换失败：宿主没起来就无从校验，必须回滚。
		out.RolledBack = true
		rollbackOK, rollbackOwner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
		out.RollbackCredentialVerified = rollbackOK
		out.RolledBack = rollbackOK
		out.Message = "启动宿主失败：" + err.Error()
		if rollbackOK {
			out.Message += "；已恢复原凭据到 " + rollbackOwner
		} else {
			out.Message += "；恢复原账号失败，请从保险库恢复"
		}
		return out, fmt.Errorf("切换账号 %s 失败；%s: %w", out.Email, out.Message, err)
	}
	out.HostRestarted = true
	got := waitForHostLoginEmail(out.Email, 8)
	out.CredentialOwner = got
	out.CredentialVerified = strings.EqualFold(got, out.Email)
	if out.CredentialVerified {
		if raw, readErr := readAntigravityCredentialRaw(); readErr == nil {
			if current, err := StoredCredentialValidation(raw, out.Email); err == nil {
				out.CredentialState, out.CredentialRecovery = current.State, current.Recovery
			}
		}
		if err := SetSelectedAccount(out.Email); err != nil {
			ok, owner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
			out.RolledBack, out.RollbackCredentialVerified = ok, ok
			out.Message = fmt.Sprintf("账号选择保存失败: %v；原凭据恢复: %t（%s）", err, ok, owner)
			return out, fmt.Errorf("%s: %w", out.Message, err)
		}
		// Pin Manager's switch to this process before reading native login status.
		// A credential write or a matching profile name is not identity evidence.
		host := processIdentity(GetManagedHostPID())
		if err := authorizeManagerHostAccount(host, out.Email); err != nil {
			out.IdentityStatus, out.Message = "unavailable", err.Error()
			return out, &IdentityUnverifiedError{Message: out.Message}
		}
		identity := waitForManagerHostIdentity(host, out.Email)
		out.IdentityStatus = identity.Status
		out.VerifiedOwner = GetActiveAccountEmail()
		if identity.Verified && strings.EqualFold(out.VerifiedOwner, out.Email) {
			out.Verified = true
			out.IdentityStatus = "confirmed"
			out.Message = "宿主内部身份已确认，账号切换完成"
			return out, nil
		}
		out.Message = "目标凭据已应用，宿主已启动；宿主内部登录身份尚未确认，未提交 active account"
		if identity.Status == "account_mismatch" {
			ok, owner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
			out.RolledBack, out.RollbackCredentialVerified = ok, ok
			out.Message = fmt.Sprintf("原生宿主登录账号与目标不一致；原凭据恢复: %t（%s）", ok, owner)
			return out, fmt.Errorf("%s", out.Message)
		}
		if out.CredentialRecovery != "" {
			out.Message += "；短期 token 待宿主恢复，尚未确认刷新成功"
		}
		return out, &IdentityUnverifiedError{Message: out.Message}
	}

	// 回滚：写回旧凭据 → 重启 → 再校验一次
	out.RolledBack = true
	rollbackOK, rollbackOwner := switchRollback(prevRaw, out.PreviousOwner, prior, hadHost)
	out.RollbackCredentialVerified = rollbackOK
	out.RolledBack = rollbackOK
	out.Message = "切换失败：系统凭据归属是 " + orNoAccount(got) + "（目标 " + out.Email + "）"
	if rollbackOK {
		out.Message += "；已恢复原凭据到 " + rollbackOwner
	} else {
		out.Message += "；恢复原账号失败，请从保险库恢复"
	}
	code := CredentialOwnerMismatch
	if strings.TrimSpace(got) == "" {
		code = CredentialOwnerMissing
	}
	errOut := credentialError(code, out.Message)
	return out, errOut
}

// switchRollback 写回旧凭据并按原形态重启，然后**再校验一次**回滚结果。
func switchRollback(prevRaw []byte, prevEmail string, prior RuntimeModeFacts, hadHost bool) (bool, string) {
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
	if hadHost {
		mode := prior.Effective
		if mode == "" || mode == "none" {
			mode = "enhanced"
		}
		if err := restartHostInMode(mode, prevEmail); err != nil {
			log.Printf("[2ag] 账号切换回滚：重启宿主失败: %v", err)
			return false, prevEmail
		}
	}
	owner := waitForHostLoginEmail(prevEmail, 8)
	ok := prevEmail != "" && strings.EqualFold(owner, prevEmail)
	return ok, owner
}

// waitForHostLoginEmail reads shared credential ownership only, never host-internal identity.
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
