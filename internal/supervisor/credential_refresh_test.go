package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeRefreshPreservesArchiveAndVerifiesIdentity(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, issueID := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "top", true: "nested"}[nested], map[bool]string{false: "no-ID", true: "new-ID"}[issueID]}, "/"), func(t *testing.T) {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/token" {
						calls++
						r.ParseForm()
						if r.Form.Get("refresh_token") != "fixture-old-refresh" || r.Form.Get("grant_type") != "refresh_token" {
							t.Error("wrong refresh grant")
						}
						if r.Form.Get("client_secret") == "wrong-client" {
							w.WriteHeader(401)
							json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client"})
							return
						}
						reply := map[string]any{"access_token": "fixture-new-access", "refresh_token": "fixture-rotated-refresh", "expires_in": 3600}
						if issueID {
							reply["id_token"] = fixtureIdentity("target@example.com")
						}
						json.NewEncoder(w).Encode(reply)
					} else {
						if r.Header.Get("Authorization") != "Bearer fixture-new-access" {
							t.Error("identity must use refreshed access")
						}
						json.NewEncoder(w).Encode(map[string]any{"email": "target@example.com", "email_verified": true})
					}
				}))
				defer srv.Close()
				stage := false
				now := time.Now()
				fresh, err := refreshCredentialWithClient(context.Background(), fixtureCredential("target@example.com", nested), "target@example.com", []nativeOAuthClient{{"123-native.apps.googleusercontent.com", "wrong-client"}, {"123-native.apps.googleusercontent.com", "correct-client"}}, srv.Client(), srv.URL+"/token", srv.URL+"/user", now, func() { stage = true }, nil)
				if err != nil {
					t.Fatal(err)
				}
				v, ok := parseCredentialBlobView(fresh)
				if !ok || v.Token.RefreshToken != "fixture-rotated-refresh" || v.Token.AccessToken != "fixture-new-access" || v.IDToken == "" || !stage || calls != 2 {
					t.Fatal("renewal, client selection or identity proof lost")
				}
				var p map[string]any
				json.Unmarshal(fresh, &p)
				if p["future_metadata"] != "preserve" || p["auth_method"] != "consumer" {
					t.Fatal("native archive metadata lost")
				}
				if owner, ok := credentialUsableEmail(fresh); !ok || owner != "target@example.com" {
					t.Fatal("not a complete fresh login")
				}
			})
		}
	}
}

func TestNativeRefreshRejectsWrongIdentityAndRevocationWithoutSecrets(t *testing.T) {
	for _, code := range []string{"identity_mismatch", "invalid_grant"} {
		t.Run(code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					if code == "invalid_grant" {
						w.WriteHeader(400)
						json.NewEncoder(w).Encode(map[string]any{"error": code, "error_description": "fixture-secret-must-not-leak"})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-new-access", "expires_in": 3600})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"email": "other@example.com", "email_verified": true})
			}))
			defer srv.Close()
			fresh, err := refreshCredentialWithClient(context.Background(), fixtureCredential("target@example.com", false), "target@example.com", []nativeOAuthClient{{"123-native.apps.googleusercontent.com", "fixture-secret"}}, srv.Client(), srv.URL+"/token", srv.URL+"/user", time.Now(), nil, nil)
			var failure *CredentialRefreshError
			if fresh != nil || !errors.As(err, &failure) || failure.Code != code || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("unsafe or incorrectly admitted refresh")
			}
		})
	}
}

func TestNativeClientScanAcrossChunkBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "native-fixture")
	aud := "123-native.apps.googleusercontent.com"
	data := strings.Repeat(" ", (64<<10)-8) + "GOC" + "SPX-" + strings.Repeat("a", 28) + aud
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	clients, err := nativeOAuthClientsFromFile(path, aud)
	if err != nil || len(clients) != 1 {
		t.Fatal("native configuration split across chunks was missed")
	}
	if _, err := nativeOAuthClientsFromFile(path, "456-other.apps.googleusercontent.com"); err == nil {
		t.Fatal("unmatched client audience accepted")
	}
}

func TestRotatedGrantRetainedBeforeIdentityNetworkFailure(t *testing.T) {
	var retained []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-new-access", "refresh_token": "fixture-rotated-refresh", "expires_in": 3600})
			return
		}
		if len(retained) == 0 {
			t.Error("rotated credential was not retained before identity lookup")
		}
		w.WriteHeader(503)
	}))
	defer srv.Close()
	fresh, err := refreshCredentialWithClient(context.Background(), fixtureCredential("target@example.com", false), "target@example.com", []nativeOAuthClient{{"123-native.apps.googleusercontent.com", "fixture-secret"}}, srv.Client(), srv.URL+"/token", srv.URL+"/user", time.Now(), nil, func(candidate []byte) error { retained = append([]byte(nil), candidate...); return nil })
	if err == nil || fresh != nil {
		t.Fatal("failed identity must not promote the candidate")
	}
	v, ok := parseCredentialBlobView(retained)
	if !ok || v.Token.RefreshToken != "fixture-rotated-refresh" {
		t.Fatal("rotated grant was lost")
	}
}

func TestIdentityRetryDoesNotReplayRefreshGrant(t *testing.T) {
	tokenCalls, identityCalls := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls++
			json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-new-access", "expires_in": 3600})
			return
		}
		identityCalls++
		if identityCalls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"email": "target@example.com", "email_verified": true})
	}))
	defer srv.Close()
	fresh, err := refreshCredentialWithClient(context.Background(), fixtureCredential("target@example.com", false), "target@example.com", []nativeOAuthClient{{"123-native.apps.googleusercontent.com", "fixture-secret"}}, srv.Client(), srv.URL+"/token", srv.URL+"/user", time.Now(), nil, nil)
	if err != nil || len(fresh) == 0 || tokenCalls != 1 || identityCalls != 2 {
		t.Fatal("temporary identity outage must retry only the GET, then verify identity")
	}
}
