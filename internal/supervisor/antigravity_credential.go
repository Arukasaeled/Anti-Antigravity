//go:build windows

package supervisor

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ============================================================================
// Antigravity 真实登录态（Windows 凭据管理器）
//
// 背景（真机实证，见 scripts/audit/probe_real_identity.js 与 P1.5 报告）：
//   Antigravity 2.x 的登录凭据**不是** profile 目录里的文件，而是 Windows 凭据管理器里
//   一条 generic credential：
//       TargetName = "gemini:antigravity"
//       UserName   = "antigravity"
//       Persist    = CRED_PERSIST_LOCAL_MACHINE(2) ⇒ 机器级，所有 --user-data-dir 共享
//   blob 是 JSON：{"token":{"access_token","token_type","refresh_token","expiry"},
//                  "auth_method":"consumer"}，宿主登录后还会补写 "id_token"。
//
// 由此得出一个决定性结论：
//   **只改 --user-data-dir 永远不可能改变原生登录身份。** 沙箱只隔离 cookie、
//   localStorage、缓存等 Chromium 侧状态；登录身份来自那条机器级凭据。
//   这正是历史实现「传了新沙箱、界面仍是旧账号」的物理根因。
//
// 因此真实换号的顺序必须是：先杀掉宿主（释放凭据与文件锁）→ 原子重写该凭据 →
// 再用目标账号的沙箱拉起宿主。本文件负责中间那一步。
// ============================================================================

const (
	antigravityCredTarget   = "gemini:antigravity"
	antigravityCredUser     = "antigravity"
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	win32ErrorNotFound      = 1168
)

var (
	advapi32        = syscall.NewLazyDLL("advapi32.dll")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// credFiletime 对应 Win32 FILETIME（两个 DWORD，不是 64 位整数）。
type credFiletime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

// credentialW 必须与 Win32 CREDENTIALW 的内存布局逐字节一致。
//
// x64 下 CREDENTIALW 的字段偏移是 0/4/8/16/24/32/(pad 36→40)/40/48/52/56/64/72，
// 结构体总长 80 字节。Go 的默认对齐规则恰好给出同一组偏移（uint32 后接指针会自动
// 补 4 字节），所以这里不需要手工 padding —— 但字段顺序与类型一个都不能错，
// 错一个字段就会读到错位的 CredentialBlob 指针。
type credentialW struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        credFiletime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         *byte
	TargetAlias        *uint16
	UserName           *uint16
}

// readAntigravityCredentialRaw 读取系统凭据的原始 blob。
// 凭据不存在时返回 (nil, nil) —— 「没有凭据」是一种合法状态，不是错误。
func readAntigravityCredentialRaw() ([]byte, error) {
	target, err := syscall.UTF16PtrFromString(antigravityCredTarget)
	if err != nil {
		return nil, fmt.Errorf("凭据目标名编码失败: %w", err)
	}
	// 用 unsafe.Pointer 变量而不是 uintptr 接住返回的指针。
	// Go 的规则是「uintptr 不是引用」：它随时可能被 GC 当成普通整数，
	// 而 unsafe.Pointer 会真正持有引用；同时这也避开了 go vet 对
	// uintptr→unsafe.Pointer 转换的告警（那是合法的，但会淹没其它真问题）。
	var pcred unsafe.Pointer
	r1, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(credTypeGeneric),
		0,
		uintptr(unsafe.Pointer(&pcred)),
	)
	if r1 == 0 {
		if callErr == syscall.Errno(win32ErrorNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("CredReadW(%s) 失败: %v", antigravityCredTarget, callErr)
	}
	// CredRead 分配的内存必须由 CredFree 释放，否则每次读取都泄漏一块。
	defer procCredFree.Call(uintptr(pcred))

	cred := (*credentialW)(pcred)
	if cred.CredentialBlobSize == 0 || cred.CredentialBlob == nil {
		return nil, nil
	}
	blob := unsafe.Slice(cred.CredentialBlob, cred.CredentialBlobSize)
	out := make([]byte, len(blob))
	copy(out, blob)
	return out, nil
}

