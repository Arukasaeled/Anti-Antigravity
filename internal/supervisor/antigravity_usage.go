package supervisor

// Narrow reader for the current Antigravity conversation schema. No credentials,
// prompt bodies, database migrations, or guessed model/window defaults.
import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type GenerationUsage struct {
	SessionID         string   `json:"session_id"`
	ResponseID        string   `json:"response_id"`
	Timestamp         *int64   `json:"timestamp"`
	Model             string   `json:"model"`
	Provider          string   `json:"provider"`
	Input             *int64   `json:"input"`
	UnclassifiedInput *int64   `json:"unclassified_input"`
	Output            *int64   `json:"output"`
	CacheRead         *int64   `json:"cache_read"`
	CacheWrite        *int64   `json:"cache_write"`
	Reasoning         *int64   `json:"reasoning"`
	Total             *int64   `json:"total"`
	ObservedTotal     *int64   `json:"observed_total"`
	RequestInput      *int64   `json:"request_input"`
	ObservedInput     *int64   `json:"observed_input"`
	CacheHitRate      *float64 `json:"cache_hit_rate"`
	Provenance        string   `json:"provenance"`
	Source            string   `json:"source"`
	Partial           bool     `json:"partial"`
}

type SessionUsage struct {
	SessionID         string            `json:"session_id"`
	Total             *int64            `json:"total"`
	ObservedTotal     *int64            `json:"observed_total"`
	Input             *int64            `json:"input"`
	UnclassifiedInput *int64            `json:"unclassified_input"`
	ObservedBreakdown UsageBreakdown    `json:"observed_breakdown"`
	Output            *int64            `json:"output"`
	CacheRead         *int64            `json:"cache_read"`
	CacheWrite        *int64            `json:"cache_write"`
	Reasoning         *int64            `json:"reasoning"`
	CacheHitRate      *float64          `json:"cache_hit_rate"`
	RequestCount      *int64            `json:"request_count"`
	ObservedRequests  int               `json:"observed_requests"`
	Models            []string          `json:"models"`
	StartedAt         *int64            `json:"started_at"`
	UpdatedAt         *int64            `json:"updated_at"`
	Latest            *GenerationUsage  `json:"latest_request"`
	Recent            []GenerationUsage `json:"recent_requests"`
	Provenance        string            `json:"provenance"`
	Sources           []string          `json:"sources"`
	DedupConfidence   string            `json:"dedup_confidence"`
	Partial           bool              `json:"partial"`
	Note              string            `json:"note,omitempty"`
}

// These fields partition counted tokens. Reasoning is an Output subset.
// ObservedBreakdown uses exactly the same deduplicated responses as ObservedTotal.
type UsageBreakdown struct {
	Input             *int64 `json:"input"`
	UnclassifiedInput *int64 `json:"unclassified_input"`
	Output            *int64 `json:"output"`
	CacheRead         *int64 `json:"cache_read"`
	CacheWrite        *int64 `json:"cache_write"`
	Reasoning         *int64 `json:"reasoning"`
}

// Search the user's actual existing stores, in authoritative priority order.
func discoverConversationStores() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var stores []string
	for _, name := range []string{"antigravity", "antigravity-ide", "antigravity-backup"} {
		root := filepath.Join(home, ".gemini", name, "conversations")
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			stores = append(stores, root)
		}
	}
	return stores
}

type usageCacheEntry struct {
	at    time.Time
	value SessionUsage
}

var nativeUsageCache = struct {
	sync.Mutex
	entries map[string]usageCacheEntry
}{entries: make(map[string]usageCacheEntry)}

// Cache only in memory for five seconds; no archive hashes or rewritten caches.
func ReadAntigravitySessionUsage(id string, stores ...string) SessionUsage {
	if !validSessionID(id) {
		return SessionUsage{SessionID: id, Provenance: "Unavailable", Note: "invalid Antigravity session ID"}
	}
	store := ""
	if len(stores) > 0 {
		store = stores[0]
	}
	source, err := resolveSessionSource(id, store)
	if err != nil {
		return SessionUsage{SessionID: id, Provenance: "Unavailable", Note: err.Error()}
	}
	key := id + "|" + source.Store
	nativeUsageCache.Lock()
	defer nativeUsageCache.Unlock()
	if cached, ok := nativeUsageCache.entries[key]; ok && time.Since(cached.at) < 5*time.Second {
		return cached.value
	}
	result := readAntigravitySessionUsage(id, filepath.Join(source.Root, "conversations"))
	if len(nativeUsageCache.entries) >= 128 {
		nativeUsageCache.entries = make(map[string]usageCacheEntry)
	}
	nativeUsageCache.entries[key] = usageCacheEntry{time.Now(), result}
	return result
}

