package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	s.mux.HandleFunc("/api/v1/dashboard", s.handleGetDashboard)
	s.mux.HandleFunc("/api/v1/host/status", s.handleGetHostStatus)
	s.mux.HandleFunc("/api/v1/host/launch", s.handleHostLaunch)
	s.mux.HandleFunc("/api/v1/host/takeover", s.handleHostTakeover)
	s.mux.HandleFunc("/api/v1/sessions", s.handleSessionsRoute)
	s.mux.HandleFunc("/api/v1/sessions/export", s.handleExportSession)
	s.mux.HandleFunc("/api/v1/sessions/delete", s.handleDeleteSession)
	s.mux.HandleFunc("/api/v1/accounts", s.handleGetAccounts)
	s.mux.HandleFunc("/api/v1/accounts/active", s.handleGetActiveAccount)
	s.mux.HandleFunc("/api/v1/accounts/primary", s.handleSetPrimaryAccount)
	s.mux.HandleFunc("/api/v1/accounts/refresh", s.handleRefreshAccounts)
	s.mux.HandleFunc("/api/v1/oauth/login-browser", s.handleOAuthLoginBrowser)
	s.mux.HandleFunc("/api/v1/oauth/status", s.handleOAuthStatus)
	s.mux.HandleFunc("/api/v1/accounts/import-json", s.handleImportAccountsJSON)
	s.mux.HandleFunc("/json", s.handleProxyCDP)
	s.mux.HandleFunc("/json/list", s.handleProxyCDP)
}

func (s *Server) HandleStatic(prefix string, fs http.FileSystem) {
	if prefix == "/" {
		s.mux.Handle("/", http.FileServer(fs))
		return
	}
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
				_ = supervisor.LaunchEnhancedHost("", "")
			case "restart":
				_ = supervisor.RestartEnhancedHost("", "")
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

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

func (s *Server) Serve(l net.Listener) error {
	s.server = &http.Server{Handler: s.mux}
	return s.server.Serve(l)
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

type AccountQuotaDTO struct {
	ID                string                 `json:"id"`
	Email             string                 `json:"email"`
	Name              string                 `json:"name"`
	Role              string                 `json:"role"`
	IsPrimary         bool                   `json:"is_primary"`
	IsActive          bool                   `json:"is_active"`
	Status            string                 `json:"status"`
	Weight            int                    `json:"weight"`
	GeminiPool        supervisor.QuotaWindow `json:"gemini_pool"`
	ClaudePool        supervisor.QuotaWindow `json:"claude_pool"`
	Gemini5hPercent   int                    `json:"gemini_5h_percent"`
	Gemini5hReset     string                 `json:"gemini_5h_reset"`
	GeminiWeekPercent int                    `json:"gemini_week_percent"`
	GeminiWeekReset   string                 `json:"gemini_week_reset"`
	Claude5hPercent   int                    `json:"claude_5h_percent"`
	Claude5hReset     string                 `json:"claude_5h_reset"`
	ClaudeWeekPercent int                    `json:"claude_week_percent"`
	ClaudeWeekReset   string                 `json:"claude_week_reset"`
	Models            []string               `json:"models"`
}

func toAccountQuotaDTO(acc supervisor.AccountInstance) AccountQuotaDTO {
	return AccountQuotaDTO{
		ID:                acc.ID,
		Email:             acc.Email,
		Name:              acc.Name,
		Role:              acc.Role,
		IsPrimary:         acc.IsPrimary,
		IsActive:          acc.IsActive,
		Status:            acc.Status,
		Weight:            acc.Weight,
		GeminiPool:        acc.GeminiPool,
		ClaudePool:        acc.ClaudePool,
		Gemini5hPercent:   acc.GeminiPool.FiveHourPercent,
		Gemini5hReset:     acc.GeminiPool.FiveHourReset,
		GeminiWeekPercent: acc.GeminiPool.WeeklyPercent,
		GeminiWeekReset:   acc.GeminiPool.WeeklyReset,
		Claude5hPercent:   acc.ClaudePool.FiveHourPercent,
		Claude5hReset:     acc.ClaudePool.FiveHourReset,
		ClaudeWeekPercent: acc.ClaudePool.WeeklyPercent,
		ClaudeWeekReset:   acc.ClaudePool.WeeklyReset,
		Models:            acc.Models,
	}
}

func (s *Server) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hostStatus := supervisor.ProbeRealHost()
	isLive := hostStatus.ProcessFound && hostStatus.CDPConnected

	if isLive {
		// 宿主拉起且 CDP 握手成功后，自动通过 CDP Runtime.evaluate 探针读取宿主内存中的真实登录邮箱
		go func() {
			if email, err := supervisor.QueryHostEmailViaCDP(1 * time.Second); err == nil && email != "" {
				supervisor.SetActiveAccount(email)
			}
		}()
	}

	activeAcc := supervisor.GetActiveAccountInstance()

	liveText := "● 预设主控 (待挂钩)"
	if isLive {
		liveText = "● 实时在线 (已挂钩)"
	}

	gatewayStatus := map[string]any{
		"address":    "127.0.0.1:8045",
		"mode":       "智能 429 自愈轮询",
		"latency_ms": 28,
		"status":     "READY",
		"protocol":   "Gemini v1internal -> OpenAI Compatible",
	}

	payload := map[string]any{
		"host_status":    hostStatus,
		"active_account": toAccountQuotaDTO(activeAcc),
		"is_live":        isLive,
		"live_status":    liveText,
		"gateway_status": gatewayStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleGetHostStatus(w http.ResponseWriter, r *http.Request) {
	status := supervisor.ProbeRealHost()
	isLive := status.ProcessFound && status.CDPConnected

	if isLive {
		// 宿主拉起且 CDP 握手成功后，自动通过 CDP Runtime.evaluate 探针读取宿主内存中的真实登录邮箱
		go func() {
			if email, err := supervisor.QueryHostEmailViaCDP(1 * time.Second); err == nil && email != "" {
				supervisor.SetActiveAccount(email)
			}
		}()
	}

	activeAcc := supervisor.GetActiveAccountInstance()
	liveText := "● 预设主控 (待挂钩)"
	if isLive {
		liveText = "● 实时在线 (已挂钩)"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"process_found":  status.ProcessFound,
		"pid":            status.PID,
		"memory_mb":      status.MemoryMB,
		"cdp_connected":  status.CDPConnected,
		"cdp_port":       status.CDPPort,
		"active_account": toAccountQuotaDTO(activeAcc),
		"is_live":        isLive,
		"live_status":    liveText,
	})
}

func (s *Server) handleGetActiveAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	activeAcc := supervisor.GetActiveAccountInstance()
	dto := toAccountQuotaDTO(activeAcc)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"account":             dto,
		"active_account":      dto,
		"email":               activeAcc.Email,
		"name":                dto.Name,
		"role":                dto.Role,
		"status":              dto.Status,
		"gemini_5h_percent":   dto.Gemini5hPercent,
		"gemini_5h_reset":     dto.Gemini5hReset,
		"gemini_week_percent": dto.GeminiWeekPercent,
		"gemini_week_reset":   dto.GeminiWeekReset,
		"claude_5h_percent":   dto.Claude5hPercent,
		"claude_5h_reset":     dto.Claude5hReset,
		"claude_week_percent": dto.ClaudeWeekPercent,
		"claude_week_reset":   dto.ClaudeWeekReset,
		"gemini_pool":         dto.GeminiPool,
		"claude_pool":         dto.ClaudePool,
	})
}

