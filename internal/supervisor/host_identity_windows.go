//go:build windows

package supervisor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	hostIdentityProbeInterval = 3 * time.Second
	hostIdentityMaxAge        = 10 * time.Second
)

type HostIdentityStatus struct {
	Verified      bool   `json:"verified"`
	Status        string `json:"status"`
	Source        string `json:"source,omitempty"`
	Email         string `json:"email,omitempty"`
	ExpectedEmail string `json:"expected_email,omitempty"`
	PID           int    `json:"pid,omitempty"`
	CheckedAt     int64  `json:"checked_at,omitempty"`
	Message       string `json:"message"`
}

var hostIdentityProbeMu sync.Mutex
var hostIdentityState = struct {
	sync.Mutex
	revision      uint64
	lastAttempt   time.Time
	everConfirmed bool
	settlingUntil time.Time
	status        HostIdentityStatus
}{status: HostIdentityStatus{Status: "unavailable", Message: "宿主身份尚未验证"}}

func managerAccountForHost(host HostProcess) string {
	for _, record := range managedHosts() {
		if sameHostProcess(record, host) {
			return record.ManagerAccount
		}
	}
	return ""
}

// Record only explicit 2Ag lifecycle/account operations, pinned to ownership
// and creation time. Discovery and credential restoration never grant this.
func authorizeManagerHostAccount(host HostProcess, email string) error {
	email = strings.TrimSpace(email)
	if email == "" || !strings.EqualFold(email, GetSelectedAccountEmail()) || !sameHostProcess(host, processIdentity(host.PID)) {
		return fmt.Errorf("Manager 账号选择或宿主进程已变化，未授权身份验证")
	}
	hostRecords.Lock()
	loadHostRecordsLocked()
	previous, exists := hostRecords.records[host.PID]
	if !exists || !sameHostProcess(previous, host) || (previous.Ownership != HostOwned && previous.Ownership != HostAdopted) {
		hostRecords.Unlock()
		return fmt.Errorf("外部宿主不能提交 Manager 身份验证")
	}
	record := previous
	record.ManagerAccount = email
	hostRecords.records[host.PID] = record
	err := saveHostRecordsLocked()
	if err != nil {
		hostRecords.records[host.PID] = previous
	}
	hostRecords.Unlock()
	if err != nil {
		return fmt.Errorf("保存 Manager 身份验证来源失败: %w", err)
	}
	hostIdentityState.Lock()
	hostIdentityState.revision++
	hostIdentityState.lastAttempt = time.Time{}
	hostIdentityState.everConfirmed = false
	hostIdentityState.settlingUntil = time.Now().Add(12 * time.Second)
	hostIdentityState.status = HostIdentityStatus{Status: "pending", ExpectedEmail: email, PID: host.PID, Message: "正在读取受管宿主登录身份"}
	clearActiveHostIdentity()
	hostIdentityState.Unlock()
	return nil
}

func resetHostIdentityObservation() {
	hostIdentityState.Lock()
	hostIdentityState.revision++
	hostIdentityState.lastAttempt = time.Time{}
	hostIdentityState.everConfirmed = false
	hostIdentityState.settlingUntil = time.Time{}
	hostIdentityState.status = HostIdentityStatus{Status: "unavailable", Message: "宿主身份尚未验证"}
	clearActiveHostIdentity()
	hostIdentityState.Unlock()
}

func hostIdentityStatusSnapshot() HostIdentityStatus {
	hostIdentityState.Lock()
	status := hostIdentityState.status
	hostIdentityState.Unlock()
	if status.Verified && !strings.EqualFold(GetActiveAccountEmail(), status.Email) {
		status.Verified, status.Status, status.Message = false, "unavailable", "宿主身份需要重新验证"
	}
	return status
}

func HostIdentityStatusNow() HostIdentityStatus {
	return hostIdentityStatusSnapshot()
}

// A confirmed identity changing outside 2Ag revokes the original authority.
// Merely changing back to the same email cannot silently restore its proof.
func revokeManagerHostAccount(host HostProcess, email string) error {
	hostRecords.Lock()
	defer hostRecords.Unlock()
	loadHostRecordsLocked()
	record, exists := hostRecords.records[host.PID]
	if !exists || !sameHostProcess(record, host) || !strings.EqualFold(record.ManagerAccount, email) {
		return nil
	}
	record.ManagerAccount = ""
	hostRecords.records[host.PID] = record
	return saveHostRecordsLocked()
}