// Decode only wire primitives needed for usage/timestamps. Malformed data fails
// closed; length and field bounds prevent unbounded protobuf traversal.
type usageWire struct {
	value  int64
	bytes  []byte
	scalar bool
}

func usageFields(data []byte) (map[int]usageWire, error) {
	if len(data) > 16<<20 {
		return nil, fmt.Errorf("metadata exceeds read limit")
	}
	fields := make(map[int]usageWire)
	for count := 0; len(data) > 0; count++ {
		if count > 4096 {
			return nil, fmt.Errorf("too many metadata fields")
		}
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return nil, fmt.Errorf("invalid metadata tag")
		}
		data = data[n:]
		field := int(tag >> 3)
		v := usageWire{}
		switch tag & 7 {
		case 0:
			x, k := binary.Uvarint(data)
			if k <= 0 || x > uint64(^uint64(0)>>1) {
				return nil, fmt.Errorf("invalid counter")
			}
			v.value = int64(x)
			v.scalar = true
			data = data[k:]
		case 1:
			if len(data) < 8 {
				return nil, fmt.Errorf("truncated fixed64")
			}
			data = data[8:]
		case 2:
			x, k := binary.Uvarint(data)
			if k <= 0 || x > uint64(len(data)-k) {
				return nil, fmt.Errorf("truncated metadata")
			}
			data = data[k:]
			v.bytes = data[:int(x)]
			data = data[int(x):]
		case 5:
			if len(data) < 4 {
				return nil, fmt.Errorf("truncated fixed32")
			}
			data = data[4:]
		default:
			return nil, fmt.Errorf("unsupported metadata wire type")
		}
		fields[field] = v
	}
	return fields, nil
}
func usageNumber(fields map[int]usageWire, field int) *int64 {
	if v, ok := fields[field]; ok && v.scalar {
		n := v.value
		return &n
	}
	return nil
}
func usageTimestamp(data []byte) *int64 {
	f, err := usageFields(data)
	if err != nil {
		return nil
	}
	seconds := usageNumber(f, 1)
	if seconds == nil || *seconds < 946684800 || *seconds > 4102444800 {
		return nil
	}
	n := *seconds * 1000
	if nanos := usageNumber(f, 2); nanos != nil && *nanos < 1000000000 {
		n += *nanos / 1000000
	}
	return &n
}
func tokenSum(values ...*int64) (*int64, *int64) {
	n := int64(0)
	known := 0
	for _, v := range values {
		if v != nil {
			if *v < 0 || n > int64(^uint64(0)>>1)-*v {
				return nil, nil
			}
			n += *v
			known++
		}
	}
	if known == 0 {
		return nil, nil
	}
	observed := n
	if known != len(values) {
		return nil, &observed
	}
	return &n, &observed
}
func normalizeGeneration(g *GenerationUsage) {
	g.Total, g.ObservedTotal = tokenSum(g.Input, g.UnclassifiedInput, g.Output, g.CacheRead, g.CacheWrite)
	g.RequestInput, g.ObservedInput = tokenSum(g.Input, g.UnclassifiedInput, g.CacheRead, g.CacheWrite)
	g.Partial = g.Total == nil
	g.CacheHitRate = nil
	if g.RequestInput != nil && *g.RequestInput > 0 && g.UnclassifiedInput != nil && *g.UnclassifiedInput == 0 && g.CacheRead != nil && g.CacheWrite != nil && (*g.CacheRead > 0 || *g.CacheWrite > 0) {
		rate := float64(*g.CacheRead) / float64(*g.RequestInput)
		g.CacheHitRate = &rate
	}
}

