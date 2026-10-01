//go:build windows

package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 真实登录态读取的诚实性验证。
//
// 这是 P1.5 阶段四的地基：整个「换号」能力都建立在「能从系统凭据读出宿主真实身份」
// 之上。因此这里必须用**真机真实凭据**验证，而不是造一个假 blob 自证 ——
// 造假的测试只能证明代码能解析我自己写的数据，证明不了它认识宿主的凭据。
func TestReadHostLoginEmailFromRealCredential(t *testing.T) {
	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		t.Fatalf("读取 Windows 凭据失败: %v", err)
	}
	if len(raw) == 0 {
		t.Skip("本机没有 gemini:antigravity 凭据（未登录过 Antigravity），跳过")
	}

	// 结构性断言：凭据必须是已知的 JSON 形态，且带着 refresh_token。
	// refresh_token 是换号时唯一无法从别处恢复的东西，缺它则切换必失败。
	var blob antigravityCredentialBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		t.Fatalf("凭据不是合法 JSON（%d 字节）: %v", len(raw), err)
	}
	if strings.TrimSpace(blob.Token.RefreshToken) == "" {
		t.Fatal("凭据缺少 refresh_token —— 宿主不会写出这种形态，说明解析结构错位")
	}
	if !strings.EqualFold(strings.TrimSpace(blob.Token.TokenType), "Bearer") {
		t.Errorf("token_type 期望 Bearer，得到 %q", blob.Token.TokenType)
	}
	t.Logf("凭据 %d 字节: access_token=%d字 refresh_token=%d字 expiry=%q auth_method=%q id_token=%v",
		len(raw), len(blob.Token.AccessToken), len(blob.Token.RefreshToken),
		blob.Token.Expiry, blob.AuthMethod, blob.IDToken != "")

	// 身份必须可解析出来 —— 这是「界面显示的账号来自宿主真实凭据」的前提。
	// 拿不到就是缺陷：宿主明明登录着，我们却不知道是谁。
	email, err := ReadHostLoginEmail()
	if err != nil {
		t.Fatalf("身份解析报错: %v", err)
	}
	if email == "" || !strings.Contains(email, "@") {
		t.Fatalf("身份解析返回空/非法邮箱 %q（凭据存在却读不出归属 ⇒ 阶段四能力失效）", email)
	}
	t.Logf("解析出的宿主真实登录身份: %s", email)

	// 必须能对应到 cockpit 账号库里的一个账号，否则「切到该账号」无从下手。
	if _, ok := loadCockpitAccountByEmail(email); !ok {
		t.Errorf("身份 %s 在 cockpit 账号库里找不到可用凭据（accounts/<id>.json 缺失或解密失败）", email)
	}
}

// 凭据反查归属：即使凭据里没有任何身份字段，也必须靠 refresh_token 精确比对认出来。
//
// 这不是假想场景 —— cockpit-tools 写入系统凭据时**只写 token + auth_method**，
// 既没有 id_token 也没有 email。若反查失效，用它切过去的账号在 2Ag 界面上
// 就会显示成「未知」，配额与身份卡片全部落空。
func TestIdentifyCredentialOwnerByToken(t *testing.T) {
	dir := cockpitDataDir()
	if dir == "" {
		t.Skip("取不到 cockpit 数据目录")
	}
	idx, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Skip("本机无 cockpit 账号库，跳过")
	}
	var parsed cockpitAccountsFile
	if err := json.Unmarshal(idx, &parsed); err != nil {
		t.Fatalf("解析 accounts.json 失败: %v", err)
	}

	checked := 0
	for _, a := range parsed.Accounts {
		acc, ok := loadCockpitAccountByEmail(a.Email)
		if !ok {
			continue // 账号库里存在但没有可用凭据文件的条目（本机有 4 条测试残留）
		}
		email, err := identifyCredentialOwner(acc.Token.RefreshToken, acc.Token.AccessToken)
		if err != nil {
			t.Fatalf("反查 %s 的 refresh_token 报错: %v", a.Email, err)
		}
		if !strings.EqualFold(email, a.Email) {
			t.Errorf("refresh_token 反查错配: 期望 %s 得到 %q", a.Email, email)
		}
		emailByAccess, _ := identifyCredentialOwner("", acc.Token.AccessToken)
		if !strings.EqualFold(emailByAccess, a.Email) {
			t.Errorf("access_token 反查错配: 期望 %s 得到 %q", a.Email, emailByAccess)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("cockpit 账号库里没有任何带可用凭据的账号，跳过")
	}
	t.Logf("已用 %d 个真实账号验证 token 反查归属", checked)

	// 阴性对照：不存在的 token 必须返回空，而不是撞上某个弱匹配。
	ghost, err := identifyCredentialOwner("definitely-not-a-real-refresh-token", "")
	if err != nil {
		t.Fatalf("幽灵 token 不应报错: %v", err)
	}
	if ghost != "" {
		t.Errorf("幽灵 refresh_token 竟然匹配到了账号 %q —— 反查存在误匹配", ghost)
	}
}

