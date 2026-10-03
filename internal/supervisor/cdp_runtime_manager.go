package supervisor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/2ag/2ag/internal/patcher"
)

type ContextComposition struct {
	Partial   bool   `json:"partial"`
	Kind      string `json:"kind"`
	Items     int    `json:"items"`
	Tokens    *int64 `json:"tokens"`
	Estimated bool   `json:"estimated"`
}

type ContextItem struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Tokens    *int64 `json:"tokens"`
	Estimated bool   `json:"estimated"`
}

type ContextSnapshot struct {
	SchemaVersion     int                  `json:"schema_version"`
	Request           *GenerationUsage     `json:"request"`
	SessionUsage      *SessionUsage        `json:"session_usage"`
	ContextTokens     *int64               `json:"context_tokens"`
	ContextProvenance string               `json:"context_provenance"`
	LoadedHistory     []ContextComposition `json:"loaded_history"`
	ViewAvailable     bool                 `json:"view_available"`
	SessionID         string               `json:"session_id"`
	Model             string               `json:"model"`
	Mode              string               `json:"mode"`
	Source            string               `json:"source"`
	SampledAt         int64                `json:"sampled_at"`
	UsedTokens        *int64               `json:"used_tokens"`
	LimitTokens       *int64               `json:"limit_tokens"`
	InputTokens       *int64               `json:"input_tokens"`
	CacheReadTokens   *int64               `json:"cache_read_tokens"`
	CacheWriteTokens  *int64               `json:"cache_write_tokens"`
	OutputTokens      *int64               `json:"output_tokens"`
	UsageStep         *int64               `json:"usage_step"`
	UsageAt           *int64               `json:"usage_at"`
	CompositionScope  string               `json:"composition_scope"`
	Composition       []ContextComposition `json:"composition"`
	Items             []ContextItem        `json:"items"`
	Steps             int                  `json:"steps"`
	Turns             int                  `json:"turns"`
	ToolOutputs       int                  `json:"tool_outputs"`
	WorkspaceDirs     []string             `json:"workspace_dirs"`
	Activity          string               `json:"activity"`
	Note              string               `json:"note"`
	Partial           bool                 `json:"partial"`
}

type ContextPoint struct {
	At     int64  `json:"at"`
	Tokens int64  `json:"tokens"`
	Mode   string `json:"mode"`
	Delta  *int64 `json:"delta"`
	Cause  string `json:"cause"`
}

type RuntimeCounters struct {
	Connections       int64 `json:"connections"`
	Reconnects        int64 `json:"reconnects"`
	Injections        int64 `json:"injections"`
	InjectionFailures int64 `json:"injection_failures"`
	DOMEvents         int64 `json:"dom_events"`
	ActivityEvents    int64 `json:"activity_events"`
	ContextUpdates    int64 `json:"context_updates"`
}

type RuntimeTarget struct {
	InjectedFeatures map[string]bool  `json:"injected_features"`
	ID               string           `json:"id"`
	Title            string           `json:"title"`
	Kind             string           `json:"kind"`
	Address          string           `json:"address"`
	Connected        bool             `json:"connected"`
	HubActive        bool             `json:"hub_active"`
	LastSeen         int64            `json:"last_seen"`
	LastFailure      string           `json:"last_failure,omitempty"`
	Context          *ContextSnapshot `json:"context"`
	Timeline         []ContextPoint   `json:"timeline"`
	Counters         RuntimeCounters  `json:"counters"`
	wsURL            string
	seenConnect      bool
	pageEpoch        string
	pageDOM          int64
	pageActivity     int64
}

// CDPRuntimeManager adds target/injection/context bookkeeping to the existing persistent
// sessions. It never owns an Antigravity process and cannot launch, stop or restart one.
type CDPRuntimeManager struct {
	mu        sync.Mutex
	observeMu sync.Mutex
	targets   map[string]*RuntimeTarget
	Counters  RuntimeCounters
}

