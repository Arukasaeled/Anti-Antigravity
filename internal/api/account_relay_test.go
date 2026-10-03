package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectedRelayClaimCannotClearAnotherSender(t *testing.T) {
	relay := &accountRelay{path: filepath.Join(t.TempDir(), "relay.json"), state: relayState{Enabled: true, Phase: "dispatching", Handoff: &relayHandoff{ID: "handoff", To: "fixture@example.com", SendClaimed: true, ClaimID: "winner-claim"}}}
	s := &Server{relay: relay}
	req := httptest.NewRequest("POST", "/api/v1/accounts/relay/handoff", strings.NewReader(`{"id":"handoff","phase":"resume-failed","not_sent":true,"claim_id":"loser-claim"}`))
	res := httptest.NewRecorder()
	s.handleRelayHandoff(res, req)
	if res.Code != 409 || !relay.state.Handoff.SendClaimed || relay.state.Phase != "dispatching" {
		t.Fatalf("rejected sender cleared another interface's claim: status=%d phase=%s", res.Code, relay.state.Phase)
	}
	req = httptest.NewRequest("POST", "/api/v1/accounts/relay/handoff", strings.NewReader(`{"id":"handoff","phase":"resume-failed","not_sent":true,"claim_id":"winner-claim"}`))
	res = httptest.NewRecorder()
	s.handleRelayHandoff(res, req)
	if res.Code != 200 || relay.state.Handoff.SendClaimed {
		t.Fatal("confirmed owner failure before click must remain retryable")
	}
}
