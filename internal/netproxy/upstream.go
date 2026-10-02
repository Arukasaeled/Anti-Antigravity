package netproxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// upstreamProxy 描述「用户自己已经有的那个代理」。
//
// 为什么必须有这一层：宿主一旦被加上 --proxy-server=http://<2Ag 代理>，
// Chromium 就不再读自己的环境变量/系统代理设置了 —— 显式 --proxy-server
// 会完全接管代理决策。而 2Ag 的转发代理原先只做裸直连（transport.Proxy 为 nil、
// CONNECT 用 net.Dialer 直连目标），宿主的全部流量因此被从用户的上游代理上
// 摘了下来。在必须经代理才能访问外网的环境里，宿主会整体连不通，表现就是
// Google 登录时 oauth2.googleapis.com 连接失败，而用户的原生 Antigravity
// 明明是好的。
//
// 结论：2Ag 对用户网络的正确姿势是「插入一层」，不是「替换一层」。这里把用户
// 的上游代理接回来，隐私拦截与端点改写仍在 2Ag 这一层完成。
type upstreamProxy struct {
	http    *url.URL
	https   *url.URL
	noProxy []string
}

// directTargetCheck 判定某个目标是否必须直连（环回与未指定地址不进代理：
// 既避免转发自环，也避免把 Manager/CDP 的回环流量绕出去）。
//
// 声明成变量是为了让测试能够在本地回环上验证「借道上游代理」这条链路本身，
// 生产语义由 isDirectTarget 决定，不变。
var directTargetCheck = isDirectTarget

// loadUpstreamProxy 解析用户的上游代理，没有则返回 nil（行为与本层引入前一致）。
//
// 优先级：环境变量（HTTPS_PROXY / HTTP_PROXY / ALL_PROXY，与 curl 等工具一致）
// 高于 Windows 系统代理设置。很多代理工具只写系统代理、不设环境变量，而宿主
// 现在读不到那份设置了，所以必须由 2Ag 代读。
func loadUpstreamProxy(selfAddr string) *upstreamProxy {
	up := &upstreamProxy{noProxy: noProxyEntries()}
	up.https = envProxy("HTTPS_PROXY", "https_proxy")
	up.http = envProxy("HTTP_PROXY", "http_proxy")
	if all := envProxy("ALL_PROXY", "all_proxy"); all != nil {
		if up.https == nil {
			up.https = all
		}
		if up.http == nil {
			up.http = all
		}
	}
	source := "环境变量"
	if up.http == nil && up.https == nil {
		systemHTTP, systemHTTPS, override := systemUpstreamProxy()
		up.http, up.https = systemHTTP, systemHTTPS
		source = "Windows 系统代理设置"
		if entries := splitProxyOverride(override); len(entries) > 0 {
			up.noProxy = append(up.noProxy, entries...)
		}
	}
	up.https = usableUpstream(up.https, selfAddr, source)
	up.http = usableUpstream(up.http, selfAddr, source)
	if up.http == nil && up.https == nil {
		return nil
	}
	log.Printf("[2ag] 转发代理将借道用户的上游代理（%s）：%s", source, up.describe())
	return up
}

// usableUpstream 丢弃 2Ag 无法借道、或会造成转发自环的上游代理。
func usableUpstream(u *url.URL, selfAddr, source string) *url.URL {
	if u == nil || u.Host == "" {
		return nil
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "":
		u.Scheme = "http"
	default:
		log.Printf("[2ag] 忽略不支持的上游代理 scheme %q（来自%s，2Ag 只支持 http 代理）：%s", u.Scheme, source, redactProxyURL(u))
		return nil
	}
	if sameEndpoint(u, selfAddr) {
		log.Printf("[2ag] 忽略指回 2Ag 自身的上游代理（会造成转发自环）：%s", redactProxyURL(u))
		return nil
	}
	return u
}

// proxyFor 返回访问 host 时应当借道的上游代理，nil 表示直连。
func (u *upstreamProxy) proxyFor(host string, https bool) *url.URL {
	if u == nil || (u.http == nil && u.https == nil) {
		return nil
	}
	if directTargetCheck(host) || noProxyMatch(host, u.noProxy) {
		return nil
	}
	if https {
		if u.https != nil {
			return u.https
		}
		return u.http
	}
	if u.http != nil {
		return u.http
	}
	return u.https
}

