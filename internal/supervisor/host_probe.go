//go:build windows

package supervisor

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	psapiDLL                       = syscall.NewLazyDLL("psapi.dll")
	procCreateToolhelp32Snap       = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW            = kernel32.NewProc("Process32FirstW")
	procProcess32NextW             = kernel32.NewProc("Process32NextW")
	procGetProcessMemoryInfo       = psapiDLL.NewProc("GetProcessMemoryInfo")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	th32csSnapProcess = 0x00000002
	processQueryInfo  = 0x0400 | 0x0010 // PROCESS_QUERY_INFORMATION | PROCESS_VM_READ
)

type processEntry32W struct {
	Size            uint32
	Usage           uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	Threads         uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type RealHostMetrics struct {
	PID          int     `json:"pid"`
	MemoryMB     float64 `json:"memory_mb"`
	ProcessFound bool    `json:"process_found"`
	CDPConnected bool    `json:"cdp_connected"`
	StatusText   string  `json:"status_text"`
	IsRunning    bool    `json:"is_running"`
	CDPPort      int     `json:"cdp_port"`
	Status       string  `json:"status"`
}

type HostStatus = RealHostMetrics

func getProcessExePath(pid int) string {
	if pid <= 0 {
		return ""
	}
	pHandle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if pHandle == 0 {
		pHandle, _, _ = procOpenProcess.Call(processQueryInfo, 0, uintptr(pid))
	}
	if pHandle == 0 {
		return ""
	}
	defer procCloseHandle.Call(pHandle)

	var buf [1024]uint16
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageNameW.Call(pHandle, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

// IsProtectedIDEProcess 检查 PID 是否属于受保护的自身进程、父级祖先链、或外部独立运行的 IDE 进程
func IsProtectedIDEProcess(pid int) bool {
	if pid <= 0 {
		return true
	}
	if pid == os.Getpid() {
		return true
	}

	handle, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if handle == uintptr(syscall.InvalidHandle) || handle == 0 {
		return true
	}
	defer procCloseHandle.Call(handle)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return true
	}

	parentMap := make(map[int]int)
	for {
		p := int(entry.ProcessID)
		pp := int(entry.ParentProcessID)
		parentMap[p] = pp
		ret, _, _ = procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}

	// 1. 自身进程的祖先链判定：自身及所有父进程、祖先进程绝对受保护
	curr := os.Getpid()
	for i := 0; i < 30; i++ {
		p, exists := parentMap[curr]
		if !exists || p <= 0 || p == curr {
			break
		}
		if p == pid {
			return true
		}
		curr = p
	}

	// 2. 如果该 pid 恰好是 2Ag 显式记录并拉起的托管 PID，则不作为受保护的外部 IDE
	managedPID := GetManagedHostPID()
	if managedPID > 0 && pid == managedPID {
		return false
	}

	// 3. 向上追溯该 PID 及其父链的可执行文件路径
	checkPID := pid
	for i := 0; i < 10; i++ {
		exePath := getProcessExePath(checkPID)
		if exePath != "" {
			lowerExe := strings.ToLower(filepath.Clean(exePath))
			// IsFrozenHostPath 而不是朴素前缀：进程路径是 junction 解析后的落点，
			// 与 2Ag 根目录的字符串前缀对不上（本机 app/ 就是 junction）。
			if IsFrozenHostPath(lowerExe) {
				return false
			}
			localApp := strings.ToLower(os.Getenv("LOCALAPPDATA"))
			if localApp != "" && strings.HasPrefix(lowerExe, localApp) {
				return true
			}
		}
		p, exists := parentMap[checkPID]
		if !exists || p <= 0 || p == checkPID {
			break
		}
		checkPID = p
	}

	return false
}

func findProcessInsensitive(targetExe string) (int, float64) {
	handle, _, _ := procCreateToolhelp32Snap.Call(th32csSnapProcess, 0)
	if handle == uintptr(syscall.InvalidHandle) || handle == 0 {
		return 0, 0
	}
	defer procCloseHandle.Call(handle)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	ret, _, _ := procProcess32FirstW.Call(handle, uintptr(unsafe.Pointer(&entry)))
	if ret == 0 {
		return 0, 0
	}

	type procInfo struct {
		pid  int
		ppid int
		name string
	}
	var procs []procInfo
	parentMap := make(map[int]int)

	for {
		pid := int(entry.ProcessID)
		ppid := int(entry.ParentProcessID)
		name := syscall.UTF16ToString(entry.ExeFile[:])
		parentMap[pid] = ppid
		procs = append(procs, procInfo{
			pid:  pid,
			ppid: ppid,
			name: name,
		})

		ret, _, _ = procProcess32NextW.Call(handle, uintptr(unsafe.Pointer(&entry)))
		if ret == 0 {
			break
		}
	}

	// 1. 识别所有受保护的 PID 集合 (自身、所有祖先、系统安装的外部 IDE 根进程)
	protected := make(map[int]bool)
	myPID := os.Getpid()
	protected[myPID] = true

	curr := myPID
	for i := 0; i < 30; i++ {
		p, exists := parentMap[curr]
		if !exists || p <= 0 || p == curr {
			break
		}
		protected[p] = true
		curr = p
	}

	managedPID := GetManagedHostPID()
	localApp := strings.ToLower(os.Getenv("LOCALAPPDATA"))

	// 2. 2Ag 托管宿主的专属子树。这些进程是「我们自己的」：既要被 ProbeHostStatus
	// 如实统计出来，也要在用户点「停止」时能被精准查杀，因此它们绝不能在下面的
	// 保护传递阶段被反向吞掉。
	//
	// 历史缺陷：2ag.exe 自身必然落在 protected 里（myPID 在上方被无条件标记），
	// 于是保护传递会把「2ag.exe 的所有子孙」一并标记为受保护 —— 而 2ag.exe 亲手
	// 拉起的托管宿主恰恰就是它的子孙。结果 findProcessInsensitive 扫描
	// 「非受保护的 Antigravity.exe」时一个都匹配不到，走到 mainPID == 0 直接返回
	// (0, 0)：host/status 于是显示 pid=0 / process_found=false / memory_mb=0，
	// StopHostClient 的 ProbeRealHost 兜底分支也随之失效，只能打印
	// 「当前未发现运行中的 2Ag 宿主进程，无需停止」。
	exempt := make(map[int]bool)
	for i := range procs {
		p := &procs[i]
		if !strings.EqualFold(p.name, targetExe) {
			continue
		}
		if managedPID > 0 && p.pid == managedPID {
			exempt[p.pid] = true
			continue
		}
		exePath := getProcessExePath(p.pid)
		// 走 IsFrozenHostPath 而不是朴素前缀比较：本机 D:\Anti-antigravity\app 是一个
		// junction，进程路径是解析后的真实落点，朴素的 HasPrefix(rootDir) 永远匹配不上。
		// 今天这处失配被 managedPID 分支掩盖着，但只要用户手动启动一次冻结宿主就会暴露。
		if IsFrozenHostPath(exePath) {
			exempt[p.pid] = true
			continue
		}
		lowerExe := strings.ToLower(filepath.Clean(exePath))
		if localApp != "" && strings.HasPrefix(lowerExe, localApp) {
			protected[p.pid] = true
		}
	}

	// 豁免同样向子孙传播：托管宿主拉起的 GPU / 渲染器 / 网络等子进程都属于它的
	// 进程树，taskkill /T 需要它们保持可查杀。
	for iter := 0; iter < 10; iter++ {
		grew := false
		for _, p := range procs {
			if !exempt[p.pid] && exempt[p.ppid] {
				exempt[p.pid] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	// 3. 保护传递：受保护进程的所有子孙进程同样必须受到强保护（如 IDE 的 GPU、渲染器、网络等子进程）
	changed := true
	for iter := 0; iter < 10 && changed; iter++ {
		changed = false
		for _, p := range procs {
			if !protected[p.pid] && protected[p.ppid] && !exempt[p.pid] {
				protected[p.pid] = true
				changed = true
			}
		}
	}

	// 4. 统计 2Ag 托管的宿主（冻结副本或显式托管 PID）
	mainPID := 0
	var totalWorkingSet uintptr = 0

	for _, p := range procs {
		if !strings.EqualFold(p.name, targetExe) {
			continue
		}
		if protected[p.pid] {
			continue
		}

		if mainPID == 0 || p.pid < mainPID {
			mainPID = p.pid
		}

		// 累加工作集内存
		pHandle, _, _ := procOpenProcess.Call(processQueryInfo, 0, uintptr(p.pid))
		if pHandle != 0 {
			var pmc processMemoryCounters
			pmc.CB = uint32(unsafe.Sizeof(pmc))
			ok, _, _ := procGetProcessMemoryInfo.Call(pHandle, uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
			if ok != 0 {
				totalWorkingSet += pmc.WorkingSetSize
			}
			procCloseHandle.Call(pHandle)
		}
	}

	if mainPID == 0 {
		return 0, 0
	}

	// 真实内存读数。历史实现在 memMB < 1.0 时硬编码回填 104.2，
	// 这让「宿主未运行」和「宿主运行中但读不到工作集」在界面上毫无区别，
	// 属于纯伪造数据 —— 现在如实返回 0，由前端显示为「未运行」。
	return mainPID, float64(totalWorkingSet) / (1024 * 1024)
}

// ProbeHostStatus 彻底解耦系统级进程探活与 CDP 握手
func ProbeHostStatus() RealHostMetrics {
	var metrics RealHostMetrics
	metrics.CDPPort = ResolveCDPPort()
	cdpAddr := CDPAddrForPort(metrics.CDPPort)

	// 1. 系统快照不区分大小写匹配 "antigravity.exe"
	pid, mem := findProcessInsensitive("antigravity.exe")
	if pid > 0 {
		metrics.PID = pid
		metrics.MemoryMB = mem
		metrics.ProcessFound = true
		metrics.IsRunning = true
	}

	// 2. 独立探针检测动态解析出的 CDP 端口
	conn, err := net.DialTimeout("tcp", cdpAddr, 150*time.Millisecond)
	if err == nil {
		conn.Close()
		metrics.CDPConnected = true
	}

	// 3. 严谨状态文案推导
	if metrics.ProcessFound && metrics.CDPConnected {
		metrics.StatusText = "RUNNING_CDP_ACTIVE" // 宿主在线 (CDP 已就绪)
		metrics.Status = "RUNNING_CDP_ACTIVE"
	} else if metrics.ProcessFound && !metrics.CDPConnected {
		metrics.StatusText = "RUNNING_STANDALONE" // 宿主运行中 (未附加 CDP)
		metrics.Status = "RUNNING_STANDALONE"
	} else {
		metrics.StatusText = "OFFLINE" // 宿主未运行
		metrics.Status = "OFFLINE"
	}

	return metrics
}

func ProbeRealHost() HostStatus {
	return ProbeHostStatus()
}