// writeAntigravityCredentialRaw 原子替换系统凭据。
//
// 顺序刻意与 cockpit-tools 不同：先直接 CredWriteW 覆盖（同 target+type 的写入是
// 一次替换，不存在「已删除但未写入」的空窗）；只有在覆盖失败时才退回
// 「CredDeleteW 忽略错误 → CredWriteW 重试」这条 cockpit-tools 的既定路径。
// 目的是让最常见的情形只经过一次真正的凭据写操作。
func writeAntigravityCredentialRaw(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("拒绝写入空凭据 blob")
	}
	target, err := syscall.UTF16PtrFromString(antigravityCredTarget)
	if err != nil {
		return fmt.Errorf("凭据目标名编码失败: %w", err)
	}
	user, err := syscall.UTF16PtrFromString(antigravityCredUser)
	if err != nil {
		return fmt.Errorf("凭据用户名编码失败: %w", err)
	}
	blob := payload // 必须活到 CredWriteW 返回之后
	cred := credentialW{
		Type:               credTypeGeneric,
		TargetName:         target,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}

	r1, _, firstErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r1 == 0 {
		// 覆盖失败（常见于目标已被其他会话改写）：走 delete + write。
		procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
		r1, _, secondErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
		if r1 == 0 {
			return fmt.Errorf("写入 Windows 凭据管理器失败: 覆盖=%v 删除重写=%v", firstErr, secondErr)
		}
		log.Printf("[2ag] 凭据覆盖失败(%v)，已改用 delete+write 路径成功写入", firstErr)
	}
	runtime.KeepAlive(blob)
	return nil
}

// ---------------------------------------------------------------------------
// 凭据 blob 结构
// ---------------------------------------------------------------------------

type antigravityCredentialBlob struct {
	Token struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		RefreshToken string `json:"refresh_token"`
		Expiry       string `json:"expiry"`
	} `json:"token"`
	AuthMethod string `json:"auth_method"`
	IDToken    string `json:"id_token"`
	// 宿主在某些版本里会把 email 平铺在顶层；cockpit-tools 的写入格式两者都没有。
	Email string `json:"email"`
}

// decodeJWTPayload 已移到 credential_parse.go（与平台无关），
// 因为 Login Broker 的判定逻辑也要用它，而那一份必须跨平台可编译。

// ---------------------------------------------------------------------------
// cockpit 账号库（.antigravity_cockpit）
//
// 2Ag 不自己维护账号凭据，而是复用用户已有的 cockpit 账号库 —— 那里是**唯一**
// 存有 refresh_token 的地方。账号详情文件是整文件 AES-256-GCM 加密信封，
// 密钥在同目录 secure-account-storage.key（base64，32 字节）。
// ---------------------------------------------------------------------------

const cockpitKeyFile = "secure-account-storage.key"

