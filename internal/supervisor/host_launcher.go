package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func GetManagedHostPID() int {
	hosts := managedHosts()
	if len(hosts) == 0 {
		return 0
	}
	return hosts[0].PID
}

func killPIDSafely(pid int) error {
	if pid <= 0 {
		return nil
	}
	if IsProtectedIDEProcess(pid) {
		log.Printf("[2ag] 安全防护拦截: 拦截到目标 PID %d 属于受保护的自身或外部开发 IDE 进程，严禁查杀！", pid)
		return fmt.Errorf("refusing to kill protected IDE process %d", pid)
	}
	log.Printf("[2ag] 正在安全停止 2Ag 托管的宿主子进程 (PID %d)...", pid)
	cmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid), "/T")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

var candidatePaths = []string{
	filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Antigravity", "Antigravity.exe"),
	filepath.Join(os.Getenv("PROGRAMFILES"), "Antigravity", "Antigravity.exe"),
	filepath.Join(os.Getenv("PROGRAMFILES(X86)"), "Antigravity", "Antigravity.exe"),
}

func sanitizeEmail(email string) string {
	if email == "" {
		return "default"
	}
	clean := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(email, "_")
	h := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	hashStr := hex.EncodeToString(h[:4]) // 8 hex chars
	if len(clean) > 24 {
		clean = clean[:24]
	}
	return clean + "_" + hashStr
}

// Get2agRootDir 获取当前 2ag.exe 所在的根目录
func Get2agRootDir() string {
	if exe, err := os.Executable(); err == nil {
		if realPath, err := filepath.EvalSymlinks(exe); err == nil {
			return filepath.Dir(realPath)
		}
		return filepath.Dir(exe)
	}
	if len(os.Args) > 0 {
		if abs, err := filepath.Abs(filepath.Dir(os.Args[0])); err == nil {
			return abs
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

func detectDefaultAntigravityPath() string {
	// 1. 最高优先级（2Ag 同级冻结副本）：检测当前 2ag.exe 所在目录下的 app/Antigravity.exe
	execDir := Get2agRootDir()
	if execDir != "" {
		for _, name := range []string{"Antigravity.exe", "antigravity.exe"} {
			frozenPath := filepath.Join(execDir, "app", name)
			if fi, err := os.Stat(frozenPath); err == nil && !fi.IsDir() {
				log.Printf("[2ag] 命中最高优先级同级冻结副本宿主: %s", frozenPath)
				return frozenPath
			}
		}
	}

	// 2. 第二优先级（源码工程关联版）：检测工作目录下的 app/Antigravity.exe
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		for _, name := range []string{"Antigravity.exe", "antigravity.exe"} {
			srcAppPath := filepath.Join(cwd, "app", name)
			if fi, err := os.Stat(srcAppPath); err == nil && !fi.IsDir() {
				log.Printf("[2ag] 命中第二优先级源码工程关联版宿主: %s", srcAppPath)
				return srcAppPath
			}
		}
	}

	// 3. 兜底优先级（系统全局安装版）：回退至系统安装目录
	for _, p := range candidatePaths {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			log.Printf("[2ag] 回退至系统全局安装版宿主: %s", p)
			return p
		}
	}
	if p, err := FindAntigravity(); err == nil && p.Executable != "" {
		log.Printf("[2ag] FindAntigravity 发现宿主: %s", p.Executable)
		return p.Executable
	}
	return ""
}

// LaunchEnhancedHost 带账号沙箱与 CDP 监听拉起宿主
func LaunchEnhancedHost(exePath string, activeAccountEmail string) error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	return launchHostInMode(currentRuntimePolicy(), exePath, activeAccountEmail)
}

