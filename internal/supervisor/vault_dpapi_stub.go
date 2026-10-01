//go:build !windows

package supervisor

import "errors"

// 非 Windows 平台的占位实现。
//
// DPAPI 是 Windows 独有的用户级加密原语；macOS/Linux 对应的是钥匙串。
// 本工程当前只交付 Windows 宿主托管，所以这里如实返回「不支持」，
// 而不是退回明文写入 —— 那会让「保险库」在别的平台上静默失效。
var errVaultDPAPIUnsupported = errors.New("Account Vault 的 DPAPI 加密仅在 Windows 上实现")

func dpapiProtect(plain []byte) ([]byte, error) { return nil, errVaultDPAPIUnsupported }

func dpapiUnprotect(cipher []byte) ([]byte, error) { return nil, errVaultDPAPIUnsupported }
