package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 2Ag 原生配额探测
//
// 公开协议形状（Antigravity / Cloud Code 的 v1internal，不是 2Ag 私有协议）：
//
//	POST https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist
//	POST https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary
//
// 请求带宿主自己写下的 Bearer access_token。响应里的 quota_summary.groups[].buckets[]
// 就是 Gemini / Claude+GPT 的 5h 与 weekly 窗口。
//
// 这不是 Cockpit Tools 的缓存读取。Cockpit 只在 live 探测失败时作为明确标注的
// fallback，而且 Source 会写成 cockpit-cache，界面不得把它显示成实时读数。
//
// 2Ag 不持有、也不内置任何 Google OAuth client_id / client_secret。
// access_token 过期时，只在凭据 blob 自己带了 oauth client 字段时才刷新；
// 没有客户端字段就如实返回 expired，让用户走官方登录，而不是由 2Ag 冒充 OAuth 客户端。
// ============================================================================

const (
	quotaCloudCodeDaily = "https://daily-cloudcode-pa.googleapis.com"
	quotaLoadPath       = "/v1internal:loadCodeAssist"
	quotaSummaryPath    = "/v1internal:retrieveUserQuotaSummary"

	quotaSourceLive         = "live · antigravity cloud code"
	quotaSourceCockpitCache = "cockpit-cache"
	quotaUserAgent          = "antigravity windows/amd64"
)

