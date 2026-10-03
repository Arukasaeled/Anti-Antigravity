package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// 「添加账号不需要本机装 cockpit-tools」的回归测试
//
// 这组测试守的是一条产品级承诺，而不是某个函数的实现细节：
// 一个从没装过第三方工具的机器，装完 2Ag 之后必须能：
//   1. 登记账号（写进 ~/.2ag/accounts.json，而不是别人的目录）；
//   2. 在账号列表里看到它；
//   3. 从保险库取出凭据用于切换登录身份。
//
// 所有测试都通过改写 USERPROFILE 指到临时目录来模拟一台干净机器 ——
// 直接读本机真实 ~/.2ag 会让测试结果取决于开发机状态（这正是原实现的问题）。
// ============================================================================

// isolateHome 把「当前用户主目录」指到临时目录，返回该目录。
//
// os.UserHomeDir() 在 Windows 上读 $USERPROFILE，每次调用都重新读环境变量，
// 所以 t.Setenv 对它有效；vaultDirPath() / ownedAccountsPath() 都走它。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home) // 非 Windows 兼容
	return home
}

// TestOwnedRegistryRoundTrip 自有登记表的写入 / 列举 / 幂等 / 删除。
func TestOwnedRegistryRoundTrip(t *testing.T) {
	home := isolateHome(t)

	const email = "newuser@example.com"
	if err := SaveOwnedAccountEntry(email, "New User"); err != nil {
		t.Fatalf("登记账号失败: %v", err)
	}

	// 必须落在 2Ag 自己的根目录下。
	wantPath := filepath.Join(home, ".2ag", "accounts.json")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("自有登记表应写在 %s，实际: %v", wantPath, err)
	}

	entries := LoadOwnedAccountEntries()
	if len(entries) != 1 {
		t.Fatalf("登记后应有 1 个账号，实际 %d", len(entries))
	}
	if !strings.EqualFold(entries[0].Email, email) {
		t.Errorf("登记邮箱应为 %s，实际 %s", email, entries[0].Email)
	}
	if entries[0].ID != ownedAccountID(email) {
		t.Errorf("登记 id 应为 %s，实际 %s", ownedAccountID(email), entries[0].ID)
	}

	// 幂等：重复登记不得产生第二条记录。
	if err := SaveOwnedAccountEntry(email, "New User"); err != nil {
		t.Fatalf("重复登记失败: %v", err)
	}
	if got := len(LoadOwnedAccountEntries()); got != 1 {
		t.Errorf("重复登记后仍应只有 1 个账号，实际 %d", got)
	}

	// 大小写不同视为同一账号。
	if err := SaveOwnedAccountEntry(strings.ToUpper(email), ""); err != nil {
		t.Fatalf("大小写变体登记失败: %v", err)
	}
	if got := len(LoadOwnedAccountEntries()); got != 1 {
		t.Errorf("大小写变体不应新增记录，实际 %d 条", got)
	}

	if err := DeleteOwnedAccountEntry(email); err != nil {
		t.Fatalf("删除登记失败: %v", err)
	}
	if got := len(LoadOwnedAccountEntries()); got != 0 {
		t.Errorf("删除后应为空，实际 %d 条", got)
	}
	// 删除不存在的账号不该报错（界面上的移除按钮允许重复点击）。
	if err := DeleteOwnedAccountEntry(email); err != nil {
		t.Errorf("删除不存在的账号应成功，实际: %v", err)
	}
}

// TestOwnedRegistryRejectsGarbage 没有 @ 的字符串不是邮箱，不许登记。
func TestOwnedRegistryRejectsGarbage(t *testing.T) {
	isolateHome(t)
	for _, bad := range []string{"", "   ", "not-an-email", "550e8400-e29b"} {
		if err := SaveOwnedAccountEntry(bad, "x"); err == nil {
			t.Errorf("%q 不该被接受为账号邮箱", bad)
		}
	}
	if got := len(LoadOwnedAccountEntries()); got != 0 {
		t.Errorf("垃圾输入不该留下记录，实际 %d 条", got)
	}
}