var RuntimeManager = &CDPRuntimeManager{targets: make(map[string]*RuntimeTarget)}

// On-demand snapshots also work before injection and in Official mode. This path
// enables no domains, installs no scripts, changes no DOM and only closes its own socket.
func ObserveContextReadOnly(ctx context.Context) {
	if !RuntimeManager.observeMu.TryLock() {
		return
	}
	defer RuntimeManager.observeMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	address := ResolveCDPAddr()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/json/list", nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var targets []cdpTarget
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&targets) != nil {
		return
	}
	RuntimeManager.discover(address, targets)
	for _, target := range targets {
		if !IsRealWorkbenchTarget(target) || ctx.Err() != nil {
			continue
		}
		RuntimeManager.mu.Lock()
		entry := RuntimeManager.targets[target.WebSocketDebuggerURL]
		fresh := entry != nil && entry.Context != nil && time.Now().UnixMilli()-entry.Context.SampledAt < 3000
		RuntimeManager.mu.Unlock()
		if fresh {
			continue
		}
		if session := lookupLiveSession(target.WebSocketDebuggerURL); session != nil {
			RuntimeManager.sample(target.WebSocketDebuggerURL, session)
			continue
		}
		conn, reader, err := openCDPWebSocket(ctx, target.WebSocketDebuggerURL)
		if err != nil {
			continue
		}
		session := &persistentCDPSession{wsURL: target.WebSocketDebuggerURL, conn: conn, reader: reader}
		RuntimeManager.sample(target.WebSocketDebuggerURL, session)
		_ = conn.Close()
	}
}

func (m *CDPRuntimeManager) discover(address string, targets []cdpTarget) {
	m.mu.Lock()
	active := make(map[string]bool)
	var removed []string
	for _, target := range targets {
		if !IsRealWorkbenchTarget(target) {
			continue
		}
		active[target.WebSocketDebuggerURL] = true
		entry := m.targets[target.WebSocketDebuggerURL]
		if entry == nil {
			entry = &RuntimeTarget{wsURL: target.WebSocketDebuggerURL, Timeline: []ContextPoint{}}
			m.targets[target.WebSocketDebuggerURL] = entry
		}
		entry.ID, entry.Title, entry.Kind, entry.Address = target.ID, target.Title, target.Type, address
		entry.LastSeen = time.Now().UnixMilli()
	}
	for key, entry := range m.targets {
		if entry.Address == address && !active[key] {
			delete(m.targets, key)
			removed = append(removed, key)
		}
	}
	m.mu.Unlock()
	// Lock ordering: never acquire the connection registry while holding the manager lock.
	for _, key := range removed {
		if session := lookupLiveSession(key); session != nil {
			session.detach()
		}
	}
}

func (m *CDPRuntimeManager) connection(wsURL string, connected bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.targets[wsURL]
	if entry == nil {
		return
	}
	if connected && !entry.Connected {
		entry.Counters.Connections++
		m.Counters.Connections++
		if entry.seenConnect {
			entry.Counters.Reconnects++
			m.Counters.Reconnects++
		}
		entry.seenConnect = true
	}
	entry.Connected = connected
	if !connected {
		entry.HubActive = false
		entry.InjectedFeatures = nil
	}
}

func (m *CDPRuntimeManager) injection(wsURL string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.targets[wsURL]
	if err != nil {
		m.Counters.InjectionFailures++
		if entry != nil {
			entry.Counters.InjectionFailures++
			entry.LastFailure = "Runtime injection failed"
			entry.HubActive = false
			entry.InjectedFeatures = nil
		}
		return
	}
	m.Counters.Injections++
	if entry != nil {
		entry.Counters.Injections++
		entry.LastFailure = ""
	}
}

