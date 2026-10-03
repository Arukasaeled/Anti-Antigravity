// Package control holds only the current Manager process's control capability.
// It is never persisted or returned by telemetry endpoints.
package control

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"sync"
)

var session = struct {
	sync.RWMutex
	once    sync.Once
	token   string
	apiURL  string
	origins map[string]bool
}{origins: make(map[string]bool)}

func Token() string {
	session.once.Do(func() {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			panic("cannot generate Manager control token")
		}
		session.token = hex.EncodeToString(secret[:])
	})
	return session.token
}

func SetAPIURL(value string) {
	session.Lock()
	session.apiURL = value
	session.origins[value] = true
	session.Unlock()
}

func APIURL() string {
	session.RLock()
	defer session.RUnlock()
	return session.apiURL
}

// RegisterHostPage is called only for a workbench target selected by the CDP
// injector. Arbitrary localhost ports do not become trusted by making requests.
func RegisterHostPage(value string) {
	u, err := url.Parse(value)
	if err != nil {
		return
	}
	if u.Scheme == "vscode-file" {
		// An opaque native workbench origin still needs the secret on every API
		// request; registering it never grants unauthenticated control access.
		session.Lock()
		session.origins["null"] = true
		session.Unlock()
		return
	}
	host := strings.ToLower(u.Hostname())
	if (u.Scheme != "http" && u.Scheme != "https") || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return
	}
	session.Lock()
	session.origins[u.Scheme+"://"+u.Host] = true
	session.Unlock()
}

func TrustedOrigin(value string) bool {
	session.RLock()
	defer session.RUnlock()
	return session.origins[value]
}
