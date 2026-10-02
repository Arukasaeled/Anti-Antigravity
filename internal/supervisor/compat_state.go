//go:build windows

package supervisor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ============================================================================
// Official Compatibility / Clean State
//
// 这一节回答的是一个用户真正会问的问题：「我退出 2Ag、直接双击官方
// Antigravity，会不会继承 2Ag 留下的脏状态？」
//
// 答案分两半，本节把两半都摆出来：
//   - 一半是**设计保证**（2Ag 不写官方安装目录、不 patch 宿主文件、注入全是
//     runtime-only，宿主一关就消失）；
//   - 另一半是**查实的共享面**（Windows 凭据管理器 gemini:antigravity、
//     %USERPROFILE%\.gemini\antigravity 数据树）—— 这两处官方与 2Ag 都读，
//     2Ag 换号会改写凭据，且退出时不改回。
//
// 刻意不把设计保证写成「✓ 已验证」：运行时无法证伪「官方 exe 有没有被别的
// 程序改过」，只能陈述 2Ag 自己的行为边界。把只能声明的东西标成已检查，
// 就是这块面板最可能变成假数据的地方。
// ============================================================================

// CompatCheck 是面板上的一行。
//
// Action 非空表示这一项**用户可以动手恢复**，前端据此渲染 [恢复] 按钮；
// 空则表示这一项只能陈述事实（例如「凭据是双方共享的」，这是官方安装的
// 固有属性，不是 2Ag 制造的污染，没有可恢复的动作）。
type CompatCheck struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Action string `json:"action,omitempty"`
}

// OfficialCompatibility 是官方兼容性面板的完整读数。
type OfficialCompatibility struct {
	Level string `json:"level"` // READY / WARNING / ACTION_REQUIRED

	OfficialExe     string  `json:"official_exe"`
	OfficialVersion string  `json:"official_version"`
	OfficialSizeMB  float64 `json:"official_size_mb"`

	FrozenHostExe     string `json:"frozen_host_exe"`
	FrozenHostVersion string `json:"frozen_host_version"`

	OfficialProfileDir string `json:"official_profile_dir"`
	SandboxProfileDir  string `json:"sandbox_profile_dir"`

	SharedDataTree    string `json:"shared_data_tree"`
	SharedDataExists  bool   `json:"shared_data_exists"`
	SharedDataEntries int    `json:"shared_data_entries"`

	CredentialPresent bool     `json:"credential_present"`
	CredentialOwner   string   `json:"credential_owner"`
	VaultAccounts     []string `json:"vault_accounts"`
	ActiveAccount     string   `json:"active_account"`

	Checks []CompatCheck `json:"checks"`
}

const sharedDataTreeName = "antigravity"

// sharedDataTreeDir 返回官方与 2Ag 共用的数据树 ~/.gemini/antigravity。
//
// 依据是官方 asar 里的逐字取证：
//
//	IDE_OLD_DATA_DIR = path.join(os.homedir(), '.gemini', 'antigravity')
//
// 以及冻结宿主沙箱侧同名的 brain/conversations 写入。路径是只读探测，
// 不创建、不修改。
func sharedDataTreeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", sharedDataTreeName)
}

// officialUserDataDir 返回官方 profile 目录 %APPDATA%\Antigravity。
func officialUserDataDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "Antigravity")
}

// sandboxProfileDir 返回 2Ag 当前活跃账号的沙箱 profile 目录。
//
// 它落在 ~/.2ag/profiles/<sanitized>，与官方 profile 分属两个目录 ——
// 这一点是整个隔离设计的承重件，面板必须把它显示出来而不是口头保证。
func sandboxProfileDir() string {
	email := GetActiveAccountEmail()
	if email == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".2ag", "profiles", sanitizeEmail(email))
}