func launchEnhancedHost(exePath string, activeAccountEmail string) error {
	if exePath == "" {
		// 增强形态只能跑 2Ag 自己的冻结副本 —— 这是形态隔离的物理前提，
		// 不是可选的优化。0.1.1 之前那份副本由安装包直接提供（等于随包分发
		// Google 运行时），现在改为用户第一次启动增强形态时，从**他本机**的
		// 官方安装复制一份出来；已有副本时 EnsureFrozenHost 原样返回，
		// 既不删除也不重新复制（升级用户的副本是他机器上的既成事实）。
		frozen, err := EnsureFrozenHost()
		if err != nil {
			return err
		}
		exePath = frozen
	}

	if !IsFrozenHostPath(exePath) {
		return fmt.Errorf("增强形态只允许启动 2Ag 冻结副本")
	}
	if activeAccountEmail == "" {
		activeAccountEmail = GetSelectedAccountEmail()
	}

	// 1. 为当前账号分配独立的 Profile 沙箱路径 (对标 cockpit-tools)
	userHome, _ := os.UserHomeDir()
	profileDir := filepath.Join(userHome, ".2ag", "profiles", sanitizeEmail(activeAccountEmail))
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("创建账号沙箱目录失败: %w", err)
	}

	// A profile name is a requested account, not evidence of a signed-in user.
	if activeAccountEmail != "" {
		if err := ValidateLaunchAccount(activeAccountEmail); err != nil {
			return err
		}
	}
	if err := RequireManagedHosts(); err != nil {
		return err
	}

	// 2. 构造启动命令：附加 CDP 调试端口与用户隔离目录
	// CDP 端口改为动态预留：历史硬编码 28472 一旦被占用，宿主会静默换用随机端口，
	// 而 2Ag 仍死盯 28472 → 注入链路整体哑火（502 / CDP 未就绪）。
	cdpPort := ReserveCDPPort()
	cdpAddr := CDPAddrForPort(cdpPort)
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", cdpPort),
		"--user-data-dir=" + profileDir,
		"--disable-features=IsolateOrigins,site-per-process",
		// 区域语言锁定：Chromium 会优先采信 --lang 决定 navigator.language /
		// Accept-Language / Intl 默认区域。宿主默认从系统区域或出口 IP 协商语言，
		// 一旦漂移到 en-US，工作台界面会整片切成英文。
		"--lang=zh-CN",
		"--accept-lang=zh-CN",
		// 同一把锁的第二道：Chromium 在 Windows 上会读 LANG 环境变量参与 ICU 区域决策，
		// 显式写入进程环境，避免被系统区域覆盖。
		"--env=LANG=zh_CN.UTF-8",
	}

	// 确保旧的无 CDP 实例或卡死实例被清理，释放端口与文件锁
	if err := stopManagedHosts(); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// Shared credentials must be written and read back before starting. On a
	// failure restore the original credential; never run under an old identity.
	previous, err := readAntigravityCredentialRaw()
	if err != nil {
		return err
	}
	if activeAccountEmail != "" {
		if err := ApplyAntigravityCredential(activeAccountEmail); err != nil {
			if restoreErr := restoreLaunchCredential(previous); restoreErr != nil {
				return fmt.Errorf("%v；恢复凭据失败: %w", err, restoreErr)
			}
			return err
		}
	}

	cmd := exec.Command(exePath, args...)

	// 工作目录与注入素材联动保障：
	// 将宿主进程的 cmd.Dir 显式绑定为 2ag.exe 的所在根目录
	// 确保宿主能顺利读到同级的 themes/、plugins/ 与 assets/ 壁纸资源
	workDir := Get2agRootDir()
	dirName := filepath.Dir(exePath)
	if strings.EqualFold(filepath.Base(dirName), "app") {
		workDir = filepath.Dir(dirName)
	}
	if workDir == "" {
		workDir = filepath.Dir(exePath)
	}
	cmd.Dir = workDir

	if err := cmd.Start(); err != nil {
		if restoreErr := restoreLaunchCredential(previous); restoreErr != nil {
			return fmt.Errorf("启动失败: %v；恢复凭据失败: %w", err, restoreErr)
		}
		return fmt.Errorf("启动宿主失败: %w", err)
	}

	if err := registerHost(cmd.Process.Pid, HostOwned); err != nil {
		stopErr := cmd.Process.Kill()
		_ = cmd.Wait()
		if stopErr != nil && processAlive(cmd.Process.Pid) {
			return fmt.Errorf("登记失败: %v；新进程无法停止: %w", err, stopErr)
		}
		restoreErr := restoreLaunchCredential(previous)
		return fmt.Errorf("登记宿主失败: %v；停止结果: %v；凭据恢复: %v", err, stopErr, restoreErr)
	}
	go cmd.Wait()
	if err := waitOwnedHostReady(cmd.Process.Pid, cdpAddr); err != nil {
		if stopErr := stopManagedHosts(); stopErr != nil {
			return fmt.Errorf("%v；停止失败: %w", err, stopErr)
		}
		if restoreErr := restoreLaunchCredential(previous); restoreErr != nil {
			return fmt.Errorf("%v；恢复凭据失败: %w", err, restoreErr)
		}
		return err
	}
	SetRuntimeMode("enhanced")
	// 完整执行参数行：profileDir 单独打一份便于肉眼核对「换号是否真的换了沙箱」，
	// 但只有整条 args 才能证明命令行没有被静默裁剪或回退到老账号目录。
	log.Printf("[2ag] 物理启动 Antigravity 宿主 (PID %d), 启动路径: %s, 工作目录: %s, 账号沙箱: %s, CDP: %s", cmd.Process.Pid, exePath, cmd.Dir, profileDir, cdpAddr)
	log.Printf("[2ag] 宿主完整命令行: %s %s", exePath, strings.Join(args, " "))
	log.Printf("[2ag] 宿主已启动；目标凭据归属 %s，宿主内部登录身份尚未确认", activeAccountEmail)

	// 4. 启动后台异步注入协程：CDP 就绪时立即打入 anti-Antigravity 补丁
	go watchAndInjectCDP(cdpAddr, 15*time.Second)

	return nil
}

// LaunchHostClient 物理拉起宿主客户端
func LaunchHostClient(customPath string) error {
	return LaunchEnhancedHost(customPath, "")
}

// StopHostClient 停止由 2Ag 托管的宿主进程（安全模式：精准按 PID 查杀，严禁通配 taskkill /IM）
func StopHostClient() error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	if err := RequireManagedHosts(); err != nil {
		return err
	}
	return stopManagedHosts()
}

func RestartHostClient(customPath string) error { return RestartEnhancedHost(customPath, "") }

func RestartEnhancedHost(customPath, email string) error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	return launchHostInMode(currentRuntimePolicy(), customPath, email)
}

// The mode must be supplied by the API's current configured state.
func TakeoverHost(customPath, email string, configuredModes ...string) error {
	if len(configuredModes) != 1 {
		return fmt.Errorf("接管必须指定当前 configured runtime mode")
	}
	mode := configuredModes[0]
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	return launchHostInMode(mode, customPath, email)
}