// proxyFunc 适配 http.Transport.Proxy。没有上游代理时返回 nil，
// 让 transport 保持「直连」语义。
func (u *upstreamProxy) proxyFunc() func(*http.Request) (*url.URL, error) {
	if u == nil || (u.http == nil && u.https == nil) {
		return nil
	}
	return func(req *http.Request) (*url.URL, error) {
		return u.proxyFor(req.URL.Hostname(), strings.EqualFold(req.URL.Scheme, "https")), nil
	}
}

func (u *upstreamProxy) describe() string {
	parts := make([]string, 0, 3)
	if u.https != nil {
		parts = append(parts, "https→"+redactProxyURL(u.https))
	}
	if u.http != nil {
		parts = append(parts, "http→"+redactProxyURL(u.http))
	}
	if len(u.noProxy) > 0 {
		parts = append(parts, "no_proxy="+strings.Join(u.noProxy, ","))
	}
	return strings.Join(parts, " ")
}

// dialThroughUpstream 经上游代理建立一条 CONNECT 不透明隧道。
// 与 2Ag 自身的对外姿态一致：不解密 TLS，只转发字节。
func dialThroughUpstream(ctx context.Context, proxyURL *url.URL, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", proxyHostPort(proxyURL))
	if err != nil {
		return nil, fmt.Errorf("连接上游代理 %s 失败: %w", redactProxyURL(proxyURL), err)
	}
	request := "CONNECT " + address + " HTTP/1.1\r\nHost: " + address + "\r\n"
	if auth := proxyAuthorization(proxyURL); auth != "" {
		request += "Proxy-Authorization: " + auth + "\r\n"
	}
	request += "\r\n"
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		conn.Close()
		return nil, err
	}
	if strings.EqualFold(proxyURL.Scheme, "https") {
		secure := tls.Client(conn, &tls.Config{ServerName: proxyURL.Hostname(), MinVersion: tls.VersionTLS12})
		if err := secure.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("建立上游代理 TLS 连接失败: %w", err)
		}
		conn = secure
	}
	if _, err := io.WriteString(conn, request); err != nil {
		conn.Close()
		return nil, fmt.Errorf("向上游代理发起 CONNECT 失败: %w", err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("读取上游代理 CONNECT 响应失败: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("上游代理拒绝 CONNECT %s：%s", address, response.Status)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, err
	}
	return &tunnelConn{Conn: conn, reader: reader}, nil
}

// proxyAuthorization 生成上游代理的 Proxy-Authorization 值。
// 凭据只发往用户自己的代理，不会出现在任何日志里（日志走 redactProxyURL）。
func proxyAuthorization(u *url.URL) string {
	if u == nil || u.User == nil {
		return ""
	}
	password, _ := u.User.Password()
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(u.User.Username()+":"+password))
}

// tunnelConn 保证 CONNECT 响应之后已经被 bufio 预读的字节不丢失 ——
// http.ReadResponse 可能一次读进响应头之后的隧道载荷。
type tunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *tunnelConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

// CloseWrite 让上游链路同样支持半关闭（直连时 *net.TCPConn 本来就有）。
func (c *tunnelConn) CloseWrite() error {
	if writer, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return writer.CloseWrite()
	}
	return nil
}

// parseProxyServerValue 解析 HKCU Internet Settings 的 ProxyServer 值。
//
// 两种写法（Windows 两种都接受）：
//   - "host:port"                      —— 对所有协议生效
//   - "http=host:port;https=host:port" —— 按协议分别指定
func parseProxyServerValue(server string) (httpURL, httpsURL *url.URL) {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, nil
	}
	if !strings.Contains(server, "=") {
		parsed, err := parseUpstreamURL(server)
		if err != nil {
			log.Printf("[2ag] 忽略无法解析的系统代理：%v", err)
			return nil, nil
		}
		return parsed, parsed
	}
	for _, part := range strings.Split(server, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		parsed, err := parseUpstreamURL(value)
		if err != nil {
			log.Printf("[2ag] 忽略无法解析的系统代理项：%v", err)
			continue
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "http":
			httpURL = parsed
		case "https":
			httpsURL = parsed
		}
	}
	// 只配了一侧时，让另一侧沿用同一代理：只代理 HTTPS 的系统非常常见。
	if httpURL != nil && httpsURL == nil {
		httpsURL = httpURL
	}
	if httpsURL != nil && httpURL == nil {
		httpURL = httpsURL
	}
	return httpURL, httpsURL
}

