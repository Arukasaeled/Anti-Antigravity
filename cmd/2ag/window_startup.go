package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SW_HIDE affects WebView2 controller creation, not just the native frame.
// Revealing the frame later leaves a working DOM behind an invisible surface.
// Manager is interactive: normalize hidden launcher startup before creating
// WebView2, taking the singleton lock, or starting any account/host services.
func relaunchVisibleManager() (bool, error) {
	var startup windows.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	kernel32.NewProc("GetStartupInfoW").Call(uintptr(unsafe.Pointer(&startup)))
	if startup.Flags&windows.STARTF_USESHOWWINDOW == 0 || startup.ShowWindow != windows.SW_HIDE {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("无法恢复 Manager 可见启动: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	// Go's default STARTUPINFO does not request SW_HIDE. Preserve arguments,
	// environment and working directory; the child owns the normal singleton.
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("无法恢复 Manager 可见启动: %w", err)
	}
	log.Printf("[2ag] 已纠正隐藏启动，Manager 将在可见窗口初始化 (PID %d)", cmd.Process.Pid)
	_ = cmd.Process.Release()
	return true, nil
}
