//go:build !windows

package supervisor

import "errors"

// 非 Windows 平台的占位实现。
//
// Antigravity 真实登录态在 Windows 上是「凭据管理器里的 gemini:antigravity」，
// 在 macOS/Linux 上是系统钥匙串（另有一套写入方式）。本工程当前只交付 Windows 宿主托管，
// 因此这里如实返回「不支持」，而不是伪造一个必然失败的实现。
//
// 关键：ReadHostLoginEmail 返回空串 + nil，语义是「未知」而非「错误」——
// 上层因此会显示「未检测到真实身份」，而不会拿别的来源顶替。

var errCredentialUnsupported = errors.New("Antigravity 凭据读写仅在 Windows 上实现")

func ReadHostLoginEmail() (string, error) { return "", nil }

func ReadHostLoginEmailCached() (string, error) { return "", nil }

func ApplyAntigravityCredential(email string) error { return errCredentialUnsupported }

func AntigravityCredentialPresent() bool { return false }

// CredentialSnapshot 与 Windows 版保持同一定义，否则调用方（internal/api）在
// 非 Windows 上无法编译。字段语义一致：Email 为空即视为未成功归档。
type CredentialSnapshot struct {
	Email string
	Path  string
	Bytes int
}

func SnapshotAntigravityCredential() (CredentialSnapshot, error) {
	return CredentialSnapshot{}, errCredentialUnsupported
}
