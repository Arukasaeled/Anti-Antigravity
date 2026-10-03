package supervisor

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// 2Ag Account Vault
//
// 目录布局（~/.2ag/vault）：
//
//	index.json      只有非敏感元数据：account id / email / display name / 时间戳 / 文件名 / 字节数
//	<account-id>.bin DPAPI(CurrentUser) 加密后的**完整** gemini:antigravity 凭据 blob
//
// 两条硬约束（都是安全契约，不是风格问题）：
//  1. index.json 里永远不出现 access_token / refresh_token / id_token / client_secret。
//     只有 .bin 里有密文，而 .bin 只有当前 Windows 用户能解开。
//  2. account id = hex(sha256(lower(email)))，与 0.1.0 起的文件名完全一致 ——
//     升级不会让旧账号「消失」，它们只是被就地加密。
//
// 兼容迁移：0.1.1 之前的 .bin 是明文写的。MigrateLegacyVault() 会把保险库目录里
// 每一个还没加密的 .bin 读出来 → DPAPI 加密 → 原子替换（临时文件 + 改名），
// 然后把 index 升级到 v2。迁移是幂等的，重复跑不会二次加密。
// ============================================================================

const (
	vaultIndexVersion = 2
	vaultBlobVersion  = 1
	vaultBlobAlgDPAPI = "DPAPI-CurrentUser"
)

// VaultAccount 是保险库里一个账号的对外视图（**不含**任何凭据内容）。
type VaultAccount struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	File        string `json:"file"`
	Bytes       int64  `json:"bytes"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Encrypted   bool   `json:"encrypted"`
}

// vaultBlobFile 是 .bin 文件的封装格式。
//
// 之所以不把密文裸写进文件：需要一个「这是密文」的可判定标记。旧文件是明文 JSON
// （以 '{' 开头且没有 alg 字段），新文件一定是本结构。迁移与读取都靠这个判断，
// 而不是靠文件时间或扩展名猜。
type vaultBlobFile struct {
	V    int    `json:"v"`
	Alg  string `json:"alg"`
	Data string `json:"data"`
}

// vaultAccountMeta 是 index.json 里的一条记录。
type vaultAccountMeta struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	File        string `json:"file"`
	Bytes       int64  `json:"bytes"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	Encrypted   bool   `json:"encrypted"`
}

type vaultIndexV2 struct {
	Version   int                         `json:"version"`
	UpdatedAt string                      `json:"updated_at"`
	Accounts  map[string]vaultAccountMeta `json:"accounts"`
}

// vaultIndexV1 是 0.1.1 之前的 index 格式：只有 email → 文件名的映射。
// 保留结构定义只为了读得懂旧文件，写入一律走 v2。
type vaultIndexV1 struct {
	UpdatedAt string            `json:"updated_at"`
	Entries   map[string]string `json:"entries"`
}

// vaultAccountID 由邮箱推出账号 id（= 文件名主干的既定规则）。
func vaultAccountID(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// writeFileAtomic 先写临时文件再改名，避免半截文件。
//
// Windows 上 os.Rename 走 MoveFileEx(MOVEFILE_REPLACE_EXISTING)，所以它同时是
// 「原子替换已有文件」的正确做法。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := fmt.Sprintf("%s.tmp-%d", path, os.Getpid())
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// loadVaultIndex 读出保险库索引（v2 原样返回；v1 在内存里升级成 v2）。
//
// 只读，不落盘：把「读」与「迁移」分开，是为了让列表/状态这类高频只读调用
// 永远不会因为「顺手写回索引」而在只读场景下产生副作用。
func loadVaultIndex(dir string) (*vaultIndexV2, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &vaultIndexV2{Version: vaultIndexVersion, Accounts: map[string]vaultAccountMeta{}}, nil
		}
		return nil, fmt.Errorf("读取保险库索引失败: %w", err)
	}

	// 先按 v2 试；再按 v1 试。两者的判别靠结构而不是版本号 ——
	// 旧版本 index.json 里根本没有 version 字段。
	var v2 vaultIndexV2
	if json.Unmarshal(raw, &v2) == nil && v2.Accounts != nil {
		if v2.Version == 0 {
			v2.Version = vaultIndexVersion
		}
		return &v2, nil
	}
	var v1 vaultIndexV1
	if json.Unmarshal(raw, &v1) == nil && v1.Entries != nil {
		out := &vaultIndexV2{Version: vaultIndexVersion, UpdatedAt: v1.UpdatedAt, Accounts: map[string]vaultAccountMeta{}}
		for email, file := range v1.Entries {
			id := strings.TrimSuffix(filepath.Base(file), ".bin")
			if id == "" {
				id = vaultAccountID(email)
			}
			enc := false
			var size int64
			if st, statErr := os.Stat(filepath.Join(dir, filepath.Base(file))); statErr == nil {
				size = st.Size()
				if data, readErr := os.ReadFile(filepath.Join(dir, filepath.Base(file))); readErr == nil {
					enc = isEncryptedVaultBlob(data)
				}
			}
			out.Accounts[id] = vaultAccountMeta{
				ID: id, Email: email, File: filepath.Base(file), Bytes: size,
				UpdatedAt: v1.UpdatedAt, Encrypted: enc,
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("保险库索引格式无法识别（既不是 v2 也不是 v1）")
}

func writeVaultIndexV2(dir string, idx *vaultIndexV2) error {
	idx.Version = vaultIndexVersion
	idx.UpdatedAt = nowRFC3339()
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化保险库索引失败: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, "index.json"), append(raw, '\n'), 0o600)
}

