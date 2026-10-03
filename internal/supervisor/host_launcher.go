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
	"sync"
	"syscall"
	"time"
)

var (
	managedHostMu  sync.RWMutex
	managedHostPID int
)

// SetManagedHostPID 设置并持久化当前 2Ag 托管的宿主子进程 PID
func SetManagedHostPID(pid int) {
	managedHostMu.Lock()
	defer managedHostMu.Unlock()
	managedHostPID = pid

	userHome, err := os.UserHomeDir()
	if err == nil {
		pidFile := filepath.Join(userHome, ".2ag", "managed_host.pid")
		_ = os.MkdirAll(filepath.Dir(pidFile), 0755)
		if pid > 0 {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)
		} else {
			_ = os.Remove(pidFile)
		}
	}
}

// GetManagedHostPID 获取当前由 2Ag 托管拉起的宿主 PID
func GetManagedHostPID() int {
	managedHostMu.RLock()
	pid := managedHostPID
	managedHostMu.RUnlock()
	if pid > 0 {
		return pid
	}
	userHome, err := os.UserHomeDir()
	if err == nil {
		pidFile := filepath.Join(userHome, ".2ag", "managed_host.pid")
		if data, err := os.ReadFile(pidFile); err == nil {
			if savedPID, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && savedPID > 0 {
				return savedPID
			}
		}
	}
	return 0
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
func LaunchEnhancedHost(exePath string, activeAccountEmail string, workspacePaths ...string) error {
	return launchEnhancedHost(exePath, activeAccountEmail, true, workspacePaths...)
}

// A transactional restore supplies an exact identity, including an empty one.
func launchEnhancedHost(exePath, activeAccountEmail string, useRecordedAccount bool, workspacePaths ...string) error {
	if IsOfficialRuntime() {
		// 形态闸。走到这里说明调用方漏判了形态，而不是用户选错了什么 ——
		// 这条分支必须存在，因为增强形态的启动流程会做三件官方形态下绝对不能做的事：
		// 写 Windows 凭据管理器、把宿主指到 2Ag 私有沙箱、开启 CDP 调试端口等着被注入。
		// 静默按增强流程跑起来，用户看到的界面会说他处在「官方形态」，实际却带着补丁。
		launchPath := exePath
		if launchPath == "" {
			launchPath = FindOfficialAntigravity()
		}
		log.Printf("[2ag] 当前为官方形态，LaunchEnhancedHost 已改走官方启动路径（不注入、不改凭据、不开 CDP）")
		return LaunchOfficialHost(launchPath)
	}
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

	if activeAccountEmail == "" && useRecordedAccount {
		// A persisted UI selection is not a login. Native logout must remain
		// signed out until the user explicitly selects a vault account.
		raw, readErr := readAntigravityCredentialRaw()
		if readErr != nil {
			return fmt.Errorf("读取当前登录失败，未启动宿主: %w", readErr)
		}
		if current := nativeCredentialOwner(raw); current != "" {
			if _, err := credentialForArchive(raw, current); err == nil {
				activeAccountEmail = current
			}
		}
	}

	// 1. 为当前账号分配独立的 Profile 沙箱路径 (对标 cockpit-tools)
	userHome, _ := os.UserHomeDir()
	profileDir := filepath.Join(userHome, ".2ag", "profiles", sanitizeEmail(activeAccountEmail))
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("创建账号沙箱目录失败: %w", err)
	}

	// 在这里落盘「主控账号」是刻意的：这是全流程中唯一能确凿知道
	// 「命令行里 --user-data-dir 到底指向哪个沙箱」的时刻。
	// 宿主起来之后再去猜（扫 localStorage / DOM 找邮箱）在实测中读不到任何东西，
	// 于是 2Ag 重启后界面上显示的「当前主控」会退回 accounts[0]，
	// 与实际运行的沙箱不是同一个账号，配额卡片随之张冠李戴。

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
	if len(workspacePaths) > 0 && strings.TrimSpace(workspacePaths[0]) != "" {
		workspacePath := workspacePaths[0]
		if !filepath.IsAbs(workspacePath) {
			return fmt.Errorf("工作目录必须是绝对路径")
		}
		info, err := os.Stat(workspacePath)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("工作目录不可用，未启动宿主")
		}
		args = append(args, workspacePath)
	}

	// 确保旧的无 CDP 实例或卡死实例被清理，释放端口与文件锁
	if err := StopHostClient(); err != nil {
		return fmt.Errorf("停止原宿主失败，未启动新实例: %w", err)
	}
	time.Sleep(200 * time.Millisecond)

	// 3. 切换真实登录身份 —— 这一步才是「换号」的物理动作。
	//
	// Antigravity 2.x 的登录凭据在 Windows 凭据管理器的 gemini:antigravity 里
	// （CRED_PERSIST_LOCAL_MACHINE ⇒ 机器级、所有 --user-data-dir 共享），
	// 所以**只换 --user-data-dir 永远改不了原生登录态**：界面左下角依旧显示旧账号。
	// 必须在这里把目标账号的凭据原子写进去。
	//
	// 时序是承重的：必须排在 StopHostClient 之后（宿主退出时可能回写凭据，若先写会被覆盖），
	// 并且排在 cmd.Start 之前（宿主一启动就读凭据完成登录）。
	// 身份切换失败必须阻断启动，不能以旧身份假装目标账号已启动。
	// A transaction already wrote and checked its exact blob. Re-applying an
	// archive here would replace freshly renewed credentials with an older copy.
	// Normal startup also preserves the current native login of the same owner.
	if activeAccountEmail != "" && useRecordedAccount {
		current, readErr := readAntigravityCredentialRaw()
		if readErr != nil {
			return fmt.Errorf("读取当前登录失败，未启动宿主: %w", readErr)
		}
		owner := nativeCredentialOwner(current)
		_, completeErr := credentialForArchive(current, activeAccountEmail)
		if completeErr != nil || !strings.EqualFold(owner, activeAccountEmail) {
			if err := ApplyAntigravityCredential(activeAccountEmail); err != nil {
				return fmt.Errorf("切换系统登录凭据失败，未启动宿主: %w", err)
			}
		}
	} else if activeAccountEmail != "" {
		current, readErr := readAntigravityCredentialRaw()
		owner := nativeCredentialOwner(current)
		_, completeErr := credentialForArchive(current, activeAccountEmail)
		if readErr != nil || completeErr != nil || !strings.EqualFold(owner, activeAccountEmail) {
			return fmt.Errorf("已恢复登录与目标账号不一致，未启动宿主")
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
		return fmt.Errorf("启动宿主失败: %w", err)
	}

	SetManagedHostPID(cmd.Process.Pid)
	go cmd.Wait()
	if err := bindEnhancedAuthCallback(exePath, profileDir, cmd.Process.Pid, cdpPort); err != nil {
		if stopErr := StopHostClient(); stopErr != nil {
			return fmt.Errorf("绑定登录回调失败: %w；新宿主停止失败: %v", err, stopErr)
		}
		return fmt.Errorf("绑定登录回调失败，新宿主已停止: %w", err)
	}
	SetPersistedActiveAccount(activeAccountEmail)
	// 完整执行参数行：profileDir 单独打一份便于肉眼核对「换号是否真的换了沙箱」，
	// 但只有整条 args 才能证明命令行没有被静默裁剪或回退到老账号目录。
	log.Printf("[2ag] 物理启动 Antigravity 宿主 (PID %d), 启动路径: %s, 工作目录: %s, 账号沙箱: %s, CDP: %s", cmd.Process.Pid, exePath, cmd.Dir, profileDir, cdpAddr)
	log.Printf("[2ag] 宿主完整命令行: %s %s", exePath, strings.Join(args, " "))
	if loginEmail, err := ReadHostLoginEmail(); err == nil && loginEmail != "" {
		log.Printf("[2ag] 宿主真实登录身份（读自 Windows 凭据管理器 target=%s）: %s", antigravityCredTarget, loginEmail)
	} else {
		log.Printf("[2ag] 警告：无法从 Windows 凭据管理器读出宿主登录身份（err=%v），界面显示的当前账号可能不可信", err)
	}

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
	pid := GetManagedHostPID()
	if pid <= 0 {
		status := ProbeRealHost()
		if status.IsRunning && status.PID > 0 {
			pid = status.PID
		}
	}
	if pid <= 0 {
		log.Printf("[2ag] StopHostClient: 当前未发现运行中的 2Ag 宿主进程，无需停止")
		return nil
	}
	if !processAlive(pid) {
		SetManagedHostPID(0)
		return nil
	}
	if exe := getProcessExePath(pid); !strings.EqualFold(filepath.Base(exe), "Antigravity.exe") {
		return fmt.Errorf("托管 PID 已不属于 Antigravity；未终止任何进程")
	}
	err := killPIDSafely(pid)
	if err == nil {
		SetManagedHostPID(0)
	}
	return err
}

// RestartHostClient 重启宿主进程
func RestartHostClient(customPath string) error {
	if err := StopHostClient(); err != nil {
		return err
	}
	time.Sleep(600 * time.Millisecond)
	return LaunchHostClient(customPath)
}

// RestartEnhancedHost 重启并切换账号沙箱拉起宿主
func RestartEnhancedHost(customPath string, activeAccountEmail string) error {
	if err := StopHostClient(); err != nil {
		return err
	}
	time.Sleep(600 * time.Millisecond)
	return LaunchEnhancedHost(customPath, activeAccountEmail)
}

// TakeoverHost 重新拉起宿主并附加当前激活沙箱路径与动态预留的 CDP 调试端口
func TakeoverHost(customPath string, activeAccountEmail string) error {
	// 如果当前有 2Ag 托管的旧实例，先精准安全停止（严禁误杀当前 IDE）
	if err := StopHostClient(); err != nil {
		return err
	}
	time.Sleep(300 * time.Millisecond)

	return LaunchEnhancedHost(customPath, activeAccountEmail)
}
