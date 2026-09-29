package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

var candidatePaths = []string{
	filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Antigravity", "Antigravity.exe"),
	filepath.Join(os.Getenv("PROGRAMFILES"), "Antigravity", "Antigravity.exe"),
	filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Antigravity", "Antigravity.exe"),
}

// LaunchHostClient 物理拉起宿主客户端
func LaunchHostClient(customPath string) error {
	exePath := customPath
	if exePath == "" {
		for _, p := range candidatePaths {
			if _, err := os.Stat(p); err == nil {
				exePath = p
				break
			}
		}
	}
	if exePath == "" {
		return fmt.Errorf("未在默认路径找到 Antigravity.exe，请检查安装位置")
	}

	// 非侵入式：注入 CDP 远程调试端口
	cmd := exec.Command(exePath,
		"--remote-debugging-port=28472",
		"--disable-features=IsolateOrigins,site-per-process",
	)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起宿主进程失败: %w", err)
	}
	return nil
}

// StopHostClient 停止宿主进程
func StopHostClient() error {
	status := ProbeRealHost()
	if !status.IsRunning || status.PID == 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(status.PID), "/T")
	return cmd.Run()
}

// RestartHostClient 重启宿主进程
func RestartHostClient(customPath string) error {
	_ = StopHostClient()
	time.Sleep(600 * time.Millisecond)
	return LaunchHostClient(customPath)
}