// Polls are bounded and share one reader. Concurrent UI requests never queue
// extra RPCs or turn a cached credential owner into native identity evidence.
func ObserveManagedHostIdentity(force bool) HostIdentityStatus {
	if !hostIdentityProbeMu.TryLock() {
		return hostIdentityStatusSnapshot()
	}
	defer hostIdentityProbeMu.Unlock()
	hostIdentityState.Lock()
	if !force && time.Since(hostIdentityState.lastAttempt) < hostIdentityProbeInterval {
		hostIdentityState.Unlock()
		return hostIdentityStatusSnapshot()
	}
	hostIdentityState.lastAttempt = time.Now()
	revision := hostIdentityState.revision
	wasConfirmed := hostIdentityState.everConfirmed
	settlingUntil := hostIdentityState.settlingUntil
	hostIdentityState.Unlock()

	host := processIdentity(GetManagedHostPID())
	expected := managerAccountForHost(host)
	status := HostIdentityStatus{Status: "unavailable", ExpectedEmail: expected, PID: host.PID, Message: "宿主不是 Manager 已授权验证的运行实例"}
	mismatch := false
	if expected != "" && sameHostProcess(host, processIdentity(host.PID)) {
		if !strings.EqualFold(GetSelectedAccountEmail(), expected) {
			status.Status, status.Message = "selection_mismatch", "Manager 已选择其他账号，当前宿主身份未验证"
		} else if owner, err := ReadHostLoginEmail(); err != nil {
			status.Message = "系统凭据状态暂不可读，宿主身份未验证"
		} else if owner != "" && !strings.EqualFold(owner, expected) {
			status.Status, status.Message, mismatch = "account_mismatch", "系统凭据已被外部切号改变，宿主身份未验证", true
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			native := readNativeHostIdentity(ctx, host)
			cancel()
			status.Source, status.Email = native.Source, native.Email
			if !native.Available || !native.Authenticated || native.Email == "" {
				status.Message = "宿主原生登录身份暂不可读，尚未验证"
			} else if !strings.EqualFold(native.Email, expected) {
				status.Status, status.Message, mismatch = "account_mismatch", "宿主登录账号与 Manager 选择不一致，身份未验证", true
			} else {
				status.Verified, status.Status, status.Message = true, "confirmed", "受管宿主登录账号与 Manager 选择一致，身份已验证"
			}
		}
	}
	// Shared credentials can be changed by an official instance while the RPC
	// is in flight. Recheck that contradiction before publishing native proof.
	if status.Verified {
		if owner, err := ReadHostLoginEmail(); err != nil {
			status.Verified, status.Status, status.Message = false, "unavailable", "系统凭据状态暂不可读，宿主身份未验证"
		} else if owner != "" && !strings.EqualFold(owner, expected) {
			status.Verified, status.Status, status.Message, mismatch = false, "account_mismatch", "系统凭据已被外部切号改变，宿主身份未验证", true
		}
	}
	status.CheckedAt = time.Now().UnixMilli()
	hostIdentityState.Lock()
	if revision != hostIdentityState.revision {
		hostIdentityState.Unlock()
		return hostIdentityStatusSnapshot()
	}
	if status.Verified {
		if err := SetActiveAccount(expected, hostAccountEvidence{Email: status.Email, Process: host}); err != nil {
			status.Verified, status.Status, status.Message = false, "unavailable", err.Error()
		} else {
			hostIdentityState.everConfirmed = true
		}
	}
	if !status.Verified {
		clearActiveHostIdentity()
		if mismatch && (wasConfirmed || time.Now().After(settlingUntil)) {
			if err := revokeManagerHostAccount(host, expected); err != nil {
				status.Message += "；验证来源撤销未持久化，请重新通过 Manager 操作"
			}
			hostIdentityState.everConfirmed = false
		}
	}
	hostIdentityState.status = status
	hostIdentityState.Unlock()
	return status
}

func waitForManagerHostIdentity(host HostProcess, email string) HostIdentityStatus {
	deadline := time.Now().Add(12 * time.Second)
	for {
		status := ObserveManagedHostIdentity(true)
		if (status.Verified && status.PID == host.PID && strings.EqualFold(status.Email, email)) || !sameHostProcess(host, processIdentity(host.PID)) || time.Now().After(deadline) {
			return status
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Manager shutdown cancels this read-only observer, without touching the host.
func StartHostIdentityObserver(ctx context.Context) {
	ticker := time.NewTicker(hostIdentityProbeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ObserveManagedHostIdentity(false)
		}
	}
}
