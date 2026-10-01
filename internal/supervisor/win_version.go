//go:build windows

package supervisor

import (
	"strings"
	"syscall"
	"unsafe"
)

// ============================================================================
// 从 PE 版本资源里读版本号（Update Guardian 的读数来源）
//
// 为什么不用其它办法：
//   - 解析 asar 里的 package.json 不可靠 —— 实测用正则扫官方 asar header
//     零命中（asar 头部是二进制 JSON，字段顺序与压缩方式都随打包器版本变）。
//   - 读日志里的 "Starting app (v2.18.1)" 是旁证，不是事实：日志会被轮转、
//     会缺失，而且它描述的是「上次启动时是什么版本」，不是「现在磁盘上是什么版本」。
//
// exe 的版本资源是 Windows 自己维护的、唯一权威的、离线的版本来源，
// 一次 P/Invoke 就能拿到。
// ============================================================================

var (
	versionDLL                     = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSizeW    = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW        = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValueW             = versionDLL.NewProc("VerQueryValueW")
)

// vsFixedFileInfo 是 VS_FIXEDFILEINFO 结构里本工程需要的字段。
//
// 刻意不用完整的 13 字段结构体去接：VerQueryValueW 返回的是指向资源缓冲区的
// 指针，把整个结构体原样拷一遍除了多一处会随 SDK 版本漂移的定义以外没有任何
// 收益。这里只取 4 个版本号字段 —— 它们的位置（偏移 8/12/16/20）在
// VS_FIXEDFILEINFO 里是稳定的：dwSignature(0) dwStrucVersion(4) 之后
// 就是 dwFileVersionMS(8)。
type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
}

// fileProductVersion 读出 exe 的 ProductVersion 字符串（形如 "2.18.1"）。
//
// 失败时返回空串而不是编造的占位值：调用方必须能区分「版本是 2.18.1」
// 与「读不出来」。Update Guardian 靠这个区分来决定要不要提示用户。
func fileProductVersion(exePath string) string {
	if strings.TrimSpace(exePath) == "" {
		return ""
	}
	pathPtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return ""
	}

	var handle uint32
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(pathPtr)), uintptr(unsafe.Pointer(&handle)))
	if size == 0 {
		return ""
	}

	buf := make([]byte, size)
	ret, _, _ := procGetFileVersionInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(size),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if ret == 0 {
		return ""
	}

	if v := queryVersionString(buf, "\\StringFileInfo\\040904b0\\ProductVersion"); v != "" {
		return v
	}
	// 0x0409 是 en-US。非英文资源块（例如本地化构建）里这个子块可能不存在，
	// 此时退回翻译无关的固定字段：ProductVersionMS/LS 是纯数字，任何语言都有。
	return fixedFileVersion(buf)
}

// queryVersionString 按 \\StringFileInfo\\<langcp>\\<name> 查询字符串。
func queryVersionString(buf []byte, key string) string {
	keyPtr, err := syscall.UTF16PtrFromString(key)
	if err != nil {
		return ""
	}
	var valuePtr unsafe.Pointer
	var valueLen uint32
	ret, _, _ := procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(keyPtr)),
		uintptr(unsafe.Pointer(&valuePtr)),
		uintptr(unsafe.Pointer(&valueLen)),
	)
	if ret == 0 || valuePtr == nil || valueLen == 0 {
		return ""
	}
	// valueLen 是字符数（含结尾 NUL），不是字节数。
	return strings.TrimSpace(syscall.UTF16ToString(unsafe.Slice((*uint16)(valuePtr), valueLen)))
}

// fixedFileVersion 从 VS_FIXEDFILEINFO 拼出 "a.b.c.d"。
func fixedFileVersion(buf []byte) string {
	var valuePtr unsafe.Pointer
	var valueLen uint32
	keyPtr, err := syscall.UTF16PtrFromString("\\")
	if err != nil {
		return ""
	}
	ret, _, _ := procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(keyPtr)),
		uintptr(unsafe.Pointer(&valuePtr)),
		uintptr(unsafe.Pointer(&valueLen)),
	)
	if ret == 0 || valuePtr == nil || valueLen < uint32(unsafe.Sizeof(vsFixedFileInfo{})) {
		return ""
	}
	info := (*vsFixedFileInfo)(valuePtr)
	major := info.ProductVersionMS >> 16
	minor := info.ProductVersionMS & 0xFFFF
	patch := info.ProductVersionLS >> 16
	build := info.ProductVersionLS & 0xFFFF
	if major == 0 && minor == 0 && patch == 0 && build == 0 {
		return ""
	}
	out := itoa(uint32(major)) + "." + itoa(uint32(minor))
	if patch != 0 || build != 0 {
		out += "." + itoa(uint32(patch))
		if build != 0 {
			out += "." + itoa(uint32(build))
		}
	}
	return out
}

// itoa 避免为了拼四个数字而引入 strconv（本文件只有这一处需要它）。
func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var digits [10]byte
	i := len(digits)
	for v > 0 && i > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	return string(digits[i:])
}
