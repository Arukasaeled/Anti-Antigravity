//go:build windows

package supervisor

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ============================================================================
// 冻结宿主（Frozen Host）的按需建立
//
// 背景：0.1.0 / 0.1.1 把构建机上冻结的官方 Antigravity 运行时（app\，解包约 570 MB）
// 直接打进安装包再分发给用户。那等于把 Google 的运行时随 2Ag 一起再分发：安装包
// 因此膨胀到 156 MB，而且「2Ag 发布了什么」与「Google 发布了什么」在字节层面
// 纠缠在一起 —— 上游依赖里的任何东西（连同它们自带的邮箱、绝对路径）都会变成
// 「2Ag 的发布物」。
//
// 现在的契约（四句话，任何改动都必须先通过这段自检）：
//
//  1. 安装包里没有任何 Google Antigravity 字节（见 scripts\pack.ps1 与 installer.iss）；
//  2. 增强形态真正要拉起宿主时，若 2Ag 根目录下还没有自己的 app\ 冻结副本，
//     就从**用户本机**的官方安装目录复制一份出来（物理复制，不是 junction）；
//  3. 已有副本就原样使用 —— 既不删除，也不重新复制。升级用户的副本是他机器上的
//     既成事实，2Ag 没有资格替他重做决定；
//  4. 官方安装不存在时如实报错，绝不退化成「拿官方 exe 直接跑增强形态」——
//     那会得到一个界面说「增强」、实际没有物理隔离的宿主。
//
// 官方安装目录在整个过程中只读：本文件没有任何写入官方目录的代码路径。
// ============================================================================

// 宿主引导状态。取值是结构性的三种情形，不是程度副词：
//
//	ready         —— 2Ag 冻结副本已经在位，增强形态可以直接启动；
//	needs_bootstrap —— 官方安装在本机，冻结副本还没有（首次启动时建立）；
//	no_official   —— 两者都没有：这台机器上没有任何 Antigravity 可用。
const (
	HostBootstrapReady      = "ready"
	HostBootstrapNeedsSetup = "needs_bootstrap"
	HostBootstrapNoOfficial = "no_official"
)

// FrozenHostDir 是 2Ag 冻结副本的落点：<2ag 根目录>\app。
//
// 这个位置不是新发明：detectDefaultAntigravityPath() 的第一优先级、
// frozenHostRoots() 的判定基准、LaunchEnhancedHost 的 cmd.Dir 推导
// 都指向同一个 <root>\app，所以它必须保持不变。
func FrozenHostDir() string {
	root := Get2agRootDir()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "app")
}

// FrozenHostExePath 只在冻结副本真的存在时返回路径，否则返回空串。
//
// 「空串而不是回退到官方 exe」是本函数的全部意义：调用方（更新面板、兼容性面板）
// 要回答的是「2Ag 自己的那份宿主副本是什么版本」，拿官方 exe 顶上会让
// 「冻结宿主是否落后于官方」这件事永远比较不出来（自己跟自己比，永远相等）。
func FrozenHostExePath() string {
	dir := FrozenHostDir()
	if dir == "" {
		return ""
	}
	for _, name := range []string{"Antigravity.exe", "antigravity.exe"} {
		candidate := filepath.Join(dir, name)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate
		}
	}
	return ""
}

// HostBootstrapStatus 是「增强形态的宿主从哪儿来」的如实读数。
type HostBootstrapStatus struct {
	FrozenHostExe     string `json:"frozen_host_exe"`
	FrozenHostPresent bool   `json:"frozen_host_present"`
	OfficialExe       string `json:"official_exe"`
	OfficialPresent   bool   `json:"official_present"`
	State             string `json:"state"`
}

// ProbeHostBootstrap 只读探测，不建立任何东西（界面渲染时调用）。
func ProbeHostBootstrap() HostBootstrapStatus {
	var status HostBootstrapStatus
	if exe := FrozenHostExePath(); exe != "" {
		status.FrozenHostExe, status.FrozenHostPresent = exe, true
	}
	if exe := FindOfficialAntigravity(); exe != "" {
		status.OfficialExe, status.OfficialPresent = exe, true
	}
	switch {
	case status.FrozenHostPresent:
		status.State = HostBootstrapReady
	case status.OfficialPresent:
		status.State = HostBootstrapNeedsSetup
	default:
		status.State = HostBootstrapNoOfficial
	}
	return status
}

