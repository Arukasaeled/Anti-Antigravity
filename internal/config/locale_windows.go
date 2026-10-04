//go:build windows

package config

import (
	"syscall"
	"unsafe"
)

func SystemLanguage() string {
	var buffer [85]uint16
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 {
		return "en-US"
	}
	return syscall.UTF16ToString(buffer[:])
}
