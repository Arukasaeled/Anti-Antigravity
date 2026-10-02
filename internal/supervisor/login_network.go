package supervisor

import (
	"fmt"
	"strings"

	"github.com/2ag/2ag/internal/netproxy"
)

type loginNetwork struct {
	Mode string
	Env  []string
	Args []string
}

func buildLoginNetwork(mode string, parent []string) (loginNetwork, error) {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode == "" {
		mode = "AUTO"
	}
	out := loginNetwork{Mode: mode, Env: append([]string(nil), parent...)}
	if mode == "AUTO" {
		return out, nil
	}
	if mode != "DIRECT" && mode != "PROXY" {
		return out, fmt.Errorf("登录网络模式必须为 AUTO / DIRECT / PROXY")
	}
	out.Env = nil
	for _, entry := range parent {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		out.Env = append(out.Env, entry)
	}
	if mode == "DIRECT" {
		out.Env = append(out.Env, "NO_PROXY=*", "no_proxy=*")
		out.Args = []string{"--no-proxy-server"}
		return out, nil
	}
	proxy, err := netproxy.DetectLoginProxy(parent)
	if err != nil {
		return out, err
	}
	out.Env = append(out.Env, "HTTP_PROXY="+proxy, "HTTPS_PROXY="+proxy, "ALL_PROXY="+proxy, "NO_PROXY=localhost,127.0.0.1,::1")
	out.Args = []string{"--proxy-server=" + proxy}
	return out, nil
}
