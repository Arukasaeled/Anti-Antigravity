//go:build windows

package supervisor

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// ============================================================================
// Windows DPAPI（CurrentUser 作用域）的最小封装
//
// 为什么必须用它：Account Vault 里存的是**完整登录凭据**（access_token /
// refresh_token / id_token）。0.1.1 之前这些 .bin 是明文写的 —— 任何能读到
// 用户主目录的进程（包括另一个普通用户会话之外的备份/同步工具）都等于拿到了
// 账号。DPAPI 把解密能力绑定到**当前 Windows 用户**的登录凭据上：文件被复制到
// 别的机器或别的用户下都解不开，这正是本机账号保管需要的性质。
//
// 刻意不传 pOptionalEntropy：熵能再加一层，但也会让「换机器后无法恢复」变成
// 必然，而 vault 的语义是「本机保管」。作用域取默认（CurrentUser），
// 与凭据管理器里 gemini:antigravity 的机器级存储相比是更紧的一档。
// ============================================================================

// vaultDataBlob 对应 Win32 DATA_BLOB：一个长度 + 一个裸指针。
type vaultDataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	kernel32Vault          = syscall.NewLazyDLL("kernel32.dll")
	procLocalFreeVault     = kernel32Vault.NewProc("LocalFree")
)

// CRYPTPROTECT_UI_FORBIDDEN：失败就失败，不弹任何系统对话框。
// 缺了它，加密失败会在用户桌面上冒出一个窗口 —— 而调用方是后台协程。
const cryptprotectUIForbidden = 0x01

// dpapiProtect 用当前用户的 DPAPI 主密钥加密。
func dpapiProtect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, fmt.Errorf("DPAPI 加密：输入为空")
	}
	in := vaultDataBlob{cbData: uint32(len(plain)), pbData: &plain[0]}
	var out vaultDataBlob
	r1, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // ppszDataDescr：不需要人类可读描述
		0, // pOptionalEntropy：见文件头注释
		0, // pvReserved
		0, // pPromptStruct
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(plain) // 必须活到调用返回之后
	if r1 == 0 {
		return nil, fmt.Errorf("CryptProtectData 失败: %v", callErr)
	}
	defer procLocalFreeVault.Call(uintptr(unsafe.Pointer(out.pbData)))
	if out.pbData == nil || out.cbData == 0 {
		return nil, fmt.Errorf("CryptProtectData 返回了空结果")
	}
	// 复制一份自己的内存：out.pbData 由 LocalFree 释放，不能直接引用。
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}

// dpapiUnprotect 解开本机当前用户加密过的内容。
func dpapiUnprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, fmt.Errorf("DPAPI 解密：输入为空")
	}
	in := vaultDataBlob{cbData: uint32(len(cipher)), pbData: &cipher[0]}
	var out vaultDataBlob
	r1, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // ppszDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		uintptr(cryptprotectUIForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(cipher)
	if r1 == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 失败（该文件可能由别的用户或别的机器加密）: %v", callErr)
	}
	defer procLocalFreeVault.Call(uintptr(unsafe.Pointer(out.pbData)))
	if out.pbData == nil || out.cbData == 0 {
		return nil, fmt.Errorf("CryptUnprotectData 返回了空结果")
	}
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}