// QuotaProbeStatus 是一次探测的诚实结论。它和百分比分开：百分比为 0 可以是
// 「额度耗尽」，也可以是「根本没读到」。调用方必须看 Status，不能看数字。
type QuotaProbeStatus struct {
	Email      string `json:"email"`
	Source     string `json:"source"` // live | cockpit-cache | none
	Status     string `json:"status"` // ok | unavailable | expired | unauthorized | error
	Stale      bool   `json:"stale"`
	Message    string `json:"message,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
}

// quotaHTTPClient 是探测用的 HTTP 出口。测试替换它，生产路径用默认客户端。
var quotaHTTPClient = &http.Client{Timeout: 12 * time.Second}

// quotaNow 让过期判定可测。
var quotaNow = time.Now

type quotaToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
	ClientID     string
	ClientSecret string
	Email        string
	Raw          []byte
}

func parseQuotaToken(raw []byte) (quotaToken, bool) {
	view, ok := parseCredentialBlobView(raw)
	if !ok || view.Token == nil {
		return quotaToken{}, false
	}
	tok := quotaToken{
		AccessToken:  strings.TrimSpace(view.Token.AccessToken),
		RefreshToken: strings.TrimSpace(view.Token.RefreshToken),
		TokenType:    strings.TrimSpace(view.Token.TokenType),
		Email:        emailFromCredentialPayload(raw),
		Raw:          append([]byte(nil), raw...),
	}
	if tok.TokenType == "" {
		tok.TokenType = "Bearer"
	}
	if exp := strings.TrimSpace(view.Token.Expiry); exp != "" {
		if t, err := time.Parse(time.RFC3339Nano, exp); err == nil {
			tok.Expiry = t
		}
	}
	// client 字段只在 blob 自己带了才用。2Ag 不补、不内置。
	var extra struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		OAuthClient  struct {
			ID     string `json:"client_id"`
			Secret string `json:"client_secret"`
		} `json:"oauth_client"`
	}
	_ = json.Unmarshal(raw, &extra)
	tok.ClientID = firstNonEmpty(extra.ClientID, extra.OAuthClient.ID)
	tok.ClientSecret = firstNonEmpty(extra.ClientSecret, extra.OAuthClient.Secret)
	if tok.AccessToken == "" && tok.RefreshToken == "" {
		return quotaToken{}, false
	}
	return tok, true
}

func (t quotaToken) accessExpired(now time.Time) bool {
	if t.AccessToken == "" {
		return true
	}
	if t.Expiry.IsZero() {
		return false
	}
	return !t.Expiry.After(now.Add(30 * time.Second))
}

func (t quotaToken) canRefresh() bool {
	return t.RefreshToken != "" && t.ClientID != "" && t.ClientSecret != ""
}

// probeQuotaForAccount 是账号矩阵用的入口：先取该账号自己的凭据，再走 live 探测。
//
// 凭据来源按「新鲜度」选择，而不是按固定优先级：
//
//	系统凭据管理器（宿主自己持续刷新，永远最新）—— 仅当它确实属于这个邮箱
//	2Ag 自己的 DPAPI 保险库（broker / 切换时写入的快照）
//
// 为什么顺序如此：宿主自己会不停刷新 access_token，而 2Ag 保险库里那份是
// 切换/登录那一刻的**快照**，几十小时后必然是过期 token。若机械地优先读保险库，
// 就会出现「宿主正在用这个账号正常工作、2Ag 却报 token 过期」的假失败 ——
// 实测过：同一个 kaladaoerdun 账号，宿主 expiry 10:13（新鲜），保险库副本 01:29（已过期）。
//
// 因此：先看系统凭据是否属于该邮箱且**未过期**；否则用保险库那份
// （它可能带 refresh 能力，也可能已过期 —— 如实报 expired，不伪造）。
//
// 任何一条都对不上邮箱就直接 unavailable，绝不拿 A 的 token 去查 B 的池。
func probeQuotaForAccount(email string) (QuotaWindow, QuotaWindow, []string, QuotaProbeStatus) {
	email = strings.TrimSpace(email)
	if email == "" {
		return unavailablePools(QuotaProbeStatus{Status: "unavailable", Message: "缺少账号邮箱"})
	}
	raw := quotaCredentialResolver(email)
	if len(raw) == 0 {
		return unavailablePools(QuotaProbeStatus{Email: email, Status: "unavailable", Message: "没有该账号的本机登录凭据"})
	}
	return ProbeAccountQuota(context.Background(), email, raw)
}

// credentialForQuotaProbe 选一份最可能新鲜的、且确实属于 email 的凭据。
func credentialForQuotaProbe(email string) []byte {
	now := quotaNow()

	// 1) 系统凭据管理器：宿主持续刷新，通常是这里最新。
	if current, err := readAntigravityCredentialRaw(); err == nil && len(current) > 0 {
		if owner := emailFromCredentialPayload(current); owner != "" && strings.EqualFold(owner, email) {
			if tok, ok := parseQuotaToken(current); ok && !tok.accessExpired(now) {
				return current
			}
			// 属于该邮箱但已过期：留作兜底，继续找保险库。
			if vaulted, verr := ReadVaultCredential(email); verr == nil && len(vaulted) > 0 {
				if vtok, vok := parseQuotaToken(vaulted); vok && !vtok.accessExpired(now) {
					return vaulted
				}
			}
			return current
		}
	}

	// 2) 系统凭据不属于该邮箱（或读不到）：用保险库快照。
	if vaulted, err := ReadVaultCredential(email); err == nil && len(vaulted) > 0 {
		return vaulted
	}
	return nil
}

// ---------------------------------------------------------------------------
// 批量并发探测
//
// 账号矩阵里 N 个账号如果串行探测，每个都要走两次 HTTPS（loadCodeAssist +
// retrieveUserQuotaSummary）。正常网络下单账号约 1.2 s，N 个账号就是 N×1.2 s；
// 而断网/黑洞路由时每个账号各自撞满 HTTP 超时，N×12 s 让 Accounts 页面直接卡死。
//
// 因此批量探测必须并发，并且有三层时间闸：
//  1. 单请求 HTTP 超时（quotaHTTPClient，12 s）
//  2. 单账号总超时（quotaProbePerAccountTimeout）—— 覆盖它那两次 HTTPS
//  3. 整批总超时（quotaProbeTotalTimeout）—— 上限，保证界面不会无限等
//
// 单个账号失败绝不影响其他账号：每个账号独立 goroutine + 独立 ctx，
// 失败只体现在它自己的 status 里。
// ---------------------------------------------------------------------------

const (
	quotaProbePerAccountTimeout = 18 * time.Second
	quotaProbeTotalTimeout      = 22 * time.Second
	quotaProbeMaxConcurrency    = 4
)

// quotaProbeResult 是批量探测里单个账号的完整结论。
type quotaProbeResult struct {
	Gemini QuotaWindow
	Claude QuotaWindow
	Models []string
	Status QuotaProbeStatus
}

// probeQuotaBatch 并发探测多个账号，返回 email（原样）→ 结果。
//
// 任何账号都不会阻塞其他账号；超时/慢响应只影响它自己。
func probeQuotaBatch(emails []string) map[string]quotaProbeResult {
	out := make(map[string]quotaProbeResult, len(emails))
	if len(emails) == 0 {
		return out
	}

	totalCtx, cancelTotal := context.WithTimeout(context.Background(), quotaProbeTotalTimeout)
	defer cancelTotal()

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, quotaProbeMaxConcurrency)

	for _, email := range emails {
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		wg.Add(1)
		go func(email string) {
			defer wg.Done()
			// 并发上限：拿到槽位才发请求，避免几十个账号同时打网络。
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-totalCtx.Done():
				mu.Lock()
				out[email] = quotaProbeResult{
					Gemini: QuotaWindow{},
					Claude: QuotaWindow{},
					Status: QuotaProbeStatus{
						Email:   email,
						Source:  "none",
						Status:  "unavailable",
						Message: "探测整体超时，本次未读取该账号",
					},
				}
				mu.Unlock()
				return
			}

			acctCtx, cancel := context.WithTimeout(totalCtx, quotaProbePerAccountTimeout)
			defer cancel()

			res := probeQuotaForAccountCtx(acctCtx, email)
			// 单账号超时但探测函数没来得及分类时，补一个诚实的说明。
			if acctCtx.Err() != nil && res.Status.Status == "unavailable" && res.Status.Message == "" {
				res.Status.Message = "该账号探测超时"
			}
			mu.Lock()
			out[email] = res
			mu.Unlock()
		}(email)
	}
	wg.Wait()
	return out
}

// quotaCredentialResolver 是「按邮箱取一份可用凭据」的解析器。
// 生产路径固定用 credentialForQuotaProbe；测试替换它来注入假凭据，
// 从而在不触碰真实保险库的前提下验证并发与超时行为。
var quotaCredentialResolver = credentialForQuotaProbe

// probeQuotaForAccountCtx 与 probeQuotaForAccount 相同，但受传入 ctx 约束。
func probeQuotaForAccountCtx(ctx context.Context, email string) quotaProbeResult {
	email = strings.TrimSpace(email)
	if email == "" {
		g, c, m, s := unavailablePools(QuotaProbeStatus{Status: "unavailable", Message: "缺少账号邮箱"})
		return quotaProbeResult{g, c, m, s}
	}
	raw := quotaCredentialResolver(email)
	if len(raw) == 0 {
		g, c, m, s := unavailablePools(QuotaProbeStatus{Email: email, Status: "unavailable", Message: "没有该账号的本机登录凭据"})
		return quotaProbeResult{g, c, m, s}
	}
	g, c, m, s := ProbeAccountQuota(ctx, email, raw)
	return quotaProbeResult{g, c, m, s}
}

// ProbeAccountQuota 是对外入口：优先 live，失败且本地有 cockpit 缓存时才 fallback。
//
// email 必须与凭据里读出的身份一致。对不上就拒绝探测，避免 A 的 token 去填 B 的池。
func ProbeAccountQuota(ctx context.Context, email string, raw []byte) (QuotaWindow, QuotaWindow, []string, QuotaProbeStatus) {
	status := QuotaProbeStatus{Email: strings.TrimSpace(email), Source: "none", Status: "unavailable"}
	if ctx == nil {
		ctx = context.Background()
	}
	tok, ok := parseQuotaToken(raw)
	if !ok {
		status.Message = "没有可用的登录凭据"
		return unavailablePools(status)
	}
	if tok.Email != "" && status.Email != "" && !strings.EqualFold(tok.Email, status.Email) {
		status.Status = "error"
		status.Message = "凭据归属与请求邮箱不一致，已拒绝探测"
		return unavailablePools(status)
	}
	if status.Email == "" {
		status.Email = tok.Email
	}

	if tok.accessExpired(quotaNow()) {
		refreshed, err := refreshQuotaAccessToken(ctx, tok)
		if err != nil {
			status.Status = "expired"
			status.Message = err.Error()
			return fallbackOrUnavailable(status.Email, status)
		}
		tok.AccessToken = refreshed.AccessToken
		if !refreshed.Expiry.IsZero() {
			tok.Expiry = refreshed.Expiry
		}
	}

	gemini, claude, models, live, err := fetchLiveQuota(ctx, tok)
	if err != nil {
		status.Status = classifyQuotaError(err)
		status.Message = err.Error()
		if httpErr, ok := err.(*quotaHTTPError); ok {
			status.HTTPStatus = httpErr.Status
		}
		return fallbackOrUnavailable(status.Email, status)
	}
	status.Source = quotaSourceLive
	status.Status = "ok"
	status.Stale = false
	status.ProjectID = live.ProjectID
	stampPools(&gemini, &claude, quotaSourceLive, quotaNow().UnixMilli(), false)
	return gemini, claude, models, status
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// stampPools 给两个池写上出处、采样时刻与新鲜度。
// source 必须是真实来源标识：live 探测写 cloud code，cockpit fallback 写 cockpit-cache。
func stampPools(gemini, claude *QuotaWindow, source string, updatedAt int64, stale bool) {
	if gemini.Available {
		gemini.Source = source
		gemini.UpdatedAt = updatedAt
		gemini.Stale = stale
	}
	if claude.Available {
		claude.Source = source
		claude.UpdatedAt = updatedAt
		claude.Stale = stale
	}
}

// unavailablePools 返回两个明确不可用的池，不伪造任何百分比。
func unavailablePools(status QuotaProbeStatus) (QuotaWindow, QuotaWindow, []string, QuotaProbeStatus) {
	return QuotaWindow{Available: false}, QuotaWindow{Available: false}, nil, status
}

// fallbackOrUnavailable：live 失败时，只有本地存在明确标注的 cockpit 缓存才回落；
// 回落的池 source 一定是 cockpit-cache，绝不冒充 live。
func fallbackOrUnavailable(email string, status QuotaProbeStatus) (QuotaWindow, QuotaWindow, []string, QuotaProbeStatus) {
	gemini, claude, models, ok := queryCacheFor(email)
	if !ok || (!gemini.Available && !claude.Available) {
		status.Source = "none"
		status.Status = statusOrDefault(status)
		status.Message = firstNonEmpty(status.Message, "没有可用的实时配额读数")
		return unavailablePools(status)
	}
	status.Source = quotaSourceCockpitCache
	status.Stale = true
	status.Message = firstNonEmpty(status.Message, "实时探测失败，显示本机 Cockpit Tools 缓存（非实时）")
	// 缓存里的 updatedAt / Source / Stale 已由 queryCacheFor 写好，不覆盖成 live。
	//
	// 注意：Stale 必须同时存在于**池**上（queryCacheFor 已在池上标记）。
	// /api/v1/dashboard 与 /api/v1/host/status 的 active_account 走 AccountQuotaDTO，
	// 只搬池与百分比，本函数的 status 不进那个响应；只标 status 的话前端永远看不到
	// 「这是缓存」，而一份两分钟前写入的缓存会被渲染成「配额出处：刚刚」——
	// 把非实时读数当成实时读数展示。
	return gemini, claude, models, status
}

func statusOrDefault(status QuotaProbeStatus) string {
	if status.Status == "" {
		return "unavailable"
	}
	return status.Status
}

type quotaLiveMeta struct {
	ProjectID string
}

type quotaHTTPError struct {
	Status int
	Body   string
}

func (e *quotaHTTPError) Error() string {
	if e.Status == http.StatusUnauthorized {
		return "配额接口拒绝了当前 access_token（401）"
	}
	if e.Status == http.StatusForbidden {
		return "配额接口拒绝访问（403）"
	}
	msg := strings.TrimSpace(e.Body)
	if len(msg) > 180 {
		msg = msg[:180]
	}
	if msg == "" {
		return fmt.Sprintf("配额接口返回 HTTP %d", e.Status)
	}
	return fmt.Sprintf("配额接口返回 HTTP %d：%s", e.Status, msg)
}

func classifyQuotaError(err error) string {
	httpErr, ok := err.(*quotaHTTPError)
	if !ok {
		return "error"
	}
	switch httpErr.Status {
	case http.StatusUnauthorized:
		return "expired"
	case http.StatusForbidden:
		return "unauthorized"
	default:
		return "error"
	}
}

func fetchLiveQuota(ctx context.Context, tok quotaToken) (QuotaWindow, QuotaWindow, []string, quotaLiveMeta, error) {
	var meta quotaLiveMeta
	projectID, err := loadCodeAssistProject(ctx, tok)
	if err != nil {
		return QuotaWindow{}, QuotaWindow{}, nil, meta, err
	}
	meta.ProjectID = projectID
	body, err := postCloudCode(ctx, tok.AccessToken, quotaSummaryPath, map[string]any{"project": projectID})
	if err != nil {
		return QuotaWindow{}, QuotaWindow{}, nil, meta, err
	}
	gemini, claude, models, ok := poolsFromQuotaSummary(body)
	if !ok {
		return QuotaWindow{}, QuotaWindow{}, nil, meta, fmt.Errorf("配额响应里没有可识别的 5h / weekly 窗口")
	}
	return gemini, claude, models, meta, nil
}

func loadCodeAssistProject(ctx context.Context, tok quotaToken) (string, error) {
	payload := map[string]any{
		"metadata": map[string]any{
			"ideName":  "antigravity",
			"ideType":  "ANTIGRAVITY",
			"platform": "WINDOWS_AMD64",
		},
	}
	body, err := postCloudCode(ctx, tok.AccessToken, quotaLoadPath, payload)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Project json.RawMessage `json:"cloudaicompanionProject"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return "", fmt.Errorf("loadCodeAssist 响应无法解析")
	}
	// 接受服务端给的 project 标识。
	//
	// 实测（2026-10 官方 daily-cloudcode endpoint）：免费层账号的
	// cloudaicompanionProject 就是字面量 "aicode-consumers"，而用它查询
	// retrieveUserQuotaSummary 能正常拿到四桶真实数据。早期实现把这个值当成
	// 「没有可用 project」直接拒绝，导致 live 探测在正常账号上 100% 失败、
	// 悄悄退回 cockpit 缓存 —— 那是把正常响应误判成错误的假失败。
	//
	// project 只是接口入参标识，不是权限凭据：授权完全由 Bearer token 决定。
	//
	// 注意：同一响应里的 currentTier.userDefinedCloudaicompanionProject 是
	// **bool**（是否用自定义 project 的开关），不是 project id，别把它当字符串解，
	// 否则整个响应 unmarshal 失败。
	id := projectIDFromRaw(parsed.Project)
	if id == "" {
		return "", fmt.Errorf("loadCodeAssist 没有返回 project 标识")
	}
	return id, nil
}

func projectIDFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil && strings.TrimSpace(asString) != "" {
		return strings.TrimSpace(asString)
	}
	var asObj struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &asObj) == nil {
		return strings.TrimSpace(asObj.ID)
	}
	return ""
}

func postCloudCode(ctx context.Context, accessToken, path string, payload any) ([]byte, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaCloudCodeDaily+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", quotaUserAgent)
	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("配额接口不可达: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &quotaHTTPError{Status: resp.StatusCode, Body: string(body)}
	}
	return body, nil
}

type refreshedAccess struct {
	AccessToken string
	Expiry      time.Time
}

func refreshQuotaAccessToken(ctx context.Context, tok quotaToken) (refreshedAccess, error) {
	if !tok.canRefresh() {
		return refreshedAccess{}, fmt.Errorf("access_token 已过期，且凭据里没有可用于刷新的 OAuth 客户端字段；请用官方 Antigravity 重新登录")
	}
	form := "grant_type=refresh_token&refresh_token=" + urlQueryEscape(tok.RefreshToken) +
		"&client_id=" + urlQueryEscape(tok.ClientID) +
		"&client_secret=" + urlQueryEscape(tok.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form))
	if err != nil {
		return refreshedAccess{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := quotaHTTPClient.Do(req)
	if err != nil {
		return refreshedAccess{}, fmt.Errorf("刷新 access_token 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return refreshedAccess{}, fmt.Errorf("刷新 access_token 被拒绝（HTTP %d）", resp.StatusCode)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &parsed) != nil || strings.TrimSpace(parsed.AccessToken) == "" {
		return refreshedAccess{}, fmt.Errorf("刷新响应里没有 access_token")
	}
	out := refreshedAccess{AccessToken: parsed.AccessToken}
	if parsed.ExpiresIn > 0 {
		out.Expiry = quotaNow().Add(time.Duration(parsed.ExpiresIn) * time.Second)
	}
	return out, nil
}

func urlQueryEscape(s string) string {
	// net/url 的 QueryEscape 把空格编成 +，OAuth 表单接受这个形式。
	return strings.ReplaceAll(strings.ReplaceAll(queryEscape(s), "+", "%20"), "%2A", "*")
}

func queryEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func poolsFromQuotaSummary(body []byte) (QuotaWindow, QuotaWindow, []string, bool) {
	var parsed struct {
		Groups []quotaSummaryGroup `json:"groups"`
		// 有的响应把 summary 再包一层。
		Summary struct {
			Groups []quotaSummaryGroup `json:"groups"`
		} `json:"quota_summary"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return QuotaWindow{}, QuotaWindow{}, nil, false
	}
	groups := parsed.Groups
	if len(groups) == 0 {
		groups = parsed.Summary.Groups
	}
	return poolsFromGroups(groups)
}

func poolsFromGroups(groups []quotaSummaryGroup) (QuotaWindow, QuotaWindow, []string, bool) {
	var gemini, claude QuotaWindow
	var models []string
	seen := map[string]bool{}
	hit := false
	for _, grp := range groups {
		isGemini := strings.Contains(grp.DisplayName, "Gemini")
		isClaude := strings.Contains(grp.DisplayName, "Claude") || strings.Contains(grp.DisplayName, "GPT")
		for _, name := range parseModelsFromGroupDescription(grp.Description) {
			if !seen[name] {
				seen[name] = true
				models = append(models, name)
			}
		}
		for _, b := range grp.Buckets {
			window := bucketWindow(b)
			if window != "5h" && window != "weekly" {
				continue
			}
			which := bucketPoolKind(b, isGemini, isClaude)
			var pool *QuotaWindow
			switch which {
			case quotaPoolGemini:
				pool = &gemini
			case quotaPoolClaude:
				pool = &claude
			default:
				continue
			}
			pct := int(b.RemainingFraction * 100)
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			pool.Available = true
			hit = true
			if window == "5h" {
				pool.FiveHourPercent = pct
				pool.FiveHourReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
				pool.FiveHourKnown = true
			} else {
				pool.WeeklyPercent = pct
				pool.WeeklyReset = formatResetTime(b.ResetTime, b.RemainingFraction, "待刷新")
				pool.WeeklyKnown = true
			}
		}
	}
	return gemini, claude, models, hit
}

const (
	quotaPoolUnknown = iota
	quotaPoolGemini
	quotaPoolClaude
)

// bucketPoolKind 判定一个 summary bucket 属于哪个池。
// 优先看 bucket 自身 id / displayName，只有在 bucket 没写清楚时才回落到 group 名。
// 这样 Gemini 池里不会被 group 名误染，Claude/GPT 池也不会串。
func bucketPoolKind(b quotaSummaryBucket, groupGemini, groupClaude bool) int {
	id := strings.ToLower(strings.TrimSpace(b.BucketID + " " + b.DisplayName))
	switch {
	case strings.Contains(id, "gemini"):
		return quotaPoolGemini
	case strings.Contains(id, "claude"), strings.Contains(id, "gpt"), strings.Contains(id, "3p"):
		return quotaPoolClaude
	}
	if groupGemini && !groupClaude {
		return quotaPoolGemini
	}
	if groupClaude && !groupGemini {
		return quotaPoolClaude
	}
	return quotaPoolUnknown
}

func bucketWindow(b quotaSummaryBucket) string {
	switch strings.ToLower(strings.TrimSpace(b.Window)) {
	case "5h", "five_hour", "5-hour":
		return "5h"
	case "weekly", "week", "7d":
		return "weekly"
	}
	id := strings.ToLower(b.BucketID)
	switch {
	case strings.Contains(id, "5h") || strings.Contains(id, "five"):
		return "5h"
	case strings.Contains(id, "week"):
		return "weekly"
	default:
		return ""
	}
}
