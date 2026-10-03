package supervisor

import "strings"

func AccountFailureCode(message string) string { return classifyAccountFailure(message) }

func classifyAccountFailure(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "恢复原账号失败"), strings.Contains(lower, "恢复校验失败"):
		return "restore_failure"
	case strings.Contains(lower, "identity mismatch"), strings.Contains(lower, "归属"), strings.Contains(lower, "所属邮箱"):
		return "identity_mismatch"
	case strings.Contains(lower, "eof"), strings.Contains(lower, "connection reset"), strings.Contains(lower, "连接被中途切断"):
		return "connection_reset"
	case strings.Contains(lower, "oauth2.googleapis.com/token"), strings.Contains(lower, "token endpoint"):
		return "token_endpoint"
	case strings.Contains(lower, "超时"):
		return "timeout"
	case strings.Contains(lower, "proxy"), strings.Contains(lower, "代理"), strings.Contains(lower, "网络"), strings.Contains(lower, "dns"):
		return "network_proxy"
	case strings.Contains(lower, "未归档"), strings.Contains(lower, "凭据未"), strings.Contains(lower, "重新读取凭据"):
		return "credential_not_written"
	case strings.Contains(lower, "没有 gemini:antigravity"), strings.Contains(lower, "没有登录凭据"):
		return "credential_missing"
	case strings.Contains(lower, "尚未完成登录"), strings.Contains(lower, "登录未完成"):
		return "official_not_logged_in"
	case strings.Contains(lower, "正在进行"):
		return "account_busy"
	case strings.Contains(lower, "已取消"):
		return "cancelled"
	}
	return ""
}
