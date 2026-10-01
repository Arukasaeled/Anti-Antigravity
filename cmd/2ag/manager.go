package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/2ag/2ag/internal/api"
	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/supervisor"
	"github.com/2ag/2ag/web"
	"github.com/jchv/go-webview2"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	user32                    = syscall.NewLazyDLL("user32.dll")
	sendMessage               = user32.NewProc("SendMessageW")
	loadIcon                  = user32.NewProc("LoadImageW")
	getSystemMetrics          = user32.NewProc("GetSystemMetrics")
	setWindowPos              = user32.NewProc("SetWindowPos")
	isWindow                  = user32.NewProc("IsWindow")
	isWindowVisible           = user32.NewProc("IsWindowVisible")
	getDpiForWindow           = user32.NewProc("GetDpiForWindow")
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	imageIcon      = 1
	lrLoadFromFile = 0x0010
)

// Manager 主视窗的尺寸规约。
//
// DPI 关键点：cmd/2ag/2ag.exe.manifest 声明了 dpiAwareness=PerMonitorV2，
// 因此 Win32 的 CreateWindowExW / SetWindowPos 全部按「物理像素」解释尺寸，
// 而 WebView2 内部渲染视口与 DOM 的 CSS 像素之间按缩放比换算。
// 在 150% 缩放的屏幕上，"宽 1280 物理像素" 只等于约 853 CSS 像素，
// 于是前端布局被横向吞掉 —— 这正是「必须手动拉大窗口才能看全」的根因。
// 因此所有目标尺寸都以 CSS 像素（逻辑像素）定义，再乘以窗口所在显示器的 DPI 缩放
// 换算成物理像素后交给 Win32，保证 100% / 125% / 150% 下得到等量的内容视口。
const (
	managerWidthCSS     = 1240
	managerHeightCSS    = 820
	managerMinWidthCSS  = 1024
	managerMinHeightCSS = 680
)

// hostPushDebounce 是「状态变更 → 增量推送」的去抖窗口。
//
// 比 hostReloadDebounce 短一个数量级，因为它要做的事完全不同：
// 增量推送不重连、不重跑补丁，只是往已建立的常驻 CDP 会话上发一条
// Runtime.evaluate。它不需要等 schedulePersistLocked 的 300ms 落盘 —— 推的是
// 手里的 state（就是用户刚改的那个值），磁盘上的旧值与此无关。
//
// 唯一要防的是「一次拖动打几十条 CDP」：前端 100ms 粒度的事件流在这里收束成
// 一次推送，用户松手后约 100ms 内就能看到宿主变化。
//
// 注：这条路径确实会读盘一次（PushHubState 内部要重建跳转自愈注册，
// 需要补丁源码与壁纸），但那是几百 KB 的顺序读，与「重建 CDP 连接 + 在宿主里
// 重跑整个补丁」不在一个量级上 —— 用户感知的延迟来自后者。
const hostPushDebounce = 120 * time.Millisecond

// hostReloadDebounce 是「兜底整段重注入」的去抖窗口。
//
// 只在增量推送无法落地时才会走到这里（补丁未挂上 / 宿主实例是旧版不认识新字段 /
// 补丁源码本身变了）。因此它的窗口可以放得很宽 —— 这条路径应该是罕见的。
// 仍保留两个下界的历史理由：
//  1. Manager 前端的滑块以 100ms 粒度连续 POST（index.html 的 updateVal），一次拖动
//     会产生几十个状态事件；逐个转录成 CDP 重注入会让宿主在拖动期间被反复打断。
//  2. HotReloadCDP 内部走的是 BuildHubConfig() —— 它是**重新读盘**，而状态机把这次
//     变更写进 2ag.json 要等 schedulePersistLocked 的 300ms 定时器。若热重载早于落盘，
//     注入进去的就是上一次的旧值，表现为「改了但没生效」。取 700ms 给落盘留出余量。
const hostReloadDebounce = 700 * time.Millisecond