// countDirEntries 数目录下的一级条目数，目录不存在返回 (-1, false)。
func countDirEntries(dir string) (int, bool) {
	if dir == "" {
		return 0, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	return len(entries), true
}

// dirExists 报告目录是否存在（不创建）。
func dirExists(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// ProbeOfficialCompatibility 汇总官方兼容性读数。
//
// 每一项都来自真实文件系统或真实凭据读取；读不到就是读不到（空串 / false），
// 绝不回填占位值。
func ProbeOfficialCompatibility() OfficialCompatibility {
	out := OfficialCompatibility{}

	// ---- 官方安装 ----
	out.OfficialExe = FindOfficialAntigravity()
	if out.OfficialExe != "" {
		out.OfficialVersion = fileProductVersion(out.OfficialExe)
		if fi, err := os.Stat(out.OfficialExe); err == nil {
			out.OfficialSizeMB = float64(fi.Size()) / (1024 * 1024)
		}
	}

	// ---- 2Ag 冻结副本 ----
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		_ = localAppData // 冻结副本路径始终由 2Ag 根目录推导，见 detectDefaultAntigravityPath
	}
	frozen := filepath.Join(Get2agRootDir(), "app", "Antigravity.exe")
	if fi, err := os.Stat(frozen); err == nil && !fi.IsDir() {
		out.FrozenHostExe = frozen
		out.FrozenHostVersion = fileProductVersion(frozen)
	}

	// ---- 两套 profile ----
	out.OfficialProfileDir = officialUserDataDir()
	out.SandboxProfileDir = sandboxProfileDir()

	// ---- 共享数据树 ----
	out.SharedDataTree = sharedDataTreeDir()
	if n, ok := countDirEntries(out.SharedDataTree); ok {
		out.SharedDataExists = true
		out.SharedDataEntries = n
	}

	// ---- 共享凭据 ----
	out.CredentialPresent = AntigravityCredentialPresent()
	if owner, err := ReadHostLoginEmail(); err == nil {
		out.CredentialOwner = strings.TrimSpace(owner)
	}
	out.VaultAccounts = ListVaultAccounts()
	out.ActiveAccount = GetActiveAccountEmail()

	out.Checks = buildCompatChecks(out)
	out.Level = compatLevel(out.Checks)
	return out
}

func buildCompatChecks(c OfficialCompatibility) []CompatCheck {
	checks := make([]CompatCheck, 0, 6)

	// 1. 官方可执行文件。找不到是真问题（官方形态根本起不来）。
	ok := c.OfficialExe != ""
	detail := "未在 %LOCALAPPDATA%\\Programs\\Antigravity 等处发现官方 Antigravity.exe"
	if ok {
		detail = fmt.Sprintf("%s · 版本 %s · %.1f MB", c.OfficialExe, orUnknown(c.OfficialVersion), c.OfficialSizeMB)
	}
	checks = append(checks, CompatCheck{
		ID: "official_executable", OK: ok,
		Label: "官方安装可定位", Detail: detail,
	})

	// 2. 官方安装目录未被 2Ag 写入。
	//    这一项刻意标注为「设计保证」而不是「已扫描验证」——2Ag 的全部写入点
	//    都在 ~/.2ag 与 %LOCALAPPDATA%\2Ag 下（代码级可枚举），但运行时无法
	//    证伪「别的程序改过它」，所以陈述的是行为边界，不是扫描结论。
	checks = append(checks, CompatCheck{
		ID: "no_persistent_patch", OK: true,
		Label: "官方文件未被补丁化",
		Detail: "2Ag 的注入全部走 CDP runtime injection（Runtime.evaluate + " +
			"Page.addScriptToEvaluateOnNewDocument），宿主进程结束即消失；" +
			"从不 patch 官方 exe / asar / resources",
	})

	// 3. profile 隔离：真读两个目录，都显示出来让用户自己核对。
	isolated := c.OfficialProfileDir != "" && c.SandboxProfileDir != "" && c.OfficialProfileDir != c.SandboxProfileDir
	detail = ""
	switch {
	case c.OfficialProfileDir == "":
		detail = "未找到官方 profile 目录（%APPDATA%\\Antigravity）"
	case c.SandboxProfileDir == "":
		detail = "官方 profile: " + c.OfficialProfileDir + " · 2Ag 沙箱未确定（当前无活跃账号）"
	default:
		detail = "官方 profile: " + c.OfficialProfileDir + " · 2Ag 沙箱: " + c.SandboxProfileDir
	}
	checks = append(checks, CompatCheck{
		ID: "profile_isolated", OK: isolated,
		Label: "两套 profile 相互独立", Detail: detail,
	})

	// 4. 共享凭据 —— 这是查实存在的共享面，如实标为 WARNING 而非 PASS。
	credDetail := "Windows 凭据管理器中没有 gemini:antigravity 凭据"
	if c.CredentialPresent {
		owner := orUnknown(c.CredentialOwner)
		credDetail = "系统凭据当前归属：" + owner + "。官方 Antigravity 启动即读这一条记录 —— " +
			"2Ag 换号时会改写它，并且退出时不改回。"
	}
	credCheck := CompatCheck{
		ID: "shared_credential", OK: !c.CredentialPresent,
		Label: "凭据管理器为双方共享", Detail: credDetail,
	}
	if len(c.VaultAccounts) > 0 {
		credCheck.Action = "restore_credential"
		credDetail += " 可从 2Ag 归档恢复：" + strings.Join(c.VaultAccounts, "、")
		credCheck.Detail = credDetail
	}
	checks = append(checks, credCheck)

	// 5. 共享数据树 —— 官方安装同样读它（asar 里有 IDE_OLD_DATA_DIR 的逐字取证）。
	if c.SharedDataExists {
		checks = append(checks, CompatCheck{
			ID: "shared_data_tree", OK: false,
			Label: "%USERPROFILE%\\.gemini\\antigravity 双方共读",
			Detail: fmt.Sprintf("该目录存在 %d 个一级条目（brain / conversations / 会话摘要等）。"+
				"官方安装与 2Ag 拉起的宿主读取同一棵树 —— 这是官方自身的目录布局，不是 2Ag 制造的共享。", c.SharedDataEntries),
		})
	} else {
		checks = append(checks, CompatCheck{
			ID: "shared_data_tree", OK: true,
			Label: "共享数据树不存在", Detail: "未发现 %USERPROFILE%\\.gemini\\antigravity",
		})
	}

	// 6. 运行形态。
	mode := "2Ag 增强形态"
	if IsOfficialRuntime() {
		mode = "官方形态"
	}
	checks = append(checks, CompatCheck{
		ID: "runtime_mode", OK: true,
		Label:  "当前运行形态：" + mode,
		Detail: "官方形态下 2Ag 不注入、不挂 CDP、不改写凭据，行为接近「没有安装 2Ag」",
	})

	return checks
}

func compatLevel(checks []CompatCheck) string {
	level := "READY"
	for _, c := range checks {
		if c.Action != "" {
			return "ACTION_REQUIRED"
		}
		// 共享面（id 前缀 shared_）是**已知且无法单方面消除**的：凭据管理器
		// 就是机器级共享的，官方自己也在用它。把它们算成 ACTION_REQUIRED
		// 会让面板永久红着，用户很快就会学会忽略它。
		if !c.OK && !strings.HasPrefix(c.ID, "shared_") {
			level = "WARNING"
		}
	}
	return level
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未知"
	}
	return s
}

