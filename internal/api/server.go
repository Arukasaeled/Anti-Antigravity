package api

import (
	"encoding/json"
	"fmt"
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
	s.mux.HandleFunc("/api/v1/accounts", s.handleGetAccounts)
	s.mux.HandleFunc("/api/v1/accounts/refresh", s.handleRefreshAccounts)
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
		"accounts": accounts,
		"refreshed": true,
		"timestamp": time.Now().Unix(),
	})
}
