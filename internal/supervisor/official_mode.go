//go:build windows

package supervisor

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ============================================================================
// 官方形态（Official Mode）
//
// 背景：Official Compatibility Audit 查实了 2Ag 与官方 Antigravity 之间存在两处
// 跨形态共享状态 —— Windows 凭据管理器 gemini:antigravity（2Ag 换号会改写它，
// 官方启动读它）与 %USERPROFILE%\.gemini\antigravity 数据树（会话/脑图/快照，
// 官方安装同样读它）。用户因此需要一个「随时可以安全回到官方 Antigravity」的出口。
//
// 契约（写死在这里，任何新增功能都必须先通过这段自检）：
//   - 不注入 Dream Skin / Gravity Boost / G-Hub（走的是 LaunchOfficialHost，
//     整条 CDP 注入链路根本不会被启动）；
//   - 不使用 2Ag 私有 profile 作为官方 user-data-dir（一个启动参数都不加）；
//   - 不改写 Windows 凭据（不调 ApplyAntigravityCredential）；
//   - 不修改官方安装目录里的任何文件（2Ag 从来只做运行时注入，见 compat 报告）；
//   - 从官方安装目录启动，而不是从 2Ag 同级冻结副本。
//
// 刻意不做的事：不采用「改官方配置 → 使用 → 退出时再恢复」的架构。
// 恢复机制只能是保险丝，不是正常路径 —— 一旦进程崩溃，恢复就永远不会发生。
// ============================================================================

// officialInstallRoots 列出官方安装的候选根目录。
//
// 顺序即优先级，且**不含** 2Ag 自己的 app/ 冻结副本 —— 这是它与
// detectDefaultAntigravityPath() 的根本区别：后者刻意把 2Ag 同级副本排在第一位。
func officialInstallRoots() []string {
	var roots []string
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		roots = append(roots,
			filepath.Join(localAppData, "Programs", "Antigravity"),
			filepath.Join(localAppData, "Antigravity"),
		)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		roots = append(roots, filepath.Join(programFiles, "Antigravity"))
	}
	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		roots = append(roots, filepath.Join(programFilesX86, "Antigravity"))
	}
	return roots
}

