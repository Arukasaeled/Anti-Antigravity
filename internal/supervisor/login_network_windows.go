//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

func launchOfficialLoginHost(network loginNetwork) error {
	if err := RequireManagedHosts(); err != nil {
		return err
	}
	exe := FindOfficialAntigravity()
	if exe == "" || IsFrozenHostPath(exe) {
		return fmt.Errorf("未找到官方 Antigravity，请先安装官方客户端")
	}
	// Diagnostics remain read-only; a fresh loopback debug port lets the broker
	// observe onboarding errors without relying on an old DevToolsActivePort.
	args := append(append([]string(nil), network.Args...), "--remote-debugging-port=0")
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.Env = network.Env
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动官方客户端失败")
	}
	if err := registerHost(cmd.Process.Pid, HostOwned); err != nil {
		stopErr := cmd.Process.Kill()
		return fmt.Errorf("记录登录宿主失败: %v；停止新进程: %v", err, stopErr)
	}
	go cmd.Wait()
	return nil
}
