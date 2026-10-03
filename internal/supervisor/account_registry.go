package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// 2Ag 自有账号登记表（~/.2ag/accounts.json）
//
// 这里登记的是「本机有哪些账号」，**不含任何 token** —— 凭据本体只存在于两处：
// Windows 凭据管理器的 gemini:antigravity（当前登录态）与 2Ag 的 DPAPI 保险库
// （account_vault.go）。本文件与保险库索引的分工：
//
//   - 保险库索引：只登记**真的持有一份完整凭据**的账号（登录 / 切换 / 归档时写入）；
//   - 本文件：登记只有元数据、没有凭据的账号（JSON 导入的清单）。
//
// 为什么必须存在这份自有登记表，而不是继续写进第三方 cockpit 账号库：
// 0.1.1 之前账号清单的读与写都只认 ~/.antigravity_cockpit，于是**没装过
// cockpit-tools 的用户**导入账号时，2Ag 会在别人的目录里创建文件；而界面能读到
// 哪些账号又取决于那个目录在不在。结果是「添加账号」这件事本身以「用户装了什么
// 第三方工具」为前提 —— 一个发布版产品不该有这个前提。
//
// cockpit 账号库因此降级为**可选的、只读的历史来源**（见 collectAccountEntries），
// 老用户已有的账号不会消失，新用户则完全不需要它。
// ============================================================================

const (
	ownedAccountsFileName = "accounts.json"
	ownedAccountsVersion  = "2ag-1"
)

// ownedAccountEntry 是自有登记表里的一条记录。
type ownedAccountEntry struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	LastUsed  int64  `json:"last_used"`
}

type ownedAccountsDoc struct {
	Version  string              `json:"version"`
	Accounts []ownedAccountEntry `json:"accounts"`
}

// ownedAccountsPath 返回自有登记表的绝对路径（~/.2ag/accounts.json）。
//
// 与 active_account.txt 同目录：那一个是「上次真实拉起宿主用的账号」，
// 这一个才是「本机登记了哪些账号」，两者都属于 2Ag 自己的状态根目录。
func ownedAccountsPath() string {
	// 与保险库同根：~/.2ag/vault ⇒ 父目录 ~/.2ag。
	return filepath.Join(filepath.Dir(vaultDirPath()), ownedAccountsFileName)
}

// loadOwnedAccountsDoc 读取自有登记表。文件不存在或损坏时返回空文档（不报错）：
// 调用方只关心「有哪些账号」，读不出来就当成没有，绝不因此中断整个扫描。
func loadOwnedAccountsDoc() ownedAccountsDoc {
	doc := ownedAccountsDoc{Version: ownedAccountsVersion}
	raw, err := os.ReadFile(ownedAccountsPath())
	if err != nil {
		return doc
	}
	var parsed ownedAccountsDoc
	if err := json.Unmarshal(raw, &parsed); err != nil {
		log.Printf("[2ag] 自有账号登记表解析失败（按空表处理）: %v", err)
		return doc
	}
	if parsed.Version == "" {
		parsed.Version = ownedAccountsVersion
	}
	return parsed
}

// LoadOwnedAccountEntries 列出 2Ag 自有登记表里的账号（只读）。
func LoadOwnedAccountEntries() []ownedAccountEntry {
	return loadOwnedAccountsDoc().Accounts
}

// SaveOwnedAccountEntry 把一个账号登记进 2Ag 自己的登记表（幂等）。
//
// 已存在（邮箱大小写不敏感）时只刷新 Name 与 LastUsed，不重复追加。
func SaveOwnedAccountEntry(email, name string) error {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return os.ErrInvalid
	}
	if name == "" {
		name = strings.Split(email, "@")[0]
	}

	doc := loadOwnedAccountsDoc()
	if doc.Version == "" {
		doc.Version = ownedAccountsVersion
	}
	now := time.Now().Unix()

	found := false
	for i := range doc.Accounts {
		if strings.EqualFold(strings.TrimSpace(doc.Accounts[i].Email), email) {
			if name != "" {
				doc.Accounts[i].Name = name
			}
			doc.Accounts[i].LastUsed = now
			// 顺手把历史 id 修成当前规则：只用 hex(email) 前 6 字节的旧 id
			// 会让前缀相同的账号撞车（见 ownedAccountID 注释）。
			doc.Accounts[i].ID = ownedAccountID(email)
			found = true
			break
		}
	}
	if !found {
		doc.Accounts = append(doc.Accounts, ownedAccountEntry{
			ID:        ownedAccountID(email),
			Email:     email,
			Name:      name,
			CreatedAt: now,
			LastUsed:  now,
		})
	}

	path := ownedAccountsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	// 登记表只有元数据，但仍是用户目录下的状态文件：原子写，避免中断留下半截 JSON。
	return writeFileAtomic(path, append(data, '\n'), 0o644)
}