// FindOfficialAntigravity 只发现官方安装的 Antigravity.exe。
//
// 与 FindAntigravity() / detectDefaultAntigravityPath() 的关键差异：它会把任何
// 落在 2Ag 自身根目录下的候选剔除掉。否则「官方形态」会一本正经地拉起 2Ag 的
// 冻结副本 —— 界面说 OFFICIAL，跑的却是带补丁的那一份，这正是本轮要根除的
// 「界面与事实不符」。找不到时返回空串，由调用方如实报错。
func FindOfficialAntigravity() string {
	ownRoot := strings.ToLower(resolvedPath(Get2agRootDir()))
	for _, root := range officialInstallRoots() {
		cleaned := strings.ToLower(resolvedPath(root))
		if ownRoot != "" && (cleaned == ownRoot || strings.HasPrefix(cleaned, ownRoot+string(os.PathSeparator))) {
			continue
		}
		for _, name := range []string{"Antigravity.exe", "antigravity.exe"} {
			candidate := filepath.Join(root, name)
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

// antigravityProc 是一次进程快照里的一条 Antigravity 记录。
type antigravityProc struct {
	PID   int
	PPID  int
	Exe   string
	MemMB float64
}

// scanAntigravityProcesses 用一次 toolhelp 快照列出所有 antigravity.exe。
//
// 与 findProcessInsensitive 的区别：本函数**不做任何保护/豁免过滤**。
// 官方形态下必须能看见「用户自己手动拉起的那个官方实例」，而它恰恰是
// findProcessInsensitive 刻意排除掉的那一类（LOCALAPPDATA 下的受保护进程）。
// 用错误的口径去回答「官方 Antigravity 在不在跑」，答案必然是错的。
func scanAntigravityProcesses() []antigravityProc {
	handle, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if handle == uintptr(syscall.InvalidHandle) || handle == 0 {
		return nil
	}
	defer procCloseHandle.Call(handle)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	first, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if first == 0 {
		return nil
	}

	var out []antigravityProc
	for {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		if strings.EqualFold(name, "antigravity.exe") {
			pid := int(entry.ProcessID)
			rec := antigravityProc{PID: pid, PPID: int(entry.ParentProcessID), Exe: getProcessExePath(pid)}
			if pHandle, _, _ := procOpenProcess.Call(processQueryInfo, 0, uintptr(pid)); pHandle != 0 {
				var pmc processMemoryCounters
				pmc.CB = uint32(unsafe.Sizeof(pmc))
				if ok, _, _ := procGetProcessMemoryInfo.Call(pHandle, uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB)); ok != 0 {
					rec.MemMB = float64(pmc.WorkingSetSize) / (1024 * 1024)
				}
				procCloseHandle.Call(pHandle)
			}
			out = append(out, rec)
		}
		ret, _, _ := procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}
	return out
}

// isUnderOfficialInstall 判定一个 exe 路径是否位于官方安装根目录之下。
func isUnderOfficialInstall(exePath string) bool {
	if exePath == "" {
		return false
	}
	// 两侧都解析：进程路径可能带 junction（2Ag 宿主就是这样），
	// 而官方安装目录也可能被用户挪到别处并留了个链接。
	lowerExe := strings.ToLower(resolvedPath(exePath))
	for _, root := range officialInstallRoots() {
		cleaned := strings.ToLower(resolvedPath(root))
		if cleaned == "" {
			continue
		}
		if lowerExe == cleaned || strings.HasPrefix(lowerExe, cleaned+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// resolvedPath 把路径解析成它的真实落点。
//
// 存在的理由是本机的一个硬事实：D:\Anti-antigravity\app 是一个 junction，
// 指向 D:\娱乐软件\Anti-Antigravity\app。而 QueryFullProcessImageNameW 返回的是
// **解析后**的路径 —— 于是「运行中的宿主 exe 是 D:\娱乐软件\...\app\Antigravity.exe」
// 与「2Ag 根目录是 D:\Anti-antigravity」用朴素的字符串前缀比较永远对不上，
// 表现为界面报 effective=none（明明有 6 个宿主在跑）。
// Get2agRootDir 自身已经 EvalSymlinks 过，但那只解析了根目录本身，
// 解析不到子目录 app 这一层的 junction；EvalSymlinks 作用在完整文件路径上才会穿透。
//
// 解析失败（路径不存在、权限不足）时原样返回：判定退回朴素比较，
// 宁可少认一个宿主，也不能把「解析不了」当成「匹配成功」。
func resolvedPath(p string) string {
	if p == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != "" {
		return resolved
	}
	return p
}

// frozenHostRoots 列出「2Ag 冻结宿主副本」的所有合法落点。
//
// 为什么不止一个：本机 D:\Anti-antigravity\app 是一个 junction，指向
// D:\娱乐软件\Anti-Antigravity\app —— 也就是**真实落点根本不在 2Ag 根目录之下**。
// 而 QueryFullProcessImageNameW 返回的永远是解析后的路径，于是「根目录 + 前缀比较」
// 在任一方向上都不成立（解析根目录得到根、解析 exe 得到另一个盘符下的路径）。
//
// 所以这里直接问「detectDefaultAntigravityPath 会去哪些目录找宿主」：它找的是
// <root>/app/Antigravity.exe。把那个 app 目录解析成真实落点后作为判定基准，
// junction 与否都一样正确。
func frozenHostRoots() []string {
	root := Get2agRootDir()
	if root == "" {
		return nil
	}
	roots := []string{resolvedPath(root)}
	if appDir := resolvedPath(filepath.Join(root, "app")); appDir != "" && appDir != roots[0] {
		roots = append(roots, appDir)
	}
	return roots
}

// pathUnderAny 报告某个路径是否落在任一基准目录之下（大小写不敏感，Windows 语义）。
func pathUnderAny(target string, roots []string) bool {
	if target == "" {
		return false
	}
	lower := strings.ToLower(filepath.Clean(target))
	for _, root := range roots {
		if root == "" {
			continue
		}
		cleaned := strings.ToLower(filepath.Clean(root))
		if lower == cleaned || strings.HasPrefix(lower, cleaned+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// IsFrozenHostPath 报告一个 exe 路径是否属于 2Ag 的冻结宿主副本。
func IsFrozenHostPath(exePath string) bool {
	if exePath == "" {
		return false
	}
	roots := frozenHostRoots()
	if len(roots) == 0 {
		return false
	}
	// 两个方向都试：进程路径通常已解析，但也可能因为权限拿不到解析结果；
	// 基准目录同理。任一侧命中即算冻结副本。
	return pathUnderAny(exePath, roots) || pathUnderAny(resolvedPath(exePath), roots)
}

// OfficialHostPresence 是「官方 Antigravity 现在有没有在跑」的如实读数。
type OfficialHostPresence struct {
	Running bool
	PID     int
	Memory  float64
	Count   int
	Exe     string
}

// ProbeOfficialHostPresence 扫描所有 antigravity.exe，只统计官方安装目录下的那些。
//
// 不做保护过滤（见 scanAntigravityProcesses 的注释）：官方形态下用户最可能的情形
// 就是「他自己双击启动过官方 Antigravity」，那个实例必须能被看见。
func ProbeOfficialHostPresence() OfficialHostPresence {
	var presence OfficialHostPresence
	for _, p := range scanAntigravityProcesses() {
		if !isUnderOfficialInstall(p.Exe) {
			continue
		}
		presence.Count++
		presence.Memory += p.MemMB
		if presence.PID == 0 || p.PID < presence.PID {
			presence.PID = p.PID
			presence.Exe = p.Exe
		}
	}
	presence.Running = presence.Count > 0
	return presence
}

// ProbeOfficialHost 在官方形态下替代 ProbeHostStatus 提供宿主读数。
//
// CDPConnected 恒为 false —— 这不是缺陷，而是官方形态的定义：
// 一个调试端口都不会开，因此没有任何 CDP 可连，也不会被注入。
func ProbeOfficialHost() RealHostMetrics {
	presence := ProbeOfficialHostPresence()
	metrics := RealHostMetrics{
		PID:          presence.PID,
		MemoryMB:     presence.Memory,
		ProcessFound: presence.Running,
		IsRunning:    presence.Running,
		CDPConnected: false,
		CDPPort:      0,
	}
	switch {
	case presence.Running:
		metrics.StatusText = "OFFICIAL_RUNNING"
		metrics.Status = "OFFICIAL_RUNNING"
	default:
		metrics.StatusText = "OFFLINE"
		metrics.Status = "OFFLINE"
	}
	return metrics
}

// RuntimeModeFacts 把「配置里写的形态」与「实际跑着的形态」并排摆出来。
//
// 这两者分家是**结构性必然**，不是 bug：形态切换不重启宿主（重启会打断用户
// 正在编辑的工作台），所以点下切换之后、重启之前，配置说的是 A、机器上跑的是 B。
// 只报配置不报事实，就是又一次「界面与事实不符」；只报事实不报配置，用户会以为
// 自己的点击没生效。
type RuntimeModeFacts struct {
	Configured string `json:"configured"`
	Effective  string `json:"effective"`
	HostPID    int    `json:"host_pid"`
	HostExe    string `json:"host_exe"`
	Note       string `json:"note"`

	// 宿主来源事实。0.1.1 起安装包里不再有官方运行时，增强形态的宿主只能由
	// 「用户本机的官方安装」在本机复制出来，所以界面必须能同时看到三件事：
	// 冻结副本在不在、官方安装在不在、以及因此该怎么办（State）。
	FrozenHostExe     string `json:"frozen_host_exe"`
	FrozenHostPresent bool   `json:"frozen_host_present"`
	OfficialExe       string `json:"official_exe"`
	OfficialPresent   bool   `json:"official_present"`
	BootstrapState    string `json:"bootstrap_state"`
}

// RuntimeModeFactsNow 裁定当前真实运行形态。
func RuntimeModeFactsNow(configured string) RuntimeModeFacts {
	if configured != RuntimeModeOfficialValue {
		configured = "enhanced"
	}
	facts := RuntimeModeFacts{Configured: configured}

	managedPID := GetManagedHostPID()
	for _, p := range scanAntigravityProcesses() {
		switch {
		case IsFrozenHostPath(p.Exe):
			// 冻结副本可能同时跑着好几个（GPU/渲染/网络子进程都是同一个 exe）。
			// 报哪个 PID：优先 2Ag 亲手拉起并登记过的那个；没有登记就取最小的
			// PID（主进程总是先起来的）。这里必须能区分「已经选中了托管进程」
			// 与「只是恰好选中了某个子进程」—— 前者不该被后面的子进程覆盖。
			better := facts.Effective != "enhanced" ||
				(managedPID > 0 && p.PID == managedPID) ||
				(facts.HostPID != managedPID && facts.HostPID > p.PID)
			if better {
				facts.Effective, facts.HostPID, facts.HostExe = "enhanced", p.PID, p.Exe
			}
		case isUnderOfficialInstall(p.Exe):
			if facts.Effective == "" {
				facts.Effective, facts.HostPID, facts.HostExe = "official", p.PID, p.Exe
			}
		}
	}
	if facts.Effective == "" {
		facts.Effective = "none"
	}

	// 宿主来源读数与形态读数同源返回：界面上一句「下次启动将使用增强形态」
	// 到底意味着「点一下就好」还是「你还没装官方 Antigravity」，取决于这两项。
	bootstrap := ProbeHostBootstrap()
	facts.FrozenHostExe, facts.FrozenHostPresent = bootstrap.FrozenHostExe, bootstrap.FrozenHostPresent
	facts.OfficialExe, facts.OfficialPresent = bootstrap.OfficialExe, bootstrap.OfficialPresent
	facts.BootstrapState = bootstrap.State

	switch {
	case facts.Effective == "none":
		facts.Note = idleHostNote(configured, bootstrap)
	case facts.Effective == configured:
		facts.Note = "运行中的宿主与所选形态一致。"
	default:
		facts.Note = "注意：运行中的宿主目前是「" + modeLabel(facts.Effective) + "」，而配置已改为「" +
			modeLabel(configured) + "」。重启宿主后才会真正切换 —— 这里不自动重启，以免打断你正在编辑的工作台。"
	}
	return facts
}

func modeLabel(mode string) string {
	if mode == RuntimeModeOfficialValue {
		return "官方形态"
	}
	return "2Ag 增强形态"
}

// idleHostNote 在「机器上没有任何宿主在跑」时如实说明下一步会发生什么。
//
// 0.1.1 起安装包里不再有官方运行时，所以「没有宿主在跑」有两种完全不同的成因，
// 界面必须把它们分开讲：这台机器上根本没有 Antigravity（用户得先去装官方版），
// 还是有官方版但 2Ag 的冻结副本尚未建立（首次启动时从他本机建立，需要时间与磁盘）。
// 把两者混成一句「下次启动将使用增强形态」，就是让用户在点下按钮之后才发现真相 ——
// 而那时他已经为一个注定失败的动作等过一轮了。
func idleHostNote(configured string, bootstrap HostBootstrapStatus) string {
	mode := "「" + modeLabel(configured) + "」形态"
	switch bootstrap.State {
	case HostBootstrapNoOfficial:
		return "未检测到 Antigravity：本机既没有官方 Antigravity 安装，也没有 2Ag 冻结宿主副本。" +
			"2Ag 不再随安装包分发官方运行时 —— 请先安装官方 Antigravity，再回到这里启动" + mode + "。"
	case HostBootstrapNeedsSetup:
		return "已检测到本机官方 Antigravity；当前没有正在运行的 Antigravity 实例。" +
			"下次启动" + mode + "时，2Ag 会先从这份官方安装复制出一份自己的冻结宿主" +
			"（物理复制，不改动官方安装，首次需要数十秒），之后一直使用它。"
	default:
		return "当前没有正在运行的 Antigravity 实例；下次启动将使用" + mode + "。冻结宿主已就绪。"
	}
}

// LaunchOfficialHost 以「接近没有安装 2Ag」的方式拉起官方 Antigravity。
//
// 与 LaunchEnhancedHost 的逐条差异，都是刻意的：
//   - 不加任何启动参数（没有 --remote-debugging-port、没有 --user-data-dir、
//     没有 --lang / --accept-lang / --env）。官方形态下 2Ag 不参与宿主的任何决策。
//   - 不创建 ~/.2ag/profiles/<account> 沙箱，不写 active_account.txt。
//   - 不调 ApplyAntigravityCredential：这是共享状态里最重的一项，
//     它会把 Windows 凭据管理器里的登录态换成 2Ag 选的账号。
//   - 不启动 watchAndInjectCDP：没有任何注入协程会被拉起。
//
// 唯一保留的是「登记托管 PID」：宿主是 2Ag 亲手拉起来的，用户点「结束进程」时
// 它必须真的能被结束，否则界面上的按钮就成了空头承诺。登记的 PID 同样让
// findProcessInsensitive 把它视作可查杀进程，而不是受保护的外部 IDE。
func LaunchOfficialHost(exePath string) error {
	if exePath == "" {
		exePath = FindOfficialAntigravity()
	}
	if exePath == "" {
		return fmt.Errorf("未找到官方 Antigravity 安装（已排除 2Ag 自己的宿主副本）；请先安装官方版本，或切回增强形态")
	}
	if IsFrozenHostPath(exePath) {
		return fmt.Errorf("官方形态拒绝启动 2Ag 的冻结宿主副本（%s）—— 那会得到一个界面说官方、实际带补丁的宿主", exePath)
	}

	_ = StopHostClient()
	time.Sleep(300 * time.Millisecond)

	cmd := exec.Command(exePath)
	cmd.Dir = filepath.Dir(exePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动官方宿主失败: %w", err)
	}
	SetManagedHostPID(cmd.Process.Pid)

	log.Printf("[2ag] 官方形态：已启动官方 Antigravity（PID %d，零启动参数、零注入、零凭据改写）: %s", cmd.Process.Pid, exePath)

	// Electron 的单实例锁：如果用户已经自己开着官方 Antigravity，刚拉起的这个进程
	// 只会把既有窗口唤到前台然后立刻退出。此时登记一个已经死掉的 PID 会让后续
	// 「结束进程」和状态显示都指向空气，所以这里如实复核一次。
	time.Sleep(1200 * time.Millisecond)
	if !processAlive(cmd.Process.Pid) {
		presence := ProbeOfficialHostPresence()
		if presence.Running {
			SetManagedHostPID(presence.PID)
			log.Printf("[2ag] 官方实例此前已在运行（PID %d）：本次启动并入既有窗口，未新建实例", presence.PID)
		} else {
			SetManagedHostPID(0)
			log.Printf("[2ag] 警告：官方宿主启动后立即退出（PID %d），且未发现其他运行中的官方实例", cmd.Process.Pid)
		}
	}
	return nil
}

// processAlive 用一次进程快照判断 PID 是否仍然存在。
// 不用 OpenProcess 判活：对已退出但句柄未释放的进程，OpenProcess 仍可能成功。
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if handle == uintptr(syscall.InvalidHandle) || handle == 0 {
		return false
	}
	defer procCloseHandle.Call(handle)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))
	ret, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return false
	}
	for {
		if int(entry.ProcessID) == pid {
			return true
		}
		ret, _, _ = procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			return false
		}
	}
}
