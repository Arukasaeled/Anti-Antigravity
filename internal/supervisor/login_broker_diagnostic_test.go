//go:build windows

package supervisor

import (
	"strings"
	"testing"
)

// TestClassifyLoginPageTextRecognisesProxyFailure 钉住用户实际遇到的那一幕：
// 官方登录页显示 proxyconnect 报错时，2Ag 必须能把它转述成可诊断的提示，
// 而不是让用户对着「等待 Google 登录」干等到超时。
func TestClassifyLoginPageTextRecognisesProxyFailure(t *testing.T) {
	// 这是用户截图里官方页面报错的原文。
	pageText := `There was an unexpected issue setting up your account.
Post "https://oauth2.googleapis.com/token": proxyconnect tcp: dial tcp 127.0.0.1:1: connectex: No connection could be made because the target machine actively refused it.
Continue with different account
Having trouble? Let us know`

	got := classifyLoginPageText(pageText)
	if got == "" {
		t.Fatal("官方登录页报出 proxyconnect 时，必须给出可诊断提示")
	}
	if !strings.Contains(got, "代理") {
		t.Errorf("提示应把排查方向指向代理，实际: %s", got)
	}
	if strings.Contains(got, "2Ag") && strings.Contains(got, "故障") {
		t.Errorf("2Ag 不改写网络，不应把自己的名字和「故障」并列: %s", got)
	}
}

func TestClassifyLoginPageTextRecognisesCommonNetErrors(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		mustHave string
	}{
		{name: "连接被拒绝", text: "ERR_CONNECTION_REFUSED", mustHave: "拒绝"},
		{name: "DNS 解析失败", text: "ERR_NAME_NOT_RESOLVED", mustHave: "解析"},
		{name: "网络断开", text: "ERR_INTERNET_DISCONNECTED", mustHave: "断开"},
		{name: "隧道建立失败", text: "ERR_TUNNEL_CONNECTION_FAILED", mustHave: "隧道"},
		{name: "连接超时", text: "ERR_CONNECTION_TIMED_OUT", mustHave: "超时"},
		{name: "Chromium 代理失败码", text: "ERR_PROXY_CONNECTION_FAILED", mustHave: "代理"},
		{name: "官方通用报错", text: "There was an unexpected issue setting up your account.", mustHave: "官方窗口"},
		{
			// 用户 2026-10-02 在干净环境里点登录后看到的原文。裸 EOF 不是
			// 「连不上」，而是「连接已建立然后被切断」，必须与 dial 类错误区分开。
			name: "Google 连接被中途切断 (loadCodeAssist EOF)",
			text: `There was an unexpected issue setting up your account.
failed to get load code assist response: Post "https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist": EOF`,
			mustHave: "EOF",
		},
		{
			name:     "Google 令牌端点被中途切断 (token EOF)",
			text:     `There was an unexpected issue setting up your account.` + "\n" + `Post "https://oauth2.googleapis.com/token": EOF`,
			mustHave: "EOF",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLoginPageText(tc.text)
			if got == "" {
				t.Fatalf("应识别出故障: %q", tc.text)
			}
			if !strings.Contains(got, tc.mustHave) {
				t.Errorf("提示应包含 %q，实际: %s", tc.mustHave, got)
			}
		})
	}
}

// TestClassifyLoginPageTextStaysQuietOnNormalPages 是反向断言：
// 正常的登录页（没有报错）绝不能被谎报成故障，否则用户会收到假警报。
func TestClassifyLoginPageTextStaysQuietOnNormalPages(t *testing.T) {
	benign := []string{
		"",
		"Sign in with Google\nContinue with Google",
		"Choose an account\nto continue to Antigravity",
		"Welcome to Antigravity\nAgree and continue",
		"正在登录，请稍候…",
		// 只提到 enable/disable，不是故障。
		"Proxy settings are managed by your organization",
		// 反向对照：单独一个 EOF 没有指向 Google 端点时不得触发（否则任何
		// 提到 EOF 的普通文案都会被谎报成网络故障）。
		"EOF is a common term in programming",
	}
	for _, text := range benign {
		if got := classifyLoginPageText(text); got != "" {
			t.Errorf("正常页面不应被判为故障: %q -> %q", text, got)
		}
	}
}

// TestClassifyLoginPageTextIsCaseInsensitive 覆盖 Chromium 有时以小写形式
// 出现在 DOM 文本里的情况。
func TestClassifyLoginPageTextIsCaseInsensitive(t *testing.T) {
	if got := classifyLoginPageText("err_proxy_connection_failed"); got == "" {
		t.Error("小写错误码也应被识别")
	}
	if got := classifyLoginPageText("ProxyConnect tcp: dial tcp 127.0.0.1:1"); got == "" {
		t.Error("混写大小写的 proxyconnect 也应被识别")
	}
}