// ReconcileRequestUsage enriches only the same native response. It never adds
// Runtime samples to session lifetime or merges two requests by time/model.
func ReconcileRequestUsage(runtime *GenerationUsage, session SessionUsage) *GenerationUsage {
	if runtime == nil {
		if session.Latest == nil {
			return nil
		}
		copy := *session.Latest
		return &copy
	}
	copy := *runtime
	candidates := append([]GenerationUsage{}, session.Recent...)
	if session.Latest != nil {
		candidates = append(candidates, *session.Latest)
	}
	for _, stored := range candidates {
		if copy.ResponseID == "" || copy.ResponseID != stored.ResponseID || copy.SessionID != stored.SessionID {
			continue
		}
		if copy.Input == nil {
			copy.Input = stored.Input
		}
		if copy.UnclassifiedInput == nil {
			copy.UnclassifiedInput = stored.UnclassifiedInput
		}
		if copy.Output == nil {
			copy.Output = stored.Output
		}
		if copy.CacheRead == nil {
			copy.CacheRead = stored.CacheRead
		}
		if copy.CacheWrite == nil {
			copy.CacheWrite = stored.CacheWrite
		}
		if copy.Reasoning == nil {
			copy.Reasoning = stored.Reasoning
		}
		copy.Source += " · matched responseId: " + stored.Source
		break
	}
	normalizeGeneration(&copy)
	return &copy
}

type nativeStoreUsage struct {
	generations []GenerationUsage
	startedAt   *int64
	partial     bool
}

func readGenerationUsage(path, id string) (nativeStoreUsage, error) {
	store := nativeStoreUsage{}
	byResponse, byGeneration := make(map[string]*int64), make(map[int64]*int64)
	err := readNativeUsageRows(path, "SELECT idx, metadata FROM steps WHERE step_type = 15 ORDER BY idx", func(_ int64, data []byte) error {
		fields, err := usageFields(data)
		if err != nil {
			store.partial = true
			return nil
		}
		at := usageTimestamp(fields[1].bytes)
		if at == nil {
			return nil
		}
		usage, _ := usageFields(fields[9].bytes)
		if response := string(usage[11].bytes); response != "" {
			byResponse[response] = at
		}
		info, _ := usageFields(fields[20].bytes)
		if idx := usageNumber(info, 3); idx != nil {
			byGeneration[*idx] = at
		}
		return nil
	})
	if err != nil {
		store.partial = true
	}
	_ = readNativeUsageRows(path, "SELECT 0, data FROM trajectory_metadata_blob WHERE id = 'main'", func(_ int64, data []byte) error {
		fields, err := usageFields(data)
		if err == nil {
			store.startedAt = usageTimestamp(fields[2].bytes)
		}
		return nil
	})
	err = readNativeUsageRows(path, "SELECT idx, data FROM gen_metadata ORDER BY idx", func(idx int64, data []byte) error {
		fields, err := usageFields(data)
		if err != nil {
			store.partial = true
			return nil
		}
		chat, err := usageFields(fields[1].bytes)
		if err != nil {
			store.partial = true
			return nil
		}
		if len(chat[4].bytes) == 0 {
			store.partial = true
			return nil
		}
		usage, err := usageFields(chat[4].bytes)
		if err != nil {
			store.partial = true
			return nil
		}
		g := GenerationUsage{SessionID: id, Provider: "antigravity", Source: path + " · gen_metadata", Provenance: "Native", ResponseID: strings.TrimSpace(string(usage[11].bytes)), Model: string(chat[19].bytes)}
		// #1 is a readable native input counter whose category is unresolved.
		// Keep it visible as Unclassified Input; never relabel it as Cache.
		g.UnclassifiedInput = usageNumber(usage, 1)
		g.Input = usageNumber(usage, 2)
		g.Output = usageNumber(usage, 3) // includes text (#9) and reasoning (#10).
		g.CacheRead = usageNumber(usage, 5)
		g.Reasoning = usageNumber(usage, 10)
		// No confirmed cache-write field in this schema. Do not map an
		// unexplained field or copy the reference reader's hardcoded zero.
		g.Timestamp = byResponse[g.ResponseID]
		if g.Timestamp == nil {
			g.Timestamp = byGeneration[idx]
		}
		if g.Model == "" {
			g.Model = string(chat[21].bytes)
		}
		if len(g.ResponseID) > 1024 || len(g.Model) > 256 {
			store.partial = true
			return nil
		}
		normalizeGeneration(&g)
		store.generations = append(store.generations, g)
		return nil
	})
	return store, err
}