// applyHostVisibleChange 把一次「宿主外观配置变更」落到正在运行的宿主上。
//
// 顺序是刻意的：先试增量推送（快路径，毫秒级），推不动才整段重注入（慢路径，秒级）。
// 反过来就是历史实现的样子 —— 每次改滑块都要把整个补丁重跑一遍，用户看到的就是那一秒延迟。
func applyHostVisibleChange(cfg config.Config) {
	result, err := supervisor.PushHubState(cfg)
	if err == nil {
		log.Printf("[2ag] 状态变更已增量生效: %s", result.Message)
		if !result.NeedFullReload {
			return
		}
		// 推送落地了，但宿主上的补丁实例没能消化这次壁纸（多半是旧版实例）。
		// 继续往下走，用整段重注入把新源码带进去。
	} else {
		// 没有常驻会话（补丁还没挂上）或会话已失效 —— 这恰恰是最需要整段重注入的情形，
		// 因为它会重新建立会话并注册跳转自愈契约。
		log.Printf("[2ag] 增量推送未落地（%v），回落到整段重注入", err)
	}

	report, err := supervisor.HotReloadCDP(supervisor.ResolveCDPAddr())
	if err != nil {
		log.Printf("[2ag] 状态变更热生效失败: %v", err)
		return
	}
	// 重注入已经把新补丁实例连同 INITIAL_CONFIG 一起送上去了，推送侧的壁纸记账随之作废。
	supervisor.ResetPushedWallpaperHint()
	log.Printf("[2ag] 状态变更已热生效: %s", report.Message)
}

// hostVisibleSignature 把「宿主外观上看得见的配置」压成一个可比较的字符串。
//
// 只纳入真正会改变宿主渲染结果的字段：壁纸、三个透明度/模糊数值、语言、
// 以及九项 GravityBoost。刻意排除 Network / Privacy / Plugins / EnvOverrides /
// GlobalRules —— 它们要么由别处各自的热路径处理，要么需要重启宿主才有效，
// 纳进来只会让「改了隐私名单」这种与外观无关的操作触发一次无意义的重注入。
func hostVisibleSignature(cfg config.Config) string {
	return fmt.Sprintf("wp=%s|blur=%d|op=%.4f|modal=%.4f|lang=%s|boost=%+v",
		cfg.WallpaperPath, cfg.Blur, cfg.Opacity, cfg.ModalOpacity, cfg.Language, cfg.GravityBoost)
}

