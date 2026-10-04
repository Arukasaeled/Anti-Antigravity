package api

import (
	"encoding/json"
	"net/http"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/supervisor"
	"github.com/jchv/go-webview2/webviewloader"
)

// Only combines existing probes. Looking at readiness cannot start a host,
// create a copy, modify credentials, or claim configured features are effective.
func (s *Server) handleLaunchReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	webview, err := webviewloader.GetInstalledVersion()
	if err != nil {
		webview = ""
	}
	cfg := s.stateMachine.GetState()
	getJSON(w, r, map[string]any{
		"version": s.Version, "bootstrap": supervisor.ProbeHostBootstrap(),
		"webview2": webview, "environment": supervisor.DetectAntigravityEnvironment(),
		"doctor": supervisor.ReadDoctorReport(), "configured_mode": cfg.RuntimeMode,
		"locale": map[string]string{"preference": cfg.Language, "effective": cfg.UILanguage(), "system": config.SystemLanguage()},
	})
}

func (s *Server) handleCreateEnhancedHost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// EnsureFrozenHost only copies the installed official files to the local
	// host directory. It performs no process lifecycle or account operation.
	path, err := supervisor.EnsureFrozenHost()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "path": path})
}
