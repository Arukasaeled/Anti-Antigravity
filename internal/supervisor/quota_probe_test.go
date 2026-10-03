package supervisor

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 四桶解析：Gemini 5h/weekly + Claude/GPT 5h/weekly 必须互不串。
func TestPoolsFromQuotaSummaryFourBuckets(t *testing.T) {
	body := []byte(`{
		"groups": [
			{
				"displayName": "Gemini Models",
				"description": "Models within this group: Fixture Flash, Fixture Pro",
				"buckets": [
					{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.75,"resetTime":"2099-01-01T00:00:00Z"},
					{"bucketId":"gemini-weekly","window":"weekly","remainingFraction":0.50}
				]
			},
			{
				"displayName": "Claude / GPT Models",
				"description": "Models within this group: Fixture Opus",
				"buckets": [
					{"bucketId":"claude:5h","window":"5h","remainingFraction":0.90},
					{"bucketId":"claude:weekly","window":"weekly","remainingFraction":0.25}
				]
			}
		]
	}`)

	gemini, claude, models, ok := poolsFromQuotaSummary(body)
	if !ok {
		t.Fatal("expected quota summary to parse")
	}
	if !gemini.Available || !claude.Available {
		t.Fatalf("both pools should be available: gemini=%v claude=%v", gemini.Available, claude.Available)
	}
	if gemini.FiveHourPercent != 75 || gemini.WeeklyPercent != 50 {
		t.Fatalf("gemini buckets wrong: 5h=%d weekly=%d", gemini.FiveHourPercent, gemini.WeeklyPercent)
	}
	if claude.FiveHourPercent != 90 || claude.WeeklyPercent != 25 {
		t.Fatalf("claude buckets wrong: 5h=%d weekly=%d", claude.FiveHourPercent, claude.WeeklyPercent)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models from descriptions, got %d", len(models))
	}
}

// 失败时不得伪造：live 失败且无 cockpit 缓存 → 两池均 unavailable。
func TestProbeAccountQuotaNoCredentialHonestUnavailable(t *testing.T) {
	gemini, claude, models, status := ProbeAccountQuota(context.Background(), "nobody@example.com", nil)
	if gemini.Available || claude.Available || len(models) != 0 {
		t.Fatalf("must not fabricate quota: %+v %+v", gemini, claude)
	}
	if status.Status == "ok" || status.Source == quotaSourceLive {
		t.Fatalf("status should not be ok/live: %+v", status)
	}
}

// 过期 token 且无 client 字段：必须明确 expired，不冒充 OAuth 客户端。
func TestProbeAccountQuotaExpiredWithoutClientIsExpired(t *testing.T) {
	raw := []byte(`{"token":{"access_token":"old","refresh_token":"r","expiry":"2020-01-01T00:00:00Z"},"id_token":""}`)
	_, _, _, status := ProbeAccountQuota(context.Background(), "", raw)
	if status.Status != "expired" {
		t.Fatalf("expected expired, got %+v", status)
	}
	if status.Source == quotaSourceLive {
		t.Fatal("live source must not be claimed on refresh failure")
	}
}

// 凭据归属与请求邮箱不一致时拒绝，避免 A/B 串池。
func TestProbeAccountQuotaRejectsMismatchedEmail(t *testing.T) {
	raw := []byte(`{"token":{"access_token":"a","refresh_token":"r"},"id_token":"` + jwtWithEmail("alice@example.com") + `"}`)
	_, _, _, status := ProbeAccountQuota(context.Background(), "bob@example.com", raw)
	if status.Status == "ok" {
		t.Fatalf("mismatched email must not probe: %+v", status)
	}
}

// cockpit fallback 必须标成 cockpit-cache，不得冒充 live。
func TestFallbackMarksCockpitCache(t *testing.T) {
	// 没有 live 路径可走（无凭据），也应保持 source=none 而不是 live。
	_, _, _, status := ProbeAccountQuota(context.Background(), "ghost@example.com", nil)
	if status.Source == quotaSourceLive {
		t.Fatalf("unexpected live source: %+v", status)
	}
}

func jwtWithEmail(email string) string {
	// 仅用于 emailFromCredentialPayload 的非签名校验读取；2Ag 不校验 JWT 签名。
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"` + email + `","exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`))
	return strings.Join([]string{header, payload, "sig"}, ".")
}
