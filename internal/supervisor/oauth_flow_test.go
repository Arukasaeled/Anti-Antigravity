package supervisor

import (
	"strings"
	"testing"
)

// TestImportAccountsJSON 覆盖「导入账号清单」的三条受支持路径。
//
// 第四条路径（GCP OAuth 客户端凭据文件）连同整条自有 OAuth 路线一起被删除，
// 所以这里如实断死：那种输入必须**被拒绝**，而不是被当成一个账号收下。
// 这条负例比前三条正例更要紧 —— 它守住的是「2Ag 不接受、不持有 Google OAuth
// 客户端凭据」这条发布契约。
func TestImportAccountsJSON(t *testing.T) {
	// Test 1: cockpit-tools format
	cockpitJSON := []byte(`{
		"version": "2.0",
		"accounts": [
			{
				"id": "test-1",
				"email": "tester1@example.com",
				"name": "Tester One"
			}
		]
	}`)
	accounts, err := ImportAccountsJSON(cockpitJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(cockpitJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 2: Array format
	arrayJSON := []byte(`[
		{
			"email": "tester2@example.com",
			"name": "Tester Two",
			"refresh_token": "1//test_refresh_token"
		}
	]`)
	accounts, err = ImportAccountsJSON(arrayJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(arrayJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 3: Single account object
	singleJSON := []byte(`{
		"email": "tester3@example.com",
		"name": "Tester Three"
	}`)
	accounts, err = ImportAccountsJSON(singleJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(singleJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 4: GCP OAuth client credentials must be REJECTED.
	gcpJSON := []byte(`{
		"installed": {
			"client_id": "testclientid12345.apps.googleusercontent.com"
		}
	}`)
	if _, err := ImportAccountsJSON(gcpJSON); err == nil {
		t.Fatal("GCP OAuth 客户端凭据文件必须被拒绝：2Ag 不再是任何人的 OAuth 客户端")
	} else if !strings.Contains(err.Error(), "无法识别") {
		t.Fatalf("拒绝 GCP 凭据时给出的原因不合预期: %v", err)
	}
}

// TestCredentialUsableEmail 守住「中间态不得入库」这条判定。
//
// 官方 Antigravity 在 OAuth 过程中会先写一个 {"token": null} 占位凭据；
// 如果把它当成登录成功，保险库里就会多出一个没有 refresh_token 的空账号。
func TestCredentialUsableEmail(t *testing.T) {
	if email, ok := credentialUsableEmail([]byte(`{"token":null}`)); ok {
		t.Fatalf("中间态 {\"token\":null} 不得被判为可用登录（误判为 %q）", email)
	}
	if _, ok := credentialUsableEmail([]byte(`{}`)); ok {
		t.Fatal("空对象不得被判为可用登录")
	}
	if _, ok := credentialUsableEmail([]byte(`{"token":{"access_token":"x","token_type":"Bearer"}}`)); ok {
		t.Fatal("缺少 id_token 时不得被判为可用登录")
	}
	// 一份带过期 exp 的 id_token：也不算可用登录。
	expired := []byte(`{"token":{"access_token":"x"},"id_token":"eyJhbGciOiJub25lIn0.eyJlbWFpbCI6ImFAYi5jIiwiZXhwIjoxMDAwMDAwMDAwfQ."}`)
	if _, ok := credentialUsableEmail(expired); ok {
		t.Fatal("id_token 已过期时不得被判为可用登录")
	}
	// 完整凭据：必须能解析出邮箱。
	good := []byte(`{"token":{"access_token":"x","token_type":"Bearer"},"id_token":"eyJhbGciOiJub25lIn0.eyJlbWFpbCI6ImFAYi5jIiwiZXhwIjo0MTAyNDQ0ODAwfQ."}`)
	email, ok := credentialUsableEmail(good)
	if !ok || email != "a@b.c" {
		t.Fatalf("完整凭据应被判为可用并解析出邮箱，得到 ok=%v email=%q", ok, email)
	}
}