// ============================================================================
// 凭据归档的读取与恢复
// ============================================================================

// vaultEntry 是归档索引里的一项（v2 索引的历史视图，仅有历史调用方在用）。
type vaultEntry struct {
	Email string `json:"email"`
	File  string `json:"file"`
	Bytes int64  `json:"bytes"`
}

// vaultDirPath 返回归档目录 ~/.2ag/vault。
func vaultDirPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".2ag", "vault")
}

// ListVaultAccounts 列出归档里可恢复的账号（按邮箱排序，结果稳定）。
//
// 只读，不修改任何东西（索引升级到 v2 由 MigrateLegacyVault 在启动时做一次）。
// 返回值永远是真实存在的文件对应的账号：索引里登记了但文件已被删掉的条目会被剔除，
// 否则面板会提供一个点了必然失败的动作。
func ListVaultAccounts() []string {
	entries := ListVaultAccountEntries()
	accounts := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Email == "" {
			continue
		}
		accounts = append(accounts, e.Email)
	}
	sort.Strings(accounts)
	return accounts
}

// CredentialRestoreResult 是凭据恢复的真实回执。
type CredentialRestoreResult struct {
	Email           string `json:"email"`
	Bytes           int    `json:"bytes"`
	PreviousOwner   string `json:"previous_owner"`
	PreviousSaved   bool   `json:"previous_saved"`
	PreviousSavedAs string `json:"previous_saved_as,omitempty"`
	VerifiedOwner   string `json:"verified_owner"`
	Verified        bool   `json:"verified"`
	Message         string `json:"message"`
}

