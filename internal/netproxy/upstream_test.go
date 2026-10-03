package netproxy

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/2ag/2ag/internal/config"
)

// clearProxyEnv 清掉可能影响解析的环境变量，让每个用例从确定状态出发。
func clearProxyEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy",
		"ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD",
	} {
		t.Setenv(name, "")
	}
}

func TestParseUpstreamURL(t *testing.T) {
	cases := []struct {
		raw      string
		wantHost string
		wantSch  string
		wantErr  bool
	}{
		{raw: "http://127.0.0.1:7897", wantHost: "127.0.0.1:7897", wantSch: "http"},
		{raw: "127.0.0.1:7897", wantHost: "127.0.0.1:7897", wantSch: "http"},
		{raw: "  http://proxy.local:8080  ", wantHost: "proxy.local:8080", wantSch: "http"},
		{raw: "https://proxy.local:8443", wantHost: "proxy.local:8443", wantSch: "https"},
		{raw: "http://user:pw@proxy.local:8080", wantHost: "proxy.local:8080", wantSch: "http"},
		{raw: "", wantErr: true},
		{raw: "   ", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseUpstreamURL(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseUpstreamURL(%q) 期望报错，实际得到 %v", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseUpstreamURL(%q) 意外报错: %v", tc.raw, err)
			continue
		}
		if got.Host != tc.wantHost || got.Scheme != tc.wantSch {
			t.Errorf("parseUpstreamURL(%q) = %s://%s，期望 %s://%s", tc.raw, got.Scheme, got.Host, tc.wantSch, tc.wantHost)
		}
	}
}

func TestParseProxyServerValue(t *testing.T) {
	t.Run("裸 host:port 对所有协议生效", func(t *testing.T) {
		httpURL, httpsURL := parseProxyServerValue("127.0.0.1:7897")
		if httpURL == nil || httpsURL == nil {
			t.Fatalf("裸写法应同时给出 http/https，实际 http=%v https=%v", httpURL, httpsURL)
		}
		if httpURL.Host != "127.0.0.1:7897" || httpsURL.Host != "127.0.0.1:7897" {
			t.Errorf("裸写法解析错误: http=%s https=%s", httpURL.Host, httpsURL.Host)
		}
	})
	t.Run("按协议分别指定", func(t *testing.T) {
		httpURL, httpsURL := parseProxyServerValue("http=127.0.0.1:7897;https=127.0.0.1:7898")
		if httpURL == nil || httpURL.Host != "127.0.0.1:7897" {
			t.Errorf("http 项解析错误: %v", httpURL)
		}
		if httpsURL == nil || httpsURL.Host != "127.0.0.1:7898" {
			t.Errorf("https 项解析错误: %v", httpsURL)
		}
	})
	t.Run("只配一侧时另一侧沿用", func(t *testing.T) {
		httpURL, httpsURL := parseProxyServerValue("https=127.0.0.1:7897")
		if httpURL == nil || httpsURL == nil {
			t.Fatalf("只配 https 时应让 http 沿用，实际 http=%v https=%v", httpURL, httpsURL)
		}
		if httpURL.Host != "127.0.0.1:7897" {
			t.Errorf("http 应沿用 https 的值，实际 %s", httpURL.Host)
		}
	})
	t.Run("空值与垃圾值", func(t *testing.T) {
		if h, s := parseProxyServerValue(""); h != nil || s != nil {
			t.Errorf("空字符串应返回 nil，实际 http=%v https=%v", h, s)
		}
		if h, s := parseProxyServerValue("http=;https="); h != nil || s != nil {
			t.Errorf("空项应返回 nil，实际 http=%v https=%v", h, s)
		}
	})
}

