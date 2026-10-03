package supervisor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExpiredArchiveRequiresNativeRenewal(t *testing.T) {
	const email = "renewal@example.com"
	credential := map[string]any{"token": map[string]any{"access_token": "fixture-access", "refresh_token": "fixture-refresh", "expiry": time.Now().Add(-time.Hour).Format(time.RFC3339)}, "id_token": "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6InJlbmV3YWxAZXhhbXBsZS5jb20iLCJleHAiOjEwMDAwMDAwMDB9."}
	raw, _ := json.Marshal(credential)
	if _, ok := credentialUsableEmail(raw); ok {
		t.Fatal("expired archive is not a newly completed login")
	}
	if owner, ok := credentialRestorableEmail(raw); !ok || owner != email {
		t.Fatal("complete renewable archive must be eligible for native bootstrap")
	}
	native := NativeAuthState{Available: true, Valid: true, Email: email}
	if nativeCredentialReady(raw, native, email) {
		t.Fatal("native confirmation alone must not accept expired access")
	}
	credential["token"].(map[string]any)["expiry"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	raw, _ = json.Marshal(credential)
	if !nativeCredentialReady(raw, native, email) {
		t.Fatal("renewed access + native identity must work even when refresh does not reissue the ID token")
	}
	native.Email = "other@example.com"
	if nativeCredentialReady(raw, native, email) {
		t.Fatal("a stored identity cannot override native identity")
	}
	native.Email = email
	native.Valid = false
	if nativeCredentialReady(raw, native, email) {
		t.Fatal("ineligible native auth must never count as a successful switch")
	}
	delete(credential, "id_token")
	raw, _ = json.Marshal(credential)
	if _, ok := credentialRestorableEmail(raw); ok {
		t.Fatal("missing ID token must still be rejected")
	}
}
