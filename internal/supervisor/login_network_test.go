package supervisor

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoginNetworkChildIsolation(t *testing.T) {
	parent := []string{"PATH=fixture", "HTTPS_PROXY=http://127.0.0.1:1", "http_proxy=http://127.0.0.1:1", "NO_PROXY=googleapis.com"}
	before := append([]string(nil), parent...)
	direct, err := buildLoginNetwork("DIRECT", parent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(direct.Env, "\n"), "127.0.0.1:1") || !reflect.DeepEqual(direct.Args, []string{"--no-proxy-server"}) {
		t.Fatal("dead proxy inherited by DIRECT child")
	}
	proxy, err := buildLoginNetwork("PROXY", []string{"HTTPS_PROXY=http://127.0.0.1:7897", "NO_PROXY=*"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(proxy.Args, []string{"--proxy-server=http://127.0.0.1:7897"}) || strings.Contains(strings.Join(proxy.Env, "\n"), "NO_PROXY=*") {
		t.Fatal("PROXY bypassed")
	}
	auto, err := buildLoginNetwork("AUTO", parent)
	if err != nil || !reflect.DeepEqual(auto.Env, parent) || len(auto.Args) != 0 {
		t.Fatal("AUTO changed default behavior")
	}
	if !reflect.DeepEqual(before, parent) {
		t.Fatal("parent environment mutated")
	}
	if _, err := buildLoginNetwork("invalid", parent); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