func TestNoProxyMatch(t *testing.T) {
	entries := []string{"localhost", ".internal", "10.*", "example.com", "127.0.0.1", "192.168.0.0/16", "<local>"}
	cases := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "api.internal", want: true},
		{host: "internal", want: true},
		{host: "10.1.2.3", want: true},
		{host: "example.com", want: true},
		{host: "sub.example.com", want: true},
		{host: "127.0.0.1", want: true},
		{host: "192.168.5.9", want: true},
		{host: "intranet", want: true}, // <local>：无点主机名
		{host: "oauth2.googleapis.com", want: false},
		{host: "google.com", want: false},
		{host: "11.1.2.3", want: false}, // 不匹配 10.*
		{host: "172.16.0.1", want: false},
	}
	for _, tc := range cases {
		if got := noProxyMatch(tc.host, entries); got != tc.want {
			t.Errorf("noProxyMatch(%q) = %v，期望 %v", tc.host, got, tc.want)
		}
	}
	if !noProxyMatch("anything.at.all", []string{"*"}) {
		t.Error("NO_PROXY=* 应旁路一切")
	}
	if noProxyMatch("google.com", nil) {
		t.Error("空旁路清单不应命中")
	}
}

func TestIsDirectTarget(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{host: "127.0.0.1", want: true},
		{host: "::1", want: true},
		{host: "0.0.0.0", want: true},
		{host: "localhost", want: true},
		{host: "LOCALHOST", want: true},
		{host: "", want: true},
		{host: "oauth2.googleapis.com", want: false},
		{host: "8.8.8.8", want: false},
	}
	for _, tc := range cases {
		if got := isDirectTarget(tc.host); got != tc.want {
			t.Errorf("isDirectTarget(%q) = %v，期望 %v", tc.host, got, tc.want)
		}
	}
}

// TestLoadUpstreamProxyPrefersEnv 是「宿主不再读环境代理」这个缺陷的核心回归：
// 用户的代理写在环境变量里时，2Ag 必须把它解析出来并用于自己的转发链路。
func TestLoadUpstreamProxyPrefersEnv(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")

	up := loadUpstreamProxy("127.0.0.1:55555")
	if up == nil {
		t.Fatal("环境变量里配了 HTTPS_PROXY，应解析出上游代理")
	}
	got := up.proxyFor("oauth2.googleapis.com", true)
	if got == nil || got.Host != "127.0.0.1:7897" {
		t.Fatalf("HTTPS 目标应借道 127.0.0.1:7897，实际 %v", got)
	}
}

func TestLoadUpstreamProxyALLProxyFallback(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("ALL_PROXY", "127.0.0.1:7899")

	up := loadUpstreamProxy("127.0.0.1:55555")
	if up == nil {
		t.Fatal("ALL_PROXY 应作为兜底被解析")
	}
	if got := up.proxyFor("example.com", true); got == nil || got.Host != "127.0.0.1:7899" {
		t.Errorf("HTTPS 目标应借道 ALL_PROXY，实际 %v", got)
	}
	if got := up.proxyFor("example.com", false); got == nil || got.Host != "127.0.0.1:7899" {
		t.Errorf("HTTP 目标应借道 ALL_PROXY，实际 %v", got)
	}
}

func TestLoadUpstreamProxyNoneConfigured(t *testing.T) {
	clearProxyEnv(t)
	// 系统代理是否开启取决于运行环境，这里只断言「没有环境代理时不 panic」且
	// 解析结果要么是 nil，要么是有效 URL —— 不能凭空造出一个代理。
	up := loadUpstreamProxy("127.0.0.1:55555")
	if up != nil {
		if up.http == nil && up.https == nil {
			t.Error("返回非 nil 却没有任何代理地址")
		}
	}
}

// TestLoadUpstreamProxyRejectsSelf 防止把 2Ag 自己配成上游造成无限转发。
func TestLoadUpstreamProxyRejectsSelf(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:55555")

	up := loadUpstreamProxy("127.0.0.1:55555")
	if up != nil && up.proxyFor("example.com", true) != nil {
		t.Error("指回 2Ag 自身的上游代理必须被拒绝，否则会转发自环")
	}
}

func TestLoadUpstreamProxyIgnoresUnsupportedScheme(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "socks5://127.0.0.1:1080")

	up := loadUpstreamProxy("127.0.0.1:55555")
	if up != nil && up.proxyFor("example.com", true) != nil {
		t.Error("2Ag 只支持 http 代理，socks5 应被忽略而不是解析成错误目标")
	}
}