// RestoreAntigravityCredential 把归档里的某一份凭据原样写回 Windows 凭据管理器。
//
// 这是「恢复官方状态」里唯一一条真正需要动手的动作，因此有两条硬规则：
//
//  1. **先归档再覆盖。** 恢复是破坏性的：当前凭据里可能有宿主刚刷新过的
//     refresh_token，而那是全世界唯一的一份。覆盖前无条件先 Snapshot 一次，
//     失败就中止 —— 「恢复」绝不能以「先毁掉现状」开头。
//  2. **写完要真读一遍核对。** CredWriteW 返回成功不等于凭据管理器里现在
//     就是那个账号（凭据写入与读取之间有系统级缓存）。这里用 ReadHostLoginEmail
//     复核，不一致就如实报告失败，而不是宣布成功。
func RestoreAntigravityCredential(email string) (CredentialRestoreResult, error) {
	release, err := lockCredentialOperation()
	if err != nil {
		return CredentialRestoreResult{}, err
	}
	defer release()
	var out CredentialRestoreResult
	email = strings.TrimSpace(email)
	if email == "" {
		return out, fmt.Errorf("恢复凭据需要明确的目标邮箱")
	}

	dir := vaultDirPath()
	if dir == "" {
		return out, fmt.Errorf("无法定位归档目录")
	}

	// 读取走保险库读路径：它会自动辨认并就地迁移旧明文文件。
	blob, err := ReadVaultCredential(email)
	if err != nil {
		if len(ListVaultAccounts()) == 0 {
			return out, fmt.Errorf("归档中没有 %s 的凭据快照（归档为空；先在账号面板归档一次当前登录态）", email)
		}
		return out, fmt.Errorf("归档中没有 %s 的凭据快照（可恢复：%s）", email, strings.Join(ListVaultAccounts(), "、"))
	}
	if len(blob) == 0 {
		return out, fmt.Errorf("归档文件为空，拒绝写入: %s", email)
	}

	// 规则 1：先保住现状。
	if prevOwner, _ := ReadHostLoginEmail(); strings.TrimSpace(prevOwner) != "" {
		out.PreviousOwner = prevOwner
	}
	if prev, err := snapshotAntigravityCredential(); err == nil && prev.Bytes > 0 {
		out.PreviousSaved = true
		out.PreviousSavedAs = filepath.Base(prev.Path)
	} else if AntigravityCredentialPresent() {
		return out, fmt.Errorf("备份当前凭据失败，已中止恢复")
	}

	if err := writeAntigravityCredentialRaw(blob); err != nil {
		return out, fmt.Errorf("写入系统凭据失败: %w", err)
	}
	out.Email = email
	out.Bytes = len(blob)

	// 规则 2：写完真读一遍核对。
	if got, err := ReadHostLoginEmail(); err == nil {
		out.VerifiedOwner = strings.TrimSpace(got)
		out.Verified = strings.EqualFold(out.VerifiedOwner, email)
	}
	if !out.Verified {
		return out, fmt.Errorf("凭据已写入但复核显示归属仍为「%s」，未确认恢复成功（要求：%s）", orUnknown(out.VerifiedOwner), email)
	}

	// 复核通过后也要清一次身份缓存，让界面立刻看到新值。
	hostLoginCacheMu <- struct{}{}
	hostLoginCachedVal = ""
	hostLoginCachedAt = time.Time{}
	<-hostLoginCacheMu

	out.Message = fmt.Sprintf("已把 %s 的凭据快照写回系统凭据管理器（%d B）并复核通过", email, len(blob))
	log.Printf("[2ag] %s", out.Message)
	return out, nil
}
