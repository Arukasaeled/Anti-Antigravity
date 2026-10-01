package supervisor

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// 账号元数据（**不包含任何 OAuth 客户端**）
//
// 0.1.1 起 2Ag 不再自己发起 Google OAuth。本文件曾经装着一条完整的自有 OAuth
// 客户端路线：内置 client_id / client_secret、授权 URL 生成、本地回环回调、
// authorization code → token 交换、以及用内置客户端刷新 token。那条路线已被删除，
// 因为它把「2Ag 持有 Google OAuth 凭据」变成了事实 —— 既是发布时的 secret 泄漏，
// 也是权限上的错位：2Ag 不该是任何人的 OAuth 客户端。
//
// 现在添加账号的唯一路径是本机官方 Antigravity 的原生登录（Login Broker，
// 见 login_broker.go）：OAuth 全程由官方 Antigravity + 系统浏览器完成，
// 2Ag 只负责捕获登录结果、DPAPI 落库（account_vault.go）、切换与失败回滚。
//
// 本文件只剩**账号清单**的读写：把账号登记进 cockpit 账号库（SaveScannedAccount）
// 与从 JSON 导入账号（ImportAccountsJSON）。这两条路径不接触 Google 的任何端点。
// ============================================================================

// SaveScannedAccount 把一个账号登记进 cockpit 账号库（accounts.json）。
//
// 它只写「有哪些账号」这份清单，不写任何 token：凭据本体只存在于两个地方 ——
// Windows 凭据管理器的 gemini:antigravity（当前登录态）与 2Ag 的 DPAPI 保险库。
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

// ImportAccountsJSON 从 JSON 导入账号清单（cockpit 账号库 / 数组 / 单对象）。
//
// 刻意不支持「GCP OAuth 客户端凭据文件」：那正是自有 OAuth 路线的残留。
// 2Ag 不需要、也不接受用户的 OAuth 客户端凭据 —— 添加账号请走官方原生登录。
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
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
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
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(data, &single); err == nil && single.Email != "" {
		_, _ = SaveScannedAccount(single.Email, single.Name)
		return ScanLocalAccounts(), nil
	}

	return nil, errors.New("无法识别的 JSON 账号清单格式（支持 cockpit accounts.json / 账号数组 / 单个账号对象）")
}