// TestSaveScannedAccountNeverTouchesCockpit 是本轮修复的核心断言：
// 登记账号**只能**写 2Ag 自己的目录，绝不替用户在第三方工具的数据目录里建文件。
func TestSaveScannedAccountNeverTouchesCockpit(t *testing.T) {
	home := isolateHome(t)

	// 把用户指向一个不存在的第三方数据目录，并观察它有没有被创建。
	legacyDir := filepath.Join(home, "legacy-cockpit-should-stay-absent")
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", legacyDir)

	if _, err := SaveScannedAccount("fresh@example.com", "Fresh"); err != nil {
		t.Fatalf("登记账号失败: %v", err)
	}

	if _, err := os.Stat(legacyDir); err == nil {
		t.Errorf("登记账号不得在第三方目录 %s 下创建任何内容", legacyDir)
	}
	if _, err := os.Stat(filepath.Join(home, ".2ag", "accounts.json")); err != nil {
		t.Errorf("账号应登记在 2Ag 自己的目录下，实际: %v", err)
	}

	// 登记完必须能在账号清单里看到（即使这台机器没有第三方工具）。
	found := false
	for _, e := range collectAccountEntries() {
		if strings.EqualFold(e.Email, "fresh@example.com") {
			found = true
			if e.Origin != "registry" {
				t.Errorf("该账号来源应为 registry，实际 %s", e.Origin)
			}
		}
	}
	if !found {
		t.Error("登记后的账号必须出现在账号清单里")
	}
}

// TestCollectAccountEntriesWithoutCockpit 干净机器上：清单来自 2Ag 自己，
// 第三方库不存在也不影响。
func TestCollectAccountEntriesWithoutCockpit(t *testing.T) {
	isolateHome(t)
	// 明确指向一个空目录，模拟「装过但里面没东西」和「根本没装」两种情况之一。
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())

	if err := SaveOwnedAccountEntry("only-in-2ag@example.com", "Only"); err != nil {
		t.Fatalf("登记失败: %v", err)
	}

	entries := collectAccountEntries()
	if len(entries) != 1 {
		t.Fatalf("应有 1 个账号（来自 2Ag 自有登记表），实际 %d", len(entries))
	}
	if entries[0].Origin != "registry" {
		t.Errorf("来源应为 registry，实际 %s", entries[0].Origin)
	}
	if !strings.EqualFold(entries[0].Email, "only-in-2ag@example.com") {
		t.Errorf("邮箱不符: %s", entries[0].Email)
	}

	// ScanLocalAccounts 是界面实际调用的入口，同样必须看到它。
	accounts := ScanLocalAccounts()
	if len(accounts) != 1 {
		t.Fatalf("ScanLocalAccounts 应返回 1 个账号，实际 %d", len(accounts))
	}
	if accounts[0].Status == "ACTIVE" {
		t.Error("刚登记、没有任何宿主在跑的账号不该被标为 ACTIVE")
	}
}

// TestEmptyMachineReturnsNoAccounts 全新机器（既无第三方库也无任何账号）
// 必须返回空列表 —— 界面显示「未检测到账号」，绝不能凭空造出账号。
func TestEmptyMachineReturnsNoAccounts(t *testing.T) {
	isolateHome(t)
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())

	if got := collectAccountEntries(); len(got) != 0 {
		t.Errorf("干净机器上不该有任何账号，实际 %d 个: %v", len(got), got)
	}
}