// EnsureFrozenHost 保证 <root>\app\Antigravity.exe 存在，并返回它的路径。
//
// 用户按「启动 / 热接管」时才走到这里 —— 这是要求里的「用户触发」，
// 不是开机自动行为：Manager 启动时不复制任何东西（见 cmd\2ag\manager.go）。
var frozenHostSetupMu sync.Mutex

func EnsureFrozenHost() (string, error) {
	frozenHostSetupMu.Lock()
	defer frozenHostSetupMu.Unlock()
	if exe := FrozenHostExePath(); exe != "" {
		return exe, nil
	}

	official := FindOfficialAntigravity()
	if official == "" {
		return "", fmt.Errorf("未检测到 Antigravity：本机既没有官方 Antigravity 安装，也没有 2Ag 冻结宿主副本。" +
			"2Ag 不再随安装包分发官方运行时，请先安装官方 Antigravity，再启动「2Ag 增强形态」")
	}

	sourceDir := filepath.Dir(official)
	targetDir := FrozenHostDir()
	if targetDir == "" {
		return "", fmt.Errorf("无法确定 2Ag 根目录，不能在本地建立冻结宿主")
	}

	// 复制到 app.tmp-<pid> 再整体改名落位：中途失败（磁盘满、权限、用户取消）
	// 不会留下一个「看着像宿主」的半棵树 —— EnsureFrozenHost 的在位判据是
	// app\Antigravity.exe 是否存在，而半棵树里那个 exe 很可能已经先落地了。
	tmpDir := fmt.Sprintf("%s.tmp-%d", targetDir, os.Getpid())
	_ = os.RemoveAll(tmpDir)

	started := time.Now()
	log.Printf("[2ag] 首次建立冻结宿主（物理复制，官方安装目录只读）: %s -> %s", sourceDir, targetDir)
	files, err := copyTree(sourceDir, tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("从官方安装建立冻结宿主失败（%s -> %s）: %w", sourceDir, targetDir, err)
	}

	if err := os.Rename(tmpDir, targetDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		// 目标可能是并发建立出来的（两个入口同时触发启动）：已经有现成的就用它，
		// 不能因为「改名失败」就让用户看到一个假失败。
		if exe := FrozenHostExePath(); exe != "" {
			log.Printf("[2ag] 冻结宿主已由其他入口建立，直接复用: %s", exe)
			return exe, nil
		}
		return "", fmt.Errorf("冻结宿主落位失败（%s）: %w", targetDir, err)
	}

	exe := FrozenHostExePath()
	if exe == "" {
		return "", fmt.Errorf("冻结宿主已复制到 %s，但里面没有 Antigravity.exe —— 官方安装来源可能不完整", targetDir)
	}
	log.Printf("[2ag] 冻结宿主已就绪: %s（%d 个文件，耗时 %s）", exe, files, time.Since(started).Round(time.Second))
	return exe, nil
}

// copyTree 递归复制一棵目录树，返回复制的普通文件数量。
//
// 符号链接按**目标内容**复制：冻结副本必须是自立的物理副本，
// 留下一根指回官方安装目录的链接就不再是隔离了（官方更新会穿透它）。
// 非普通文件（管道、设备、套接字）跳过：它们不属于可复制的安装内容。
func copyTree(source, destination string) (int, error) {
	info, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("源不是目录")
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, entry := range entries {
		src := filepath.Join(source, entry.Name())
		dst := filepath.Join(destination, entry.Name())

		realSrc := src
		isLink := entry.Type()&os.ModeSymlink != 0
		if isLink {
			resolved, err := filepath.EvalSymlinks(src)
			if err != nil {
				return copied, fmt.Errorf("解析符号链接失败 %s: %w", src, err)
			}
			realSrc = resolved
		}
		if fi, err := os.Stat(realSrc); err == nil && fi.IsDir() {
			sub, err := copyTree(realSrc, dst)
			copied += sub
			if err != nil {
				return copied, err
			}
			continue
		}
		if err := copyRegularFile(realSrc, dst); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, nil
}

// copyRegularFile 复制单个普通文件（保留权限位）。
func copyRegularFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("复制 %s 失败: %w", source, err)
	}
	return out.Close()
}
