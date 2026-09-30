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
				Title:  "2Ag Manager - Anti-Antigravity Control Center",
				Width:  1280,
				Height: 840,
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
					go supervisor.WatchAndInjectCDP(supervisor.ResolveCDPAddr(), 10*time.Second)
				}
			}
		}
	}()

	// Map both root and /dist/ to static assets
	apiServer.HandleStatic("/dist/", http.FS(web.Assets))
	apiServer.HandleStatic("/", http.FS(web.Assets))

	// Bind API Server: try 28470, then 28471, then dynamic loopback port,
	// deliberately reserving 28472 for Antigravity's CDP remote debugging!
	var listener net.Listener
	var managerPort int
	for _, port := range []int{28470, 28471} {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			listener = l
			managerPort = port
			break
		}
	}
	if listener == nil {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatalf("API Server failed to bind listener: %v", err)
		}
		listener = l
		managerPort = listener.Addr().(*net.TCPAddr).Port
	}

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
	w.SetSize(1280, 840, webview2.HintNone)

	hwnd := uintptr(w.Window())
	r0, _, _ := isWindow.Call(hwnd)
	v0, _, _ := isWindowVisible.Call(hwnd)
	log.Printf("[2ag] WebView2 视窗初始状态 (HWND: 0x%X, IsWindow: %v, Visible: %v)", hwnd, r0 != 0, v0 != 0)

	// 强制 1280x840 居中，覆盖系统历史小尺寸缓存
	if hwnd != 0 {
		screenWidth, _, _ := getSystemMetrics.Call(0)
		screenHeight, _, _ := getSystemMetrics.Call(1)
		if screenWidth > 0 && screenHeight > 0 {
			x := uintptr((int(screenWidth) - 1280) / 2)
			y := uintptr((int(screenHeight) - 840) / 2)
			setWindowPos.Call(hwnd, 0, x, y, 1280, 840, 0x0040|0x0020)
		}
	}
	r1, _, _ := isWindow.Call(hwnd)
	log.Printf("[2ag] SetWindowPos 后 (IsWindow: %v)", r1 != 0)

	darkMode := int32(1)
	// DWMWA_USE_IMMERSIVE_DARK_MODE (19 on Win10 1809-1909, 20 on Win10 20H1+ and Win11)
	procDwmSetWindowAttribute.Call(hwnd, 19, uintptr(unsafe.Pointer(&darkMode)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&darkMode)), 4)

	// DWMWA_CAPTION_COLOR (35) and DWMWA_TEXT_COLOR (36) on Windows 11
	captionColor := uint32(0x00141313) // 0x00BBGGRR for Google dark #131314
	textColor := uint32(0x00E3E3E3)
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&captionColor)), 4)
	procDwmSetWindowAttribute.Call(hwnd, 36, uintptr(unsafe.Pointer(&textColor)), 4)

	setWindowIcon(hwnd, "assets/icon.ico")

	targetURL := fmt.Sprintf("http://127.0.0.1:%d/", managerPort)
	log.Printf("[2ag] 正在导航至 Manager 控制台: %s", targetURL)
	w.Navigate(targetURL)

	log.Println("[2ag] Manager 视窗已展示，进入主消息循环...")
	w.Run()
	log.Println("[2ag] Manager 视窗已关闭，安全退出进程。")
	os.Exit(0)
	return nil
}
