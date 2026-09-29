package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/2ag/2ag/internal/config"
	"github.com/2ag/2ag/internal/core"
	"github.com/2ag/2ag/internal/supervisor"
)

func TestAPISessionsAndHost(t *testing.T) {
	bus := core.NewEventBus()
	cfg := config.Config{}
	sm := core.NewStateMachine(cfg, "2ag.json", bus)
	s := NewServer(sm, bus)

	// 1. Test GET /api/v1/sessions
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/sessions, got %d", w.Code)
	}

	var sessionsRes supervisor.SessionsResult
	if err := json.NewDecoder(w.Body).Decode(&sessionsRes); err != nil {
		t.Fatalf("failed to decode sessions response: %v", err)
	}
	t.Logf("API returned %d sessions", sessionsRes.Total)
	if sessionsRes.Total > 0 {
		first := sessionsRes.Sessions[0]
		t.Logf("First session: ID=%s, Title=%s, Turns=%d", first.ID, first.Title, first.Turns)

		// 2. Test GET /api/v1/sessions/export?id=...
		reqExport := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/export?id="+first.ID, nil)
		wExport := httptest.NewRecorder()
		s.mux.ServeHTTP(wExport, reqExport)
		if wExport.Code == http.StatusOK {
			t.Logf("Session export successful, content length: %d", wExport.Body.Len())
		}
	}

	// 3. Test GET /api/v1/host/status
	reqHost := httptest.NewRequest(http.MethodGet, "/api/v1/host/status", nil)
	wHost := httptest.NewRecorder()
	s.mux.ServeHTTP(wHost, reqHost)
	if wHost.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/host/status, got %d", wHost.Code)
	}
	var status supervisor.HostStatus
	if err := json.NewDecoder(wHost.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode host status: %v", err)
	}
	t.Logf("Host status: running=%v, pid=%d, memory=%.1fMB", status.IsRunning, status.PID, status.MemoryMB)
}
