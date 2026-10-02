//go:build windows

package netproxy

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// internetSettingsKey 是 Windows「设置 → 网络和 Internet → 代理」写入的位置。
// 大多数代理工具（含系统自带的设置面板）只写这里的环境，不设置进程环境变量。
const internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// systemUpstreamProxy 读取 Windows 系统代理设置。
// 返回值第三项是 ProxyOverride（旁路清单的原始字符串，分号分隔）。
//
// 只读 HKCU：代理是当前用户的设置，2Ag 不改写它。
func systemUpstreamProxy() (httpURL, httpsURL *url.URL, override string) {
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil, ""
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil, ""
	}
	override, _, _ = key.GetStringValue("ProxyOverride")
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil {
		return nil, nil, override
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, nil, override
	}
	httpURL, httpsURL = parseProxyServerValue(server)
	return httpURL, httpsURL, override
}