func readAntigravitySessionUsage(id string, selectedRoots ...string) SessionUsage {
	result := SessionUsage{SessionID: id, Provenance: "Unavailable", Models: []string{}, Sources: []string{}, Recent: []GenerationUsage{}, DedupConfidence: "lower"}
	seen := make(map[string]int)
	generations := []GenerationUsage{}
	primaryRead := false
	incomplete := false
	lower := false
	unidentifiedPrimary := false
	roots := selectedRoots
	if len(roots) == 0 {
		roots = discoverConversationStores()
	}
	for _, root := range roots {
		path := filepath.Join(root, id+".db")
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		store, err := readGenerationUsage(path, id)
		if err != nil {
			incomplete = true
			if len(store.generations) == 0 {
				continue
			}
		}
		result.Sources = append(result.Sources, path)
		incomplete = incomplete || store.partial
		if primaryRead && unidentifiedPrimary {
			incomplete = true
			continue
		}
		if !primaryRead {
			result.StartedAt = store.startedAt
		}
		for _, g := range store.generations {
			if g.ResponseID == "" {
				lower = true
				if !primaryRead {
					unidentifiedPrimary = true
				}
				// Unidentified rows are only included from the authoritative
				// first readable store. No synthetic content/time dedup key.
				if primaryRead {
					incomplete = true
					continue
				}
			} else if i, ok := seen[g.ResponseID]; ok {
				// A repeated response can fill missing telemetry, never add
				// another request. Authoritative non-null fields win.
				old := &generations[i]
				if old.Input == nil {
					old.Input = g.Input
				}
				if old.UnclassifiedInput == nil {
					old.UnclassifiedInput = g.UnclassifiedInput
				}
				if old.Output == nil {
					old.Output = g.Output
				}
				if old.CacheRead == nil {
					old.CacheRead = g.CacheRead
				}
				if old.CacheWrite == nil {
					old.CacheWrite = g.CacheWrite
				}
				if old.Reasoning == nil {
					old.Reasoning = g.Reasoning
				}
				if old.Timestamp == nil {
					old.Timestamp = g.Timestamp
				}
				if old.Model == "" {
					old.Model = g.Model
				}
				normalizeGeneration(old)
				continue
			} else {
				seen[g.ResponseID] = len(generations)
			}
			generations = append(generations, g)
		}
		primaryRead = true
	}
	if !primaryRead {
		result.Note = "原生会话库不可读或未找到；未使用 Runtime 切片冒充会话累计。"
		return result
	}
	result.Provenance = "Native"
	result.DedupConfidence = "responseId"
	if lower {
		result.DedupConfidence = "lower"
		incomplete = true
	}
	result.ObservedRequests = len(generations)
	if !incomplete {
		count := int64(len(generations))
		result.RequestCount = &count
	}
	models := make(map[string]bool)
	all := func(get func(GenerationUsage) *int64) *int64 {
		if len(generations) == 0 || incomplete {
			return nil
		}
		sum := int64(0)
		for _, g := range generations {
			v := get(g)
			if v == nil || sum > int64(^uint64(0)>>1)-*v {
				return nil
			}
			sum += *v
		}
		return &sum
	}
	result.Input = all(func(g GenerationUsage) *int64 { return g.Input })
	result.UnclassifiedInput = all(func(g GenerationUsage) *int64 { return g.UnclassifiedInput })
	result.Output = all(func(g GenerationUsage) *int64 { return g.Output })
	result.CacheRead = all(func(g GenerationUsage) *int64 { return g.CacheRead })
	result.CacheWrite = all(func(g GenerationUsage) *int64 { return g.CacheWrite })
	result.Reasoning = all(func(g GenerationUsage) *int64 { return g.Reasoning })
	result.Total, _ = tokenSum(result.Input, result.UnclassifiedInput, result.Output, result.CacheRead, result.CacheWrite)
	counted := []GenerationUsage{}
	for _, g := range generations {
		// Unidentified rows could repeat a response. They are visible as raw
		// telemetry, but cannot contribute to a mathematical session lower bound.
		if g.ResponseID != "" && g.ObservedTotal != nil {
			counted = append(counted, g)
		}
		if g.Model != "" {
			models[g.Model] = true
		}
		if g.Timestamp != nil && (result.UpdatedAt == nil || *g.Timestamp > *result.UpdatedAt) {
			result.UpdatedAt = g.Timestamp
		}
	}
	observed := func(get func(GenerationUsage) *int64) *int64 {
		values := make([]*int64, 0, len(counted))
		for _, g := range counted {
			values = append(values, get(g))
		}
		_, sum := tokenSum(values...)
		return sum
	}
	result.ObservedBreakdown = UsageBreakdown{
		Input:             observed(func(g GenerationUsage) *int64 { return g.Input }),
		UnclassifiedInput: observed(func(g GenerationUsage) *int64 { return g.UnclassifiedInput }),
		Output:            observed(func(g GenerationUsage) *int64 { return g.Output }),
		CacheRead:         observed(func(g GenerationUsage) *int64 { return g.CacheRead }),
		CacheWrite:        observed(func(g GenerationUsage) *int64 { return g.CacheWrite }),
		Reasoning:         observed(func(g GenerationUsage) *int64 { return g.Reasoning }),
	}
	b := result.ObservedBreakdown
	_, result.ObservedTotal = tokenSum(b.Input, b.UnclassifiedInput, b.Output, b.CacheRead, b.CacheWrite)
	denominator, _ := tokenSum(result.Input, result.UnclassifiedInput, result.CacheRead, result.CacheWrite)
	if denominator != nil && *denominator > 0 && result.UnclassifiedInput != nil && *result.UnclassifiedInput == 0 && (*result.CacheRead > 0 || *result.CacheWrite > 0) {
		rate := float64(*result.CacheRead) / float64(*denominator)
		result.CacheHitRate = &rate
	}
	for model := range models {
		result.Models = append(result.Models, model)
	}
	sort.Strings(result.Models)
	// Unknown timestamps are not replaced by the file's mtime. Latest means
	// latest timestamped native response, not an invented request time.
	sort.SliceStable(generations, func(i, j int) bool {
		if generations[i].Timestamp == nil {
			return false
		}
		if generations[j].Timestamp == nil {
			return true
		}
		return *generations[i].Timestamp > *generations[j].Timestamp
	})
	if len(generations) > 0 && generations[0].Timestamp != nil {
		g := generations[0]
		result.Latest = &g
	}
	for _, g := range generations {
		if len(result.Recent) == 5 {
			break
		}
		result.Recent = append(result.Recent, g)
	}
	result.Partial = incomplete || result.Total == nil
	if result.Partial {
		result.Note = "≥ 表示已读取的原生 Token 下界；字段 #1 单列为 Unclassified Input，类别归属尚未确认。Cache Write 未提供；Reasoning 已包含在 Output 中。"
		if lower {
			result.Note += " 无 responseId 的 usage 未计入累计下界。"
		}
	}
	return result
}

// List enrichment reads workspace metadata and the last generation's timestamp,
// never message payloads or the complete generation history.
func readNativeSessionMetadata(path string) (string, *int64, *int64) {
	project := ""
	var created, updated *int64
	_ = readNativeUsageRows(path, "SELECT 0, data FROM trajectory_metadata_blob WHERE id = 'main'", func(_ int64, data []byte) error {
		fields, err := usageFields(data)
		if err != nil {
			return err
		}
		created = usageTimestamp(fields[2].bytes)
		workspace, _ := usageFields(fields[1].bytes)
		project = workspaceURIPath(string(workspace[1].bytes))
		return nil
	})
	_ = readNativeUsageRows(path, "SELECT idx, metadata FROM steps WHERE step_type = 15 ORDER BY idx DESC LIMIT 1", func(_ int64, data []byte) error {
		fields, err := usageFields(data)
		if err == nil {
			updated = usageTimestamp(fields[1].bytes)
		}
		return nil
	})
	return strings.TrimSpace(project), created, updated
}