func TestLoadUpstreamProxyRespectsNoProxy(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	t.Setenv("NO_PROXY", "googleapis.com,.internal")

	up := loadUpstreamProxy("127.0.0.1:55555")
	if up == nil {
		t.Fatal("应解析出上游代理")
	}
	if got := up.proxyFor("oauth2.googleapis.com", true); got != nil {
		t.Errorf("命中 NO_PROXY 的目标应直连，实际借道 %v", got)
	}
	if got := up.proxyFor("api.internal", true); got != nil {
		t.Errorf("命中 NO_PROXY 后缀的目标应直连，实际借道 %v", got)
	}
	if got := up.proxyFor("example.com", true); got == nil {
		t.Error("未命中 NO_PROXY 的目标应借道上游代理")
	}
}

func TestProxyForAlwaysDirectsLoopback(t *testing.T) {
	clearProxyEnv(t)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	up := loadUpstreamProxy("127.0.0.1:55555")
	if up == nil {
		t.Fatal("应解析出上游代理")
	}
	// 环回不走代理：CDP、Manager API 都是环回，绕出去会自伤。
	if got := up.proxyFor("127.0.0.1", false); got != nil {
		t.Errorf("环回应直连，实际借道 %v", got)
	}
	if got := up.proxyFor("localhost", true); got != nil {
		t.Errorf("localhost 应直连，实际借道 %v", got)
	}
}

func TestRedactProxyURLHidesPassword(t *testing.T) {
	u, err := url.Parse("http://alice:supersecret@proxy.local:8080")
	if err != nil {
		t.Fatal(err)
	}
	redacted := redactProxyURL(u)
	if strings.Contains(redacted, "supersecret") {
		t.Errorf("口令不得出现在日志里: %s", redacted)
	}
	if !strings.Contains(redacted, "alice") || !strings.Contains(redacted, "***") {
		t.Errorf("应保留用户名并遮蔽口令: %s", redacted)
	}
}

func TestProxyHostPortDefaults(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{raw: "http://proxy.local", want: "proxy.local:80"},
		{raw: "https://proxy.local", want: "proxy.local:443"},
		{raw: "http://proxy.local:8080", want: "proxy.local:8080"},
	}
	for _, tc := range cases {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := proxyHostPort(u); got != tc.want {
			t.Errorf("proxyHostPort(%q) = %q，期望 %q", tc.raw, got, tc.want)
		}
	}
}

// countingProxy 是一个最小的上游代理：记录收到的 CONNECT 目标，并把流量
// 透传到真实目标。用来证明 2Ag 的转发链路确实「借道」而不是「直连」。
type countingProxy struct {
	server      *httptest.Server
	connects    atomic.Int64
	targets     chan string
	requireAuth atomic.Bool
	authSeen    atomic.Bool
}

func newCountingProxy(t *testing.T) *countingProxy {
	t.Helper()
	cp := &countingProxy{targets: make(chan string, 8)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "only CONNECT", http.StatusMethodNotAllowed)
			return
		}
		if cp.requireAuth.Load() {
			want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:supersecret"))
			if r.Header.Get("Proxy-Authorization") == want {
				cp.authSeen.Store(true)
			} else {
				http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
				return
			}
		}
		cp.connects.Add(1)
		select {
		case cp.targets <- r.Host:
		default:
		}
		upstream, err := net.DialTimeout("tcp", r.Host, 5*time.Second)
		if err != nil {
			http.Error(w, "upstream down", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			upstream.Close()
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		client, buffered, err := hijacker.Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		go func() {
			_, _ = io.Copy(upstream, buffered)
			upstream.Close()
		}()
		_, _ = io.Copy(client, upstream)
		client.Close()
	})
	cp.server = httptest.NewServer(handler)
	t.Cleanup(cp.server.Close)
	return cp
}

func (cp *countingProxy) url() string { return cp.server.URL }

