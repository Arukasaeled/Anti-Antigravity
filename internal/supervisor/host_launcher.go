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
	// 1. 最高优先级（同级便携版）：检测当前 2ag.exe 所在目录下的 app/Antigravity.exe
	execDir := Get2agRootDir()
	if execDir != "" {
		for _, name := range []string{"Antigravity.exe", "antigravity.exe"} {
			portablePath := filepath.Join(execDir, "app", name)
			if fi, err := os.Stat(portablePath); err == nil && !fi.IsDir() {
				log.Printf("[2ag] 命中最高优先级同级便携版宿主: %s", portablePath)
				return portablePath
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
	if exePath == "" {
		exePath = detectDefaultAntigravityPath()
	}
	if exePath == "" {
		return fmt.Errorf("未在默认路径找到 Antigravity.exe，请检查安装位置")
	}

	if activeAccountEmail == "" {
		activeAccountEmail = GetActiveAccountEmail()
	}

	// 1. 为当前账号分配独立的 Profile 沙箱路径 (对标 cockpit-tools)
	userHome, _ := os.UserHomeDir()
	profileDir := filepath.Join(userHome, ".2ag", "profiles", sanitizeEmail(activeAccountEmail))
	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return fmt.Errorf("创建账号沙箱目录失败: %w", err)
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
	}

	// 确保旧的无 CDP 实例或卡死实例被清理，释放端口与文件锁
	_ = StopHostClient()
	time.Sleep(200 * time.Millisecond)

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
	log.Printf("[2ag] 物理启动 Antigravity 宿主 (PID %d), 启动路径: %s, 工作目录: %s, 账号沙箱: %s, CDP: %s", cmd.Process.Pid, exePath, cmd.Dir, profileDir, cdpAddr)

	// 3. 启动后台异步注入协程：CDP 就绪时立即打入 anti-Antigravity 补丁
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
	err := killPIDSafely(pid)
	SetManagedHostPID(0)
	return err
}

// RestartHostClient 重启宿主进程
func RestartHostClient(customPath string) error {
	_ = StopHostClient()
	time.Sleep(600 * time.Millisecond)
	return LaunchHostClient(customPath)
}

// RestartEnhancedHost 重启并切换账号沙箱拉起宿主
func RestartEnhancedHost(customPath string, activeAccountEmail string) error {
	_ = StopHostClient()
	time.Sleep(600 * time.Millisecond)
	return LaunchEnhancedHost(customPath, activeAccountEmail)
}

// TakeoverHost 重新拉起宿主并附加当前激活沙箱路径与动态预留的 CDP 调试端口
func TakeoverHost(customPath string, activeAccountEmail string) error {
	// 如果当前有 2Ag 托管的旧实例，先精准安全停止（严禁误杀当前 IDE）
	_ = StopHostClient()
	time.Sleep(300 * time.Millisecond)

	if activeAccountEmail == "" {
		activeAccountEmail = GetActiveAccountEmail()
	}
	return LaunchEnhancedHost(customPath, activeAccountEmail)
}

