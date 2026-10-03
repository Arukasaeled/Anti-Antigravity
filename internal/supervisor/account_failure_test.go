package supervisor

import "testing"

func TestAccountFailuresAreDistinct(t *testing.T) {
	cases := map[string]string{
		"恢复原账号失败，请重启 2Ag":                  "restore_failure",
		"identity mismatch：凭据归属变化":         "identity_mismatch",
		"oauth2.googleapis.com/token: EOF": "connection_reset",
		"connection reset":                 "connection_reset",
		"代理连接失败":                           "network_proxy",
		"token endpoint HTTP 400":          "token_endpoint",
		"官方窗口报告 token endpoint 错误。请检查网络后重新登录。": "token_endpoint",
		"官方窗口报告连接超时，请检查代理。":                    "timeout",
		"等待登录超时":                   "timeout",
		"重新读取凭据失败，未归档":             "credential_not_written",
		"没有 gemini:antigravity 凭据": "credential_missing",
		"官方尚未完成登录":                 "official_not_logged_in",
		"账号操作正在进行":                 "account_busy",
	}
	for message, want := range cases {
		if got := AccountFailureCode(message); got != want {
			t.Errorf("%q: got %s want %s", message, got, want)
		}
	}
	if got := AccountFailureCode("登录成功"); got != "" {
		t.Fatal("successful login classified as failure")
	}
}
