package supervisor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 真正走到网络层的并发验证：注入新鲜假凭据，让每个账号都必须尝试连接。
func TestBatchProbeConcurrentUnderNetworkStall(t *testing.T) {
	const stall = 1200 * time.Millisecond
	stallClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				select {
				case <-time.After(stall):
					return nil, errors.New("simulated stall")
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		},
	}
	oldClient := quotaHTTPClient
	quotaHTTPClient = stallClient
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	oldResolver := quotaCredentialResolver
	quotaCredentialResolver = func(email string) []byte {
		return []byte(`{"token":{"access_token":"ya29.fresh","refresh_token":"1//0x","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `","auth_method":"consumer"}`)
	}
	t.Cleanup(func() { quotaCredentialResolver = oldResolver })

	emails := []string{"a@example.com", "b@example.com", "c@example.com", "d@example.com"}

	st := time.Now()
	res := probeQuotaBatch(emails)
	el := time.Since(st)
	t.Logf("batch of 4 under %v stall => %v", stall, el.Round(time.Millisecond))

	serial := time.Duration(len(emails)) * stall
	if el >= serial {
		t.Fatalf("batch %v not faster than serial estimate %v — concurrency ineffective", el, serial)
	}
	for _, e := range emails {
		r, ok := res[e]
		if !ok {
			t.Fatalf("missing result for %s", e)
		}
		if r.Status.Status == "ok" {
			t.Fatalf("stalled network must not report ok: %+v", r.Status)
		}
		t.Logf("  %s status=%s source=%s msg=%s", e, r.Status.Status, r.Status.Source, r.Status.Message)
	}
}

// 并发上限：12 个账号、单槽位 300ms，验证不会一次性全放出去，也不会退化成串行。
func TestBatchProbeRespectsConcurrencyCap(t *testing.T) {
	const stall = 300 * time.Millisecond
	var peak, cur int32
	var mu sync.Mutex
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				mu.Lock()
				cur++
				if cur > peak {
					peak = cur
				}
				mu.Unlock()
				defer func() { mu.Lock(); cur--; mu.Unlock() }()
				select {
				case <-time.After(stall):
					return nil, errors.New("stall")
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		},
	}
	oldClient := quotaHTTPClient
	quotaHTTPClient = client
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	oldResolver := quotaCredentialResolver
	quotaCredentialResolver = func(email string) []byte {
		return []byte(`{"token":{"access_token":"ya29.fresh","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `"}`)
	}
	t.Cleanup(func() { quotaCredentialResolver = oldResolver })

	emails := make([]string, 12)
	for i := range emails {
		emails[i] = "u" + string(rune('a'+i)) + "@example.com"
	}
	res := probeQuotaBatch(emails)
	mu.Lock()
	p := peak
	mu.Unlock()
	t.Logf("12 accounts, peak concurrent dials = %d (cap %d)", p, quotaProbeMaxConcurrency)
	if p > quotaProbeMaxConcurrency {
		t.Fatalf("peak %d exceeded cap %d", p, quotaProbeMaxConcurrency)
	}
	if p < 2 {
		t.Fatalf("peak %d — probes are effectively serial", p)
	}
	if len(res) != 12 {
		t.Fatalf("expected 12 results, got %d", len(res))
	}
}

// 「永不回应的网络」是最容易让界面卡死的场景：每个账号都会撞满 HTTP 超时。
// 这里让每个连接都阻塞到超过单账号超时，验证：
//  1. 整批耗时被总超时预算封顶，不随账号数线性增长；
//  2. 没有任何账号被伪造成 live。
func TestBatchProbeBoundedByTotalTimeout(t *testing.T) {
	stallClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				<-ctx.Done() // 永远不回应，直到调用方超时
				return nil, ctx.Err()
			},
		},
	}
	oldClient := quotaHTTPClient
	quotaHTTPClient = stallClient
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	oldResolver := quotaCredentialResolver
	quotaCredentialResolver = func(email string) []byte {
		return []byte(`{"token":{"access_token":"ya29.fresh","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `"}`)
	}
	t.Cleanup(func() { quotaCredentialResolver = oldResolver })

	emails := make([]string, 12)
	for i := range emails {
		emails[i] = "s" + string(rune('a'+i)) + "@example.com"
	}

	st := time.Now()
	res := probeQuotaBatch(emails)
	el := time.Since(st)

	// 单账号超时 18s，整批预算 22s。12 个账号串行至少 12*18s = 3m36s。
	serial := time.Duration(len(emails)) * quotaProbePerAccountTimeout
	t.Logf("12 账号、连接永不回应 => %v（串行下界 %v）", el.Round(time.Millisecond), serial)

	if el > quotaProbeTotalTimeout+3*time.Second {
		t.Fatalf("整批 %v 超过总超时预算 %v —— 界面会长时间卡死", el, quotaProbeTotalTimeout)
	}
	if len(res) != len(emails) {
		t.Fatalf("每个账号都必须有结论，得到 %d / %d", len(res), len(emails))
	}
	for e, r := range res {
		if r.Status.Source == quotaSourceLive || r.Status.Status == "ok" {
			t.Fatalf("网络不回应时不得报 live/ok: %s %+v", e, r.Status)
		}
		if r.Status.Message == "" {
			t.Fatalf("每个失败账号都必须带说明: %s %+v", e, r.Status)
		}
	}
}
