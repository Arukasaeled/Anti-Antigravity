package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
	UserInfoURL    = "https://www.googleapis.com/oauth2/v2/userinfo"

	// Antigravity Hub Official OAuth Client ID & Secret
	DefaultClientID     = "REDACTED.apps.googleusercontent.com"
	DefaultClientSecret = "REDACTED"
	DefaultScope        = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
)

type OAuthStatusResponse struct {
	Status    string           `json:"status"` // "IDLE", "WAITING", "SUCCESS", "ERROR"
	Account   *AccountInstance `json:"account,omitempty"`
	Message   string           `json:"message,omitempty"`
	UpdatedAt int64            `json:"updated_at"`
}

var (
	oauthMu     sync.RWMutex
	oauthStatus = OAuthStatusResponse{Status: "IDLE"}
)

func GetOAuthStatus() OAuthStatusResponse {
	oauthMu.RLock()
	defer oauthMu.RUnlock()
	return oauthStatus
}

func setOAuthStatus(status string, acc *AccountInstance, msg string) {
	oauthMu.Lock()
	defer oauthMu.Unlock()
	oauthStatus = OAuthStatusResponse{
		Status:    status,
		Account:   acc,
		Message:   msg,
		UpdatedAt: time.Now().Unix(),
	}
}

func buildGoogleAuthURL(redirectURI, state string) string {
	v := url.Values{}
	v.Set("client_id", DefaultClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("response_type", "code")
	v.Set("scope", DefaultScope)
	v.Set("access_type", "offline")
	v.Set("prompt", "consent")
	v.Set("state", state)
	return GoogleAuthURL + "?" + v.Encode()
}

func randomState() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartBrowserOAuth sets up local loopback listener and opens system browser
func StartBrowserOAuth(ctx context.Context, proxyURL string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("failed to bind loopback port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth/callback", port)
	state := randomState()
	authLink := buildGoogleAuthURL(redirectURI, state)

	setOAuthStatus("WAITING", nil, "正在等待浏览器授权回调 (请在浏览器中完成登录)...")

	go func() {
		mux := http.NewServeMux()
		server := &http.Server{Handler: mux}

		mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				errMsg := r.URL.Query().Get("error")
				if errMsg == "" {
					errMsg = "未获取到授权凭证 code"
				}
				setOAuthStatus("ERROR", nil, errMsg)
				http.Error(w, "授权失败: "+errMsg, 400)
				return
			}

			account, err := exchangeCodeAndSave(code, redirectURI, proxyURL)
			if err != nil {
				setOAuthStatus("ERROR", nil, "令牌交换失败: "+err.Error())
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, `<html><body style="background:#121319;color:#fff;font-family:sans-serif;text-align:center;padding:50px;">
					<h1 style="color:#e60012;">2Ag Suite // 授权异常</h1>
					<p style="color:#8b949e;">%v</p>
				</body></html>`, err)
				return
			}

			setOAuthStatus("SUCCESS", account, "账号已成功接入")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`
				<html><body style="background:#121319;color:#fff;font-family:sans-serif;text-align:center;padding:50px;">
					<h1 style="color:#00ff66;">2Ag Suite // 授权成功</h1>
					<p style="color:#8b949e;font-size:16px;">账号 <b style="color:#fcee0a;">` + account.Email + `</b> 已成功加入多账号矩阵！</p>
					<p style="color:#585f73;font-size:13px;margin-top:20px;">您可以关闭本标签页，返回 2Ag 控制台继续操作。</p>
				</body></html>
			`))

			go func() {
				time.Sleep(1500 * time.Millisecond)
				_ = server.Shutdown(context.Background())
			}()
		})

		_ = server.Serve(listener)
	}()

	openBrowser(authLink)
	return authLink, nil
}

func exchangeCodeAndSave(code, redirectURI, proxyURL string) (*AccountInstance, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	if strings.TrimSpace(proxyURL) != "" {
		if pURL, err := url.Parse(proxyURL); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pURL)}
		}
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", DefaultClientID)
	form.Set("client_secret", DefaultClientSecret)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequest(http.MethodPost, GoogleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Google 令牌服务: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Google 令牌换取失败 (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("解析令牌响应失败: %w", err)
	}

	// Fetch user info
	uReq, _ := http.NewRequest(http.MethodGet, UserInfoURL, nil)
	uReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	uResp, err := client.Do(uReq)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}
	defer uResp.Body.Close()

	uBody, _ := io.ReadAll(uResp.Body)
	var userInfo struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(uBody, &userInfo); err != nil || userInfo.Email == "" {
		return nil, fmt.Errorf("解析用户邮箱失败: %s", string(uBody))
	}

	return SaveScannedAccount(userInfo.Email, userInfo.Name)
}

