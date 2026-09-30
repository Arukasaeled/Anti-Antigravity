package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	// 4. Test GET /api/v1/dashboard
	reqDash := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	wDash := httptest.NewRecorder()
	s.mux.ServeHTTP(wDash, reqDash)
	if wDash.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/dashboard, got %d", wDash.Code)
	}
	var dashRes struct {
		ActiveAccount supervisor.AccountInstance `json:"active_account"`
	}
	if err := json.NewDecoder(wDash.Body).Decode(&dashRes); err != nil {
		t.Fatalf("failed to decode dashboard response: %v", err)
	}
	t.Logf("Dashboard Active Account: %s, Gemini 5h: %d%%, Claude 5h: %d%%",
		dashRes.ActiveAccount.Email, dashRes.ActiveAccount.GeminiPool.FiveHourPercent, dashRes.ActiveAccount.ClaudePool.FiveHourPercent)

	// 5. Test POST /api/v1/accounts/primary -> switch to user@example.com
	switchBody := `{"email":"user@example.com"}`
	reqSwitch := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/primary", strings.NewReader(switchBody))
	wSwitch := httptest.NewRecorder()
	s.mux.ServeHTTP(wSwitch, reqSwitch)
	if wSwitch.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/accounts/primary, got %d", wSwitch.Code)
	}

	// 6. Test GET /api/v1/accounts/active -> verify user is active
	reqActive := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/active", nil)
	wActive := httptest.NewRecorder()
	s.mux.ServeHTTP(wActive, reqActive)
	if wActive.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/v1/accounts/active, got %d", wActive.Code)
	}
	var activeRes struct {
		ActiveAccount supervisor.AccountInstance `json:"active_account"`
		Email         string                     `json:"email"`
	}
	if err := json.NewDecoder(wActive.Body).Decode(&activeRes); err != nil {
		t.Fatalf("failed to decode active account: %v", err)
	}
	if !strings.EqualFold(activeRes.Email, "user@example.com") {
		t.Fatalf("expected user@example.com to be active, got %s", activeRes.Email)
	}
	t.Logf("Active account verified as: %s (Gemini: %d%%, Claude: %d%%)",
		activeRes.Email, activeRes.ActiveAccount.GeminiPool.FiveHourPercent, activeRes.ActiveAccount.ClaudePool.FiveHourPercent)

	// 7. Test /api/v1/host/takeover method restriction
	reqTakeoverGet := httptest.NewRequest(http.MethodGet, "/api/v1/host/takeover", nil)
	wTakeoverGet := httptest.NewRecorder()
	s.mux.ServeHTTP(wTakeoverGet, reqTakeoverGet)
	if wTakeoverGet.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for GET /api/v1/host/takeover, got %d", wTakeoverGet.Code)
	}
}

