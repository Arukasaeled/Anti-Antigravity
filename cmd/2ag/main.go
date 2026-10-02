package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/2ag/2ag/internal/api"
	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/mcp"
	"github.com/2ag/2ag/internal/netproxy"
	"github.com/2ag/2ag/internal/patcher"
	"github.com/2ag/2ag/internal/profile"
	"github.com/2ag/2ag/internal/supervisor"
	"github.com/2ag/2ag/web"
)

func init() {
	// 如果是通过终端命令行运行子命令，附加到父进程控制台以便输出日志与命令回显
	if len(os.Args) > 1 {
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		attachConsole := kernel32.NewProc("AttachConsole")
		if attachConsole.Find() == nil {
			const ATTACH_PARENT_PROCESS = ^uintptr(0) // (DWORD)-1
			attachConsole.Call(ATTACH_PARENT_PROCESS)
		}
	}

	// 强制声明 Per-Monitor DPI Aware V2 (Context = -4)，防止 Windows 125%/150% 缩放导致 WebView2 模糊
	user32 := syscall.NewLazyDLL("user32.dll")
	setDpiAwareness := user32.NewProc("SetProcessDpiAwarenessContext")
	if setDpiAwareness.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		// 必须是符号扩展后的 64 位 HANDLE：^uintptr(3) == 0xFFFFFFFFFFFFFFFC。
		// 写成 uintptr(0xfffffffc) 在 amd64 上是 4294967292 而非 -4，调用会失败返回 FALSE。
		setDpiAwareness.Call(^uintptr(3))
	}
}

func main() {
	closeLog := setupLogging()
	defer closeLog()
	if err := runCLI(os.Args[1:]); err != nil {
		log.Printf("2ag: %v", err)
		fmt.Fprintln(os.Stderr, "2ag:", err)
		os.Exit(1)
	}
}

