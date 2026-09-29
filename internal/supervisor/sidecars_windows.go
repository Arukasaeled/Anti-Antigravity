//go:build windows

package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/2ag/2ag/internal/config"
)

type SidecarStatus struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	Enabled    bool   `json:"enabled"`
	Running    bool   `json:"running"`
	PID        int    `json:"pid,omitempty"`
	Error      string `json:"error,omitempty"`
}

type SidecarManager struct {
	ctx       context.Context
	host      *ManagedProcess
	baseDir   string
	cdpPort   int
	plugins   map[string]config.Plugin
	processes map[string]*managedSidecar
	mu        sync.Mutex
	server    *http.Server
	listener  net.Listener
}

type managedSidecar struct {
	cmd *exec.Cmd
	err string
}

func NewSidecarManager(ctx context.Context, host *ManagedProcess, plugins []config.Plugin, baseDir string, cdpPort int) (*SidecarManager, error) {
	if host == nil {
		return nil, fmt.Errorf("managed host is nil")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for sidecar control: %w", err)
	}
	m := &SidecarManager{ctx: ctx, host: host, baseDir: baseDir, cdpPort: cdpPort, plugins: make(map[string]config.Plugin), processes: make(map[string]*managedSidecar), listener: listener}
	for _, plugin := range plugins {
		m.plugins[plugin.Name] = plugin
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/plugins", m.handlePlugins)
	mux.HandleFunc("/plugins/", m.handlePlugins)
	m.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = m.server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = m.server.Shutdown(shutdownCtx)
		m.StopAll()
	}()
	for _, plugin := range plugins {
		if plugin.Enabled {
			_ = m.Start(plugin.Name)
		}
	}
	return m, nil
}

func (m *SidecarManager) Address() string { return m.listener.Addr().String() }

func (m *SidecarManager) Start(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	plugin, ok := m.plugins[name]
	if !ok {
		return fmt.Errorf("unknown sidecar %q", name)
	}
	if current := m.processes[name]; current != nil && current.cmd.ProcessState == nil {
		return nil
	}
	executable := plugin.Executable
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(m.baseDir, executable)
	}
	variables := map[string]string{
		"2AG_CDP_PORT":      strconv.Itoa(m.cdpPort),
		"2AG_APP_DIR":       m.baseDir,
		"2AG_PLUGIN_ID":     name,
		"2AG_PLUGIN_DIR":    plugin.Directory,
		"2AG_CONTROL_URL":   "http://" + m.Address(),
		"2AG_PLUGIN_UI_URL": "http://" + m.Address() + "/plugins/" + url.PathEscape(name) + "/ui/",
		"2AG_PORT":          strconv.Itoa(m.listener.Addr().(*net.TCPAddr).Port),
	}
	args := make([]string, len(plugin.Args))
	for index, arg := range plugin.Args {
		args[index] = os.Expand(arg, func(key string) string { return variables[key] })
	}
	cmd := exec.CommandContext(m.ctx, executable, args...)
	cmd.Dir = m.baseDir
	cmd.Env = mergedEnvironment(variables)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		m.processes[name] = &managedSidecar{err: err.Error()}
		return err
	}
	handle, err := openProcess(syscall.Handle(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := assignProcess(m.host.job, handle); err != nil {
		closeHandle(handle)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	closeHandle(handle)
	plugin.Enabled = true
	m.plugins[name] = plugin
	m.processes[name] = &managedSidecar{cmd: cmd}
	log.Printf("[2ag] sidecar %q started with PID %d", name, cmd.Process.Pid)
	return nil
}

func (m *SidecarManager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.processes[name]
	if current == nil || current.cmd == nil || current.cmd.ProcessState != nil {
		plugin := m.plugins[name]
		plugin.Enabled = false
		m.plugins[name] = plugin
		delete(m.processes, name)
		return nil
	}
	_ = current.cmd.Process.Kill()
	_ = current.cmd.Wait()
	plugin := m.plugins[name]
	plugin.Enabled = false
	m.plugins[name] = plugin
	delete(m.processes, name)
	return nil
}

func (m *SidecarManager) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.processes))
	for name := range m.processes {
		names = append(names, name)
	}
	m.mu.Unlock()
	for _, name := range names {
		_ = m.Stop(name)
	}
}

func (m *SidecarManager) Status() []SidecarStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]SidecarStatus, 0, len(m.plugins))
	for name, plugin := range m.plugins {
		item := SidecarStatus{Name: name, Executable: plugin.Executable, Enabled: plugin.Enabled}
		if current := m.processes[name]; current != nil {
			item.Error = current.err
			if current.cmd != nil {
				item.PID = current.cmd.Process.Pid
				item.Running = current.cmd.ProcessState == nil
			}
		}
		result = append(result, item)
	}
	return result
}

func (m *SidecarManager) handlePlugins(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method == http.MethodGet && strings.Contains(strings.TrimPrefix(r.URL.Path, "/plugins/"), "/ui/") {
		m.handlePluginUI(w, r)
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(m.Status())
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/plugins/")
	name, _ = url.PathUnescape(name)
	if name == "" || name == "plugins" {
		http.Error(w, "sidecar name is required", http.StatusBadRequest)
		return
	}
	var request struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var err error
	if request.Enabled {
		err = m.Start(name)
	} else {
		err = m.Stop(name)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(m.Status())
}

// handlePluginUI serves only the manifest-declared entry file. Keeping this
// route on the loopback sidecar server lets a plugin contribute a tab without
// granting it a general file server or changing the host's packaged assets.
func (m *SidecarManager) handlePluginUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	path := strings.TrimPrefix(r.URL.Path, "/plugins/")
	parts := strings.SplitN(path, "/ui/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "plugin UI entry is required", http.StatusBadRequest)
		return
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		http.Error(w, "invalid plugin id", http.StatusBadRequest)
		return
	}
	plugin, ok := m.plugins[name]
	if !ok || plugin.UIEntry == "" {
		http.NotFound(w, r)
		return
	}
	entry := filepath.Clean(plugin.UIEntry)
	declared := filepath.Clean(plugin.UIEntry)
	rel, err := filepath.Rel(plugin.Directory, entry)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		http.Error(w, "invalid plugin UI path", http.StatusForbidden)
		return
	}
	requested, err := url.PathUnescape(parts[1])
	if err != nil || filepath.Clean(filepath.FromSlash(requested)) != filepath.Clean(rel) {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(declared); err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(declared)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	http.ServeFile(w, r, declared)
}
