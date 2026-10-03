//go:build windows

package supervisor

import (
	"context"
	"net/http"
	"path/filepath"
	"time"

	"github.com/2ag/2ag/internal/netproxy"
)

func refreshTargetCredential(raw []byte, email, mode string) ([]byte, error) {
	parent := raw
	if candidate, err := readRefreshCandidate(email, parent); err != nil {
		return nil, &CredentialRefreshError{Code: "candidate_unavailable"}
	} else if len(candidate) > 0 {
		raw = candidate
	}
	v, ok := parseCredentialBlobView(raw)
	if !ok {
		return nil, &CredentialRefreshError{Code: "identity_mismatch"}
	}
	audience, _ := decodeJWTPayload(v.IDToken)["aud"].(string)
	exe := getProcessExePath(GetManagedHostPID())
	if exe == "" {
		if mode == RuntimeModeOfficialValue {
			exe = FindOfficialAntigravity()
		} else {
			exe = FrozenHostExePath()
			if exe == "" {
				exe = FindOfficialAntigravity()
			}
		}
	}
	root := filepath.Dir(exe)
	var clients []nativeOAuthClient
	for _, rel := range []string{"resources/bin/language_server.exe", "resources/app/extensions/antigravity/bin/language_server_windows_x64.exe"} {
		if found, err := nativeOAuthClientsFromFile(filepath.Join(root, filepath.FromSlash(rel)), audience); err == nil {
			clients = found
			break
		}
	}
	if len(clients) == 0 {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	transport := netproxy.NewUserNetworkTransport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return refreshCredentialWithClient(ctx, raw, email, clients, client, "https://oauth2.googleapis.com/token", "https://www.googleapis.com/oauth2/v3/userinfo", time.Now(), func() { transactionStage("ValidatingTargetIdentity", "") }, func(fresh []byte) error { return saveRefreshCandidate(email, parent, fresh) })
}
