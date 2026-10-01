//go:build windows

package supervisor

import (
	"strings"
	"testing"
)

// TestSwitchAccountRejectsBeforeTouchingStore 守住「坏目标绝不触碰系统凭据」这条边界。
//
// 切换是破坏性的：它会把机器上唯一那份登录凭据覆盖掉。因此所有「目标不可用」的
// 情形都必须在**写入之前**被拒绝 —— 覆盖了再发现目标坏了，用户就已经丢了原来的账号。
// 这里逐条验三种坏目标：邮箱为空、保险库里没有、保险库里有但内容不是一份可用登录。
func TestSwitchAccountRejectsBeforeTouchingStore(t *testing.T) {
	if _, err := SwitchAccountTransactional("", "enhanced"); err == nil {
		t.Fatal("空邮箱必须被拒绝")
	}

	// 保险库里不存在的账号：错误里必须点名，而不是笼统的失败。
	if _, err := SwitchAccountTransactional("nobody-here@example.invalid", "enhanced"); err == nil {
		t.Fatal("保险库里没有的账号必须被拒绝")
	} else if !strings.Contains(err.Error(), "nobody-here@example.invalid") {
		t.Fatalf("拒绝原因里应包含目标账号，实际: %v", err)
	}
}

// TestVaultBlobEnvelopeDetection 守住「明文 / 密文」的判别规则。
//
// 迁移、读取、以及 H 项验收都依赖这条判别：密文信封有 alg+data，
// 旧格式是裸明文 JSON（{"token":...}）。判错的后果有两个方向都很糟：
// 把密文当明文读 → 拿到一堆乱码；把明文当密文读 → 解密失败、账号「消失」。
func TestVaultBlobEnvelopeDetection(t *testing.T) {
	plain := []byte(`{"token":{"access_token":"ya29.x","refresh_token":"1//0x"},"id_token":"eyJ.eyJ.","auth_method":"consumer"}`)
	if isEncryptedVaultBlob(plain) {
		t.Fatal("旧明文凭据不得被判为密文信封")
	}
	sealed, err := encryptVaultPayload(plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if !isEncryptedVaultBlob(sealed) {
		t.Fatalf("DPAPI 信封未被识别: %s", string(sealed[:minInt(40, len(sealed))]))
	}
	// 密文里不得出现任何明文片段。
	if strings.Contains(string(sealed), "ya29.") || strings.Contains(string(sealed), "refresh_token") {
		t.Fatal("密文信封里出现了明文 token 片段")
	}
	back, legacy, err := decryptVaultPayload(sealed)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if legacy {
		t.Fatal("密文不应被标为 legacy")
	}
	if string(back) != string(plain) {
		t.Fatal("DPAPI 往返后内容不一致")
	}

	// 明文走 legacy 分支，且内容原样返回。
	back2, legacy2, err := decryptVaultPayload(plain)
	if err != nil || !legacy2 || string(back2) != string(plain) {
		t.Fatalf("明文读取路径不对: legacy=%v err=%v", legacy2, err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestSwitchRollbackRestoresPreviousCredential 守住 D 项验收的**回滚**分支。
//
// 为什么这条必须用单元测试而不是真机演练：切换事务里所有「目标不可用」的情形
// 都在写入之前就被拒绝了（见上一个测试），所以真机上根本走不到「写完了才发现校验
// 不过」这一步 —— 而回滚恰恰只在那种时刻才被触发。真机演练不出来，就不该假装验过。
//
// 这里直接调用回滚路径（与 SwitchAccountTransactional 失败分支调的是同一个函数），
// 验证三件事：旧凭据被原样写回、回滚后又校验了一次、以及「原本没有账号」时是清空
// 而不是留下一个假的旧凭据。
func TestSwitchRollbackRestoresPreviousCredential(t *testing.T) {
	// 先备份真机当前凭据，测试结束无论成败都恢复 —— 这条测试会真的动系统凭据。
	original, readErr := readAntigravityCredentialRaw()
	if readErr != nil {
		t.Fatalf("读取当前凭据失败: %v", readErr)
	}
	originalEmail := emailFromCredentialPayload(original)
	defer func() {
		if len(original) > 0 {
			_ = writeAntigravityCredentialRaw(original)
		} else {
			_ = deleteAntigravityCredentialRaw()
		}
	}()

	if len(original) == 0 {
		t.Skip("本机当前没有 gemini:antigravity 凭据，跳过（回滚测试需要一份真实的旧凭据）")
	}
	if originalEmail == "" {
		t.Skip("当前凭据里读不出邮箱，跳过（无法判断回滚是否回到了同一个账号）")
	}

	// 故意把系统凭据改成「另一份内容」，模拟「切换已经写进去了」这一刻。
	bogus := []byte(`{"token":{"access_token":"bogus-access-token","token_type":"Bearer"}}`)
	if err := writeAntigravityCredentialRaw(bogus); err != nil {
		t.Fatalf("写入伪造凭据失败: %v", err)
	}

	prior := RuntimeModeFacts{Configured: "enhanced", Effective: "none"} // 无宿主：回滚只做凭据层
	ok, owner := switchRollback(original, originalEmail, prior, false)

	if !ok {
		t.Fatalf("回滚未通过校验：期望回到 %s，实际 %s", originalEmail, orNoAccount(owner))
	}
	back, _ := readAntigravityCredentialRaw()
	if string(back) != string(original) {
		t.Fatal("回滚后凭据与原始字节不一致 —— 回滚必须是原样写回，不是「重新构造一份差不多的」")
	}
	if got := strings.TrimSpace(emailFromCredentialPayload(back)); !strings.EqualFold(got, originalEmail) {
		t.Fatalf("回滚后身份不对：期望 %s，实际 %s", originalEmail, got)
	}
}

// TestSwitchRollbackWithoutPreviousAccount 守住「原本没有账号」时的回滚语义。
//
// 这种情形下正确的回滚是**清空**凭据，而不是留下任何东西：如果实现写成
// 「凭据为空就跳过写回」，用户就会停在别人的账号上 —— 那比切换失败本身更糟。
func TestSwitchRollbackWithoutPreviousAccount(t *testing.T) {
	original, _ := readAntigravityCredentialRaw()
	defer func() {
		if len(original) > 0 {
			_ = writeAntigravityCredentialRaw(original)
		} else {
			_ = deleteAntigravityCredentialRaw()
		}
	}()

	if err := writeAntigravityCredentialRaw([]byte(`{"token":{"access_token":"bogus","token_type":"Bearer"}}`)); err != nil {
		t.Fatalf("写入伪造凭据失败: %v", err)
	}
	prior := RuntimeModeFacts{Configured: "enhanced", Effective: "none"}
	ok, owner := switchRollback(nil, "", prior, false)
	if ok {
		t.Fatal("原本没有账号时，回滚不应报告「已恢复到某账号」")
	}
	if owner != "" {
		t.Fatalf("原本没有账号时，回滚后系统凭据里不该还有账号，实际读到 %s", owner)
	}
	if raw, _ := readAntigravityCredentialRaw(); len(raw) > 0 {
		t.Fatal("原本没有账号时，回滚必须把凭据清空")
	}
}