// 凭据载荷必须能被自己读回来（写入格式与读取格式自洽），且不携带过期身份字段。
func TestBuildCredentialPayloadRoundTrip(t *testing.T) {
	dir := cockpitDataDir()
	if dir == "" {
		t.Skip("取不到 cockpit 数据目录")
	}
	idxRaw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Skip("本机无 cockpit 账号库，跳过")
	}
	var idx cockpitAccountsFile
	if err := json.Unmarshal(idxRaw, &idx); err != nil {
		t.Fatalf("解析 accounts.json 失败: %v", err)
	}
	var acc cockpitAccountPlain
	found := false
	for _, a := range idx.Accounts {
		if v, ok := loadCockpitAccountByEmail(a.Email); ok {
			acc, found = v, true
			break
		}
	}
	if !found {
		t.Skip("cockpit 账号库里没有带可用凭据的账号，跳过")
	}

	payload, err := BuildAntigravityCredentialPayload(acc)
	if err != nil {
		t.Fatalf("构造载荷失败: %v", err)
	}
	var round antigravityCredentialBlob
	if err := json.Unmarshal(payload, &round); err != nil {
		t.Fatalf("载荷不是合法 JSON: %v", err)
	}
	if round.Token.RefreshToken != acc.Token.RefreshToken {
		t.Error("载荷未带上 refresh_token —— 宿主将无法刷新令牌，等于把用户登出")
	}
	if round.AuthMethod != "consumer" {
		t.Errorf("auth_method 期望 consumer，得到 %q", round.AuthMethod)
	}
	if round.IDToken != "" {
		t.Error("载荷不该带 id_token（cockpit 里存的多半已过期，宿主会自行刷新）")
	}
	// expiry 必须是 UTC Z 结尾：带本地偏移的写法在部分版本上会被宿主判为无效时间。
	if round.Token.Expiry != "" && !strings.HasSuffix(round.Token.Expiry, "Z") {
		t.Errorf("expiry 必须以 Z 结尾（UTC），得到 %q", round.Token.Expiry)
	}
	t.Logf("载荷 %d 字节: expiry=%q refresh_token=%d字",
		len(payload), round.Token.Expiry, len(round.Token.RefreshToken))

	// 缺少 refresh_token 必须被拒绝，而不是写出一个会让宿主登出的空凭据。
	if _, err := BuildAntigravityCredentialPayload(cockpitAccountPlain{Email: "x@y.z"}); err == nil {
		t.Error("没有 refresh_token 的账号必须拒绝构造载荷")
	}
}

// 幂等性：目标与当前身份一致时不得覆写 —— 宿主刷新过的 token 比 cockpit 里的新，
// 用旧值覆盖会让登录态倒退。这里通过「调用前后凭据字节完全一致」来验证。
func TestApplyCredentialIsIdempotentForSameAccount(t *testing.T) {
	raw, err := readAntigravityCredentialRaw()
	if err != nil || len(raw) == 0 {
		t.Skip("本机没有系统凭据，跳过")
	}
	email, err := ReadHostLoginEmail()
	if err != nil || email == "" {
		t.Skip("读不出当前身份，跳过")
	}
	before := append([]byte(nil), raw...)

	if err := ApplyAntigravityCredential(email); err != nil {
		t.Fatalf("对同一账号调用应成功并跳过写入，却报错: %v", err)
	}
	after, err := readAntigravityCredentialRaw()
	if err != nil {
		t.Fatalf("复读凭据失败: %v", err)
	}
	if string(before) != string(after) {
		t.Errorf("对同一账号调用竟然改写了凭据（%d → %d 字节）—— 宿主刷新的 token 被旧值覆盖", len(before), len(after))
	}

	// 空邮箱必须被拒绝：静默接受会让调用方以为切换成功。
	if err := ApplyAntigravityCredential("   "); err == nil {
		t.Error("空邮箱必须被拒绝")
	}
}
