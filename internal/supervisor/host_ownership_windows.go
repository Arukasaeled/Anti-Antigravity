//go:build windows

package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unsafe"
)

const (
	HostOwned    = "owned"
	HostAdopted  = "adopted"
	HostExternal = "external"
)

type HostProcess struct {
	PID       int    `json:"pid"`
	Exe       string `json:"exe"`
	Started   string `json:"started"`
	Ownership string `json:"ownership"`
	// ManagerAccount is a control-plane request, never proof of native login.
	ManagerAccount string `json:"manager_account,omitempty"`
}

type ExternalHostsError struct {
	Instances []HostProcess `json:"external_instances"`
}

func (e *ExternalHostsError) Error() string {
	return "需要用户明确确认关闭外部 Antigravity 实例；外部实例未被停止"
}

var hostRecords = struct {
	sync.Mutex
	loaded  bool
	records map[int]HostProcess
}{records: make(map[int]HostProcess)}

func hostRecordsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".2ag", "managed_hosts.json")
}

func processIdentity(pid int) HostProcess {
	identity := HostProcess{PID: pid, Exe: getProcessExePath(pid), Ownership: HostExternal}
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return identity
	}
	defer procCloseHandle.Call(handle)
	var creation, exit, kernel, user uint64
	result, _, _ := kernel32.NewProc("GetProcessTimes").Call(handle,
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if result != 0 && exit == 0 {
		identity.Started = fmt.Sprintf("%016x", creation)
	}
	return identity
}

func sameHostProcess(a, b HostProcess) bool {
	return a.PID > 0 && a.PID == b.PID && a.Started != "" && a.Started == b.Started && a.Exe != "" && strings.EqualFold(filepath.Clean(a.Exe), filepath.Clean(b.Exe))
}

func loadHostRecordsLocked() {
	if hostRecords.loaded {
		return
	}
	hostRecords.loaded = true
	var records []HostProcess
	data, err := os.ReadFile(hostRecordsPath())
	if err != nil || json.Unmarshal(data, &records) != nil {
		return
	}
	for _, record := range records {
		if (record.Ownership == HostOwned || record.Ownership == HostAdopted) && sameHostProcess(record, processIdentity(record.PID)) {
			hostRecords.records[record.PID] = record
		}
	}
	// Legacy bare PID files are deliberately not accepted as ownership proof.
}

func saveHostRecordsLocked() error {
	records := []HostProcess{}
	for _, record := range hostRecords.records {
		records = append(records, record)
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	path := hostRecordsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func registerHost(pid int, ownership string) error {
	identity := processIdentity(pid)
	if identity.Exe == "" || identity.Started == "" {
		return fmt.Errorf("无法记录宿主进程身份 %d", pid)
	}
	identity.Ownership = ownership
	hostRecords.Lock()
	defer hostRecords.Unlock()
	loadHostRecordsLocked()
	hostRecords.records[pid] = identity
	// Keep the in-memory record even if persistence fails: the process we just
	// started must remain stoppable, but the operation cannot report success.
	return saveHostRecordsLocked()
}

func forgetHost(pid int) error {
	hostRecords.Lock()
	defer hostRecords.Unlock()
	loadHostRecordsLocked()
	delete(hostRecords.records, pid)
	return saveHostRecordsLocked()
}

func managedHosts() []HostProcess {
	hostRecords.Lock()
	defer hostRecords.Unlock()
	loadHostRecordsLocked()
	result := []HostProcess{}
	for _, record := range hostRecords.records {
		if sameHostProcess(record, processIdentity(record.PID)) {
			result = append(result, record)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Started < result[j].Started })
	return result
}

func CanManageHostPID(pid int) bool {
	for _, record := range managedHosts() {
		if record.PID == pid {
			return true
		}
	}
	return false
}

func HostProcesses() []HostProcess {
	return hostProcessesFromSnapshot(scanAntigravityProcesses())
}

func CheckedHostProcesses() ([]HostProcess, error) {
	procs, err := scanAntigravityProcessesChecked()
	if err != nil {
		return nil, err
	}
	return hostProcessesFromSnapshot(procs), nil
}

func hostProcessesFromSnapshot(procs []antigravityProc) []HostProcess {
	parents := make(map[int]int)
	for _, proc := range procs {
		parents[proc.PID] = proc.PPID
	}
	roots := managedHosts()
	result := []HostProcess{}
	for _, proc := range procs {
		identity := processIdentity(proc.PID)
		current := proc.PID
		for depth := 0; depth < 30 && current > 0; depth++ {
			for _, root := range roots {
				if current == root.PID && identity.Started >= root.Started {
					identity.Ownership = root.Ownership
				}
			}
			if identity.Ownership != HostExternal {
				break
			}
			next := parents[current]
			if next == current {
				break
			}
			current = next
		}
		result = append(result, identity)
	}
	return result
}

func RequireManagedHosts() error {
	processes, err := CheckedHostProcesses()
	if err != nil {
		return err
	}
	external := []HostProcess{}
	for _, proc := range processes {
		if proc.Ownership == HostExternal {
			external = append(external, proc)
		}
	}
	if len(external) > 0 {
		return &ExternalHostsError{Instances: external}
	}
	return nil
}

// Adoption is a separate explicit capability grant, pinned to the exact
// executable and creation time displayed in the confirmation response.
func AdoptExternalHosts(confirmed []HostProcess) error {
	parents := make(map[int]int)
	for _, proc := range scanAntigravityProcesses() {
		parents[proc.PID] = proc.PPID
	}
	selected := make(map[int]bool)
	for _, proc := range confirmed {
		selected[proc.PID] = true
	}
	for _, expected := range confirmed {
		if !sameHostProcess(expected, processIdentity(expected.PID)) {
			return fmt.Errorf("外部实例已变化，请重新确认")
		}
		if IsProtectedIDEProcess(expected.PID) {
			// Path protection may be overridden by adoption, ancestor protection may not.
			if isManagerAncestor(expected.PID) {
				return fmt.Errorf("不能接管 Manager 自身或祖先进程")
			}
		}
		if !strings.EqualFold(filepath.Base(expected.Exe), "antigravity.exe") {
			return fmt.Errorf("目标不是 Antigravity")
		}
	}
	for _, expected := range confirmed {
		if selected[parents[expected.PID]] {
			continue
		}
		if err := registerHost(expected.PID, HostAdopted); err != nil {
			return err
		}
	}
	return nil
}