// TestConnectChainsThroughUpstreamProxy 是本次缺陷的核心回归：
// 宿主把 HTTPS 交给 2Ag 之后，2Ag 的 CONNECT 必须经用户的上游代理出去。
func TestConnectChainsThroughUpstreamProxy(t *testing.T) {
	clearProxyEnv(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "hello from origin")
	}))
	defer upstream.Close()
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	cp := newCountingProxy(t)
	// 让本用例的目标（127.0.0.1）也走代理，才能观察到借道行为本身。
	restore := directTargetCheck
	directTargetCheck = func(string) bool { return false }
	t.Cleanup(func() { directTargetCheck = restore })
	t.Setenv("HTTPS_PROXY", cp.url())

	proxy, err := Start(context.Background(), config.NewRuntime("", config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if proxy.upstream == nil {
		t.Fatal("应解析出上游代理")
	}

	// 经 2Ag 代理发起 CONNECT。
	conn, err := net.DialTimeout("tcp", proxy.Address(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", upstreamHost, upstreamHost); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("读取 2Ag 代理的 CONNECT 响应失败: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("2Ag 代理应返回 200 Connection Established，实际 %s", response.Status)
	}

	if cp.connects.Load() == 0 {
		t.Fatal("上游代理没有收到 CONNECT —— 说明 2Ag 绕过了用户的上游代理直连目标")
	}
	select {
	case target := <-cp.targets:
		if target != upstreamHost {
			t.Errorf("上游代理收到的目标是 %q，期望 %q", target, upstreamHost)
		}
	case <-time.After(2 * time.Second):
		t.Error("上游代理没有记录到 CONNECT 目标")
	}
}

// TestConnectDirectWithoutUpstream 证明没有上游代理时行为不变（仍直连）。
func TestConnectDirectWithoutUpstream(t *testing.T) {
	clearProxyEnv(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "direct ok")
	}))
	defer origin.Close()
	originHost := strings.TrimPrefix(origin.URL, "http://")

	proxy, err := Start(context.Background(), config.NewRuntime("", config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	conn, err := net.DialTimeout("tcp", proxy.Address(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", originHost, originHost); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("读取 CONNECT 响应失败: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("无上游代理时应直连成功，实际 %s", response.Status)
	}
}

// TestConnectTunnelCarriesPayload 验证隧道是双向透明的：请求真正送达源站。
func TestConnectTunnelCarriesPayload(t *testing.T) {
	clearProxyEnv(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "payload-through-tunnel")
	}))
	defer origin.Close()
	originHost := strings.TrimPrefix(origin.URL, "http://")

	proxy, err := Start(context.Background(), config.NewRuntime("", config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	conn, err := net.DialTimeout("tcp", proxy.Address(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", originHost, originHost)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT 失败: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT 应成功，实际 %s", response.Status)
	}
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", originHost)
	payload, err := io.ReadAll(reader)
	if err != nil && len(payload) == 0 {
		t.Fatalf("读取隧道载荷失败: %v", err)
	}
	if !strings.Contains(string(payload), "payload-through-tunnel") {
		t.Errorf("隧道未把请求送达源站，实际响应: %q", string(payload))
	}
}

// TestConnectThroughUpstreamWithAuth 验证带凭据的上游代理被正确握手。
func TestConnectThroughUpstreamWithAuth(t *testing.T) {
	clearProxyEnv(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	}))
	defer upstream.Close()
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	cp := newCountingProxy(t)
	cp.requireAuth.Store(true)
	restore := directTargetCheck
	directTargetCheck = func(string) bool { return false }
	t.Cleanup(func() { directTargetCheck = restore })

	authenticated := strings.Replace(cp.url(), "http://", "http://alice:supersecret@", 1)
	t.Setenv("HTTPS_PROXY", authenticated)

	proxy, err := Start(context.Background(), config.NewRuntime("", config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()

	conn, err := net.DialTimeout("tcp", proxy.Address(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", upstreamHost, upstreamHost)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT 失败: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("带凭据的上游代理应握手成功，实际 %s", response.Status)
	}
	if !cp.authSeen.Load() {
		t.Error("上游代理没有收到正确的 Proxy-Authorization")
	}
}

// TestHTTPTransportChainsThroughUpstreamProxy 覆盖明文 HTTP 分支：
// transport.Proxy 以前是 nil，用户的上游代理同样会被绕过。
func TestHTTPTransportChainsThroughUpstreamProxy(t *testing.T) {
	clearProxyEnv(t)
	cp := newCountingProxy(t)
	restore := directTargetCheck
	directTargetCheck = func(string) bool { return false }
	t.Cleanup(func() { directTargetCheck = restore })
	t.Setenv("HTTP_PROXY", cp.url())

	proxy, err := Start(context.Background(), config.NewRuntime("", config.Default()))
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	if proxy.upstream == nil || proxy.transport.Proxy == nil {
		t.Fatal("配置了 HTTP_PROXY 后 transport.Proxy 不应为 nil")
	}
}