type cockpitEnvelope struct {
	Version    int    `json:"version"`
	Kind       string `json:"kind"`
	Algorithm  string `json:"algorithm"`
	KeyID      string `json:"key_id"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type cockpitAccountToken struct {
	AccessToken     string `json:"access_token"`
	RefreshToken    string `json:"refresh_token"`
	ExpiresIn       int64  `json:"expires_in"`
	ExpiryTimestamp int64  `json:"expiry_timestamp"`
	TokenType       string `json:"token_type"`
	Email           string `json:"email"`
	OAuthClientKey  string `json:"oauth_client_key"`
	IDToken         string `json:"id_token"`
	SessionID       string `json:"session_id"`
}

type cockpitAccountPlain struct {
	ID    string              `json:"id"`
	Email string              `json:"email"`
	Name  string              `json:"name"`
	Token cockpitAccountToken `json:"token"`
}

// cockpitDataDir 返回 cockpit 账号库根目录。
// 优先级与 cockpit-tools 的 config.rs 一致：环境变量 COCKPIT_TOOLS_DATA_DIR 优先，
// 否则 ~/.antigravity_cockpit（部分用户会把它做成指向别处的 Junction，路径本身不重要）。
func cockpitDataDir() string {
	if v := strings.TrimSpace(os.Getenv("COCKPIT_TOOLS_DATA_DIR")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".antigravity_cockpit")
}

// loadCockpitAccountByEmail 按邮箱（大小写不敏感）从 cockpit 账号库里取出一条完整账号
// （含已解密的 token）。找不到时第二个返回值为 false，绝不返回半个对象。
func loadCockpitAccountByEmail(email string) (cockpitAccountPlain, bool) {
	var zero cockpitAccountPlain
	email = strings.TrimSpace(email)
	if email == "" {
		return zero, false
	}
	dir := cockpitDataDir()
	if dir == "" {
		return zero, false
	}

	idxRaw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		log.Printf("[2ag] 读取 cockpit 账号索引失败: %v", err)
		return zero, false
	}
	var idx cockpitAccountsFile
	if err := json.Unmarshal(idxRaw, &idx); err != nil {
		log.Printf("[2ag] 解析 cockpit 账号索引失败: %v", err)
		return zero, false
	}

	accountID := ""
	for _, a := range idx.Accounts {
		if strings.EqualFold(strings.TrimSpace(a.Email), email) {
			accountID = a.ID
			break
		}
	}
	if accountID == "" {
		return zero, false
	}

	filePath := filepath.Join(dir, "accounts", accountID+".json")
	raw, err := os.ReadFile(filePath)
	if err != nil {
		// 账号索引里有、凭据文件却没有，是账号库的常态（用户在 cockpit 里删过账号，
		// 或条目只是历史残留）。这不是错误，只是「这个账号没有可用凭据」。
		// 必须区分对待：真正读取失败（权限、坏了）要留痕，文件不存在则静默跳过 ——
		// 否则身份反查每轮都要为每条无凭据账号打一遍日志，把真问题淹掉。
		if !os.IsNotExist(err) {
			log.Printf("[2ag] 读取 cockpit 账号详情失败 (%s): %v", accountID, err)
		}
		return zero, false
	}

	// 两种格式并存：加密信封（当前）与明文 JSON（旧格式，cockpit-tools 亦兼容）。
	var plain cockpitAccountPlain
	if json.Unmarshal(raw, &plain) == nil && plain.Token.RefreshToken != "" {
		return plain, true
	}

	var env cockpitEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("[2ag] 解析 cockpit 账号信封失败 (%s): %v", accountID, err)
		return zero, false
	}
	if !strings.EqualFold(env.Algorithm, "AES-256-GCM") {
		log.Printf("[2ag] cockpit 账号信封算法不受支持: %q", env.Algorithm)
		return zero, false
	}
	keyRaw, err := os.ReadFile(filepath.Join(dir, cockpitKeyFile))
	if err != nil {
		log.Printf("[2ag] 读取 cockpit 主密钥失败: %v", err)
		return zero, false
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyRaw)))
	if err != nil || len(key) != 32 {
		log.Printf("[2ag] cockpit 主密钥不是合法的 32 字节 base64")
		return zero, false
	}
	decrypted, err := decryptCockpitEnvelope(env, key)
	if err != nil {
		log.Printf("[2ag] 解密 cockpit 账号失败 (%s): %v", accountID, err)
		return zero, false
	}
	if err := json.Unmarshal(decrypted, &plain); err != nil {
		log.Printf("[2ag] 解析 cockpit 账号明文失败 (%s): %v", accountID, err)
		return zero, false
	}
	return plain, true
}

// decryptCockpitEnvelope 解 AES-256-GCM 信封。
// Go 的 crypto/cipher GCM 约定密文为 body||tag（最后 16 字节是认证标签），
// 与 cookie 端的信封格式天然一致，无需手工切分。
func decryptCockpitEnvelope(env cockpitEnvelope, key []byte) ([]byte, error) {
	nonce, err := base64.StdEncoding.DecodeString(strings.TrimSpace(env.Nonce))
	if err != nil {
		return nil, fmt.Errorf("nonce base64 解码失败: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(env.Ciphertext))
	if err != nil {
		return nil, fmt.Errorf("ciphertext base64 解码失败: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("构造 AES cipher 失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("构造 GCM 失败: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("nonce 长度非法: 期望 %d 得到 %d", gcm.NonceSize(), len(nonce))
	}
	if len(ct) < gcm.Overhead() {
		return nil, fmt.Errorf("密文长度非法: %d", len(ct))
	}
	return gcm.Open(nil, nonce, ct, nil)
}

// BuildAntigravityCredentialPayload 由 cockpit 账号构造系统凭据 blob。
//
// 结构对齐 cockpit-tools 的 build_antigravity_credential_payload —— 只写
// token{access_token, token_type, refresh_token, expiry} + auth_method，
// **刻意不写 id_token**：cockpit 里存的 id_token 往往已过期，而宿主在拿到
// refresh_token 后会自行刷新并补写自己的 id_token。身份识别因此不依赖这里，
// 由 identifyCredentialOwner 用 refresh_token 精确比对得出（见下）。
func BuildAntigravityCredentialPayload(acc cockpitAccountPlain) ([]byte, error) {
	if strings.TrimSpace(acc.Token.RefreshToken) == "" {
		return nil, fmt.Errorf("账号 %s 的凭据缺少 refresh_token，无法切换", acc.Email)
	}
	tokenType := strings.TrimSpace(acc.Token.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	expiry := ""
	if acc.Token.ExpiryTimestamp > 0 {
		// cockpit-tools 用 SecondsFormat::Micros + 强制 Z：形如
		// 2026-09-30T14:36:46.000000Z。宿主自身写回的是带本地偏移的形式，
		// 两种都被接受，这里对齐 cockpit-tools 这支已被验证可用的写法。
		expiry = time.Unix(acc.Token.ExpiryTimestamp, 0).UTC().Format("2006-01-02T15:04:05.000000Z")
	}
	payload := map[string]any{
		"token": map[string]any{
			"access_token":  acc.Token.AccessToken,
			"token_type":    tokenType,
			"refresh_token": acc.Token.RefreshToken,
			"expiry":        expiry,
		},
		"auth_method": "consumer",
	}
	return json.Marshal(payload)
}

// ---------------------------------------------------------------------------
// 对外能力
// ---------------------------------------------------------------------------

// ReadHostLoginEmail 读取宿主**真实登录身份**。
//
// 解析顺序（前一步拿不到才退下一步）：
//  1. 凭据 blob 里的 id_token —— 宿主登录/刷新后会补写，是最直接的答案；
//  2. 平铺的 email 字段（宿主某些版本会写）；
//  3. 用 refresh_token / access_token 与 cockpit 账号库逐条精确比对 —— token 字符串
//     是每个账号唯一的，命中即等价于「这条凭据属于该账号」。这一条保证了即使凭据里
//     没有任何身份字段（cockpit-tools 写入的就是这种形态），身份依然可读。
//
// 读不到凭据、或凭据里没有任何可归属信息时返回 ("", nil)：这是「未知」，不是错误，
// 调用方必须按未知处理，绝不允许拿别的来源顶上。
func ReadHostLoginEmail() (string, error) {
	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", nil
	}
	var blob antigravityCredentialBlob
	if err := json.Unmarshal(raw, &blob); err != nil {
		return "", fmt.Errorf("解析凭据 blob 失败: %w", err)
	}
	if payload := decodeJWTPayload(blob.IDToken); payload != nil {
		if email, ok := payload["email"].(string); ok && strings.Contains(email, "@") {
			return strings.TrimSpace(email), nil
		}
	}
	if e := strings.TrimSpace(blob.Email); strings.Contains(e, "@") {
		return e, nil
	}
	if e := strings.TrimSpace(blob.Token.AccessToken); e == "" && strings.TrimSpace(blob.Token.RefreshToken) == "" {
		return "", nil
	}
	return identifyCredentialOwner(blob.Token.RefreshToken, blob.Token.AccessToken)
}

// identifyCredentialOwner 用 token 字符串反查这份凭据属于谁。
//
// 先查 2Ag 自己的保险库，再查第三方 cockpit 账号库，逐条解密做精确字符串比对；
// 找不到归属返回 ("", nil)。
//
// 保险库必须排在前面：它是 2Ag 自己的账号库，而 cockpit 只是可选的历史来源 ——
// 一个没装过 cockpit-tools 的用户，他的凭据只可能在保险库里。
// 顺序反了就意味着一份凭据「认不出主人」，而认不出主人会直接导致归档被拒。
func identifyCredentialOwner(refreshToken, accessToken string) (string, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	accessToken = strings.TrimSpace(accessToken)
	if refreshToken == "" && accessToken == "" {
		return "", nil
	}

	matches := func(raw []byte) bool {
		var blob antigravityCredentialBlob
		if json.Unmarshal(raw, &blob) != nil {
			return false
		}
		if refreshToken != "" && strings.TrimSpace(blob.Token.RefreshToken) == refreshToken {
			return true
		}
		return accessToken != "" && strings.TrimSpace(blob.Token.AccessToken) == accessToken
	}

	// ① 2Ag 自有保险库。
	for _, e := range ListVaultAccountEntries() {
		raw, err := ReadVaultCredential(e.Email)
		if err != nil || len(raw) == 0 {
			continue
		}
		if matches(raw) {
			return strings.TrimSpace(e.Email), nil
		}
	}

	// ② 第三方 cockpit 账号库（可选的历史来源）。
	dir := cockpitDataDir()
	if dir == "" {
		return "", nil
	}
	idxRaw, err := os.ReadFile(filepath.Join(dir, "accounts.json"))
	if err != nil {
		return "", nil
	}
	var idx cockpitAccountsFile
	if err := json.Unmarshal(idxRaw, &idx); err != nil {
		return "", nil
	}
	for _, a := range idx.Accounts {
		acc, ok := loadCockpitAccountByEmail(a.Email)
		if !ok {
			continue
		}
		if refreshToken != "" && acc.Token.RefreshToken == refreshToken {
			return strings.TrimSpace(acc.Email), nil
		}
		if accessToken != "" && acc.Token.AccessToken == accessToken {
			return strings.TrimSpace(acc.Email), nil
		}
	}
	return "", nil
}

// ReadHostLoginEmailCached 带短缓存的读取，供 HTTP 状态接口高频调用。
// 缓存 5 秒：CredRead 本身很快，但 identifyCredentialOwner 会解密账号文件，
// 不该被每次面板轮询触发。
var (
	hostLoginCacheMu   = make(chan struct{}, 1)
	hostLoginCachedVal string
	hostLoginCachedAt  time.Time
)

func ReadHostLoginEmailCached() (string, error) {
	const ttl = 5 * time.Second
	hostLoginCacheMu <- struct{}{}
	if time.Since(hostLoginCachedAt) < ttl {
		v := hostLoginCachedVal
		<-hostLoginCacheMu
		return v, nil
	}
	<-hostLoginCacheMu

	email, err := ReadHostLoginEmail()

	hostLoginCacheMu <- struct{}{}
	if err == nil {
		hostLoginCachedVal = email
		hostLoginCachedAt = time.Now()
	}
	<-hostLoginCacheMu
	return email, err
}

// invalidateHostLoginCache 立刻作废身份缓存。
//
// 每一次对系统凭据的写/删都必须跟一次它：缓存 5 秒是给面板轮询用的，
// 而「我刚刚删掉了凭据 / 刚写入了新账号」这种时刻，读到 5 秒前的旧账号
// 就是事实错误 —— 登录 Broker 的轮询正建立在这个前提上。
func invalidateHostLoginCache() {
	hostLoginCacheMu <- struct{}{}
	hostLoginCachedVal = ""
	hostLoginCachedAt = time.Time{}
	<-hostLoginCacheMu
}

// deleteAntigravityCredentialRaw 删除系统凭据（原本就不存在时视为成功）。
//
// 这是「添加账号」流程里唯一会**主动清空**登录态的动作，因此刻意做得保守：
// 只删那一条 target=gemini:antigravity 的 machine 级凭据，不碰别的任何凭据。
func deleteAntigravityCredentialRaw() error {
	target, err := syscall.UTF16PtrFromString(antigravityCredTarget)
	if err != nil {
		return fmt.Errorf("凭据目标名编码失败: %w", err)
	}
	r1, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(credTypeGeneric), 0)
	invalidateHostLoginCache()
	if r1 == 0 && callErr != syscall.Errno(win32ErrorNotFound) {
		return fmt.Errorf("CredDeleteW(%s) 失败: %v", antigravityCredTarget, callErr)
	}
	return nil
}

// credentialPayloadForAccount 取出一份可用于写入系统凭据管理器的凭据 blob。
//
// 顺序即优先级，且**第一来源不依赖任何第三方工具**：
//  1. 2Ag 自己的 DPAPI 保险库（account_vault.go）。用户在 2Ag 里加过的每个账号
//     都在这里，保险库保存的就是凭据管理器原始 blob，可直接回写；
//  2. 第三方 cockpit 账号库 —— 只服务于「0.1.1 之前就把账号放在那里」的老用户。
//
// 返回的 source 只用于日志（"vault" / "cockpit"），便于排障时看清凭据从哪来。
//
// 为什么不能只保留第 2 项（历史实现）：那会让「切换账号」以「本机装过 cockpit-tools」
// 为前提，报错文案本身就是「cockpit 账号库里没有…」。一个发布版产品的核心动作
// 不该有这样的前提。
func credentialPayloadForAccount(email string) (payload []byte, source string, err error) {
	email = strings.TrimSpace(email)

	// ① 2Ag 自有保险库。
	if raw, err := ReadVaultCredential(email); err == nil && len(raw) > 0 {
		var blob antigravityCredentialBlob
		if json.Unmarshal(raw, &blob) == nil && strings.TrimSpace(blob.Token.RefreshToken) != "" {
			return raw, "vault", nil
		}
		// 保险库里那一份不足以完成登录（缺 refresh_token）：
		// 继续往下找一个真的可用的来源，而不是把半份凭据写进去。
		log.Printf("[2ag] 保险库里 %s 的凭据缺少 refresh_token，尝试其他来源", email)
	}
	// 保险库里没有该账号是**正常路径**（例如账号只登记在旧库里），
	// 不在此处打日志，避免给每一次切换刷出误导性的「失败」。

	// ② 第三方 cockpit 账号库（可选的历史来源）。
	if acc, ok := loadCockpitAccountByEmail(email); ok {
		built, err := BuildAntigravityCredentialPayload(acc)
		if err != nil {
			return nil, "", err
		}
		return built, "cockpit", nil
	}

	return nil, "", fmt.Errorf("本机没有 %s 的可用登录凭据：请先在「账号矩阵」里用该账号登录一次（登录后凭据会存进 2Ag 自己的保险库）", email)
}

// ApplyAntigravityCredential 把指定账号写进 Windows 凭据管理器，使其成为宿主真实登录身份。
//
// 幂等：若当前凭据已经属于该账号，直接返回 nil —— 这既是省一次写，更重要的是
// **避免用可能更旧的 token 覆盖宿主刚刚刷新过的凭据**。宿主刷新后凭据里会带上
// 自己的 id_token，把它换成旧值只会让登录态倒退。
func ApplyAntigravityCredential(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("切换凭据需要明确的目标邮箱")
	}

	if current, err := ReadHostLoginEmail(); err == nil && current != "" && strings.EqualFold(current, email) {
		log.Printf("[2ag] 系统凭据已是目标账号 %s，跳过写入（保留宿主刷新过的 token）", email)
		return nil
	}

	payload, source, err := credentialPayloadForAccount(email)
	if err != nil {
		return err
	}
	if err := writeAntigravityCredentialRaw(payload); err != nil {
		return err
	}
	log.Printf("[2ag] 已重写 Windows 凭据管理器 target=%s ⇒ %s (来源 %s, blob %d B)",
		antigravityCredTarget, email, source, len(payload))

	// 写入即失效身份缓存：下一次读取必须反映新事实，而不是 5 秒前的旧账号。
	invalidateHostLoginCache()
	return nil
}

// AntigravityCredentialPresent 报告系统凭据是否存在，供诊断接口使用。
func AntigravityCredentialPresent() bool {
	raw, err := readAntigravityCredentialRaw()
	return err == nil && len(raw) > 0
}

// CredentialSnapshot 是一次凭据归档的结果。零值不是合法的成功结果：
// Email 为空意味着「无法确定这份凭据属于谁」，此时调用方必须把归档判为失败
// —— 用哈希文件名把一份无主凭据悄悄存起来，日后无人能认出它是谁的。
type CredentialSnapshot struct {
	Email string // 归档凭据所属邮箱（从 id_token 的 JWT payload 解析）
	Path  string // 落盘绝对路径 ~/.2ag/vault/<email_hash>.bin
	Bytes int    // 实际写入的字节数
}

// SnapshotAntigravityCredential 把当前 Windows 凭据管理器里 gemini:antigravity 的
// 真实 blob 归档到 ~/.2ag/vault/<email_hash>.bin —— **DPAPI(CurrentUser) 加密后**。
//
// 为什么需要它：宿主换号（ApplyAntigravityCredential）会用账号库里的 token
// 覆盖系统凭据，而宿主自己刷新过的 token 只存在于凭据管理器里 —— 一旦被覆盖就再也拿不回来。
// 归档是那条唯一能把「当前真实登录态」留存的路径。
//
// 四处刻意的诚实/安全设计：
//  1. 原样归档。不解析、不重编码、不裁剪 —— 这个文件的用途就是「将来能原封不动写回去」，
//     任何加工都会引入「归档的和当时的不一样」的风险。（加密不算加工：加解密是恒等映射。）
//  2. 归属必须真读出来。邮箱来自 id_token 的 JWT payload（宿主登录后补写的字段），
//     读不到就返回错误并拒绝落盘，绝不用 active_account.txt 之类的旁证顶替。
//  3. 文件名是 email 的 SHA-256 全值（64 位十六进制）。用哈希而非清洗后的邮箱，
//     是为了让「同一邮箱永远同一个文件」且不受大小写/字符集差异影响；
//     代价是不可逆，因此同时在索引文件里留下明文映射（索引只存元数据）。
//  4. 落盘内容一定是 DPAPI 密文。0.1.1 之前这里是明文 WriteFile ——
//     任何能读用户主目录的进程就等于拿到了别人的 refresh_token。
func SnapshotAntigravityCredential() (CredentialSnapshot, error) {
	release, err := lockCredentialOperation()
	if err != nil {
		return CredentialSnapshot{}, err
	}
	defer release()
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		return CredentialSnapshot{}, fmt.Errorf("请重启 2Ag 先恢复原账号")
	}
	return snapshotAntigravityCredential()
}

func snapshotAntigravityCredential() (CredentialSnapshot, error) {
	var out CredentialSnapshot

	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		return out, fmt.Errorf("读取系统凭据失败: %w", err)
	}
	if len(raw) == 0 {
		return out, fmt.Errorf("Windows 凭据管理器中没有 %s 凭据，宿主可能尚未登录", antigravityCredTarget)
	}

	view, ok := parseCredentialBlobView(raw)
	if !ok || view.Token == nil || strings.TrimSpace(view.Token.AccessToken) == "" || strings.TrimSpace(view.Token.RefreshToken) == "" {
		return out, fmt.Errorf("官方尚未完成登录，当前凭据不完整；请在官方客户端完成登录后重试")
	}
	email := emailFromCredentialPayload(raw)
	if email == "" {
		email, err = identifyCredentialOwner(view.Token.RefreshToken, view.Token.AccessToken)
	}
	if err != nil {
		return out, fmt.Errorf("解析凭据归属失败")
	}
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return out, fmt.Errorf("无法从凭据中确定所属邮箱（id_token 缺少 email 字段），拒绝写入无名归档")
	}

	account, err := StoreVaultCredential(email, displayNameFromCredentialPayload(raw), raw)
	if err != nil {
		return out, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return out, fmt.Errorf("定位用户目录失败: %w", err)
	}
	out.Email = email
	out.Path = filepath.Join(home, ".2ag", "vault", account.File)
	out.Bytes = len(raw)
	if err := SaveOwnedAccountEntry(email, account.DisplayName); err != nil {
		return out, fmt.Errorf("凭据已保存，账号登记失败；请重试导入")
	}
	log.Printf("[2ag] 已归档凭据快照（DPAPI 加密）: %s (%d B) ⇒ %s", email, len(raw), out.Path)
	return out, nil
}
