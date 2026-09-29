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

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/mcp"
	"github.com/2ag/2ag/internal/netproxy"
	"github.com/2ag/2ag/internal/patcher"
	"github.com/2ag/2ag/internal/profile"
	"github.com/2ag/2ag/internal/supervisor"
)

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
		return run(configPath, cfg)
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
				entryPath := filepath.ToSlash(manifest.UI.Entry)
				segments := strings.Split(entryPath, "/")
				for index := range segments {
					segments[index] = url.PathEscape(segments[index])
				}
				entry["uiURL"] = "http://" + sidecars.Address() + "/plugins/" + url.PathEscape(manifest.ID) + "/ui/" + strings.Join(segments, "/")
			}
		}
		injector.Initial = patcher.HubConfig{Language: cfg.Language, WallpaperPath: cfg.WallpaperPath, GlobalRules: cfg.GlobalRules, Network: cfg.Network, Privacy: cfg.Privacy, Plugins: pluginState, PluginURL: "http://" + sidecars.Address(), CDPPort: cdpPort, HostPID: managed.PID(), Env: cfg.EnvOverrides}
		injector.BridgeHandlers = coreBridgeHandlers(runtimeConfig, sidecars, injector.SetWallpaperPath, func(ctx context.Context) (any, error) { return openDevTools(ctx, cdpPort) }, func(ctx context.Context) (any, error) { return probeGateway(ctx, injector.ProxyURL) })
		// The wallpaper service uses the launcher context. The CDP wait has
		// its own deadline, so the image remains available after injection.
		if err := injector.WaitAndInject(ctx, cfg.WallpaperPath, cfg.Blur, cfg.Opacity, 15*time.Second); err != nil {
			if ctx.Err() == nil {
				log.Printf("[2ag] CDP injection failed: %v", err)
			}
			return
		}
		log.Println("[2ag] Dream Skin injected successfully via CDP!")
		// Antigravity can replace its renderer document after the initial local
		// service boot. Reapply the hub periodically so a navigation cannot
		// remove the runtime skin or extension shell.
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				if err := injector.Inject(ctx, cfg.WallpaperPath, cfg.Blur, cfg.Opacity); err != nil && ctx.Err() == nil {
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

func coreBridgeHandlers(runtimeConfig *config.Runtime, sidecars *supervisor.SidecarManager, setWallpaper func(string) error, openDevToolsHandler func(context.Context) (any, error), probeHandler func(context.Context) (any, error)) map[string]patcher.BridgeHandler {
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
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", "Add-Type -AssemblyName System.Windows.Forms; "+command).Output()
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
