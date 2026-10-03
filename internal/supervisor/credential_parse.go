package supervisor

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// ============================================================================
// 凭据 blob 的解析（与平台无关）
//
// 这里刻意只依赖 encoding/json 与 base64：Login Broker 要在「官方 Antigravity
// 刚写完凭据」的那一刻判断这份凭据**是不是一个可用的登录结果**，而那个判断
// 不能依赖 Windows 专有的读写路径（否则非 Windows 编译不过、逻辑也会分叉）。
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
	if len(raw) == 0 {
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

// credentialUsableEmail 判定「这份凭据算不算一次完成的登录」。
//
// 这是 Login Broker 的关键判定，也是 E 项验收（中间态不得入库）的实现：
// 官方 Antigravity 在 OAuth 过程中会先写一个中间态
//
//	{"token": null}
//
// 如果只看「凭据存在了」，就会把中间态当成登录成功入库 —— 库里于是多出一个
// 没有 refresh_token 的空账号。因此必须三项同时成立：
//  1. token 结构有效（access_token 非空）
//  2. id_token 是可解析的 JWT，且带 email
//  3. id_token 的 exp（若有）尚未过期
func credentialUsableEmail(raw []byte) (string, bool) {
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
	return email, true
}

func nativeCredentialReady(raw []byte, native NativeAuthState, email string) bool {
	owner, ok := credentialRestorableEmail(raw)
	if !ok || !strings.EqualFold(owner, email) || !native.Available || !native.Valid || !strings.EqualFold(native.Email, email) {
		return false
	}
	v, _ := parseCredentialBlobView(raw)
	expiry, err := time.Parse(time.RFC3339, v.Token.Expiry)
	return err == nil && expiry.After(time.Now().Add(15*time.Second))
}