func runCLI(args []string) error {
	configPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return startManager(configPath, cfg)
	}
	switch args[0] {
	case "run":
		return runCommand(configPath, cfg, args[1:])
	case "restore":
		return restore()
	case "profile":
		return profileCommand(args[1:])
	case "skin":
		return skinCommand(configPath, cfg, args[1:])
	case "manager":
		return startManager(configPath, cfg)
	case "doctor":
		return doctor(configPath, cfg)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func run(configPath string, cfg config.Config) error { return runCommand(configPath, cfg, nil) }

func runCommand(configPath string, cfg config.Config, args []string) error {
	if _, err := supervisor.RecoverPendingLoginBroker(); err != nil {
		return fmt.Errorf("恢复上次登录失败: %w", err)
	}
	// 进程级形态登记：注入链路的三个闸门（HotReloadCDP / WatchAndInjectCDP /
	// PushHubState）都问 IsOfficialRuntime()，这里必须先把它与配置对齐。
	supervisor.SetRuntimeMode(cfg.RuntimeMode)

	// 官方形态闸：`2ag run` 是另一条完整的启动链路（自己 reserveCDPPort、
	// 自己拼 --remote-debugging-port、自己 NewCDPInjector + WaitAndInject），
	// 它完全不经过 LaunchEnhancedHost。只在那边拦会留下一个洞：
	// 官方形态下敲一次 `2ag run` 照样把补丁注入进官方宿主。
	if supervisor.IsOfficialRuntime() {
		log.Printf("[2ag] 官方形态：run 命令改为启动官方 Antigravity（不注入、不代代理、不改凭据）")
		return supervisor.LaunchOfficialHost("")
	}

	debug := false
	for _, arg := range args {
		switch arg {
		case "--debug":
			debug = true
		case "", "--":
		default:
			return fmt.Errorf("unknown run option %q", arg)
		}
	}
	paths, err := supervisor.FindAntigravity()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cdpPort, err := reserveCDPPort()
	if err != nil {
		return err
	}
	var proxy *netproxy.Server
	runtimeConfig := config.NewRuntime(configPath, cfg)
	if cfg.Network.Enabled {
		proxy, err = netproxy.Start(ctx, runtimeConfig)
		if err != nil {
			return err
		}
		log.Printf("[2ag] local network proxy listening on %s", proxy.Address())
	}
	hostArgs := []string{fmt.Sprintf("--remote-debugging-port=%d", cdpPort)}
	if proxy != nil {
		hostArgs = append(hostArgs, "--proxy-server=http://"+proxy.Address())
	}
	if debug {
		hostArgs = append(hostArgs, "--auto-open-devtools-for-tabs")
	}
	log.Printf("[2ag] launching %s with CDP port %d", paths.Executable, cdpPort)
	managed, err := supervisor.Start(ctx, supervisor.HostOptions{
		Executable: paths.Executable,
		Args:       hostArgs,
		Dir:        paths.Root,
		Env:        cfg.EnvOverrides,
	})
	if err != nil {
		return err
	}
	launcherDir, _ := os.Executable()
	launcherDir = filepath.Dir(launcherDir)
	if launcherDir == "." || launcherDir == "" {
		launcherDir, _ = os.Getwd()
	}
	manifests, manifestErr := supervisor.DiscoverPlugins(filepath.Join(launcherDir, "plugins"))
	if manifestErr != nil {
		log.Printf("[2ag] plugin discovery failed: %v", manifestErr)
	}
	configuredPlugins := supervisor.MergePluginConfig(manifests, cfg.Plugins)
	for _, manifest := range manifests {
		log.Printf("[2ag] discovered plugin %s %s", manifest.ID, manifest.Version)
	}
	sidecars, err := supervisor.NewSidecarManager(ctx, managed, configuredPlugins, launcherDir, cdpPort)
	if err != nil {
		_ = managed.Stop()
		return err
	}
	defer sidecars.StopAll()
	defer managed.Close()

	bus := core.NewEventBus()
	sm := core.NewStateMachine(cfg, configPath, bus)
	apiServer := api.NewServer(sm, bus)
	apiServer.HandleStatic("/", http.FS(web.Assets))
	// API 监听端口不再硬绑 28472：28472 是 CDP 的历史默认值
	// （supervisor.DefaultCDPPort），一旦 CDP 解析回落该值就是端口冲突，
	// 而 CLI 模式下宿主与 API 跑在同一进程里，冲突会直接让其中一方静默失败。
	// 改为与图形模式一致的 28470 → 28471 → 动态端口序列。
	apiListener, _, apiErr := listenManagerAPI()
	if apiErr != nil {
		return apiErr
	}
	go func() {
		log.Printf("[2ag] starting local API on %s", apiListener.Addr().String())
		if err := apiServer.Serve(apiListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[2ag] API server error: %v", err)
		}
	}()
	defer apiServer.Stop()

	done := make(chan error, 1)
	go func() { done <- managed.Wait() }()
	go func() {
		injector := patcher.NewCDPInjector(cdpPort)
		injector.Lifetime = ctx
		if proxy != nil {
			injector.ProxyURL = "http://" + proxy.Address()
		}
		pluginState := make(map[string]any, len(configuredPlugins))
		for _, plugin := range configuredPlugins {
			pluginState[plugin.Name] = map[string]any{
				"enabled":     plugin.Enabled,
				"executable":  plugin.Executable,
				"displayName": plugin.DisplayName,
				"version":     plugin.Version,
				"author":      plugin.Author,
				"description": plugin.Description,
			}
		}
		for _, manifest := range manifests {
			entry := pluginState[manifest.ID].(map[string]any)
			entry["displayName"], entry["version"], entry["author"], entry["description"] = manifest.Name, manifest.Version, manifest.Author, manifest.Description
			if manifest.UI != nil {
				entry["ui"] = manifest.UI
				if manifest.UI.Entry != "" {
					entryPath := filepath.ToSlash(manifest.UI.Entry)
					segments := strings.Split(entryPath, "/")
					for index := range segments {
						segments[index] = url.PathEscape(segments[index])
					}
					entry["uiURL"] = "http://" + sidecars.Address() + "/plugins/" + url.PathEscape(manifest.ID) + "/ui/" + strings.Join(segments, "/")
				}
			}
		}
		injector.Initial = patcher.HubConfig{Language: cfg.Language, WallpaperPath: cfg.WallpaperPath, GlobalRules: cfg.GlobalRules, Network: cfg.Network, Privacy: cfg.Privacy, Plugins: pluginState, PluginURL: "http://" + sidecars.Address(), CDPPort: cdpPort, HostPID: managed.PID(), Env: cfg.EnvOverrides, GravityBoost: cfg.GravityBoost}
		injector.BridgeHandlers = coreBridgeHandlers(runtimeConfig, sidecars, injector.SetWallpaperPath, func(ctx context.Context) (any, error) { return openDevTools(ctx, cdpPort) }, func(ctx context.Context) (any, error) { return probeGateway(ctx, injector.ProxyURL) }, sm)

		currentState := sm.GetState()
		if err := injector.WaitAndInject(ctx, currentState.WallpaperPath, currentState.Blur, currentState.Opacity, currentState.ModalOpacity, 15*time.Second); err != nil {
			if ctx.Err() == nil {
				log.Printf("[2ag] CDP injection failed: %v", err)
			}
			return
		}
		log.Println("[2ag] Dream Skin injected successfully via CDP!")

		stateCh := bus.Subscribe(core.StateChangedEvent)
		defer bus.Unsubscribe(core.StateChangedEvent, stateCh)

		cmdCh := bus.Subscribe(core.CommandEvent)
		defer bus.Unsubscribe(core.CommandEvent, cmdCh)

		for {
			select {
			case <-ctx.Done():
				return
			case event := <-stateCh:
				if newState, ok := event.Payload.(config.Config); ok {
					injector.Initial.GravityBoost = newState.GravityBoost
					if err := injector.Inject(ctx, newState.WallpaperPath, newState.Blur, newState.Opacity, newState.ModalOpacity); err != nil && ctx.Err() == nil {
						log.Printf("[2ag] CDP event injection failed: %v", err)
					}
					stateData, _ := json.Marshal(newState)
					_ = injector.Evaluate(ctx, fmt.Sprintf("if (typeof window.__2ag_onStateUpdate === 'function') window.__2ag_onStateUpdate(%s);", string(stateData)))
				}
			case event := <-cmdCh:
				if action, ok := event.Payload.(core.StateAction); ok {
					switch action.Type {
					case core.OpenDevtoolsAction:
						openDevTools(ctx, cdpPort)
					case core.HostCtrlAction:
						payload, ok := action.Payload.(map[string]any)
						if ok {
							cmd, _ := payload["cmd"].(string)
							if cmd == "stop" {
								managed.Stop()
							}
						}
					}
				}
			case <-time.After(5 * time.Second):
				currentState := sm.GetState()
				if err := injector.Inject(ctx, currentState.WallpaperPath, currentState.Blur, currentState.Opacity, currentState.ModalOpacity); err != nil && ctx.Err() == nil {
					log.Printf("[2ag] CDP maintenance injection failed: %v", err)
				}
			}
		}
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("Antigravity exited: %w", err)
		}
		return nil
	case <-ctx.Done():
		_ = managed.Stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		return nil
	}
}