// DeleteOwnedAccountEntry 从自有登记表移除一个账号（不存在时视为成功）。
func DeleteOwnedAccountEntry(email string) error {
	want := strings.ToLower(strings.TrimSpace(email))
	if want == "" {
		return nil
	}
	doc := loadOwnedAccountsDoc()
	kept := doc.Accounts[:0]
	for _, a := range doc.Accounts {
		if strings.EqualFold(strings.TrimSpace(a.Email), want) {
			continue
		}
		kept = append(kept, a)
	}
	if len(kept) == len(doc.Accounts) {
		return nil
	}
	doc.Accounts = kept
	if doc.Version == "" {
		doc.Version = ownedAccountsVersion
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(ownedAccountsPath(), append(data, '\n'), 0o644)
}

// ownedAccountID 由邮箱推出登记 id。
//
// 用邮箱的 SHA-256 前 12 位十六进制。**不能用邮箱原文的十六进制前缀** ——
// 0.1.1 之前写进 cockpit 账号库的规则是 hex(email)[:12]，那只有 6 个字节，
// 邮箱前缀相同的账号会得到同一个 id（tester1/tester2/tester3@example.com
// 全部塌成 acc-746573746572）。id 目前只用于展示与日志，但一个「不同账号
// 撞同一个 id」的登记表本身就是坏的，日后任何按 id 去重/查找的代码都会静默合并账号。
func ownedAccountID(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "acc-" + hex.EncodeToString(sum[:])[:12]
}

// ---------------------------------------------------------------------------
// 账号来源汇总
// ---------------------------------------------------------------------------

// accountSourceEntry 是「账号清单」里的一条登记记录（不含任何 token）。
type accountSourceEntry struct {
	ID    string
	Email string
	Name  string
	// Origin 只用于日志排障："vault" / "registry" / "cockpit"。
	Origin string
}

// collectAccountEntries 汇总本机所有真实账号登记来源，按邮箱去重（先出现者胜）。
//
// 顺序即优先级：
//  1. ~/.2ag/vault 保险库 —— 2Ag 自己的账号库。凡是在本机成功登录过的账号都在这里，
//     与用户装没装第三方工具无关，是唯一「必然存在」的来源；
//  2. ~/.2ag/accounts.json —— 2Ag 自己的登记表（JSON 导入的元数据账号）；
//  3. cockpit accounts.json —— 第三方工具的历史遗留，仅当本机真的装过它时才存在。
//
// 第 3 项刻意保留：老用户升级到 0.1.2 之后，他原先在 cockpit 里的账号必须还在，
// 不能因为 2Ag 换了自家账号库就把用户看得见的东西删掉。
// 但它**只是**兼容来源：读它之前不需要它存在，读不到也不影响前两项。
func collectAccountEntries() []accountSourceEntry {
	var out []accountSourceEntry
	seen := map[string]bool{}

	add := func(id, email, name, origin string) {
		email = strings.TrimSpace(email)
		if email == "" || !strings.Contains(email, "@") {
			return
		}
		key := strings.ToLower(email)
		if seen[key] {
			return
		}
		seen[key] = true
		if id == "" {
			id = ownedAccountID(email)
		}
		out = append(out, accountSourceEntry{ID: id, Email: email, Name: name, Origin: origin})
	}

	// ① 2Ag DPAPI 保险库（主来源）。
	for _, e := range ListVaultAccountEntries() {
		add(e.ID, e.Email, e.DisplayName, "vault")
	}

	// ② 2Ag 自有登记表（JSON 导入的元数据账号）。
	for _, e := range LoadOwnedAccountEntries() {
		add(e.ID, e.Email, e.Name, "registry")
	}

	// ③ 第三方 cockpit 账号库（可选的历史来源）。
	for _, e := range loadLegacyCockpitEntries() {
		add(e.ID, e.Email, e.Name, "cockpit")
	}

	return out
}

// legacyCockpitEntry 是第三方账号库里的一条记录。
type legacyCockpitEntry struct {
	ID    string
	Email string
	Name  string
}

// legacyCockpitAccountsPath 返回第三方账号库中账号清单文件的候选路径（可能都不存在）。
//
// 两个位置都试：$COCKPIT_TOOLS_DATA_DIR 优先（那个工具自己的规则），
// 否则当前用户的 ~/.antigravity_cockpit。全部基于**当前用户**解析 ——
// 历史实现把开发机的绝对路径写死在两处，别的用户永远读不到。
func legacyCockpitAccountsPath() []string {
	var paths []string
	if dir := cockpitDataDir(); dir != "" {
		paths = append(paths, filepath.Join(dir, "accounts.json"))
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		paths = append(paths, filepath.Join(appData, "antigravity_cockpit", "accounts.json"))
	}
	return paths
}

// loadLegacyCockpitEntries 读取第三方账号库的账号清单（读不到返回 nil）。
// 这是纯粹的只读兼容路径：**永不创建、永不修改**那个目录。
func loadLegacyCockpitEntries() []legacyCockpitEntry {
	for _, p := range legacyCockpitAccountsPath() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var caf cockpitAccountsFile
		if err := json.Unmarshal(data, &caf); err != nil || len(caf.Accounts) == 0 {
			continue
		}
		out := make([]legacyCockpitEntry, 0, len(caf.Accounts))
		for _, a := range caf.Accounts {
			out = append(out, legacyCockpitEntry{ID: a.ID, Email: a.Email, Name: a.Name})
		}
		return out
	}
	return nil
}

// legacyCockpitPrimaryEmail 返回第三方账号库自报的「当前账号」邮箱（读不到返回空）。
//
// 只作为**兜底**答案：主控身份的权威来源是 GetActiveAccountEmail()
// （宿主 CDP 探针读到的真实登录邮箱，或用户在界面上的显式选择）。
// 只有在 2Ag 自己没有任何记录时才回落到这里，纯粹为了不让老用户的
// 「主控」标记在升级后凭空消失。
func legacyCockpitPrimaryEmail() string {
	for _, p := range legacyCockpitAccountsPath() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var caf cockpitAccountsFile
		if err := json.Unmarshal(data, &caf); err != nil {
			continue
		}
		if caf.CurrentAccountID == "" {
			return ""
		}
		for _, a := range caf.Accounts {
			if a.ID == caf.CurrentAccountID {
				return strings.TrimSpace(a.Email)
			}
		}
		return ""
	}
	return ""
}
