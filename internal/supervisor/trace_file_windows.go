//go:build windows

package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func OpenTraceFile(target string) error {
	return RunIndependentHostOperation(func() error {
		target = filepath.Clean(strings.TrimSpace(target))
		if !filepath.IsAbs(target) {
			return fmt.Errorf("请选择本机文件 / Select a local file")
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			return fmt.Errorf("文件已不存在或无法访问 / File no longer accessible")
		}
		host := ProbeRealHost()
		exe := getProcessExePath(host.PID)
		if !host.ProcessFound || !IsFrozenHostPath(exe) || IsOfficialRuntime() {
			return fmt.Errorf("请先打开增强宿主 / Open the enhanced host first")
		}
		email, err := ReadHostLoginEmail()
		if err != nil || email == "" {
			return fmt.Errorf("当前宿主身份未取得 / Host identity unavailable")
		}
		home, _ := os.UserHomeDir()
		profile := filepath.Join(home, ".2ag", "profiles", sanitizeEmail(email))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "--user-data-dir="+profile, "--reuse-window", target)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("宿主未确认打开文件 / Host did not confirm file open")
		}
		return nil
	})
}