// dpiScaleForWindow 返回窗口所在显示器的缩放比（1.0 = 96 DPI）。
// 取不到时回退 1.0，保证非 DPI 环境下行为不变。
func dpiScaleForWindow(hwnd uintptr) float64 {
	if hwnd == 0 || getDpiForWindow.Find() != nil {
		return 1.0
	}
	dpi, _, _ := getDpiForWindow.Call(hwnd)
	if dpi < 96 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

// primaryDPIScale 在主视窗尚未创建、拿不到 HWND 时，用系统 DPI 估算主显示器缩放比。
func primaryDPIScale() float64 {
	getDpiForSystem := user32.NewProc("GetDpiForSystem")
	if getDpiForSystem.Find() != nil {
		return 1.0
	}
	dpi, _, _ := getDpiForSystem.Call()
	if dpi < 96 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

// physicalSize 把 CSS 像素尺寸换算成当前窗口 DPI 下的物理像素尺寸。
func physicalSize(cssWidth, cssHeight int, scale float64) (int, int) {
	w := int(float64(cssWidth)*scale + 0.5)
	h := int(float64(cssHeight)*scale + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// primaryPhysicalWidth / primaryPhysicalHeight：创建窗口时的初始物理像素尺寸。
// 上限钳制到主显示器分辨率，避免在低分辨率屏上创建出比屏幕还大的窗口。
func primaryPhysicalWidth() int {
	w, h := physicalSize(managerWidthCSS, managerHeightCSS, primaryDPIScale())
	if screenWidth, _, _ := getSystemMetrics.Call(0); screenWidth > 0 && w > int(screenWidth) {
		w = int(screenWidth)
	}
	_ = h
	return w
}

func primaryPhysicalHeight() int {
	w, h := physicalSize(managerWidthCSS, managerHeightCSS, primaryDPIScale())
	if screenHeight, _, _ := getSystemMetrics.Call(1); screenHeight > 0 && h > int(screenHeight) {
		h = int(screenHeight)
	}
	_ = w
	return h
}

func setWindowIcon(hwnd uintptr, iconPath string) {
	if hwnd == 0 {
		return
	}
	absPath, err := filepath.Abs(iconPath)
	if err == nil {
		iconPath = absPath
	}
	pathPtr, err := syscall.UTF16PtrFromString(iconPath)
	if err != nil {
		return
	}
	hIconSmall, _, _ := loadIcon.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 16, 16, lrLoadFromFile)
	hIconBig, _, _ := loadIcon.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 32, 32, lrLoadFromFile)
	if hIconSmall != 0 {
		sendMessage.Call(hwnd, wmSetIcon, iconSmall, hIconSmall)
	}
	if hIconBig != 0 {
		sendMessage.Call(hwnd, wmSetIcon, iconBig, hIconBig)
	}
}

// cleanupWebViewLocks 清理因异常退出残留的 WebView2 文件锁
func cleanupWebViewLocks(dataDir string) {
	_ = os.Remove(filepath.Join(dataDir, "EBWebView", "SingletonLock"))
	_ = os.Remove(filepath.Join(dataDir, "EBWebView", "SingletonCookie"))
	_ = os.Remove(filepath.Join(dataDir, "EBWebView", "SingletonSocket"))
}

// createWebViewSafely 安全创建 WebView2 视窗并具备自动熔断与锁重试自愈能力
func createWebViewSafely(dataDir string) webview2.WebView {
	cleanupWebViewLocks(dataDir)

	var w webview2.WebView
	safeCreate := func(attempt int) bool {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[2ag] WebView2 初始化捕获到异常 (第 %d 次尝试): %v", attempt, r)
				w = nil
			}
		}()
		w = webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:     true,
			AutoFocus: true,
			DataPath:  dataDir,
			WindowOptions: webview2.WindowOptions{
				Title: "2Ag Manager - Anti-Antigravity Control Center",
				// 这里传给 CreateWindowExW 的值已经是「物理像素」（go-webview2 不做 DPI 换算）。
				// 先用系统 DPI 缩放把 1240x820 CSS 像素折算成物理像素，避免高缩放屏上视口被压缩。
				Width:  uint(primaryPhysicalWidth()),
				Height: uint(primaryPhysicalHeight()),
				Center: true,
			},
		})
		return w != nil
	}

	if safeCreate(1) {
		return w
	}

	// 首次失败或异常，执行锁文件抹除后微重试
	log.Printf("[2ag] WebView2 首次初始化受阻，执行环境净化与锁释放后重试...")
	cleanupWebViewLocks(dataDir)
	time.Sleep(200 * time.Millisecond)

	if safeCreate(2) {
		return w
	}

	return nil
}

// listenManagerAPI 按 28470 → 28471 → 动态回环端口的顺序绑定 Manager API 监听器。
//
// 刻意跳过 28472：那是 CDP 的历史默认端口（supervisor.DefaultCDPPort），
// 保留给 Antigravity 的远程调试，避免 API 与调试端口互抢。
// 抽成函数是因为 CLI 模式（runCommand）与图形模式（startManager）必须用同一套策略 ——
// 历史实现里 CLI 硬绑 :28472，与宿主 CDP 默认端口完全重叠。
func listenManagerAPI() (net.Listener, int, error) {
	for _, port := range []int{28470, 28471} {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return l, port, nil
		}
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("bind manager API listener: %w", err)
	}
	return l, l.Addr().(*net.TCPAddr).Port, nil
}

