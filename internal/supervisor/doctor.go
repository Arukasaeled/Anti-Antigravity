package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type DoctorCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

// Export-safe by construction: no emails, paths, page titles, runtime bodies or credentials.
type DoctorReport struct {
	SampledAt        int64           `json:"sampled_at"`
	Checks           []DoctorCheck   `json:"checks"`
	ConnectedTargets int             `json:"connected_targets"`
	ActiveInjections int             `json:"active_injections"`
	Counters         RuntimeCounters `json:"counters"`
}

func ReadDoctorReport() DoctorReport {
	targets, counters := RuntimeManager.Snapshot()
	result := DoctorReport{SampledAt: time.Now().UnixMilli(), Counters: counters, Checks: []DoctorCheck{}}
	add := func(id, label, state, detail string) {
		result.Checks = append(result.Checks, DoctorCheck{id, label, state, detail})
	}
	host := ProbeRealHost()
	if host.ProcessFound {
		add("process", "Antigravity Process", "ok", "Running")
	} else {
		add("process", "Antigravity Process", "unknown", "No process observed by the existing host probe")
	}
	for _, target := range targets {
		if target.Connected {
			result.ConnectedTargets++
		}
		if target.Connected && target.HubActive {
			result.ActiveInjections++
		}
	}
	state := "unknown"
	if result.ConnectedTargets > 0 {
		state = "ok"
	}
	add("cdp", "CDP Targets", state, fmt.Sprintf("%d connected / %d discovered", result.ConnectedTargets, len(targets)))
	state = "unknown"
	if result.ActiveInjections > 0 {
		state = "ok"
	}
	if IsOfficialRuntime() {
		add("injection", "Runtime Injection", "unknown", "Official mode: injection disabled")
	} else {
		add("injection", "Runtime Injection", state, fmt.Sprintf("%d active; %d failed attempts", result.ActiveInjections, counters.InjectionFailures))
	}
	email, credentialErr := ReadHostLoginEmailCached()
	identityKnown := credentialErr == nil && email != ""
	if identityKnown {
		add("credential", "Credential Slot", "ok", "Readable identity metadata; credential contents omitted")
	} else {
		add("credential", "Credential Slot", "unknown", "Identity metadata unavailable")
	}
	if identityKnown && host.ProcessFound {
		add("session", "Official Session", "ok", "Local identity observed; remote authentication not probed")
	} else {
		add("session", "Official Session", "unknown", "No confirmed local session")
	}
	dir := vaultDirPath()
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		add("vault", "2Ag Vault", "unknown", "Index unavailable or not initialized")
	} else if index, err := loadVaultIndex(dir); err != nil {
		add("vault", "2Ag Vault", "error", "Index could not be read")
	} else {
		missing := 0
		for _, account := range index.Accounts {
			if _, err := os.Stat(filepath.Join(dir, account.File)); err != nil {
				missing++
			}
		}
		state := "ok"
		if missing > 0 {
			state = "error"
		}
		add("vault", "2Ag Vault", state, fmt.Sprintf("%d metadata entries; %d missing files; encrypted contents not inspected", len(index.Accounts), missing))
	}
	add("quota_api", "Quota API", "unknown", "Doctor does not probe the remote API; account quota flow handles live reads and cache fallback")
	if entries, err := os.ReadDir(cockpitCacheDir()); err == nil {
		add("quota_cache", "Quota Cache", "ok", fmt.Sprintf("Readable; %d directory entries", len(entries)))
	} else {
		add("quota_cache", "Quota Cache", "unknown", "Authorized cache unavailable")
	}
	compatible := 0
	for _, target := range targets {
		if target.Connected && target.Context != nil && target.Context.ViewAvailable {
			compatible++
		}
	}
	state = "unknown"
	if compatible > 0 {
		state = "ok"
	}
	add("dom", "DOM Compatibility", state, fmt.Sprintf("%d targets exposing conversation metadata", compatible))
	env := DetectAntigravityEnvironment()
	if env.StorageRoot != "" {
		if _, err := os.ReadDir(env.StorageRoot); err == nil {
			add("storage", "Storage Path", "ok", "Readable Antigravity storage; no write probe")
		} else {
			add("storage", "Storage Path", "unknown", "Storage unavailable")
		}
	} else {
		add("storage", "Storage Path", "unknown", "Home directory unavailable")
	}
	if env.LanguageServer != "" {
		add("language_server", "Language Server", "ok", "Installed binary discovered; process health not inferred")
	} else {
		add("language_server", "Language Server", "unknown", "Installed binary not discovered")
	}
	return result
}
