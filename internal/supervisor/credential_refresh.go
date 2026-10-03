package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// Original client configuration comes from the user's installed native runtime.
// It is never bundled, persisted, returned through the API, or logged.
type nativeOAuthClient struct{ id, secret string }

type CredentialRefreshError struct {
	Code       string
	HTTPStatus int
	Operation  string
	Cause      string
}

func (e *CredentialRefreshError) Error() string {
	return fmt.Sprintf("目标登录预检失败（OAuth %s / %s / %s / HTTP %d）；当前宿主未停止", e.Code, e.Operation, e.Cause, e.HTTPStatus)
}

func refreshNetworkFailure(err error, operation string) *CredentialRefreshError {
	cause := "connection_failed"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		cause = "timeout"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		cause = "connection_closed"
	case strings.Contains(strings.ToLower(err.Error()), "connection reset"):
		cause = "connection_reset"
	case strings.Contains(strings.ToLower(err.Error()), "proxyconnect"):
		cause = "proxy_connection"
	}
	return &CredentialRefreshError{Code: "network", Operation: operation, Cause: cause}
}

var oauthAudiencePattern = regexp.MustCompile(`^[0-9]+-[a-z0-9]+\.apps\.googleusercontent\.com$`)
var nativeClientSecretPattern = regexp.MustCompile("GOC" + "SPX-[A-Za-z0-9_-]{28}")

// Native 2.19's public desktop client constants are Go string data, not JSON.
// Read bounded chunks, match the archived audience, then let Google's server
// validate candidate configurations. Unsupported formats fail before host stop.
func nativeOAuthClientsFromFile(file, audience string) ([]nativeOAuthClient, error) {
	if !oauthAudiencePattern.MatchString(audience) {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 512<<20 {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	buf := make([]byte, 64<<10)
	var carry []byte
	matched := false
	seen := map[string]bool{}
	var clients []nativeOAuthClient
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			chunk := append(carry, buf[:n]...)
			matched = matched || bytes.Contains(chunk, []byte(audience))
			for _, match := range nativeClientSecretPattern.FindAll(chunk, -1) {
				secret := string(match)
				if !seen[secret] {
					seen[secret] = true
					clients = append(clients, nativeOAuthClient{audience, secret})
					if len(clients) > 8 {
						return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
					}
				}
			}
			start := len(chunk) - 512
			if start < 0 {
				start = 0
			}
			carry = append([]byte(nil), chunk[start:]...)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
		}
	}
	if !matched || len(clients) == 0 {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	return clients, nil
}

type credentialRefreshResponse struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	ID      string `json:"id_token"`
	Expires int64  `json:"expires_in"`
	Error   string `json:"error"`
}

func safeOAuthCode(code string) string {
	switch code {
	case "invalid_grant", "invalid_client", "unauthorized_client", "access_denied", "temporarily_unavailable", "invalid_request":
		return code
	}
	return "rejected"
}

