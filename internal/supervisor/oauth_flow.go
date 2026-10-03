package supervisor

import (
	"encoding/json"
	"errors"
	"strings"
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
// 本文件只剩**账号清单**的读写：把账号登记进 2Ag 自己的账号登记表
// （SaveOwnedAccountEntry，见 account_registry.go）与从 JSON 导入账号
// （ImportAccountsJSON）。这两条路径不接触 Google 的任何端点，也不依赖任何第三方工具。
// ============================================================================

// SaveScannedAccount 把一个账号登记进 2Ag 自己的账号登记表（~/.2ag/accounts.json）。
//
// 它只写「有哪些账号」这份清单，不写任何 token：凭据本体只存在于两个地方 ——
// Windows 凭据管理器的 gemini:antigravity（当前登录态）与 2Ag 的 DPAPI 保险库。
//
// 登记位置必须是 2Ag 自己的根目录，不能是第三方 cockpit 账号库：
// 后者是别人的数据目录，把 2Ag 的账号写进去等于替用户决定「你必须装过那个工具」。
func SaveScannedAccount(email, name string) (*AccountInstance, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("email is empty")
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	// 写自有登记表（幂等；已存在的账号只刷新 Name/LastUsed）。
	if err := SaveOwnedAccountEntry(email, name); err != nil {
		return nil, err
	}
	newID := ownedAccountID(email)

	// 模型清单与配额优先走 2Ag 原生 live 探测（见 quota_probe.go）：
	// 全新登录的账号此刻多半还没有可用读数，读不到就如实留空，由前端显示「未探测」，
	// 绝不回填写死的 5 个模型名冒充能力清单。第三方缓存只作明确标注的 fallback。
	gPool, cPool, models, _ := probeQuotaForAccount(email)

	// 登记的账号此刻**没有**宿主在跑，也没有任何一次握手 ——
	// 因此状态如实是 OFFLINE/STANDBY，绝不能写死成 "ACTIVE" 让界面显示
	// 一个并不存在的活跃账号。真正在运行的那个账号由 ScanLocalAccounts
	// 结合宿主探针结果标记为 PRIMARY/ACTIVE。
	status := "OFFLINE"
	cooldown := "未载入配额 (离线)"
	if gPool.Available || cPool.Available {
		status = "STANDBY"
		if gPool.Stale || cPool.Stale {
			cooldown = "缓存配额（非实时）"
		} else {
			cooldown = ""
		}
	}

	acc := &AccountInstance{
		ID:          newID,
		Email:       email,
		Name:        name,
		Role:        "BACKUP",
		IsPrimary:   false,
		IsActive:    false,
		Weight:      8,
		Status:      status,
		GeminiPool:  gPool,
		ClaudePool:  cPool,
		Models:      models,
		CooldownMsg: cooldown,
	}
	return acc, nil
}

// ImportAccountsJSON 从 JSON 导入账号清单（2Ag 账号清单 / 账号数组 / 单对象）。
//
// 刻意不支持「GCP OAuth 客户端凭据文件」：那正是自有 OAuth 路线的残留。
// 2Ag 不需要、也不接受用户的 OAuth 客户端凭据 —— 添加账号请走官方原生登录。
//
// 兼容读取第三方账号清单的字段形态（id/email/name），但导入结果一律写进
// 2Ag 自己的登记表，绝不替用户在别人的数据目录里创建文件。
func ImportAccountsJSON(data []byte) ([]AccountInstance, error) {
	// Try full account-list format ({version, accounts:[{id,email,name}]})
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

	return nil, errors.New("无法识别的 JSON 账号清单格式（支持账号清单 accounts.json / 账号数组 / 单个账号对象）")
}