// TestLegacyCockpitEntriesAreReadOnly 老用户兼容：第三方库里已有的账号
// 仍要出现在清单里（升级不能让用户的账号消失），且读取过程不得修改那个目录。
func TestLegacyCockpitEntriesAreReadOnly(t *testing.T) {
	isolateHome(t)
	legacyDir := t.TempDir()
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", legacyDir)

	// 造一个第三方账号清单（老用户升级前的状态）。
	const legacyEmail = "legacy-user@example.com"
	doc := map[string]any{
		"version":            "2.0",
		"current_account_id": "acc-legacy",
		"accounts": []map[string]any{
			{"id": "acc-legacy", "email": legacyEmail, "name": "Legacy", "created_at": 1, "last_used": 2},
		},
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	accountFile := filepath.Join(legacyDir, "accounts.json")
	if err := os.WriteFile(accountFile, raw, 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}
	before, _ := os.ReadFile(accountFile)

	entries := collectAccountEntries()
	if len(entries) != 1 {
		t.Fatalf("老库里的账号必须仍被列出，实际 %d 个", len(entries))
	}
	if entries[0].Origin != "cockpit" {
		t.Errorf("来源应为 cockpit，实际 %s", entries[0].Origin)
	}
	if !strings.EqualFold(entries[0].Email, legacyEmail) {
		t.Errorf("邮箱不符: %s", entries[0].Email)
	}

	// 主控标记兜底：2Ag 自己没有记录时，沿用老库自报的当前账号。
	if got := legacyCockpitPrimaryEmail(); !strings.EqualFold(got, legacyEmail) {
		t.Errorf("主控兜底应返回 %s，实际 %q", legacyEmail, got)
	}

	// 只读承诺：文件内容一字未改。
	after, _ := os.ReadFile(accountFile)
	if string(before) != string(after) {
		t.Error("读取第三方账号库不得修改其内容")
	}
}

// TestDedupPrefersOwnedSources 同一账号同时存在于保险库 / 登记表 / 老库时，
// 只输出一条，且来源以 2Ag 自己的为准。
func TestDedupPrefersOwnedSources(t *testing.T) {
	isolateHome(t)
	legacyDir := t.TempDir()
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", legacyDir)

	const email = "dupe@example.com"

	// ① 2Ag 登记表
	if err := SaveOwnedAccountEntry(email, "Dupe"); err != nil {
		t.Fatalf("登记失败: %v", err)
	}
	// ② 第三方老库里的同一个人
	doc := map[string]any{
		"version": "2.0",
		"accounts": []map[string]any{
			{"id": "acc-dupe", "email": strings.ToUpper(email), "name": "Dupe Legacy"},
		},
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	// 老库的账号文件要能被 loadLegacyCockpitEntries 读到（它只看 accounts.json）。
	if err := os.WriteFile(filepath.Join(legacyDir, "accounts.json"), raw, 0o644); err != nil {
		t.Fatalf("写夹具失败: %v", err)
	}

	entries := collectAccountEntries()
	if len(entries) != 1 {
		t.Fatalf("同一账号（大小写不同）应去重为 1 条，实际 %d: %v", len(entries), entries)
	}
	if entries[0].Origin != "registry" {
		t.Errorf("去重应保留 2Ag 自己的来源 registry，实际 %s", entries[0].Origin)
	}
}

// TestCredentialPayloadPrefersVault 凭据取出顺序：保险库优先于第三方库。
//
// 这条守的是「切换账号不依赖 cockpit-tools」：用户的账号是在 2Ag 里登录的，
// 凭据只可能在 2Ag 自己的保险库里。
func TestCredentialPayloadPrefersVault(t *testing.T) {
	isolateHome(t)
	// 第三方库里什么也没有 —— 一个没装过那个工具的机器就是这样。
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())

	const email = "vault-only@example.com"
	// 造一份**可用的**凭据 blob（BuildAntigravityCredentialPayload 要求的形态）。
	blob := map[string]any{
		"id_token": jwtWithEmail(email),
		"token": map[string]any{
			"access_token":  "ya29.fixture-access",
			"token_type":    "Bearer",
			"refresh_token": "1//fixture-refresh",
			"expiry":        "2030-01-01T00:00:00.000000Z",
		},
		"auth_method": "consumer",
	}
	raw, _ := json.Marshal(blob)

	if _, err := StoreVaultCredential(email, "Vault Only", raw); err != nil {
		t.Fatalf("写入保险库失败: %v", err)
	}

	payload, source, err := credentialPayloadForAccount(email)
	if err != nil {
		t.Fatalf("应从保险库取出凭据，实际失败: %v", err)
	}
	if source != "vault" {
		t.Errorf("来源应为 vault，实际 %s", source)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("取出的凭据不是合法 JSON: %v", err)
	}
	tok, _ := got["token"].(map[string]any)
	if tok == nil || tok["refresh_token"] != "1//fixture-refresh" {
		t.Errorf("取出的凭据必须原样保留 refresh_token，实际: %s", payload)
	}
}

// TestCredentialPayloadErrorIsActionable 没有任何来源时，
// 报错必须告诉用户「去登录一次」，而不是暴露内部目录名。
func TestCredentialPayloadErrorIsActionable(t *testing.T) {
	isolateHome(t)
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())

	_, _, err := credentialPayloadForAccount("nobody@example.com")
	if err == nil {
		t.Fatal("没有凭据时必须报错")
	}
	msg := err.Error()
	if strings.Contains(msg, "cockpit") {
		t.Errorf("报错不该把内部实现（cockpit）暴露给用户: %s", msg)
	}
	if !strings.Contains(msg, "登录") {
		t.Errorf("报错必须给出可执行的下一步（去登录一次），实际: %s", msg)
	}
}

