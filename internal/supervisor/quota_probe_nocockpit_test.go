package supervisor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// roundTripFunc 让测试直接提供 http.Response，不经过真实网络。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// 无 Cockpit Tools 独立性：把 cockpit 数据目录指向空目录后，
// 探测仍然必须能靠 2Ag 自己的凭据走到 live 接口，而不是依赖 cockpit 缓存。
//
// 这里注入一个「假的但结构正确」的云端响应，证明探测链路不经过 cockpit 目录。
func TestQuotaProbeIndependentOfCockpit(t *testing.T) {
	emptyDir := t.TempDir()
	os.Setenv("COCKPIT_TOOLS_DATA_DIR", emptyDir)
	t.Cleanup(func() { os.Unsetenv("COCKPIT_TOOLS_DATA_DIR") })

	if got := cockpitCacheDir(); got != filepath.Join(emptyDir, "cache", "quota_api_v1_desktop", "authorized") {
		t.Fatalf("cockpitCacheDir() = %q, want it under the empty temp dir", got)
	}

	// 空 cockpit：缓存必须查不到，任何 fallback 都不成立。
	if _, _, _, ok := queryCacheFor("nobody@example.com"); ok {
		t.Fatal("queryCacheFor must not find anything in an empty cockpit dir")
	}

	summaryBody := []byte(`{
		"groups": [
			{"displayName":"Gemini Models","description":"Models within this group: Fake Flash, Fake Pro","buckets":[
				{"bucketId":"gemini-5h","window":"5h","remainingFraction":0.66},
				{"bucketId":"gemini-weekly","window":"weekly","remainingFraction":0.44}
			]},
			{"displayName":"Claude and GPT Models","description":"Models within this group: Fake Opus","buckets":[
				{"bucketId":"3p-5h","window":"5h","remainingFraction":0.88},
				{"bucketId":"3p-weekly","window":"weekly","remainingFraction":0.77}
			]}
		]
	}`)

	oldClient := quotaHTTPClient
	quotaHTTPClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == quotaLoadPath {
				return jsonResponse(200, `{"cloudaicompanionProject":"aicode-consumers"}`), nil
			}
			if req.URL.Path == quotaSummaryPath {
				return jsonResponse(200, string(summaryBody)), nil
			}
			return nil, errors.New("unexpected path: " + req.URL.Path)
		}),
	}
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	email := "independent@example.com"
	raw := []byte(`{"token":{"access_token":"ya29.fake","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `"}`)

	g, c, models, st := ProbeAccountQuota(context.Background(), email, raw)
	if st.Status != "ok" {
		t.Fatalf("expected ok from live path, got %+v", st)
	}
	if st.Source != quotaSourceLive {
		t.Fatalf("source = %q, want the live source (not a cockpit fallback)", st.Source)
	}
	if st.Stale {
		t.Fatal("a successful live probe must not be marked stale")
	}
	if !g.Available || g.FiveHourPercent != 66 || g.WeeklyPercent != 44 {
		t.Fatalf("gemini pool not parsed from live body: %+v", g)
	}
	if g.Stale || c.Stale {
		t.Fatalf("a live probe must leave both pools unmarked (stale=false): gemini=%+v claude=%+v", g, c)
	}
	if !c.Available || c.FiveHourPercent != 88 || c.WeeklyPercent != 77 {
		t.Fatalf("claude pool not parsed from live body: %+v", c)
	}
	if len(models) != 3 {
		t.Fatalf("models = %v, want the three from the group description", models)
	}
}

// 空 cockpit 且 live 不可达时：必须如实 unavailable，绝不因为「曾经有缓存」而编造数字。
func TestQuotaProbeNoCockpitAndNoNetworkIsUnavailable(t *testing.T) {
	os.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())
	t.Cleanup(func() { os.Unsetenv("COCKPIT_TOOLS_DATA_DIR") })

	oldClient := quotaHTTPClient
	quotaHTTPClient = &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return nil, errors.New("offline")
			},
		},
	}
	t.Cleanup(func() { quotaHTTPClient = oldClient })

	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	email := "offline@example.com"
	raw := []byte(`{"token":{"access_token":"ya29.fake","token_type":"Bearer","expiry":"` + future + `"},"id_token":"` + jwtWithEmail(email) + `"}`)

	g, c, models, st := ProbeAccountQuota(context.Background(), email, raw)
	if st.Status == "ok" {
		t.Fatalf("offline probe must not be ok: %+v", st)
	}
	if st.Source != "none" {
		t.Fatalf("source = %q, want none when there is no cache to fall back to", st.Source)
	}
	if g.Available || c.Available || len(models) != 0 {
		t.Fatalf("must not fabricate pools/models: %+v %+v %v", g, c, models)
	}
	if st.Message == "" {
		t.Fatal("an unavailable result must carry an explanation")
	}
}
