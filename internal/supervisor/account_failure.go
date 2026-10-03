package supervisor

import (
	"errors"
	"strings"
)

const (
	CredentialMissing            = "credential_missing"
	CredentialDecryptFailed      = "decrypt_failed"
	CredentialParseFailed        = "parse_failed"
	CredentialOwnerMissing       = "owner_missing"
	CredentialOwnerMismatch      = "owner_mismatch"
	CredentialTokenMissing       = "token_missing"
	CredentialExpiredRefreshable = "token_expired_refreshable"
	CredentialRefreshFailed      = "refresh_failed"
)

// CredentialValidationError contains only a category and safe metadata.
// Never include raw payloads or token endpoint response bodies here.
type CredentialValidationError struct {
	Code    string
	Message string
}

func (e *CredentialValidationError) Error() string { return e.Code + ": " + e.Message }

func credentialError(code, message string) error {
	return &CredentialValidationError{Code: code, Message: message}
}

func AccountErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var credentialErr *CredentialValidationError
	if errors.As(err, &credentialErr) {
		return credentialErr.Code
	}
	return classifyAccountFailure(err.Error())
}

func AccountFailureCode(message string) string { return classifyAccountFailure(message) }

func classifyAccountFailure(message string) string {
	lower := strings.ToLower(message)
	for _, code := range []string{CredentialMissing, CredentialDecryptFailed, CredentialParseFailed,
		CredentialOwnerMissing, CredentialOwnerMismatch, CredentialTokenMissing, CredentialRefreshFailed} {
		if strings.Contains(lower, code+":") {
			return code
		}
	}
	switch {
	case strings.Contains(lower, "invalid_grant"), strings.Contains(lower, "refresh token rejected"):
		return CredentialRefreshFailed
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