// isEncryptedVaultBlob 判断 .bin 内容是不是 DPAPI 密文信封。
func isEncryptedVaultBlob(data []byte) bool {
	var f vaultBlobFile
	if json.Unmarshal(data, &f) != nil {
		return false
	}
	return f.Alg != "" && f.Data != ""
}

// encryptVaultPayload 把凭据明文封装成 DPAPI 密文信封。
func encryptVaultPayload(raw []byte) ([]byte, error) {
	sealed, err := dpapiProtect(raw)
	if err != nil {
		return nil, err
	}
	env := vaultBlobFile{V: vaultBlobVersion, Alg: vaultBlobAlgDPAPI, Data: base64.StdEncoding.EncodeToString(sealed)}
	out, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("序列化保险库密文失败: %w", err)
	}
	return append(out, '\n'), nil
}

// decryptVaultPayload 解开 .bin 内容；legacy=true 表示这是未加密的旧文件。
func decryptVaultPayload(data []byte) (plain []byte, legacy bool, err error) {
	var f vaultBlobFile
	if json.Unmarshal(data, &f) == nil && f.Alg == vaultBlobAlgDPAPI {
		sealed, decErr := base64.StdEncoding.DecodeString(f.Data)
		if decErr != nil {
			return nil, false, fmt.Errorf("保险库密文 base64 解码失败: %w", decErr)
		}
		plain, decErr = dpapiUnprotect(sealed)
		if decErr != nil {
			return nil, false, decErr
		}
		return plain, false, nil
	}
	// 旧格式：裸明文。
	return data, true, nil
}

// StoreVaultCredential 把一份凭据写入保险库（DPAPI 加密），并更新 index.json。
func StoreVaultCredential(email, displayName string, raw []byte) (VaultAccount, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return VaultAccount{}, fmt.Errorf("拒绝归档没有邮箱的凭据")
	}
	if len(raw) == 0 {
		return VaultAccount{}, fmt.Errorf("拒绝归档空凭据")
	}
	dir := vaultDirPath()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return VaultAccount{}, fmt.Errorf("创建保险库目录失败: %w", err)
	}

	id := vaultAccountID(email)
	name := id + ".bin"
	sealed, err := encryptVaultPayload(raw)
	if err != nil {
		return VaultAccount{}, err
	}
	if err := writeFileAtomic(filepath.Join(dir, name), sealed, 0o600); err != nil {
		return VaultAccount{}, fmt.Errorf("写入保险库文件失败: %w", err)
	}

	idx, err := loadVaultIndex(dir)
	if err != nil {
		return VaultAccount{}, err
	}
	meta, existed := idx.Accounts[id]
	meta.ID = id
	meta.Email = email
	meta.File = name
	meta.Bytes = int64(len(raw)) // 报明文大小：界面上的「字节数」应是凭据本身的大小
	if strings.TrimSpace(displayName) != "" {
		meta.DisplayName = strings.TrimSpace(displayName)
	}
	meta.Encrypted = true
	meta.UpdatedAt = nowRFC3339()
	if !existed || meta.CreatedAt == "" {
		meta.CreatedAt = meta.UpdatedAt
	}
	idx.Accounts[id] = meta
	if err := writeVaultIndexV2(dir, idx); err != nil {
		return VaultAccount{}, err
	}
	return vaultAccountFromMeta(meta), nil
}

// ReadVaultCredential 取出某个账号的明文凭据（仅本机当前用户可解）。
//
// This read never changes Vault data. Legacy migration has its own explicit path.
func ReadVaultCredential(email string) ([]byte, error) {
	dir := vaultDirPath()
	idx, err := loadVaultIndex(dir)
	if err != nil {
		return nil, credentialError(CredentialParseFailed, "保险库索引读取或解析失败")
	}
	meta, ok := findVaultMeta(idx, email)
	if !ok {
		return nil, credentialError(CredentialMissing, "保险库里没有账号 "+email)
	}
	data, err := os.ReadFile(filepath.Join(dir, meta.File))
	if err != nil {
		return nil, credentialError(CredentialMissing, "无法读取账号的保险库文件")
	}
	plain, _, err := decryptVaultPayload(data)
	if err != nil {
		return nil, credentialError(CredentialDecryptFailed, "保险库凭据解密失败")
	}
	return plain, nil
}