func (s *Server) handleGetAccounts(w http.ResponseWriter, r *http.Request) {
	accounts := supervisor.ScanLocalAccounts()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"accounts": accounts,
	})
}

func (s *Server) handleSetPrimaryAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Email != "" {
		supervisor.SetActiveAccount(req.Email)
	}
	activeAcc := supervisor.GetActiveAccountInstance()
	dto := toAccountQuotaDTO(activeAcc)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success":        true,
		"email":          supervisor.GetActiveAccountEmail(),
		"active_account": dto,
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

func (s *Server) handleSessionsRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.handleDeleteSession(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	result := supervisor.ScanLocalSessions()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id == "" {
		http.Error(w, "缺少会话 ID", http.StatusBadRequest)
		return
	}
	if err := supervisor.DeleteSession(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"id":      id,
	})
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
		Email      string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Email != "" {
		supervisor.SetActiveAccount(req.Email)
	}
	if err := supervisor.LaunchEnhancedHost(req.CustomPath, req.Email); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "宿主已成功拉起并附加 CDP 沙箱",
	})
}

func (s *Server) handleHostTakeover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		CustomPath string `json:"custom_path"`
		Email      string `json:"email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Email != "" {
		supervisor.SetActiveAccount(req.Email)
	}
	if err := supervisor.TakeoverHost(req.CustomPath, req.Email); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "宿主已成功热接管，CDP 调试端口与账号沙箱已就绪",
	})
}

func (s *Server) handleProxyCDP(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + supervisor.ResolveCDPAddr() + r.URL.RequestURI())
	if err != nil {
		http.Error(w, "CDP 端口未就绪或未运行: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
