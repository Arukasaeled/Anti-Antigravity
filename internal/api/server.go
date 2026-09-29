package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/supervisor"
)

type Server struct {
	stateMachine *core.StateMachine
	bus          *core.EventBus
	mux          *http.ServeMux
	server       *http.Server
}

func NewServer(sm *core.StateMachine, bus *core.EventBus) *Server {
	s := &Server{
		stateMachine: sm,
		bus:          bus,
		mux:          http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/state", s.handleGetState)
	s.mux.HandleFunc("/api/v1/action", s.handlePostAction)
	s.mux.HandleFunc("/api/v1/events", s.handleEvents)
	s.mux.HandleFunc("/api/v1/host/status", s.handleGetHostStatus)
	s.mux.HandleFunc("/api/v1/host/launch", s.handleHostLaunch)
	s.mux.HandleFunc("/api/v1/sessions", s.handleGetSessions)
	s.mux.HandleFunc("/api/v1/sessions/export", s.handleExportSession)
	s.mux.HandleFunc("/api/v1/accounts", s.handleGetAccounts)
	s.mux.HandleFunc("/api/v1/accounts/refresh", s.handleRefreshAccounts)
	s.mux.HandleFunc("/api/v1/oauth/login-browser", s.handleOAuthLoginBrowser)
	s.mux.HandleFunc("/api/v1/oauth/status", s.handleOAuthStatus)
	s.mux.HandleFunc("/api/v1/accounts/import-json", s.handleImportAccountsJSON)
}

func (s *Server) HandleStatic(prefix string, fs http.FileSystem) {
	s.mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(fs)))
}

func (s *Server) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := s.stateMachine.GetState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (s *Server) handlePostAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var action core.StateAction
	if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if action.Type == core.HostCtrlAction {
		if payload, ok := action.Payload.(map[string]any); ok {
			cmd, _ := payload["cmd"].(string)
			switch cmd {
			case "start":
				_ = supervisor.LaunchHostClient("")
			case "restart":
				_ = supervisor.RestartHostClient("")
			case "stop":
				_ = supervisor.StopHostClient()
			}
		}
	}
	if err := s.stateMachine.ApplyAction(action); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Simple SSE implementation
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.bus.Subscribe(core.StateChangedEvent)
	defer s.bus.Unsubscribe(core.StateChangedEvent, ch)

	// Send initial state
	state := s.stateMachine.GetState()
	data, _ := json.Marshal(state)
	fmt.Fprintf(w, "event: state_changed\ndata: %s\n\n", data)
	flusher.Flush()

	for {
		select {
		case event := <-ch:
			data, err := json.Marshal(event.Payload)
			if err == nil {
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) Start(addr string) error {
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

func (s *Server) handleGetHostStatus(w http.ResponseWriter, r *http.Request) {
	status := supervisor.ProbeRealHost()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ScanLocalAccounts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"accounts": accounts,
	})
}

func (s *Server) handleRefreshAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ScanLocalAccounts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"accounts":  accounts,
		"refreshed": true,
		"timestamp": time.Now().Unix(),
	})
}

func (s *Server) handleOAuthLoginBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Proxy string `json:"proxy"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	authURL, err := supervisor.StartBrowserOAuth(r.Context(), req.Proxy)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"auth_url": authURL,
		"status":   "WAITING",
	})
}

func (s *Server) handleOAuthStatus(w http.ResponseWriter, r *http.Request) {
	status := supervisor.GetOAuthStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleImportAccountsJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "读取请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	accounts, err := supervisor.ImportAccountsJSON(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":  true,
		"accounts": accounts,
	})
}

func (s *Server) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := supervisor.ScanLocalSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleExportSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "缺少会话 ID", http.StatusBadRequest)
		return
	}
	md, err := supervisor.ExportSessionMarkdown(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"session-%s.md\"", shortID))
	w.Write([]byte(md))
}

func (s *Server) handleHostLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomPath string `json:"custom_path"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := supervisor.LaunchHostClient(req.CustomPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "宿主已成功拉起",
	})
}