// envProxy 返回第一个可解析的代理环境变量值。
func envProxy(names ...string) *url.URL {
	for _, name := range names {
		// http_proxy 的全小写形式在 CGI 环境里可能由请求头注入（httpoxy），
		// 这里与 net/http 的处理保持一致：存在 REQUEST_METHOD 时忽略它。
		if name == "http_proxy" && os.Getenv("REQUEST_METHOD") != "" {
			continue
		}
		raw := strings.TrimSpace(os.Getenv(name))
		if raw == "" {
			continue
		}
		u, err := parseUpstreamURL(raw)
		if err != nil {
			log.Printf("[2ag] 忽略无法解析的代理环境变量 %s：%v", name, err)
			continue
		}
		return u
	}
	return nil
}

// parseUpstreamURL 解析代理地址；缺少 scheme 时按 http 处理
// （curl、Chromium 对 "host:port" 写法也是这个约定）。
func parseUpstreamURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("代理地址为空")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		// url.Parse 的错误包含原始 URL，可能携带代理用户名和密码。
		return nil, fmt.Errorf("代理地址格式无效")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("代理地址缺少主机名")
	}
	return u, nil
}

// noProxyEntries 解析 NO_PROXY / no_proxy（逗号分隔）。
func noProxyEntries() []string {
	raw := strings.TrimSpace(os.Getenv("NO_PROXY"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("no_proxy"))
	}
	if raw == "" {
		return nil
	}
	return splitProxyOverride(raw)
}

// splitProxyOverride 把逗号或分号分隔的旁路清单拆成条目。
// 分号是 Windows ProxyOverride 的写法，逗号是 NO_PROXY 的写法。
func splitProxyOverride(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' })
	entries := make([]string, 0, len(fields))
	for _, field := range fields {
		if entry := strings.TrimSpace(field); entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// noProxyMatch 判断目标是否命中旁路清单。
// 支持 NO_PROXY 的通行写法：*、<local>、精确主机、.后缀、host:port、
// IP、CIDR，以及 Windows 系统设置里的 10.* 形式。
func noProxyMatch(host string, entries []string) bool {
	if host == "" {
		return true
	}
	host = strings.ToLower(host)
	for _, raw := range entries {
		entry := strings.ToLower(strings.TrimSpace(raw))
		if entry == "" {
			continue
		}
		if entry == "*" {
			return true
		}
		if entry == "<local>" {
			if !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if head, _, err := net.SplitHostPort(entry); err == nil {
			entry = head
		}
		if strings.HasSuffix(entry, ".*") {
			if strings.HasPrefix(host, strings.TrimSuffix(entry, "*")) {
				return true
			}
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			if _, cidr, err := net.ParseCIDR(entry); err == nil && cidr.Contains(ip) {
				return true
			}
		}
		entry = strings.TrimPrefix(entry, ".")
		if host == entry || strings.HasSuffix(host, "."+entry) {
			return true
		}
	}
	return false
}

// isDirectTarget 判断目标是否必须直连（环回、未指定地址、空主机）。
func isDirectTarget(host string) bool {
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

// proxyHostPort 返回上游代理的 host:port，端口缺省时按 scheme 补默认值。
func proxyHostPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return net.JoinHostPort(u.Hostname(), port)
	}
	if strings.EqualFold(u.Scheme, "https") {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}

// sameEndpoint 判断上游代理是否就是 2Ag 自己监听的地址。
func sameEndpoint(u *url.URL, selfAddr string) bool {
	if u == nil || u.Host == "" || selfAddr == "" {
		return false
	}
	return strings.EqualFold(proxyHostPort(u), selfAddr)
}

// redactProxyURL 输出可安全进日志的代理地址（口令永远不落日志）。
func redactProxyURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return u.Scheme + "://" + u.User.Username() + ":***@" + u.Host
		}
	}
	return u.Scheme + "://" + u.Host
}
