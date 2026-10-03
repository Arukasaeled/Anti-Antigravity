package supervisor

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func fixtureIdentity(email string) string {
	raw, _ := json.Marshal(map[string]any{"email": email, "aud": "123-native.apps.googleusercontent.com", "exp": time.Now().Add(time.Hour).Unix()})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(raw) + "."
}

func fixtureCredential(email string, nested bool) []byte {
	token := map[string]any{"access_token": "fixture-old-access", "refresh_token": "fixture-old-refresh", "expiry": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)}
	payload := map[string]any{"token": token, "auth_method": "consumer", "future_metadata": "preserve"}
	if nested {
		token["id_token"] = fixtureIdentity(email)
	} else {
		payload["id_token"] = fixtureIdentity(email)
	}
	raw, _ := json.Marshal(payload)
	return raw
}

func TestNestedNativeCredentialIdentity(t *testing.T) {
	raw := fixtureCredential("target@example.com", true)
	owner, ok := credentialRestorableEmail(raw)
	if !ok || owner != "target@example.com" {
		t.Fatal("nested native ID must preserve complete-login validation")
	}
	if got := emailFromCredentialPayload(raw); got != owner {
		t.Fatal("all credential readers must agree")
	}
}

func TestConflictingNativeCredentialIdentities(t *testing.T) {
	var payload map[string]any
	_ = json.Unmarshal(fixtureCredential("target@example.com", true), &payload)
	payload["id_token"] = fixtureIdentity("other@example.com")
	raw, _ := json.Marshal(payload)
	if _, ok := credentialRestorableEmail(raw); ok {
		t.Fatal("conflicting top and nested identities must be rejected")
	}
}
