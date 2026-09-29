package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"syscall"
	"time"
	"unsafe"

	"github.com/2ag/2ag/internal/api"
	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/web"
	"github.com/jchv/go-webview2"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	sendMessage               = user32.NewProc("SendMessageW")
	loadIcon                  = user32.NewProc("LoadImageW")
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
	pathPtr, err := syscall.UTF16PtrFromString(iconPath)
	if err != nil {
		return
	}
	hIcon, _, _ := loadIcon.Call(0, uintptr(unsafe.Pointer(pathPtr)), imageIcon, 0, 0, lrLoadFromFile)
	if hIcon != 0 {
		sendMessage.Call(hwnd, wmSetIcon, iconSmall, hIcon)
		sendMessage.Call(hwnd, wmSetIcon, iconBig, hIcon)
	}
}

func startManager(configPath string, cfg config.Config) error {
	bus := core.NewEventBus()
	sm := core.NewStateMachine(cfg, configPath, bus)
	apiServer := api.NewServer(sm, bus)

	// Map both root and /dist/ to static assets
	apiServer.HandleStatic("/dist/", http.FS(web.Assets))
	apiServer.HandleStatic("/", http.FS(web.Assets))

	go func() {
		if err := apiServer.Start(":28472"); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("API Server failed: %v", err)
		}
	}()
	defer apiServer.Stop()

	// Port Readiness Check
	ready := false
	for i := 0; i < 20; i++ {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:28472", 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		log.Fatal("Local API server failed to bind port :28472 within timeout")
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "2Ag Manager",
			Width:  1240,
			Height: 800,
		},
	})
	if w == nil {
		log.Fatal("Failed to initialize WebView2. Please ensure WebView2 Runtime is installed.")
	}
	defer w.Destroy()

	w.SetTitle("2Ag Manager - Anti-Antigravity Control Center")
	w.SetSize(1240, 800, webview2.HintNone)

	hwnd := w.Window()
	darkMode := int32(1)
	procDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(20), // DWMWA_USE_IMMERSIVE_DARK_MODE
		uintptr(unsafe.Pointer(&darkMode)),
		uintptr(unsafe.Sizeof(darkMode)),
	)

	setWindowIcon(uintptr(hwnd), "assets/icon.ico")

	w.Navigate("http://127.0.0.1:28472/dist/index.html")
	w.Run()
	return nil
}
