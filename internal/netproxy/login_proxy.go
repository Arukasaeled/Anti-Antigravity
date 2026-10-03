package netproxy

import (
	"fmt"
	"net/url"
	"strings"
)

// DetectLoginProxy reads an explicit environment snapshot or the user's system
// proxy, without mutating the parent process or permanent Windows settings.
func DetectLoginProxy(env []string) (string, error) {
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[strings.ToUpper(key)] = value
		}
	}
	var selected *url.URL
	for _, key := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY"} {
		if value := strings.TrimSpace(values[key]); value != "" {
			var err error
			selected, err = parseUpstreamURL(value)
			if err != nil {
				return "", fmt.Errorf("检测到的代理地址无效，请检查代理设置")
			}
			break
		}
	}
	if selected == nil {
		httpProxy, httpsProxy, _ := systemUpstreamProxy()
		selected = httpsProxy
		if selected == nil {
			selected = httpProxy
		}
	}
	if selected == nil {
		return "", fmt.Errorf("未检测到代理，请启用系统代理或选择直连")
	}
	if selected.Scheme != "http" && selected.Scheme != "https" {
		return "", fmt.Errorf("本次官方登录只支持 HTTP / HTTPS 代理")
	}
	if selected.User != nil {
		return "", fmt.Errorf("官方登录不支持带账号密码的代理，请选择本机代理或直连")
	}
	return selected.String(), nil
}
