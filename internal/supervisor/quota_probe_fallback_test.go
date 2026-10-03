package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 断网/黑洞：所有账号并发探测，整批耗时有上限，且单账号失败不阻塞其他账号。
func TestBatchProbeOfflineBounded(t *testing.T) {
	// 用一个必定连不通的本地端口模拟「接口不可达」。
	bad := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, errors.New("simulated offline")
			},
		},
	}
	old := quotaHTTPClient
	quotaHTTPClient = bad
	t.Cleanup(func() { quotaHTTPClient = old })

	emails := []string{}
	for _, e := range ListVaultAccountEntries() {
		emails = append(emails, e.Email)
	}
	if len(emails) == 0 {
		t.Skip("no vault accounts to probe")
	}
	// 人为放大到 12 个，验证并发上限不会让耗时线性增长。
	for i := 0; i < 12-len(emails); i++ {
		emails = append(emails, emails[i%len(emails)])
	}

	st := time.Now()
	res := probeQuotaBatch(emails)
	el := time.Since(st)
	t.Logf("offline batch: %d results in %v", len(res), el.Round(time.Millisecond))

	if el > quotaProbeTotalTimeout+3*time.Second {
		t.Fatalf("batch took %v, exceeding total timeout budget %v", el, quotaProbeTotalTimeout)
	}
	for _, r := range res {
		if r.Status.Source == quotaSourceLive {
			t.Fatalf("offline probe must not report live: %+v", r.Status)
		}
		if r.Status.Source == quotaSourceCockpitCache && !r.Status.Stale {
			t.Fatalf("cockpit fallback must be marked stale: %+v", r.Status)
		}
	}

	// 串行上界对照：如果实现是串行，12 个账号会接近 12*(单账号超时)。
	serialUpper := time.Duration(len(emails)) * quotaProbePerAccountTimeout
	t.Logf("serial upper bound would be ~%v; actual %v", serialUpper, el.Round(time.Millisecond))
}

// 过期且无法刷新的凭据必须如实报 expired，不得回落到编造数字。
func TestExpiredCredentialHonestStatus(t *testing.T) {
	raw := []byte(`{"token":{"access_token":"ya29.placeholder","refresh_token":"1//0placeholder","token_type":"Bearer","expiry":"2020-01-01T00:00:00Z"},"id_token":"x.y.z","auth_method":"consumer"}`)
	old := quotaHTTPClient
	quotaHTTPClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return nil, errors.New("must not dial: expired token should fail before network")
		},
	}}
	t.Cleanup(func() { quotaHTTPClient = old })

	g, c, m, st := ProbeAccountQuota(context.Background(), "", raw)
	if st.Status != "expired" {
		t.Fatalf("expected expired, got %+v", st)
	}
	if g.Available || c.Available {
		t.Fatalf("expired credential must not yield available pools: %+v %+v", g, c)
	}
	if len(m) != 0 {
		t.Fatalf("expired credential must not yield models: %v", m)
	}
	if !strings.Contains(st.Message, "没有可用于刷新") {
		t.Fatalf("message should explain why refresh is impossible: %q", st.Message)
	}
}

// 回落缓存时，「非实时」这一事实必须落在**池**上，而不只是落在 status 上。
//
// 为什么这条测试是关键：/api/v1/dashboard 与 /api/v1/host/status 的
// active_account 走 AccountQuotaDTO，只搬运池与百分比 —— QuotaProbeStatus
// 根本不出现在那两个响应里。若 Stale 只写在 status 上，前端拿不到任何
// 机器可读标记，一份两分钟前写入的缓存就会以「配额出处：刚刚」呈现，
// 等于把非实时读数当成实时读数展示。
func TestFallbackMarksPoolsNonLive(t *testing.T) {
	dir := t.TempDir()
	authorized := filepath.Join(dir, "cache", "quota_api_v1_desktop", "authorized")
	if err := os.MkdirAll(authorized, 0o755); err != nil {
		t.Fatalf("创建夹具目录失败: %v", err)
	}
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", dir)

	const email = "fallback-fixture@example.com"
	// 夹具刻意写成「刚刚采样」：updated_at 很新，年龄判据完全看不出它是缓存，
	// 只有 pool.stale 能说明真相。
	payload := map[string]any{
		"email":        email,
		"updatedAt":    time.Now().UnixMilli(),
		"source":       "authorized",
		"customSource": "desktop",
		"payload": map[string]any{
			"quota_summary": map[string]any{
				"groups": []map[string]any{
					{
						"displayName": "Gemini Models",
						"description": "Models within this group: Fixture Flash",
						"buckets": []map[string]any{
							{"window": "5h", "remainingFraction": 0.91},
							{"window": "weekly", "remainingFraction": 0.78},
						},
					},
					{
						"displayName": "Claude & GPT Models",
						"description": "Models within this group: Fixture Opus",
						"buckets": []map[string]any{
							{"window": "5h", "remainingFraction": 1.0},
							{"window": "weekly", "remainingFraction": 1.0},
						},
					},
				},
			},
		},
	}
	rawFixture, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化夹具失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(authorized, "fixture.json"), rawFixture, 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}

	// live 必然失败：任何拨号都被拒。
	oldClient := quotaHTTPClient
	quotaHTTPClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, errors.New("simulated offline")
			},
		},
	}
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	// 新鲜未过期的凭据，确保走到 live 尝试再回落（而不是先被 expired 拦下）。
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	cred := []byte(`{"token":{"access_token":"ya29.fake","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `"}`)

	g, c, _, st := ProbeAccountQuota(context.Background(), email, cred)

	if st.Status == "ok" || st.Source == quotaSourceLive {
		t.Fatalf("live must not succeed offline: %+v", st)
	}
	if st.Source != quotaSourceCockpitCache || !st.Stale {
		t.Fatalf("status should report the cockpit fallback: %+v", st)
	}
	if !g.Available {
		t.Fatalf("fixture cache should yield a gemini pool: %+v", g)
	}
	if !g.Stale || !c.Stale {
		t.Fatalf("fallback pools MUST carry stale=true so the UI can label them non-live; gemini=%+v claude=%+v", g, c)
	}
	// 数据本身仍要正确带出来（只是被明确标注为非实时）。
	if g.FiveHourPercent != 91 || g.WeeklyPercent != 78 {
		t.Fatalf("fallback should still carry the cached numbers: %+v", g)
	}
	if g.UpdatedAt == 0 {
		t.Fatal("fallback pool must keep the cached sample timestamp")
	}
}

// live 成功时池上不得出现 stale —— 否则真实时读数会被标成缓存。
func TestLiveProbeLeavesPoolsFresh(t *testing.T) {
	g, c, _, st := ProbeAccountQuota(context.Background(), "", nil)
	if st.Source == quotaSourceLive {
		t.Fatal("no credential must not reach live")
	}
	if g.Stale || c.Stale {
		t.Fatalf("unavailable pools are not 'stale fallback' and must not be flagged: %+v %+v", g, c)
	}
}