func SaveScannedAccount(email, name string) (*AccountInstance, error) {
	if email == "" {
		return nil, errors.New("email is empty")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	// 账号库位置走 cockpitDataDir()（认 $COCKPIT_TOOLS_DATA_DIR，否则当前用户的
	// ~/.antigravity_cockpit）。历史实现这里写死了开发机的绝对路径，
	// 别的用户走到这条分支时会去读一个不存在的文件、静默失败。
	accountFilePath := filepath.Join(cockpitDataDir(), "accounts.json")
	var caf cockpitAccountsFile
	data, err := os.ReadFile(accountFilePath)
	if err == nil {
		_ = json.Unmarshal(data, &caf)
	}
	if caf.Version == "" {
		caf.Version = "2.0"
	}

	found := false
	for _, a := range caf.Accounts {
		if strings.EqualFold(a.Email, email) {
			found = true
			break
		}
	}

	newID := "acc-" + hex.EncodeToString([]byte(email))[:12]
	if !found {
		now := time.Now().Unix()
		caf.Accounts = append(caf.Accounts, struct {
			ID        string `json:"id"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			CreatedAt int64  `json:"created_at"`
			LastUsed  int64  `json:"last_used"`
		}{
			ID:        newID,
			Email:     email,
			Name:      name,
			CreatedAt: now,
			LastUsed:  now,
		})

		_ = os.MkdirAll(filepath.Dir(accountFilePath), 0755)
		newData, _ := json.MarshalIndent(caf, "", "  ")
		_ = os.WriteFile(accountFilePath, newData, 0644)
	}

	// 模型清单必须来自该账号自己的授权缓存（见 queryCacheFor 的注释）：
	// 全新登录的账号此刻多半还没有缓存，读不到就如实留空，由前端显示「未探测」，
	// 绝不回填写死的 5 个模型名冒充能力清单。
	gPool, cPool, models, _ := queryCacheFor(email)
	acc := &AccountInstance{
		ID:          newID,
		Email:       email,
		Name:        name,
		Role:        "BACKUP",
		IsPrimary:   false,
		IsActive:    false,
		Weight:      8,
		Status:      "ACTIVE",
		GeminiPool:  gPool,
		ClaudePool:  cPool,
		Models:      models,
		CooldownMsg: "",
	}
	return acc, nil
}

// ImportAccountsJSON parses cockpit-tools or GCP credentials JSON
func ImportAccountsJSON(data []byte) ([]AccountInstance, error) {
	// Try cockpit-tools accounts format
	var caf struct {
		Accounts []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &caf); err == nil && len(caf.Accounts) > 0 {
		for _, a := range caf.Accounts {
			if a.Email != "" {
				_, _ = SaveScannedAccount(a.Email, a.Name)
			}
		}
		return ScanLocalAccounts(), nil
	}

	// Try array of accounts
	var arr []struct {
		ID           string `json:"id"`
		Email        string `json:"email"`
		Name         string `json:"name"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		count := 0
		for _, a := range arr {
			if a.Email != "" {
				_, _ = SaveScannedAccount(a.Email, a.Name)
				count++
			}
		}
		if count > 0 {
			return ScanLocalAccounts(), nil
		}
	}

	// Try single account object
	var single struct {
		Email        string `json:"email"`
		Name         string `json:"name"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(data, &single); err == nil && single.Email != "" {
		_, _ = SaveScannedAccount(single.Email, single.Name)
		return ScanLocalAccounts(), nil
	}

	// Try GCP OAuth client credentials
	var gcpCreds struct {
		Installed struct {
			ClientID string `json:"client_id"`
		} `json:"installed"`
		Web struct {
			ClientID string `json:"client_id"`
		} `json:"web"`
	}
	if err := json.Unmarshal(data, &gcpCreds); err == nil && (gcpCreds.Installed.ClientID != "" || gcpCreds.Web.ClientID != "") {
		cid := gcpCreds.Installed.ClientID
		if cid == "" {
			cid = gcpCreds.Web.ClientID
		}
		alias := "gcp-oauth-" + cid[:8]
		_, _ = SaveScannedAccount(alias+"@google-oauth.net", alias)
		return ScanLocalAccounts(), nil
	}

	return nil, errors.New("无法识别的 JSON 凭据格式，请检查文件内容")
}

func openBrowser(url string) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}
