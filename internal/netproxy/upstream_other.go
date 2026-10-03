//go:build !windows

package netproxy

import "net/url"

// systemUpstreamProxy 在非 Windows 平台没有系统级代理设置可读；
// 上游代理只从环境变量解析（见 loadUpstreamProxy）。
func systemUpstreamProxy() (httpURL, httpsURL *url.URL, override string) {
	return nil, nil, ""
}
