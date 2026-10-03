//go:build windows

package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Antigravity 2.19's desktop manager ignores ordinary file CLI arguments.
// Use a visible local text editor, never spawn a second Antigravity root.
// No file content is read, cached, or returned through the API.
func OpenTraceFile(target string) error {
	target = filepath.Clean(strings.TrimSpace(target))
	if !filepath.IsAbs(target) {
		return fmt.Errorf("请选择本机文件 / Select a local file")
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return fmt.Errorf("文件已不存在或无法访问 / File no longer accessible")
	}
	editor := filepath.Join(os.Getenv("SystemRoot"), "System32", "notepad.exe")
	if _, err := os.Stat(editor); err != nil {
		return fmt.Errorf("本机文本编辑器不可用 / Local text editor unavailable")
	}
	cmd := exec.Command(editor, target)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("无法打开本机编辑器 / Couldn't open local editor")
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
