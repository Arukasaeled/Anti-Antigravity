package config

import (
	"errors"
	"sync"
)

// Runtime serializes changes from the in-app control bridge and the launcher.
type Runtime struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func NewRuntime(path string, cfg Config) *Runtime { return &Runtime{path: path, cfg: cfg} }

func (r *Runtime) Snapshot() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copy := r.cfg
	copy.Network.EndpointOverrides = append([]EndpointOverride(nil), r.cfg.Network.EndpointOverrides...)
	copy.Network.RuleTargets = append([]RuleTarget(nil), r.cfg.Network.RuleTargets...)
	copy.Privacy.BlockedHosts = append([]string(nil), r.cfg.Privacy.BlockedHosts...)
	copy.Plugins = append([]Plugin(nil), r.cfg.Plugins...)
	copy.EnvOverrides = cloneMap(r.cfg.EnvOverrides)
	return copy
}

func (r *Runtime) Update(change func(*Config) error) (Config, error) {
	if r == nil || change == nil {
		return Config{}, errors.New("config runtime update is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.cfg
	next.Network.EndpointOverrides = append([]EndpointOverride(nil), r.cfg.Network.EndpointOverrides...)
	next.Network.RuleTargets = append([]RuleTarget(nil), r.cfg.Network.RuleTargets...)
	next.Privacy.BlockedHosts = append([]string(nil), r.cfg.Privacy.BlockedHosts...)
	next.Plugins = append([]Plugin(nil), r.cfg.Plugins...)
	next.EnvOverrides = cloneMap(r.cfg.EnvOverrides)
	if err := change(&next); err != nil {
		return Config{}, err
	}
	if err := Save(r.path, next); err != nil {
		return Config{}, err
	}
	r.cfg = next
	return next, nil
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
