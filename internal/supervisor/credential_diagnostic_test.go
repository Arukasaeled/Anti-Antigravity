//go:build windows

package supervisor

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// Read-only opt-in inspection. Values of credentials are never logged.
func TestLocalCredentialMetadata(t *testing.T) {
	if os.Getenv("TWOAG_CREDENTIAL_METADATA") != "1" {
		t.Skip("local read-only diagnostic")
	}
	inspect := func(label string, raw []byte) {
		v, ok := parseCredentialBlobView(raw)
		if !ok {
			t.Logf("%s: parseable=false bytes=%d", label, len(raw))
			return
		}
		access, refresh, expired := false, false, false
		expiry := "unknown"
		if v.Token != nil {
			access = v.Token.AccessToken != ""
			refresh = v.Token.RefreshToken != ""
			if ex, e := time.Parse(time.RFC3339, v.Token.Expiry); e == nil {
				expiry = ex.UTC().Format(time.RFC3339)
				expired = ex.Before(time.Now())
			}
		}
		claims := decodeJWTPayload(v.IDToken)
		idExpiry := "unknown"
		if ex, ok := claims["exp"].(float64); ok {
			idExpiry = time.Unix(int64(ex), 0).UTC().Format(time.RFC3339)
		}
		_, fresh := credentialUsableEmail(raw)
		_, restorable := credentialRestorableEmail(raw)
		t.Logf("%s: bytes=%d access=%v refresh=%v id=%v identity=%v accessExpiry=%s expired=%v idExpiry=%s freshLogin=%v restorable=%v", label, len(raw), access, refresh, v.IDToken != "", emailFromCredentialPayload(raw) != "", expiry, expired, idExpiry, fresh, restorable)
	}
	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		t.Fatal(err)
	}
	inspect("current", raw)
	for _, e := range ListVaultAccountEntries() {
		raw, err := ReadVaultCredential(e.Email)
		if err != nil {
			t.Fatal(err)
		}
		inspect(e.Email, raw)
	}
}

func TestLocalNativeRPC(t *testing.T) {
	if os.Getenv("TWOAG_NATIVE_RPC") != "1" {
		t.Skip("read-only local RPC diagnostic")
	}
	if pid, _ := strconv.Atoi(os.Getenv("TWOAG_NATIVE_ROOT_PID")); pid > 0 {
		// Bind only this diagnostic process; do not rewrite Manager's PID file.
		managedHostMu.Lock()
		previous := managedHostPID
		managedHostPID = pid
		managedHostMu.Unlock()
		defer func() { managedHostMu.Lock(); managedHostPID = previous; managedHostMu.Unlock() }()
	}
	state := readNativeAuthRPC()
	current, err := ReadHostLoginEmail()
	if err != nil || !nativeSessionReady(state) || state.Email != current {
		t.Fatalf("native RPC available=%v valid=%v identityMatches=%v failure=%s", state.Available, state.Valid, state.Email == current, state.Failure)
	}
	raw, _ := readAntigravityCredentialRaw()
	complete, err := credentialForArchive(raw, current)
	if err != nil || !nativeCredentialReady(complete, state, current) {
		t.Fatal("native session must match a complete renewable credential")
	}
	t.Logf("native local RPC: identity matches Credential Manager; authenticated=%v eligibilityValid=%v failure=%s credentialReady=true", state.Authenticated, state.Valid, state.Failure)
}
