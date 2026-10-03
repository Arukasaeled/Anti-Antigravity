package supervisor

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestSameAccountRecoveryPreservesFinalNativeRotation(t *testing.T) {
	original := fixtureCredential("target@example.com", false)
	var payload map[string]any
	_ = json.Unmarshal(original, &payload)
	payload["token"].(map[string]any)["refresh_token"] = "preflight-renewed-grant"
	renewed, _ := json.Marshal(payload)
	// A stop flush can only change access/expiry while retaining the old grant.
	_ = json.Unmarshal(original, &payload)
	payload["token"].(map[string]any)["access_token"] = "stop-flushed-access"
	live, _ := json.Marshal(payload)
	got, nativeRenewed := sameAccountRecoveryCredential(original, renewed, live)
	if nativeRenewed || !bytes.Equal(got, renewed) {
		t.Fatal("an unchanged native grant must not replace the renewed preflight credential")
	}
	// A genuinely new native grant must survive rollback and be revalidated.
	payload["token"].(map[string]any)["refresh_token"] = "final-native-grant"
	live, _ = json.Marshal(payload)
	got, nativeRenewed = sameAccountRecoveryCredential(original, renewed, live)
	if !nativeRenewed || !bytes.Equal(got, live) {
		t.Fatal("a later native rotation must remain the recovery credential")
	}
}

func TestNativeEligibilityIsSeparateFromAuthenticatedSession(t *testing.T) {
	s := NativeAuthState{Available: true, Email: "target@example.com", Failure: "ineligible", Authenticated: true}
	if !nativeSessionReady(s) || terminalNativeAccountRejection(s, s.Email) || terminalBrokerRejection(s, s.Email) {
		t.Fatal("an authenticated account with an eligibility warning must not be rolled back solely on that warning")
	}
	for _, failure := range []string{"verificationRequired", "tosViolation", "generalError"} {
		s.Failure = failure
		if nativeSessionReady(s) {
			t.Fatalf("%s must not count as a verified session", failure)
		}
	}
	s.Failure, s.Authenticated = "ineligible", false
	if nativeSessionReady(s) || !terminalNativeAccountRejection(s, s.Email) {
		t.Fatal("missing authentication must still reject an eligibility failure")
	}
	s.Authenticated = true
	s.Email = ""
	if nativeSessionReady(s) {
		t.Fatal("a token without native identity is insufficient")
	}
}

func TestMinimalCredentialUsesOnlyMatchingArchivedIdentity(t *testing.T) {
	archive := fixtureCredential("target@example.com", false)
	var minimal map[string]any
	_ = json.Unmarshal(archive, &minimal)
	delete(minimal, "id_token")
	minimal["token"].(map[string]any)["access_token"] = "renewed-access"
	minimal["token"].(map[string]any)["expiry"] = time.Now().Add(time.Hour).Format(time.RFC3339)
	raw, _ := json.Marshal(minimal)
	fresh, err := withArchivedCredentialIdentity(raw, archive, "target@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := NativeAuthState{Available: true, Authenticated: true, Email: "target@example.com", Failure: "ineligible"}
	if !nativeCredentialReady(fresh, s, s.Email) {
		t.Fatal("verified native identity, current access and known refresh grant must remain usable")
	}
	if _, err := withArchivedCredentialIdentity(raw, archive, "other@example.com"); err == nil {
		t.Fatal("archive owner must match")
	}
	minimal["token"].(map[string]any)["refresh_token"] = "unrelated-grant"
	raw, _ = json.Marshal(minimal)
	if _, err := withArchivedCredentialIdentity(raw, archive, s.Email); err == nil {
		t.Fatal("unrelated refresh grant must never borrow an identity")
	}
}