// TestIdentifyOwnerFindsVaultAccount 身份反查必须能查到保险库里的账号 ——
// 否则凭据归档会因为「认不出主人」被拒，而归档是切换账号的前置步骤。
func TestIdentifyOwnerFindsVaultAccount(t *testing.T) {
	isolateHome(t)
	t.Setenv("COCKPIT_TOOLS_DATA_DIR", t.TempDir())

	const email = "owner-lookup@example.com"
	blob := map[string]any{
		"id_token": jwtWithEmail(email),
		"token": map[string]any{
			"access_token":  "ya29.owner-access",
			"refresh_token": "1//owner-refresh",
		},
		"auth_method": "consumer",
	}
	raw, _ := json.Marshal(blob)
	if _, err := StoreVaultCredential(email, "Owner", raw); err != nil {
		t.Fatalf("写入保险库失败: %v", err)
	}

	got, err := identifyCredentialOwner("1//owner-refresh", "")
	if err != nil {
		t.Fatalf("反查失败: %v", err)
	}
	if !strings.EqualFold(got, email) {
		t.Errorf("反查应得到 %s，实际 %q", email, got)
	}

	// access_token 同样可作依据。
	got2, _ := identifyCredentialOwner("", "ya29.owner-access")
	if !strings.EqualFold(got2, email) {
		t.Errorf("用 access_token 反查应得到 %s，实际 %q", email, got2)
	}

	// 不存在的 token：如实返回空，不得猜。
	got3, _ := identifyCredentialOwner("1//nobody", "")
	if got3 != "" {
		t.Errorf("未知 token 应返回空串，实际 %q", got3)
	}
}

// TestOwnedAccountIDDoesNotCollide 守「不同邮箱必须得到不同 id」。
//
// 0.1.1 之前的规则是 hex(email)[:12]，只取邮箱原文的前 6 个字节 ——
// tester1/tester2/tester3@example.com 会全部塌成同一个 id。
// id 目前只用于展示，但登记表里出现重复 id 本身就是坏的：
// 日后任何按 id 去重/查找的代码都会静默把两个账号合成一个。
func TestOwnedAccountIDDoesNotCollide(t *testing.T) {
	emails := []string{
		"tester1@example.com",
		"tester2@example.com",
		"tester3@example.com",
		"user@example.com",
		"user@example.org",
		"alpha.beta@example.net",
		"gamma.delta@example.net",
	}
	seen := map[string]string{}
	for _, email := range emails {
		id := ownedAccountID(email)
		if id == "" || id == "acc-" {
			t.Errorf("%s 得到了空 id", email)
			continue
		}
		if prev, dup := seen[id]; dup {
			t.Errorf("%s 与 %s 撞了同一个 id %s", email, prev, id)
			continue
		}
		seen[id] = email
	}

	// 大小写与首尾空白不应产生新 id（同一个人）。
	if ownedAccountID("User@Example.com") != ownedAccountID("  user@example.com  ") {
		t.Errorf("大小写/空白变体应得到同一个 id")
	}
}

// TestRegistryIDMatchesAcrossSaves 保证重复登记不会改写 id 规则。
func TestRegistryIDMatchesAcrossSaves(t *testing.T) {
	isolateHome(t)
	const email = "tester1@example.com"
	if err := SaveOwnedAccountEntry(email, "Tester"); err != nil {
		t.Fatalf("首次登记失败: %v", err)
	}
	first := LoadOwnedAccountEntries()[0].ID
	if err := SaveOwnedAccountEntry(email, "Tester Renamed"); err != nil {
		t.Fatalf("重复登记失败: %v", err)
	}
	entries := LoadOwnedAccountEntries()
	if len(entries) != 1 {
		t.Fatalf("重复登记不应新增记录，实际 %d 条", len(entries))
	}
	if entries[0].ID != first || entries[0].ID != ownedAccountID(email) {
		t.Errorf("重复登记后 id 应保持 %s，实际 %s", first, entries[0].ID)
	}
	if entries[0].Name != "Tester Renamed" {
		t.Errorf("名称应被刷新为 Tester Renamed，实际 %q", entries[0].Name)
	}
}
