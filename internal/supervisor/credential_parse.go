package supervisor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// ============================================================================
// 凭据 blob 的解析（与平台无关）
//
// Broker 验收新登录，Stored 校验可恢复的已有凭据；两套策略共享解析，
// 不共享新鲜度要求。这里不依赖 Windows 专有的凭据读写路径。
//
// 只读、不校验签名：签名是宿主与 Google 的事，2Ag 只是读取身份。
// ============================================================================

// decodeJWTPayload 解出 JWT 的 payload 段。仅用于读取身份，不校验签名
// （我们不是校验方，签名校验是宿主的事）。
func decodeJWTPayload(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return nil
	}
	seg := strings.TrimRight(parts[1], "=")
	data, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		if d2, err2 := base64.StdEncoding.DecodeString(parts[1]); err2 == nil {
			data = d2
		} else {
			return nil
		}
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

// credentialBlobView 是解析任意一份凭据 JSON 的最小视图。
//
// 与 antigravity_credential.go 里的 antigravityCredentialBlob 字段同名同形，
// 但那一份在 Windows 构建标签下；Broker 的判定逻辑必须跨平台可编译，
// 所以这里是一份**只读视图**，不参与写入。
type credentialBlobView struct {
	Token *struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		Expiry       string `json:"expiry"`
	} `json:"token"`
	AuthMethod string `json:"auth_method"`
	IDToken    string `json:"id_token"`
	Email      string `json:"email"`
}

func parseCredentialBlobView(raw []byte) (*credentialBlobView, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	var v credentialBlobView
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	return &v, true
}

// emailFromCredentialPayload 尽量从凭据里读出邮箱（读不出返回空串）。
//
// 顺序：id_token 的 email claim → 顶层 email 字段。
// 不猜、不推断：读不出就是空串，调用方据此如实显示「未知」。
func emailFromCredentialPayload(raw []byte) string {
	v, ok := parseCredentialBlobView(raw)
	if !ok {
		return ""
	}
	if m := decodeJWTPayload(v.IDToken); m != nil {
		if s, _ := m["email"].(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return strings.TrimSpace(v.Email)
}

// displayNameFromCredentialPayload 从 id_token 的 name claim 取显示名。
//
// 只当作**元数据**：它会进 index.json（非敏感），所以不做任何美化或截断之外的加工。
func displayNameFromCredentialPayload(raw []byte) string {
	v, ok := parseCredentialBlobView(raw)
	if !ok {
		return ""
	}
	m := decodeJWTPayload(v.IDToken)
	if m == nil {
		return ""
	}
	s, _ := m["name"].(string)
	return strings.TrimSpace(s)
}

// BrokerFreshCredentialValidation 判定「刚完成官方登录」的凭据。
//
// 这是 Login Broker 的关键判定，也是 E 项验收（中间态不得入库）的实现：
// 官方 Antigravity 在 OAuth 过程中会先写一个中间态
//
//	{"token": null}
//
// 如果只看「凭据存在了」，就会把中间态当成登录成功入库 —— 库里于是多出一个
// 没有 refresh_token 的空账号。因此必须三项同时成立：
//  1. token 结构有效（access_token / refresh_token 非空）
//  2. id_token 是可解析的 JWT，且带 email
//  3. id_token 的 exp 与 access_token 的 expiry（若有）尚未过期
//
// This must not be used for stored credentials or post-write verification.
func BrokerFreshCredentialValidation(raw []byte) (string, bool) {
	v, ok := parseCredentialBlobView(raw)
	if !ok {
		return "", false
	}
	if v.Token == nil || strings.TrimSpace(v.Token.AccessToken) == "" || strings.TrimSpace(v.Token.RefreshToken) == "" {
		return "", false
	}
	if strings.TrimSpace(v.IDToken) == "" {
		return "", false
	}
	m := decodeJWTPayload(v.IDToken)
	if m == nil {
		return "", false
	}
	email, _ := m["email"].(string)
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", false
	}
	if exp, ok := m["exp"].(float64); ok && exp > 0 {
		if time.Unix(int64(exp), 0).Before(time.Now().Add(-1 * time.Minute)) {
			return "", false
		}
	}
	if expiry := strings.TrimSpace(v.Token.Expiry); expiry != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil || expiresAt.Before(time.Now().Add(-time.Minute)) {
			return "", false
		}
	}
	return email, true
}

// Kept for the existing Broker-focused tests; production callers use the
// explicit fresh/stored names so their validation policies cannot be confused.
func credentialUsableEmail(raw []byte) (string, bool) {
	return BrokerFreshCredentialValidation(raw)
}

type StoredCredentialStatus struct {
	Owner    string `json:"owner"`
	State    string `json:"state"`
	Recovery string `json:"recovery,omitempty"`
}

// StoredCredentialValidation checks recoverable material, not freshness.
// An expired ID token can still identify the credential owner (no signature
// verification here). It never proves the running host's internal identity.
func StoredCredentialValidation(raw []byte, requestedEmail string) (StoredCredentialStatus, error) {
	var out StoredCredentialStatus
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, credentialError(CredentialMissing, "凭据内容为空")
	}
	v, ok := parseCredentialBlobView(raw)
	if !ok {
		return out, credentialError(CredentialParseFailed, "凭据不是可解析的 Antigravity JSON")
	}
	var claims map[string]any
	if strings.TrimSpace(v.IDToken) != "" {
		claims = decodeJWTPayload(v.IDToken)
		if claims == nil {
			return out, credentialError(CredentialParseFailed, "凭据中的身份元数据不可解析")
		}
	}
	out.Owner = emailFromCredentialPayload(raw)
	if out.Owner == "" || !strings.Contains(out.Owner, "@") {
		return out, credentialError(CredentialOwnerMissing, "无法读取凭据所属邮箱")
	}
	if !strings.EqualFold(out.Owner, strings.TrimSpace(requestedEmail)) {
		return out, credentialError(CredentialOwnerMismatch, "凭据所属邮箱 "+out.Owner+" 与目标 "+strings.TrimSpace(requestedEmail)+" 不一致")
	}
	if v.Token == nil || strings.TrimSpace(v.Token.RefreshToken) == "" {
		return out, credentialError(CredentialTokenMissing, "凭据缺少 refresh_token，无法交给宿主恢复")
	}
	out.State = "stored_valid"
	now := time.Now()
	expired := false
	if expiry := strings.TrimSpace(v.Token.Expiry); expiry != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return out, credentialError(CredentialParseFailed, "凭据中的 token expiry 不可解析")
		}
		expired = !expiresAt.After(now)
	}
	if exp, ok := claims["exp"].(float64); ok && exp > 0 {
		expired = expired || !time.Unix(int64(exp), 0).After(now)
	}
	if expired {
		out.State, out.Recovery = CredentialExpiredRefreshable, "host_recovery"
	} else if strings.TrimSpace(v.Token.AccessToken) == "" || strings.TrimSpace(v.IDToken) == "" {
		out.State, out.Recovery = "token_refresh_required", "host_recovery"
	}
	// Refresh token existence is not proof that Google will accept it. Only the
	// host's actual recovery can establish that; expiration alone is not failure.
	return out, nil
}