func (m *CDPRuntimeManager) sample(wsURL string, session *persistentCDPSession) {
	// Prefer the installed runtime's cached, event-driven observation. The fallback is pure read-only.
	expression := `(typeof window.__2ag?.runtimeSnapshot === 'function' ? (()=>{const s=window.__2ag.runtimeSnapshot();return s?.context?.schema_version===2?s:null;})() : null)`
	value, err := session.evalRaw(9010, expression)
	if err == nil && value == nil {
		value, err = session.evalRaw(9011, `({context:`+patcher.ContextProbeExpression()+`,hub:false})`)
	}
	if err != nil {
		session.detach()
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	var sample struct {
		Features       map[string]bool  `json:"features"`
		Context        *ContextSnapshot `json:"context"`
		Hub            bool             `json:"hub"`
		Epoch          string           `json:"epoch"`
		DOMEvents      int64            `json:"dom_events"`
		ActivityEvents int64            `json:"activity_events"`
	}
	if json.Unmarshal(raw, &sample) != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.targets[wsURL]
	if entry == nil {
		return
	}
	entry.HubActive = sample.Hub
	entry.InjectedFeatures = sample.Features
	if sample.Epoch != entry.pageEpoch {
		entry.pageEpoch, entry.pageDOM, entry.pageActivity = sample.Epoch, 0, 0
	}
	if delta := sample.DOMEvents - entry.pageDOM; delta > 0 {
		m.Counters.DOMEvents += delta
		entry.Counters.DOMEvents += delta
	}
	if delta := sample.ActivityEvents - entry.pageActivity; delta > 0 {
		m.Counters.ActivityEvents += delta
		entry.Counters.ActivityEvents += delta
	}
	entry.pageDOM, entry.pageActivity = sample.DOMEvents, sample.ActivityEvents
	next := sample.Context
	if next == nil {
		entry.Context = nil
		entry.Timeline = nil
		return
	}
	previous := entry.Context
	if previous == nil || previous.SessionID != next.SessionID || previous.Model != next.Model {
		entry.Timeline = []ContextPoint{}
		previous = nil
	}
	if next.UsedTokens != nil && (previous == nil || previous.UsedTokens == nil || *previous.UsedTokens != *next.UsedTokens || previous.Mode != next.Mode || !sameOptionalInt(previous.UsageStep, next.UsageStep)) {
		point := ContextPoint{At: next.SampledAt, Tokens: *next.UsedTokens, Mode: next.Mode}
		if previous != nil && previous.UsedTokens != nil && previous.Mode == next.Mode {
			delta := *next.UsedTokens - *previous.UsedTokens
			point.Delta = &delta
			point.Cause = "最近请求更新 · 归因未知"
			if delta < 0 {
				point.Cause = "最近请求输入下降 · 归因未知"
			}
			if next.CompositionScope == "active_prompt" && previous.CompositionScope == "active_prompt" {
				largest, kind := int64(0), ""
				old := make(map[string]int64)
				for _, group := range previous.Composition {
					if group.Tokens != nil {
						old[group.Kind] = *group.Tokens
					}
				}
				for _, group := range next.Composition {
					if group.Tokens != nil && *group.Tokens-old[group.Kind] > largest {
						largest, kind = *group.Tokens-old[group.Kind], group.Kind
					}
				}
				if kind != "" {
					point.Cause = "≈ 最大组成增长: " + kind
				}
			}
		}
		entry.Timeline = append(entry.Timeline, point)
		if len(entry.Timeline) > 120 {
			entry.Timeline = entry.Timeline[len(entry.Timeline)-120:]
		}
		entry.Counters.ContextUpdates++
		m.Counters.ContextUpdates++
	}
	entry.Context = next
}

func sameOptionalInt(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (m *CDPRuntimeManager) Snapshot() ([]RuntimeTarget, RuntimeCounters) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := make([]RuntimeTarget, 0, len(m.targets))
	for _, entry := range m.targets {
		copy := *entry
		copy.Timeline = append([]ContextPoint{}, entry.Timeline...)
		entries = append(entries, copy)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries, m.Counters
}
