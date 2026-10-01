//go:build windows

package supervisor

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 换号写入路径的端到端验证（带还原）。
//
// 这是阶段四唯一能证明「切换真的换了登录态」的测试：写一份**另一个账号**的凭据进去，
// 立刻从系统凭据读回来确认身份真的变了，再把原凭据逐字节还原。
//
// 为什么不直接调 /api/v1/host/switch-and-restart：那条路径会杀掉并重启宿主，
// 会丢掉用户工作台里未保存的会话状态。凭据读写是换号的全部物理实质，
// 验证它即可确认能力成立，宿主重启只是调用它的时序问题（已由 host_launcher 的
// ApplyAntigravityCredential 调用点覆盖）。
//
// 还原是硬约束：测试结束时用户的登录态必须与测试前逐字节相同。
func TestCredentialSwitchRoundTripWithRestore(t *testing.T) {
	original, err := readAntigravityCredentialRaw()
	if err != nil || len(original) == 0 {
		t.Skip("本机没有系统凭据，跳过")
	}
	originalEmail, err := ReadHostLoginEmail()
	if err != nil || originalEmail == "" {
		t.Skip("读不出当前身份，跳过")
	}

	// 挑一个**与当前不同**的真实账号作为切换目标。
	//
	// 候选来自用户自己的账号库，不是写死的两个邮箱 —— 这条测试的意图是
	// 「切到另一个账号再切回来」，具体是哪个账号不承重。写死邮箱会让这条测试
	// 只在原作者机器上跑得起来（而且顺带把两个真实邮箱带进了公开源码）。
	other := ""
	if dir := cockpitDataDir(); dir != "" {
		if data, rerr := os.ReadFile(filepath.Join(dir, "accounts.json")); rerr == nil {
			var caf cockpitAccountsFile
			if json.Unmarshal(data, &caf) == nil {
				for _, a := range caf.Accounts {
					email := strings.TrimSpace(a.Email)
					if email == "" || strings.EqualFold(email, originalEmail) {
						continue
					}
					if _, ok := loadCockpitAccountByEmail(email); ok {
						other = email
						break
					}
				}
			}
		}
	}
	if other == "" {
		t.Skip("找不到第二个带可用凭据的账号，跳过")
	}

	// 还原必须在任何失败路径上都能跑：用 defer 而不是顺序语句。
	restored := false
	defer func() {
		if restored {
			return
		}
		if werr := writeAntigravityCredentialRaw(original); werr != nil {
			t.Fatalf("严重：还原原始凭据失败（%v）—— 用户登录态已被测试改写，请手工恢复", werr)
		}
		after, _ := readAntigravityCredentialRaw()
		if !bytes.Equal(after, original) {
			t.Fatalf("严重：还原后凭据与原始不一致（%d vs %d 字节）", len(after), len(original))
		}
		t.Logf("已在 defer 中还原原始凭据 %d 字节", len(original))
	}()

	// 1) 切到另一个账号。
	if err := ApplyAntigravityCredential(other); err != nil {
		t.Fatalf("切换到 %s 失败: %v", other, err)
	}
	// 2) 必须能从系统凭据里读回新身份 —— 这一步才是「真的换了」的证据。
	got, err := ReadHostLoginEmail()
	if err != nil {
		t.Fatalf("切换后读身份报错: %v", err)
	}
	if !strings.EqualFold(got, other) {
		t.Fatalf("切换后系统凭据身份应为 %s，实际读出 %q ⇒ 凭据未真正改写", other, got)
	}
	t.Logf("切换生效: %s → %s（已从 Windows 凭据管理器读回确认）", originalEmail, got)

	// 3) 切回原账号，同样必须读回。
	if err := ApplyAntigravityCredential(originalEmail); err != nil {
		t.Fatalf("切回 %s 失败: %v", originalEmail, err)
	}
	back, err := ReadHostLoginEmail()
	if err != nil {
		t.Fatalf("切回后读身份报错: %v", err)
	}
	if !strings.EqualFold(back, originalEmail) {
		t.Fatalf("切回后身份应为 %s，实际 %q", originalEmail, back)
	}

	// 4) 逐字节还原并固化还原状态。
	if err := writeAntigravityCredentialRaw(original); err != nil {
		t.Fatalf("还原原始凭据失败: %v", err)
	}
	final, err := readAntigravityCredentialRaw()
	if err != nil {
		t.Fatalf("还原后复读失败: %v", err)
	}
	if !bytes.Equal(final, original) {
		t.Fatalf("还原后凭据与原始不一致（%d vs %d 字节）", len(final), len(original))
	}
	restored = true

	finalEmail, _ := ReadHostLoginEmail()
	if !strings.EqualFold(finalEmail, originalEmail) {
		t.Errorf("还原后身份应为 %s，实际 %q", originalEmail, finalEmail)
	}
	if len(final) != len(original) {
		t.Errorf("还原后凭据长度变化 %d → %d", len(original), len(final))
	}
	t.Logf("测试后凭据已逐字节还原（%d 字节，身份 %s）", len(final), finalEmail)
}
