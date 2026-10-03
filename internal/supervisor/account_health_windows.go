//go:build windows

package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type AccountObservation struct {
	Native       NativeAuthState `json:"native"`
	LastVerified string          `json:"last_verified,omitempty"`
	LastRefresh  string          `json:"last_refresh,omitempty"`
	AccessExpiry string          `json:"access_expiry,omitempty"`
}

var accountHealth struct {
	sync.Mutex
	loaded       bool
	observations map[string]AccountObservation
	checked      time.Time
	pid          int
	stage        string
	owner        string
	current      NativeAuthState
}

func healthPath() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".2ag", "workspace", "account-health.json")
}

// Caller holds accountHealth mutex. Offline startup needs these facts too.
func loadHealthMetadataLocked() {
	if accountHealth.loaded {
		return
	}
	accountHealth.observations = map[string]AccountObservation{}
	if data, err := os.ReadFile(healthPath()); err == nil {
		_ = json.Unmarshal(data, &accountHealth.observations)
	}
	if accountHealth.observations == nil {
		accountHealth.observations = map[string]AccountObservation{}
	}
	accountHealth.loaded = true
}
func healthNativeForOwner(state NativeAuthState, email string) NativeAuthState {
	if (state.Valid || state.Authenticated) && (email == "" || !strings.EqualFold(state.Email, email)) {
		state.Valid = false
		state.Authenticated = false
		state.Failure = "identity-mismatch"
		state.Message = "Native identity does not match the stored account."
	}
	return state
}
func recordNativeObservation(state NativeAuthState) {
	if !state.Available {
		return
	}
	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		return
	}
	email := strings.ToLower(nativeCredentialOwner(raw))
	if email == "" {
		return
	}
	accountHealth.Lock()
	defer accountHealth.Unlock()
	loadHealthMetadataLocked()
	state = healthNativeForOwner(state, email)
	previous := accountHealth.observations[email]
	now := time.Now().Format(time.RFC3339)
	next := previous
	next.Native = state
	next.LastVerified = now
	if v, ok := parseCredentialBlobView(raw); ok && v.Token != nil {
		// A newly observed expiry is evidence of refresh, not startup itself.
		if previous.AccessExpiry != "" && previous.AccessExpiry != v.Token.Expiry && nativeSessionReady(state) && strings.EqualFold(state.Email, email) {
			before, _ := time.Parse(time.RFC3339Nano, previous.AccessExpiry)
			after, _ := time.Parse(time.RFC3339Nano, v.Token.Expiry)
			if after.After(before) {
				next.LastRefresh = now
			}
		}
		next.AccessExpiry = v.Token.Expiry
	}
	accountHealth.observations[email] = next
	// Persist only this explicit metadata struct, never the credential view.
	data, _ := json.Marshal(accountHealth.observations)
	dir := filepath.Dir(healthPath())
	_ = os.MkdirAll(dir, 0700)
	f, err := os.CreateTemp(dir, ".health-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(f.Name(), healthPath())
	}
}

func AccountHealthSnapshot(requested string) map[string]any {
	raw, _ := readAntigravityCredentialRaw()
	current := nativeCredentialOwner(raw)
	host := ProbeRealHost()
	tx := CurrentAccountTransaction()
	accountHealth.Lock()
	refresh := time.Since(accountHealth.checked) > 8*time.Second || accountHealth.pid != host.PID || accountHealth.stage != tx.Stage || accountHealth.owner != current
	native := accountHealth.current
	accountHealth.Unlock()
	if refresh {
		native = NativeAuthState{Source: "Antigravity LanguageServer/GetAuthStatus"}
		if host.ProcessFound {
			native = ReadNativeAuthState()
		}
		accountHealth.Lock()
		accountHealth.current = healthNativeForOwner(native, current)
		accountHealth.owner = current
		accountHealth.checked = time.Now()
		accountHealth.pid = host.PID
		accountHealth.stage = tx.Stage
		accountHealth.Unlock()
	}
	email := strings.TrimSpace(requested)
	if email == "" {
		email = current
	}
	vaultState := "missing"
	var archived string
	for _, a := range ListVaultAccountEntries() {
		if strings.EqualFold(a.Email, email) {
			vaultState = "archived"
			archived = a.UpdatedAt
			break
		}
	}
	vault, _ := ReadVaultCredential(email)
	_, complete := credentialRestorableEmail(vault)
	refreshCredential := false
	if v, ok := parseCredentialBlobView(vault); ok && v.Token != nil {
		refreshCredential = v.Token.RefreshToken != ""
	}
	if vaultState == "archived" && !complete {
		vaultState = "incomplete"
	}
	accountHealth.Lock()
	loadHealthMetadataLocked()
	observation := accountHealth.observations[strings.ToLower(email)]
	accountHealth.Unlock()
	if !strings.EqualFold(email, current) {
		native = observation.Native
	}
	native = healthNativeForOwner(native, email)
	profile := ""
	if host.ProcessFound && !IsOfficialRuntime() {
		home, _ := os.UserHomeDir()
		profile = filepath.Join(home, ".2ag", "profiles", sanitizeEmail(current))
	}
	_, pendingErr := os.Stat(refreshCandidatePath(email))
	return map[string]any{"refresh_pending": pendingErr == nil, "account": email, "current_account": current, "is_current": strings.EqualFold(email, current), "native": native, "last_verified": observation.LastVerified, "last_refresh": observation.LastRefresh, "vault": vaultState, "vault_updated_at": archived, "has_refresh_credential": refreshCredential, "credential_complete": complete, "access_expiry": observation.AccessExpiry, "host": host, "profile": profile, "transaction": tx, "accounts": ListVaultAccounts(), "native_live": host.ProcessFound && strings.EqualFold(email, current), "last_native": observation.Native}
}