func startManager(configPath string, cfg config.Config) error {
	// 1. 单实例互斥锁检测：若已有 Manager 在运行，则激活已有视窗并安全退出当前重复实例，杜绝冲突一闪而过
	createMutex := kernel32.NewProc("CreateMutexW")
	findWindow := user32.NewProc("FindWindowW")
	showWindow := user32.NewProc("ShowWindow")
	setForegroundWindow := user32.NewProc("SetForegroundWindow")

	var hMutex uintptr
	if createMutex.Find() == nil {
		mutexName, _ := syscall.UTF16PtrFromString("Local\\2Ag_Manager_SingleInstance_Mutex")
		h, _, callErr := createMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
		hMutex = h
		if hMutex != 0 {
			const ERROR_ALREADY_EXISTS = syscall.Errno(183)
			const SW_RESTORE = 9
			if errno, ok := callErr.(syscall.Errno); ok && errno == ERROR_ALREADY_EXISTS {
				log.Println("[2ag] 检测到 2Ag Manager 互斥锁已存在，正在探测并唤醒已有视窗...")
				// 轮询探测已有视窗，最多等待 1.2 秒（给先前启动的实例充足时间展示界面）
				var hwnd uintptr
				for attempt := 0; attempt < 4; attempt++ {
					title1, _ := syscall.UTF16PtrFromString("2Ag Manager - Anti-Antigravity Control Center")
					hwnd, _, _ = findWindow.Call(0, uintptr(unsafe.Pointer(title1)))
					if hwnd == 0 {
						title2, _ := syscall.UTF16PtrFromString("2Ag Manager")
						hwnd, _, _ = findWindow.Call(0, uintptr(unsafe.Pointer(title2)))
					}
					if hwnd != 0 {
						break
					}
					time.Sleep(300 * time.Millisecond)
				}
				if hwnd != 0 {
					log.Printf("[2ag] 成功找到已有 Manager 视窗 (HWND: 0x%X)，执行置前激活...", hwnd)
					showWindow.Call(hwnd, uintptr(SW_RESTORE))
					setForegroundWindow.Call(hwnd)
					syscall.CloseHandle(syscall.Handle(hMutex))
					return nil
				}
				log.Println("[2ag] 虽有互斥锁，但未发现活动视窗，后台旧进程可能僵死，强行接管以拉起主视窗！")
			}
		}
	}
	if hMutex != 0 {
		defer syscall.CloseHandle(syscall.Handle(hMutex))
	}

	bus := core.NewEventBus()
	sm := core.NewStateMachine(cfg, configPath, bus)
	apiServer := api.NewServer(sm, bus)

	// 把配置里的形态登记进进程级状态。这一步必须在任何注入/启动动作之前：
	// LaunchEnhancedHost、WatchAndInjectCDP（启动 600ms 后就会跑）、PushHubState
	// 三处闸门都只问 supervisor.IsOfficialRuntime()，它们没有耐心先去读一次磁盘。
	// 漏掉这一行的后果是「配置写了官方形态，机器上照样注入」—— 本轮最不能出的错。
	supervisor.SetRuntimeMode(cfg.RuntimeMode)

	// Account Vault 的一次性兼容迁移：0.1.1 之前的 ~/.2ag/vault/*.bin 是**明文**
	// 写的（里面是含 refresh_token 的完整凭据）。升级到本版本后第一次启动就把它们
	// 就地加密为 DPAPI(CurrentUser) 密文，并把 index.json 升级到 v2。
	// 放在启动路径上而不是懒加载：一个只读的界面（用户从不点切换）也应该把
	// 明文凭据收掉。失败不阻断启动，只如实记日志 —— 迁移失败不该让 Manager 打不开。
	if migrated, err := supervisor.MigrateLegacyVault(); err != nil {
		log.Printf("[2ag] 账号保险库迁移未完成: %v", err)
	} else if migrated > 0 {
		log.Printf("[2ag] 账号保险库已升级：%d 份明文凭据已加密为 DPAPI(CurrentUser) 密文", migrated)
	}

	// 形态切换的落地：切换运行形态是一次**结构性**变更，不能只改一个字段然后
	// 继续拿旧形态的宿主干活 —— 那样界面显示 OFFICIAL CLEAN，跑着的却仍是带补丁的
	// 增强宿主，正是本轮要根除的「界面与事实不符」。
	// 因此切换时重启宿主到新形态。这会打断用户正在编辑的工作台，但那是用户点下
	// 这个开关时明确表达的意图；相比之下，让开关看起来生效实则没有，代价更大。
	runtimeModeCh := make(chan string, 4)
	defer close(runtimeModeCh)
	go func() {
		for mode := range runtimeModeCh {
			supervisor.SetRuntimeMode(mode)
			log.Printf("[2ag] 运行形态已切换为 %s，正在把宿主重启到该形态...", mode)
			if err := supervisor.RestartHostClient(""); err != nil {
				log.Printf("[2ag] 形态切换后重启宿主失败: %v", err)
			}
		}
	}()

	// Listen for CommandEvents
	cmdCh := bus.Subscribe(core.CommandEvent)
	defer bus.Unsubscribe(core.CommandEvent, cmdCh)
	go func() {
		for event := range cmdCh {
			if action, ok := event.Payload.(core.StateAction); ok {
				switch action.Type {
				case core.HostCtrlAction:
					if payload, ok := action.Payload.(map[string]any); ok {
						cmd, _ := payload["cmd"].(string)
						switch cmd {
						case "start":
							_ = supervisor.LaunchEnhancedHost("", "")
						case "restart":
							_ = supervisor.RestartEnhancedHost("", "")
						case "stop":
							_ = supervisor.StopHostClient()
						}
					}
				case core.HotReloadAction:
					// 走真实的热重载通道：读磁盘最新 injected_hub.js → 经 CDP 重新注入。
					// 历史上这里只靠 WatchAndInjectCDP 重新注入编译期快照，日志永远"成功"，
					// 磁盘改动却一个字都进不去。现在把真实结果写进日志，失败不再静默。
					go func() {
						report, err := supervisor.HotReloadCDP(supervisor.ResolveCDPAddr())
						if err != nil {
							log.Printf("[2ag] 热重载失败: %v (来源: %s)", err, report.Source)
							return
						}
						log.Printf("[2ag] %s", report.Message)
					}()
				}
			}
		}
	}()

	// 状态变更热生效：Manager 上改的每一项「宿主看得见的东西」都必须真投射到宿主上。
	//
	// 历史实现只比较 GravityBoost 一个字段（其余变更直接 continue）。后果是：用户在
	// Manager 拖模糊/暗度/模态滑块、点主题预设卡、改壁纸之后，配置确实落盘了，宿主却
	// 一动不动 —— 只有再手动点一次「热重载补丁」才追得上。这是「假功能」的第三种形态：
	// 不是开关没消费方，而是消费方永远收不到通知（宿主侧的模糊始终是上次注入时的旧值）。
	//
	// 现在改比较一份「宿主可见外观」签名，任何一项变了都触发一次真实热重载。
	// HotReloadCDP 内部会重新执行 BuildHubConfig() —— 也就是重新读盘，所以它拿到的
	// 必然是最新配置，这里不需要（也不应该）手工把值传过去。
	// 拖动滑杆时 Manager 前端以 100ms 粒度连续 POST（见 index.html 的 updateVal），
	// 因此加一道去抖，让一次连续拖动只打一次 CDP。
	stateCh := bus.Subscribe(core.StateChangedEvent)
	defer bus.Unsubscribe(core.StateChangedEvent, stateCh)
	go func() {
		lastSig := ""
		lastMode := cfg.RuntimeMode
		var debounce *time.Timer
		for event := range stateCh {
			newState, ok := event.Payload.(config.Config)
			if !ok {
				continue
			}

			// 运行形态单独一条通道。它刻意不进 hostVisibleSignature：
			// 那个签名的语义是「宿主看得见的外观」，形态变化照它比较会被当成
			// 一次普通的滑块拖动，走增量推送 —— 而形态切换要的是重启。
			if newState.RuntimeMode != lastMode {
				lastMode = newState.RuntimeMode
				select {
				case runtimeModeCh <- newState.RuntimeMode:
				default:
					log.Printf("[2ag] 形态切换请求队列已满，已丢弃一次（%s）", newState.RuntimeMode)
				}
			}

			sig := hostVisibleSignature(newState)
			// 去重只看「和上一次相比变了没有」。
			//
			// 历史实现这里还有一个 initialized 首事件基线分支（首个事件只记录不处理，
			// 理由写的是「它可能是启动探针或与本次操作无关的变更」）。那个理由不成立：
			// StateChangedEvent 全仓只有一处发端（core.state.go 的 ApplyAction 末尾，
			// 且仅在 changed 为真时），启动过程不会自行发布任何事件 —— 换句话说，
			// 被那条基线吞掉的只可能是**用户新进程启动后的第一次真实修改**。
			//
			// 代价是实测出来的：拉起 2ag 后第一次拖滑块，状态机与磁盘都变了，
			// 日志里却连一条推送记录都没有，宿主纹丝不动；第二次拖才生效。
			// 这类「第一次不灵、第二次才对」的缺陷最容易被当成偶发而放过。
			//
			// 删掉它是安全的：lastSig 以空串起步，而 hostVisibleSignature 必定返回
			// 非空（形如 "wp=...|blur=..."），因此首个事件必然被处理；
			// 而即便某天真出现了与配置无关的重复事件，sig 相等那一条也会拦住它。
			if sig == lastSig {
				continue
			}
			lastSig = sig
			if debounce != nil {
				debounce.Stop()
			}
			// 去抖回调闭包捕获当前这一份状态，而不是去读外层变量。
			//
			// 理由有两条：
			//   1) 正确性：外层变量会被事件循环继续写入、又被定时器协程读取，
			//      而 Stop() 并不保证"已经跑起来的回调"被排除，Config 结构体赋值
			//      也不是原子的 —— 那就是一次真实的数据竞争。
			//   2) 语义：每个 sig 变化都会顶掉上一个定时器，因此最后一个被创建的
			//      定时器持有的必然就是用户最终停下的那个值，捕获传参天然得到同一结果。
			//      即便某次旧回调恰好已经在执行（值 A）而新回调随后执行（值 B），
			//      两者都推给宿主后收敛于 B，结果仍是用户最终选择。
			snapshot := newState
			debounce = time.AfterFunc(hostPushDebounce, func() {
				applyHostVisibleChange(snapshot)
			})
		}
	}()

	// Map both root and /dist/ to static assets
	apiServer.HandleStatic("/dist/", http.FS(web.Assets))
	apiServer.HandleStatic("/", http.FS(web.Assets))

	// Bind API Server: try 28470, then 28471, then dynamic loopback port,
	// deliberately reserving 28472 for Antigravity's CDP remote debugging!
	listener, managerPort, err := listenManagerAPI()
	if err != nil {
		log.Fatalf("API Server failed to bind listener: %v", err)
	}
	_ = managerPort

	go func() {
		if err := apiServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("API Server stopped: %v", err)
		}
	}()
	defer apiServer.Stop()

	// Port Readiness Check
	ready := false
	for i := 0; i < 20; i++ {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", managerPort), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		log.Fatalf("Local API server failed to bind port :%d within timeout", managerPort)
	}

	// 启动时自动探测已有 Antigravity 宿主实例（CDP 端口经 DevToolsActivePort 动态解析），若已在线则立即激活并启动长效保活
	go func() {
		time.Sleep(600 * time.Millisecond)
		_ = supervisor.WatchAndInjectCDP(supervisor.ResolveCDPAddr(), 3*time.Second)
	}()

	managerDataDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "2Ag", "manager_webview2")
	_ = os.MkdirAll(managerDataDir, 0755)

	log.Println("[2ag-step] 4. 开始调用 createWebViewSafely...")
	w := createWebViewSafely(managerDataDir)
	if w == nil {
		log.Fatal("[2ag-step] 致命错误: Failed to initialize WebView2. Please ensure WebView2 Runtime is installed.")
	}
	defer w.Destroy()

	w.SetTitle("2Ag Manager - Anti-Antigravity Control Center")
	// HintNone = 可自由缩放；尺寸按当前 DPI 折算物理像素（见 managerWidthCSS 注释）
	w.SetSize(primaryPhysicalWidth(), primaryPhysicalHeight(), webview2.HintNone)

	hwnd := uintptr(w.Window())
	r0, _, _ := isWindow.Call(hwnd)
	v0, _, _ := isWindowVisible.Call(hwnd)
	log.Printf("[2ag] WebView2 视窗初始状态 (HWND: 0x%X, IsWindow: %v, Visible: %v)", hwnd, r0 != 0, v0 != 0)

	// 强制按 DPI 感知的物理尺寸居中，覆盖系统历史小尺寸缓存
	if hwnd != 0 {
		scale := dpiScaleForWindow(hwnd)
		winW, winH := physicalSize(managerWidthCSS, managerHeightCSS, scale)
		screenWidth, _, _ := getSystemMetrics.Call(0)
		screenHeight, _, _ := getSystemMetrics.Call(1)
		if screenWidth > 0 && screenHeight > 0 {
			if winW > int(screenWidth) {
				winW = int(screenWidth)
			}
			if winH > int(screenHeight) {
				winH = int(screenHeight)
			}
			x := uintptr((int(screenWidth) - winW) / 2)
			y := uintptr((int(screenHeight) - winH) / 2)
			setWindowPos.Call(hwnd, 0, x, y, uintptr(winW), uintptr(winH), 0x0040|0x0020)
		}

		// 最小尺寸护栏：1024x680 CSS 像素（同样按 DPI 折算）。
		// go-webview2 的 WindowOptions 没有 minWidth/minHeight 字段，
		// 只能靠 Win32 子类化拦 WM_GETMINMAXINFO 来兜住用户拖拽缩放的下限。
		installMinSizeGuard(hwnd)
		// 把视窗句柄交给 supervisor：壁纸的「选择图片」对话框要用它做 owner，
		// 才能成为该视窗的模态子窗口。没有 owner 时对话框可能被 WebView2
		// 视窗压住，用户看到的现象是「点了按钮什么都没发生」。
		supervisor.SetDialogOwnerWindow(hwnd)
		log.Printf("[2ag] Manager 视窗尺寸: %dx%d CSS -> %dx%d 物理 (缩放 %.0f%%), 最小 %dx%d CSS",
			managerWidthCSS, managerHeightCSS, winW, winH, scale*100, managerMinWidthCSS, managerMinHeightCSS)
	}
	r1, _, _ := isWindow.Call(hwnd)
	log.Printf("[2ag] SetWindowPos 后 (IsWindow: %v)", r1 != 0)

	darkMode := int32(1)
	// DWMWA_USE_IMMERSIVE_DARK_MODE (19 on Win10 1809-1909, 20 on Win10 20H1+ and Win11)
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&darkMode)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&darkMode)), 4)

	// DWMWA_CAPTION_COLOR (35) and DWMWA_TEXT_COLOR (36) on Windows 11
	// 0x00BBGGRR：标题栏跟随 2Ag design token 的 --2ag-surface-base (#0e0f11)、
	// 文字跟随 --2ag-text-primary (#e8eaed)。这两个值必须与 web\dist\index.html
	// 的 :root token 保持一致，否则窗口边框和页面背景会出现一条可见的色差缝。
	captionColor := uint32(0x00110F0E)
	textColor := uint32(0x00EDEAE8)
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&captionColor)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 36, uintptr(unsafe.Pointer(&textColor)), 4)

	setWindowIcon(hwnd, "assets/icon.ico")

	targetURL := fmt.Sprintf("http://127.0.0.1:%d/", managerPort)
	log.Printf("[2ag] 正在导航至 Manager 控制台: %s", targetURL)
	w.Navigate(targetURL)

	log.Println("[2ag] Manager 视窗已展示，进入主消息循环...")
	w.Run()
	log.Println("[2ag] Manager 视窗已关闭，安全退出进程。")
	// os.Exit 不执行 defer，所以 CDP 长驻组件（持久会话保活、补丁保活巡检）
	// 必须在这里显式收束；否则它们会随着进程退出被硬切断，留下半开的
	// WebSocket 与未清理的 goroutine。
	supervisor.CloseAllCDPSessions()
	os.Exit(0)
	return nil
}