// DeleteVaultAccount 从保险库删除一个账号（文件 + 索引项）。
func DeleteVaultAccount(email string) error {
	dir := vaultDirPath()
	idx, err := loadVaultIndex(dir)
	if err != nil {
		return err
	}
	meta, ok := findVaultMeta(idx, email)
	if !ok {
		return fmt.Errorf("保险库里没有账号 %s", email)
	}
	if err := os.Remove(filepath.Join(dir, meta.File)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除保险库文件失败: %w", err)
	}
	delete(idx.Accounts, meta.ID)
	if err := writeVaultIndexV2(dir, idx); err != nil {
		return err
	}
	log.Printf("[2ag] 保险库：已删除账号 %s", meta.Email)
	return nil
}

// ListVaultAccountEntries 列出保险库里的账号（按邮箱排序，只读）。
func ListVaultAccountEntries() []VaultAccount {
	idx, err := loadVaultIndex(vaultDirPath())
	if err != nil {
		log.Printf("[2ag] 保险库索引读取失败: %v", err)
		return nil
	}
	out := make([]VaultAccount, 0, len(idx.Accounts))
	for _, meta := range idx.Accounts {
		if meta.File == "" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(vaultDirPath(), meta.File)); statErr != nil {
			continue // 索引里有、文件不在：不报给界面，避免点进去才发现是空的
		}
		out = append(out, vaultAccountFromMeta(meta))
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Email) < strings.ToLower(out[j].Email) })
	return out
}

func vaultAccountFromMeta(meta vaultAccountMeta) VaultAccount {
	return VaultAccount{
		ID: meta.ID, Email: meta.Email, DisplayName: meta.DisplayName, File: meta.File,
		Bytes: meta.Bytes, CreatedAt: meta.CreatedAt, UpdatedAt: meta.UpdatedAt, Encrypted: meta.Encrypted,
	}
}

// findVaultMeta 按邮箱（大小写不敏感）或账号 id 查找。
func findVaultMeta(idx *vaultIndexV2, email string) (vaultAccountMeta, bool) {
	want := strings.ToLower(strings.TrimSpace(email))
	if want == "" {
		return vaultAccountMeta{}, false
	}
	if meta, ok := idx.Accounts[vaultAccountID(want)]; ok {
		return meta, true
	}
	for _, meta := range idx.Accounts {
		if strings.ToLower(strings.TrimSpace(meta.Email)) == want || strings.ToLower(meta.ID) == want {
			return meta, true
		}
	}
	return vaultAccountMeta{}, false
}

// MigrateLegacyVault 把保险库里所有未加密的 .bin 就地加密，并把索引升级到 v2。
//
// 迁移是幂等的：已经加密的文件原样跳过。返回值是本次真正迁移的账号数。
// 单个文件失败不会中断其余文件 —— 一个坏文件不该让整库停在明文状态。
func MigrateLegacyVault() (int, error) {
	dir := vaultDirPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取保险库目录失败: %w", err)
	}
	idx, idxErr := loadVaultIndex(dir)
	if idxErr != nil {
		log.Printf("[2ag] 保险库索引损坏，迁移仍继续（以目录为准）: %v", idxErr)
		idx = &vaultIndexV2{Version: vaultIndexVersion, Accounts: map[string]vaultAccountMeta{}}
	}

	migrated := 0
	changed := idxErr != nil          // 索引需要重写（v1 → v2 或损坏重建）
	fileToMeta := map[string]string{} // 文件名 → account id
	for id, meta := range idx.Accounts {
		if meta.File != "" {
			fileToMeta[meta.File] = id
		}
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".bin") {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, readErr := os.ReadFile(full)
		if readErr != nil {
			log.Printf("[2ag] 保险库迁移：读取 %s 失败: %v", e.Name(), readErr)
			continue
		}
		if isEncryptedVaultBlob(data) {
			if id, ok := fileToMeta[e.Name()]; ok && !idx.Accounts[id].Encrypted {
				m := idx.Accounts[id]
				m.Encrypted = true
				idx.Accounts[id] = m
				changed = true
			}
			continue
		}
		// 明文：先解出邮箱（凭据里带 id_token），再加密回写。
		email := emailFromCredentialPayload(data)
		sealed, encErr := encryptVaultPayload(data)
		if encErr != nil {
			log.Printf("[2ag] 保险库迁移：加密 %s 失败: %v", e.Name(), encErr)
			continue
		}
		if wErr := writeFileAtomic(full, sealed, 0o600); wErr != nil {
			log.Printf("[2ag] 保险库迁移：写回 %s 失败: %v", e.Name(), wErr)
			continue
		}
		migrated++
		changed = true

		id := strings.TrimSuffix(e.Name(), ".bin")
		m := idx.Accounts[id]
		if m.ID == "" {
			m.ID = id
		}
		if m.Email == "" {
			m.Email = email
		}
		m.File = e.Name()
		m.Bytes = int64(len(data))
		m.Encrypted = true
		m.UpdatedAt = nowRFC3339()
		if m.CreatedAt == "" {
			m.CreatedAt = m.UpdatedAt
		}
		idx.Accounts[id] = m
		log.Printf("[2ag] 保险库迁移：%s 已由明文改为 DPAPI 密文", e.Name())
	}

	if changed {
		if err := writeVaultIndexV2(dir, idx); err != nil {
			return migrated, err
		}
	}
	return migrated, nil
}
