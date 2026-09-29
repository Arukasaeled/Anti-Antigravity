package netproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/2ag/2ag/internal/config"
)

const maxRuleBody = 2 << 20

type Snapshot struct {
	Requests int64 `json:"requests"`
	Blocked  int64 `json:"blocked"`
	Errors   int64 `json:"errors"`
	LastMS   int64 `json:"last_ms"`
	Status   int   `json:"status"`
}

// Server is an ordinary forward proxy. HTTPS CONNECT is passed through as
// opaque TCP; no certificate is installed and TLS payloads are never decoded.
type Server struct {
	config    *config.Runtime
	transport *http.Transport
	server    *http.Server
	listener  net.Listener
	mu        sync.RWMutex
	stats     Snapshot
}

func Start(ctx context.Context, runtime *config.Runtime) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for proxy: %w", err)
	}
	p := &Server{config: runtime, listener: listener,
		transport: &http.Transport{Proxy: nil, MaxIdleConns: 32, IdleConnTimeout: 45 * time.Second, TLSHandshakeTimeout: 10 * time.Second}}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = p.server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = p.server.Shutdown(deadline)
		p.transport.CloseIdleConnections()
	}()
	return p, nil
}

func (p *Server) Address() string    { return p.listener.Addr().String() }
func (p *Server) Snapshot() Snapshot { p.mu.RLock(); defer p.mu.RUnlock(); return p.stats }
func (p *Server) Close() error {
	if p == nil || p.server == nil {
		return nil
	}
	return p.server.Close()
}

func (p *Server) record(status int, blocked bool, elapsed time.Duration) {
	p.mu.Lock()
	p.stats.Requests++
	if blocked {
		p.stats.Blocked++
	}
	if status >= 500 {
		p.stats.Errors++
	}
	p.stats.Status = status
	p.stats.LastMS = elapsed.Milliseconds()
	p.mu.Unlock()
}

func (p *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method == http.MethodOptions && (r.URL.Path == "/status" || r.URL.Path == "/config/global-rules") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/status" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p.Snapshot())
		return
	}
	cfg := p.config.Snapshot()
	if r.URL.Path == "/config/global-rules" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]string{"global_rules": cfg.GlobalRules})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var update struct {
			GlobalRules string `json:"global_rules"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&update); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, err := p.config.Update(func(next *config.Config) error { next.GlobalRules = update.GlobalRules; return nil }); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"global_rules": update.GlobalRules})
		return
	}
	host := hostname(r.Host)
	if r.URL.IsAbs() {
		host = hostname(r.URL.Host)
	}
	if blocked(host, cfg.Privacy.BlockedHosts) {
		http.Error(w, "blocked by 2Ag Privacy Shield", http.StatusForbidden)
		p.record(http.StatusForbidden, true, time.Since(start))
		return
	}
	if r.Method == http.MethodConnect {
		p.connect(w, r, start)
		return
	}
	if r.URL.Scheme != "" && r.URL.Scheme != "http" {
		http.Error(w, "unsupported proxy scheme", http.StatusBadRequest)
		p.record(http.StatusBadRequest, false, time.Since(start))
		return
	}
	upstream := r.Clone(r.Context())
	upstream.RequestURI = ""
	if !upstream.URL.IsAbs() {
		upstream.URL.Scheme, upstream.URL.Host = "http", r.Host
	}
	if upstream.URL.Host == "" {
		http.Error(w, "missing upstream host", http.StatusBadRequest)
		p.record(400, false, time.Since(start))
		return
	}
	upstream.Header = r.Header.Clone()
	removeHopHeaders(upstream.Header)
	originalHost := hostname(upstream.URL.Host)
	if override := matchingOverride(originalHost, cfg.Network.EndpointOverrides); override != nil {
		target, _ := url.Parse(override.URL) // Config.Validate already checked this.
		upstream.URL.Scheme, upstream.URL.Host = target.Scheme, target.Host
		upstream.URL.Path = strings.TrimRight(target.Path, "/") + upstream.URL.Path
		upstream.Host = target.Host
		for key, value := range override.Headers {
			upstream.Header.Set(key, value)
		}
	}
	if err := prependRules(upstream, originalHost, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		p.record(http.StatusBadRequest, false, time.Since(start))
		return
	}
	response, err := p.transport.RoundTrip(upstream)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		p.record(http.StatusBadGateway, false, time.Since(start))
		return
	}
	defer response.Body.Close()
	removeHopHeaders(response.Header)
	copyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)
	buffer := make([]byte, 32*1024)
	for {
		n, readErr := response.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				break
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			break
		}
	}
	p.record(response.StatusCode, false, time.Since(start))
}

func (p *Server) connect(w http.ResponseWriter, r *http.Request, start time.Time) {
	address := r.Host
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, "443")
	}
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(r.Context(), "tcp", address)
	if err != nil {
		http.Error(w, "CONNECT upstream unavailable", http.StatusBadGateway)
		p.record(502, false, time.Since(start))
		return
	}
	client, buffered, err := w.(http.Hijacker).Hijack()
	if err != nil {
		upstream.Close()
		p.record(500, false, time.Since(start))
		return
	}
	defer client.Close()
	defer upstream.Close()
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	stop := context.AfterFunc(r.Context(), func() { _ = client.Close(); _ = upstream.Close() })
	defer stop()
	finished := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(upstream, buffered)
		if tcp, ok := upstream.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		finished <- struct{}{}
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.Close()
	<-finished
	p.record(200, false, time.Since(start))
}

func prependRules(r *http.Request, originalHost string, cfg config.Config) error {
	if cfg.GlobalRules == "" || r.Method == http.MethodGet || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") || r.Header.Get("Content-Encoding") != "" {
		return nil
	}
	for _, target := range cfg.Network.RuleTargets {
		if !hostMatches(originalHost, target.Host) || !strings.HasPrefix(r.URL.Path, target.PathPrefix) {
			continue
		}
		if r.Body == nil {
			return nil
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRuleBody+1))
		if err != nil {
			return err
		}
		if len(body) > maxRuleBody {
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
			return nil
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(body, &value); err != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}
		var existing string
		if raw, ok := value[target.Field]; !ok || json.Unmarshal(raw, &existing) != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
			return nil
		}
		value[target.Field], _ = json.Marshal(cfg.GlobalRules + "\n\n" + existing)
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(encoded))
		r.ContentLength = int64(len(encoded))
		r.Header.Set("Content-Length", strconv.Itoa(len(encoded)))
		return nil
	}
	return nil
}

func hostname(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.ToLower(strings.Trim(host, "[]"))
	}
	return strings.ToLower(strings.Trim(hostport, "[]"))
}
func hostMatches(host, pattern string) bool {
	pattern = strings.ToLower(strings.TrimPrefix(pattern, "*."))
	return host == pattern || strings.HasSuffix(host, "."+pattern)
}
func blocked(host string, hosts []string) bool {
	for _, pattern := range hosts {
		if hostMatches(host, pattern) {
			return true
		}
	}
	return false
}
func matchingOverride(host string, overrides []config.EndpointOverride) *config.EndpointOverride {
	for i := range overrides {
		if hostMatches(host, overrides[i].Host) {
			return &overrides[i]
		}
	}
	return nil
}

func removeHopHeaders(header http.Header) {
	for _, token := range strings.Split(header.Get("Connection"), ",") {
		header.Del(strings.TrimSpace(token))
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(key)
	}
}
func copyHeaders(destination, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

var ErrHTTPSOpaque = errors.New("HTTPS CONNECT payload is opaque; endpoint and prompt rewriting require an explicit HTTP endpoint")