// Tests inject endpoints and an HTTP client; production uses fixed Google HTTPS
// endpoints and refuses redirects. Responses and headers never reach logs.
func refreshCredentialWithClient(ctx context.Context, raw []byte, email string, clients []nativeOAuthClient, httpClient *http.Client, tokenURL, userURL string, now time.Time, identityStage func(), retain func([]byte) error) ([]byte, error) {
	owner, ok := credentialRestorableEmail(raw)
	if !ok || !strings.EqualFold(owner, email) {
		return nil, &CredentialRefreshError{Code: "identity_mismatch"}
	}
	v, _ := parseCredentialBlobView(raw)
	audience, _ := decodeJWTPayload(v.IDToken)["aud"].(string)
	var refreshed credentialRefreshResponse
	success := false
	for _, client := range clients {
		if client.id != audience {
			continue
		}
		form := url.Values{"client_id": {client.id}, "client_secret": {client.secret}, "refresh_token": {v.Token.RefreshToken}, "grant_type": {"refresh_token"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, &CredentialRefreshError{Code: "network"}
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, refreshNetworkFailure(err, "token_refresh")
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		refreshed = credentialRefreshResponse{}
		if readErr != nil || json.Unmarshal(body, &refreshed) != nil {
			return nil, &CredentialRefreshError{Code: "malformed_response", HTTPStatus: resp.StatusCode}
		}
		if resp.StatusCode != http.StatusOK {
			code := safeOAuthCode(refreshed.Error)
			if code == "invalid_client" || code == "unauthorized_client" {
				continue
			}
			return nil, &CredentialRefreshError{Code: code, HTTPStatus: resp.StatusCode}
		}
		if strings.TrimSpace(refreshed.Access) == "" || refreshed.Expires < 30 || refreshed.Expires > 86400 {
			return nil, &CredentialRefreshError{Code: "malformed_response"}
		}
		success = true
		break
	}
	if !success {
		return nil, &CredentialRefreshError{Code: "native_client_unavailable"}
	}
	fresh, err := mergeNativeRefresh(raw, refreshed, now)
	if err != nil {
		return nil, err
	}
	// Retain a rotated grant before the next network boundary. This is a
	// DPAPI candidate, never an active/verified account or system credential.
	if retain != nil {
		if err := retain(fresh); err != nil {
			return nil, &CredentialRefreshError{Code: "candidate_save_failed"}
		}
	}
	if identityStage != nil {
		identityStage()
	}
	status, body, readErr := lookupRefreshedIdentity(ctx, httpClient, userURL, refreshed.Access)
	var user struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Subject  string `json:"sub"`
	}
	if readErr != nil {
		return nil, refreshNetworkFailure(readErr, "google_identity")
	}
	if status >= 500 {
		return nil, &CredentialRefreshError{Code: "temporarily_unavailable", HTTPStatus: status}
	}
	if status != http.StatusOK {
		return nil, &CredentialRefreshError{Code: "access_denied", HTTPStatus: status}
	}
	if json.Unmarshal(body, &user) != nil {
		return nil, &CredentialRefreshError{Code: "malformed_response", HTTPStatus: status}
	}
	if !user.Verified || !strings.EqualFold(user.Email, email) {
		return nil, &CredentialRefreshError{Code: "identity_mismatch", HTTPStatus: status}
	}
	if refreshed.ID != "" {
		claims := decodeJWTPayload(refreshed.ID)
		got, _ := claims["email"].(string)
		aud, _ := claims["aud"].(string)
		sub, _ := claims["sub"].(string)
		if !strings.EqualFold(got, email) || aud != audience || (user.Subject != "" && sub != "" && user.Subject != sub) {
			return nil, &CredentialRefreshError{Code: "identity_mismatch"}
		}
	}
	return fresh, nil
}

// Only the read-only identity lookup is retried, within the original deadline.
// Never blindly replay a refresh POST whose server-side outcome is unknown.
func lookupRefreshedIdentity(ctx context.Context, client *http.Client, endpoint, access string) (int, []byte, error) {
	var status int
	var body []byte
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+access)
		var resp *http.Response
		resp, err = client.Do(req)
		if err == nil {
			status = resp.StatusCode
			body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if err == nil && status < 500 {
				return status, body, nil
			}
		}
		if ctx.Err() != nil {
			return status, nil, ctx.Err()
		}
		client.CloseIdleConnections()
	}
	return status, body, err
}

func mergeNativeRefresh(raw []byte, refreshed credentialRefreshResponse, now time.Time) ([]byte, error) {
	v, ok := parseCredentialBlobView(raw)
	if !ok || v.Token == nil {
		return nil, &CredentialRefreshError{Code: "identity_mismatch"}
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, &CredentialRefreshError{Code: "malformed_response"}
	}
	token, ok := payload["token"].(map[string]any)
	if !ok {
		return nil, &CredentialRefreshError{Code: "malformed_response"}
	}
	token["access_token"] = refreshed.Access
	token["token_type"] = "Bearer"
	token["expiry"] = now.Add(time.Duration(refreshed.Expires) * time.Second).UTC().Format(time.RFC3339Nano)
	if refreshed.Refresh != "" {
		token["refresh_token"] = refreshed.Refresh
	}
	if refreshed.ID != "" {
		if _, nested := token["id_token"]; nested {
			token["id_token"] = refreshed.ID
		}
		if _, top := payload["id_token"]; top || v.Token.IDToken == "" {
			payload["id_token"] = refreshed.ID
		}
	}
	fresh, err := json.Marshal(payload)
	if err != nil {
		return nil, &CredentialRefreshError{Code: "malformed_response"}
	}
	return fresh, nil
}
