//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/2ag/2ag/internal/api"
	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/control"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/web"
	webview2 "github.com/jchv/go-webview2"
)

// Manual real-UI check: uses the production embedded page and API with real
// local read-only data, an isolated configuration and an isolated WebView2
// profile. It never starts the injection watcher, migrates the vault, recovers
// a login broker or permits host/account mutations.
func TestManagerReadinessRealUI(t *testing.T) {
	if os.Getenv("TWOAG_REAL_MANAGER_UI") != "1" {
		t.Skip("manual isolated WebView2 validation only")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// WebView2 can retain cache handles briefly after Destroy. Keep this manual
	// inspection profile instead of failing TempDir cleanup or killing children.
	root, err := os.MkdirTemp("", "2ag-real-manager-*")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	configPath := filepath.Join(root, "2ag.json")
	if previous := os.Getenv("TWOAG_REAL_MANAGER_CONFIG"); previous != "" {
		cfg, err = config.Load(previous)
		if err != nil {
			t.Fatal(err)
		}
		configPath = previous
	}
	bus := core.NewEventBus()
	sm := core.NewStateMachine(cfg, configPath, bus)
	server := api.NewServer(sm, bus)
	server.Version = version
	if os.Getenv("TWOAG_REAL_MANAGER_SOURCE_UI") == "1" {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		server.HandleStatic("/", http.Dir(filepath.Join(cwd, "..", "..", "web", "dist")))
	} else {
		server.HandleStatic("/", http.FS(web.Assets))
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + listener.Addr().String()
	control.SetAPIURL(url)
	if err := os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--remote-debugging-port=28599"); err != nil {
		t.Fatal(err)
	}
	view := webview2.NewWithOptions(webview2.WebViewOptions{Debug: true, DataPath: filepath.Join(root, "webview2")})
	if view == nil {
		t.Fatal("WebView2 unavailable")
	}
	defer view.Destroy()
	view.SetTitle("2Ag Manager — isolated release verification")
	view.SetSize(1860, 1230, webview2.HintNone)
	// This test window can be rendered through CDP without taking input focus.
	syscall.NewLazyDLL("user32.dll").NewProc("ShowWindow").Call(uintptr(view.Window()), 0)
	protected := server.Mux()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/preview/close" {
			view.Dispatch(view.Terminate)
			w.WriteHeader(204)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodOptions {
			if r.URL.Path != "/api/v1/action" {
				http.Error(w, "Read-only validation: host and account mutations disabled", 403)
				return
			}
			var action core.StateAction
			data := new(bytes.Buffer)
			_, _ = data.ReadFrom(r.Body)
			if err := json.Unmarshal(data.Bytes(), &action); err != nil {
				http.Error(w, "Invalid action", 400)
				return
			}
			if action.Type != core.SetLanguageAction && action.Type != core.SetRuntimeModeAction && action.Type != core.CompleteOnboardingAction {
				http.Error(w, "Read-only validation: action disabled", 403)
				return
			}
			r.Body = http.NoBody
			if len(data.Bytes()) > 0 {
				r.Body = io.NopCloser(bytes.NewReader(data.Bytes()))
			}
		}
		protected.ServeHTTP(w, r)
	})
	httpServer := &http.Server{Handler: handler}
	defer httpServer.Close()
	go httpServer.Serve(listener)
	if path := os.Getenv("TWOAG_REAL_MANAGER_UI_FILE"); path != "" {
		data, _ := json.Marshal(map[string]string{"url": url, "config": configPath, "profile": root})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	view.Navigate(url)
	view.Run()
}
