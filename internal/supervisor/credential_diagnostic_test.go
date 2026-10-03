//go:build windows

package supervisor

import (
	"os"
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
	state := readNativeAuthRPC()
	current, err := ReadHostLoginEmail()
	if err != nil || !state.Available || !state.Valid || state.Email != current {
		t.Fatalf("native RPC available=%v valid=%v identityMatches=%v failure=%s", state.Available, state.Valid, state.Email == current, state.Failure)
	}
	t.Log("native local RPC: valid login and runtime identity match Credential Manager")
}