func coreBridgeHandlers(runtimeConfig *config.Runtime, sidecars *supervisor.SidecarManager, setWallpaper func(string) error, openDevToolsHandler func(context.Context) (any, error), probeHandler func(context.Context) (any, error), sm *core.StateMachine) map[string]patcher.BridgeHandler {
	return map[string]patcher.BridgeHandler{
		"core.dialog.openFile": func(ctx context.Context, params map[string]any) (any, error) { return openFileDialog(ctx, params) },
		"core.diagnostics.devtools": func(ctx context.Context, _ map[string]any) (any, error) {
			if openDevToolsHandler == nil {
				return nil, errors.New("DevTools opener is unavailable")
			}
			return openDevToolsHandler(ctx)
		},
		"core.diagnostics.probe": func(ctx context.Context, _ map[string]any) (any, error) {
			if probeHandler == nil {
				return map[string]any{"ok": false, "status": "disabled", "latency_ms": 0}, nil
			}
			return probeHandler(ctx)
		},
		"core.config.get": func(context.Context, map[string]any) (any, error) { return runtimeConfig.Snapshot(), nil },
		"core.config.set": func(_ context.Context, params map[string]any) (any, error) {
			patch, ok := params["patch"].(map[string]any)
			if !ok {
				patch = params
			}
			if wallpaper, ok := patch["wallpaper_path"].(string); ok && setWallpaper != nil {
				if err := setWallpaper(wallpaper); err != nil {
					return nil, err
				}
			}
			updated, err := runtimeConfig.Update(func(next *config.Config) error {
				data, marshalErr := json.Marshal(patch)
				if marshalErr != nil {
					return marshalErr
				}
				return json.Unmarshal(data, next)
			})
			return updated, err
		},
		"core.action": func(_ context.Context, params map[string]any) (any, error) {
			actionTypeStr, _ := params["type"].(string)
			if actionTypeStr == "" {
				return nil, errors.New("missing action type")
			}
			if sm != nil {
				return nil, sm.ApplyAction(core.StateAction{
					Type:    core.ActionType(actionTypeStr),
					Payload: params["payload"],
				})
			}
			return nil, nil
		},
		"core.plugins.list": func(context.Context, map[string]any) (any, error) { return sidecars.Status(), nil },
		"core.plugins.toggle": func(_ context.Context, params map[string]any) (any, error) {
			id, _ := params["pluginId"].(string)
			if id == "" {
				id, _ = params["id"].(string)
			}
			enable, _ := params["enable"].(bool)
			var err error
			if enable {
				err = sidecars.Start(id)
			} else {
				err = sidecars.Stop(id)
			}
			if err != nil {
				return nil, err
			}
			// Persist the toggle for configured plugins. A manifest-only plugin is
			// also added with its discovered executable so a future launch keeps
			// the user's choice without requiring a second config edit.
			var executable string
			for _, item := range sidecars.Status() {
				if item.Name == id {
					executable = item.Executable
					break
				}
			}
			if _, persistErr := runtimeConfig.Update(func(next *config.Config) error {
				for index := range next.Plugins {
					if next.Plugins[index].Name == id {
						next.Plugins[index].Enabled = enable
						return nil
					}
				}
				if executable != "" {
					next.Plugins = append(next.Plugins, config.Plugin{Name: id, Executable: executable, Enabled: enable})
				}
				return nil
			}); persistErr != nil {
				return nil, persistErr
			}
			return sidecars.Status(), nil
		},
	}
}

