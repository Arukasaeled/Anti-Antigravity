package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type QuotaPoint struct {
	At        int64  `json:"at"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"reset_at"`
}

type QuotaObservation struct {
	DropPP    *int     `json:"drop_pp"`
	CoveredMS int64    `json:"covered_ms"`
	Samples   int      `json:"samples"`
	RatePPH   *float64 `json:"rate_pph"`
}

type QuotaTrend struct {
	AccountID string           `json:"account_id"`
	Pool      string           `json:"pool"`
	Window    string           `json:"window"`
	History   []QuotaPoint     `json:"history"`
	OneHour   QuotaObservation `json:"one_hour"`
	Day       QuotaObservation `json:"day"`
}

var quotaHistory = struct {
	sync.Mutex
	loaded bool
	points map[string][]QuotaPoint
}{points: make(map[string][]QuotaPoint)}

func quotaHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".2ag", "quota-history.json")
}

func loadQuotaHistoryLocked() {
	if quotaHistory.loaded {
		return
	}
	quotaHistory.loaded = true
	if raw, err := os.ReadFile(quotaHistoryPath()); err == nil {
		var points map[string][]QuotaPoint
		if json.Unmarshal(raw, &points) == nil && points != nil {
			quotaHistory.points = points
		}
	}
}

// Records the cache's real timestamp, never a polling timestamp for an unchanged cache.
func recordQuotaHistory(email string, gemini, claude QuotaWindow) {
	quotaHistory.Lock()
	defer quotaHistory.Unlock()
	loadQuotaHistoryLocked()
	changed := false
	for _, pool := range []struct {
		name  string
		value QuotaWindow
	}{{"gemini", gemini}, {"claude", claude}} {
		q := pool.value
		if !q.Available || q.UpdatedAt <= 0 || q.UpdatedAt > time.Now().Add(time.Minute).UnixMilli() {
			continue
		}
		for _, bucket := range []struct {
			name    string
			known   bool
			percent int
			reset   string
		}{{"5h", q.FiveHourKnown, q.FiveHourPercent, q.FiveHourResetAt}, {"weekly", q.WeeklyKnown, q.WeeklyPercent, q.WeeklyResetAt}} {
			if !bucket.known {
				continue
			}
			key := vaultAccountID(email) + "/" + pool.name + "/" + bucket.name
			points := quotaHistory.points[key]
			if len(points) > 0 && q.UpdatedAt <= points[len(points)-1].At {
				continue
			}
			points = append(points, QuotaPoint{At: q.UpdatedAt, Remaining: bucket.percent, ResetAt: bucket.reset})
			cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
			first := 0
			for first < len(points) && points[first].At < cutoff {
				first++
			}
			points = points[first:]
			if len(points) > 2048 {
				points = points[len(points)-2048:]
			}
			quotaHistory.points[key] = points
			changed = true
		}
	}
	if !changed {
		return
	}
	path := quotaHistoryPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	if raw, err := json.Marshal(quotaHistory.points); err == nil {
		_ = writeFileAtomic(path, raw, 0600)
	}
}

func quotaObservation(points []QuotaPoint, window time.Duration) QuotaObservation {
	cutoff, now := time.Now().Add(-window).UnixMilli(), time.Now().UnixMilli()
	var inWindow []QuotaPoint
	for _, point := range points {
		if point.At >= cutoff && point.At <= now {
			inWindow = append(inWindow, point)
		}
	}
	result := QuotaObservation{Samples: len(inWindow)}
	if len(inWindow) < 2 {
		return result
	}
	result.CoveredMS = inWindow[len(inWindow)-1].At - inWindow[0].At
	drop := 0
	comparable := 0
	for i := 1; i < len(inWindow); i++ {
		if inWindow[i].ResetAt != "" && inWindow[i-1].ResetAt != "" && inWindow[i].ResetAt != inWindow[i-1].ResetAt {
			continue
		}
		comparable++
		if delta := inWindow[i-1].Remaining - inWindow[i].Remaining; delta > 0 {
			drop += delta
		}
	}
	if comparable == 0 {
		return result
	}
	result.DropPP = &drop
	if result.CoveredMS >= int64((10*time.Minute)/time.Millisecond) {
		rate := float64(drop) / (float64(result.CoveredMS) / 3600000)
		result.RatePPH = &rate
	}
	return result
}

func ReadQuotaTrends(accounts []AccountInstance) []QuotaTrend {
	quotaHistory.Lock()
	defer quotaHistory.Unlock()
	loadQuotaHistoryLocked()
	result := []QuotaTrend{}
	for _, account := range accounts {
		id := vaultAccountID(account.Email)
		for _, pool := range []string{"gemini", "claude"} {
			for _, window := range []string{"5h", "weekly"} {
				points := append([]QuotaPoint{}, quotaHistory.points[id+"/"+pool+"/"+window]...)
				if len(points) == 0 {
					continue
				}
				sort.Slice(points, func(i, j int) bool { return points[i].At < points[j].At })
				result = append(result, QuotaTrend{AccountID: account.ID, Pool: pool, Window: window, History: points, OneHour: quotaObservation(points, time.Hour), Day: quotaObservation(points, 24*time.Hour)})
			}
		}
	}
	return result
}