func probeGateway(ctx context.Context, endpoint string) (any, error) {
	if strings.TrimSpace(endpoint) == "" {
		return map[string]any{"ok": false, "status": "disabled", "latency_ms": 0}, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/status", nil)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	latency := time.Since(started).Round(time.Millisecond).Milliseconds()
	if err != nil {
		return map[string]any{"ok": false, "status": "unreachable", "latency_ms": latency, "error": err.Error()}, nil
	}
	defer response.Body.Close()
	return map[string]any{"ok": response.StatusCode >= 200 && response.StatusCode < 400, "status": response.Status, "code": response.StatusCode, "latency_ms": latency}, nil
}

func openDevTools(ctx context.Context, port int) (any, error) {
	if port <= 0 {
		return nil, errors.New("CDP port is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/json/list", port), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("query CDP targets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query CDP targets: HTTP %s", response.Status)
	}
	var targets []struct {
		Type                 string `json:"type"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode CDP targets: %w", err)
	}
	var websocketURL string
	for _, target := range targets {
		if target.Type == "page" && target.WebSocketDebuggerURL != "" {
			websocketURL = target.WebSocketDebuggerURL
			break
		}
	}
	if websocketURL == "" {
		return nil, errors.New("no page target is available for DevTools")
	}
	parsed, err := url.Parse(websocketURL)
	if err != nil {
		return nil, fmt.Errorf("parse DevTools target: %w", err)
	}
	devtoolsURL := fmt.Sprintf("http://127.0.0.1:%d/devtools/inspector.html?ws=%s", port, url.QueryEscape(parsed.Host+parsed.RequestURI()))
	if err := exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", devtoolsURL).Start(); err != nil {
		return nil, fmt.Errorf("open DevTools frontend: %w", err)
	}
	return map[string]any{"url": devtoolsURL}, nil
}

func openFileDialog(ctx context.Context, params map[string]any) (any, error) {
	filter := "All files (*.*)|*.*"
	if value, ok := params["filter"].(string); ok && strings.Contains(strings.ToLower(value), "image") {
		filter = "Image files|*.png;*.jpg;*.jpeg;*.gif;*.webp;*.bmp|All files (*.*)|*.*"
	}
	// The filter is selected from a fixed allow-list above, so no user input is
	// interpolated into the PowerShell command.
	command := "$d=New-Object System.Windows.Forms.OpenFileDialog; $d.Filter='" + filter + "'; if($d.ShowDialog() -eq 'OK'){Write-Output $d.FileName}"
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", "Add-Type -AssemblyName System.Windows.Forms; "+command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return nil, errors.New("file selection cancelled")
	}
	return map[string]string{"path": path}, nil
}

func restore() error {
	log.Println("2Ag uses runtime CDP injection and does not modify Antigravity files; no host restore is required.")
	return nil
}

func profileCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("profile requires list, save, or switch")
	}
	paths, err := supervisor.FindAntigravity()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	manager := profile.NewManager(paths.UserData, filepath.Join(home, ".2ag", "profiles"))
	switch args[0] {
	case "list":
		profiles, err := manager.List()
		if err != nil {
			return err
		}
		for _, name := range profiles {
			fmt.Println(name)
		}
		return nil
	case "save":
		if len(args) != 2 {
			return errors.New("usage: 2ag profile save <name>")
		}
		return manager.SaveProfile(args[1])
	case "switch":
		if len(args) != 2 {
			return errors.New("usage: 2ag profile switch <name>")
		}
		return manager.SwitchProfile(args[1])
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func skinCommand(configPath string, cfg config.Config, args []string) error {
	if len(args) == 0 || args[0] == "show" {
		fmt.Println("wallpaper:", cfg.WallpaperPath)
		fmt.Println("blur:", cfg.Blur)
		fmt.Println("opacity:", cfg.Opacity)
		return nil
	}
	switch args[0] {
	case "set-wallpaper":
		if len(args) != 2 {
			return errors.New("usage: 2ag skin set-wallpaper <path>")
		}
		cfg.WallpaperPath = args[1]
	case "set-blur":
		if len(args) != 2 {
			return errors.New("usage: 2ag skin set-blur <pixels>")
		}
		value, err := strconv.Atoi(args[1])
		if err != nil {
			return fmt.Errorf("parse blur: %w", err)
		}
		cfg.Blur = value
	case "set-opacity":
		if len(args) != 2 {
			return errors.New("usage: 2ag skin set-opacity <0..1>")
		}
		value, err := strconv.ParseFloat(args[1], 64)
		if err != nil {
			return fmt.Errorf("parse opacity: %w", err)
		}
		cfg.Opacity = value
	default:
		return fmt.Errorf("unknown skin command %q", args[0])
	}
	if err := config.Save(configPath, cfg); err != nil {
		return err
	}
	fmt.Println("skin configuration saved")
	return nil
}

func doctor(configPath string, cfg config.Config) error {
	fmt.Println("version:", version)
	fmt.Println("config:", configPath)
	fmt.Println("wallpaper:", cfg.WallpaperPath)
	fmt.Println("blur:", cfg.Blur)
	fmt.Println("opacity:", cfg.Opacity)
	fmt.Println("network proxy:", cfg.Network.Enabled)
	fmt.Println("endpoint overrides:", len(cfg.Network.EndpointOverrides))
	fmt.Println("rule targets:", len(cfg.Network.RuleTargets))
	fmt.Println("privacy blocked hosts:", len(cfg.Privacy.BlockedHosts))
	fmt.Println("sidecars:", len(cfg.Plugins))
	if executable, err := os.Executable(); err == nil {
		if manifests, err := supervisor.DiscoverPlugins(filepath.Join(filepath.Dir(executable), "plugins")); err == nil {
			fmt.Println("discovered plugins:", len(manifests))
		} else {
			fmt.Println("plugin discovery:", err)
		}
	}
	fmt.Println("cdp port: dynamic loopback port per run")
	paths, err := supervisor.FindAntigravity()
	if err != nil {
		fmt.Println("antigravity:", err)
		return nil
	}
	fmt.Println("executable:", paths.Executable)
	fmt.Println("install root:", paths.Root)
	fmt.Println("entry HTML:", paths.EntryHTML)
	fmt.Println("user data:", paths.UserData)
	fmt.Println("credentials:", emptyAsNone(paths.Credentials))
	fmt.Println("mcp config:", emptyAsNone(paths.MCPConfigPath))
	if paths.MCPConfigPath != "" {
		if _, err := mcp.NewStore(paths.MCPConfigPath).Load(); err != nil {
			fmt.Println("mcp status:", err)
		} else {
			fmt.Println("mcp status: readable")
		}
	}
	return nil
}

func emptyAsNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func usage() {
	fmt.Println("2ag - Anti-Antigravity enhancement launcher")
	fmt.Println("usage:")
	fmt.Println("  2ag manager")
	fmt.Println("  2ag run [--debug]")
	fmt.Println("  2ag restore")
	fmt.Println("  2ag skin show")
	fmt.Println("  2ag skin set-wallpaper <path>")
	fmt.Println("  2ag skin set-blur <pixels>")
	fmt.Println("  2ag skin set-opacity <0..1>")
	fmt.Println("  2ag profile list")
	fmt.Println("  2ag profile save <name>")
	fmt.Println("  2ag profile switch <name>")
	fmt.Println("  2ag doctor")
}
