//go:build windows

package supervisor

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type HostOperationResult struct {
	Command            string `json:"command"`
	PID                int    `json:"pid"`
	Running            bool   `json:"running"`
	Mode               string `json:"mode"`
	Ownership          string `json:"ownership"`
	IdentityVerified   bool   `json:"identity_verified"`
	IdentityStatus     string `json:"identity_status"`
	CredentialOwner    string `json:"credential_owner,omitempty"`
	CredentialState    string `json:"credential_state,omitempty"`
	CredentialRecovery string `json:"credential_recovery,omitempty"`
	ActiveAccount      string `json:"active_account,omitempty"`
	Message            string `json:"message"`
}

func currentRuntimePolicy() string {
	if IsOfficialRuntime() {
		return RuntimeModeOfficialValue
	}
	return "enhanced"
}

func ValidateLaunchAccount(email string) error {
	email = strings.TrimSpace(email)
	known := false
	for _, account := range collectAccountEntries() {
		if strings.EqualFold(account.Email, email) {
			known = true
			break
		}
	}
	if !known {
		return credentialError(CredentialMissing, "该账号不在本地账号文件中: "+email)
	}
	payload, _, err := credentialPayloadForAccount(email)
	if err != nil {
		return err
	}
	_, err = StoredCredentialValidation(payload, email)
	return err
}

func restoreLaunchCredential(raw []byte) error {
	processes, err := CheckedHostProcesses()
	if err != nil {
		return err
	}
	if len(processes) > 0 {
		return fmt.Errorf("宿主仍在运行，未改写恢复凭据")
	}
	if len(raw) == 0 {
		err = deleteAntigravityCredentialRaw()
	} else {
		err = writeAntigravityCredentialRaw(raw)
	}
	if err != nil {
		return err
	}
	got, err := readAntigravityCredentialRaw()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, raw) {
		return fmt.Errorf("原凭据恢复回读不一致")
	}
	invalidateHostLoginCache()
	return nil
}

func launchHostInMode(mode, path, email string) error {
	if mode == RuntimeModeOfficialValue {
		// Official launch preserves credentials. An explicit account request must
		// use the switching transaction rather than silently ignoring its email.
		if email != "" {
			if err := ValidateLaunchAccount(email); err != nil {
				return err
			}
			raw, err := readAntigravityCredentialRaw()
			if err != nil {
				return err
			}
			if _, err := StoredCredentialValidation(raw, email); err != nil {
				if AccountErrorCode(err) != CredentialOwnerMismatch {
					return err
				}
				return fmt.Errorf("官方启动不会切换凭据；请使用账号切换操作: %w", err)
			}
		}
		return launchOfficialHost(path)
	}
	if mode != "enhanced" {
		return fmt.Errorf("unknown runtime mode: %s", mode)
	}
	return launchEnhancedHost(path, email)
}

func waitOwnedHostReady(pid int, cdpAddr string) error {
	deadline := time.Now().Add(12 * time.Second)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		if !CanManageHostPID(pid) {
			return fmt.Errorf("宿主启动后退出，或进程身份发生变化")
		}
		response, err := client.Get("http://" + cdpAddr + "/json/list")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("宿主已创建进程，但 CDP 尚未就绪，启动未完成")
}

func stopManagedHosts() error {
	if err := RequireManagedHosts(); err != nil {
		return err
	}
	for _, host := range managedHosts() {
		if err := killPIDSafely(host.PID); err != nil {
			return err
		}
		deadline := time.Now().Add(10 * time.Second)
		for sameHostProcess(host, processIdentity(host.PID)) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if sameHostProcess(host, processIdentity(host.PID)) {
			return fmt.Errorf("宿主 %d 未停止", host.PID)
		}
		if err := forgetHost(host.PID); err != nil {
			return fmt.Errorf("宿主已停止，但所有权记录清理失败: %w", err)
		}
	}
	remaining, err := scanAntigravityProcessesChecked()
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return fmt.Errorf("仍有 Antigravity 进程，未完成停止")
	}
	return nil
}

// Critical commands bypass the lossy EventBus and finish before HTTP success.
func ExecuteHostLifecycle(command, mode, path, email string, adopt []HostProcess) (HostOperationResult, error) {
	out := HostOperationResult{Command: command, Mode: mode, IdentityStatus: "unavailable"}
	if command != "start" && command != "stop" && command != "restart" && command != "takeover" {
		return out, fmt.Errorf("unknown host command: %s", command)
	}
	release, err := lockCredentialOperation()
	if err != nil {
		return out, err
	}
	defer release()
	if len(adopt) > 0 {
		if command != "takeover" {
			return out, fmt.Errorf("外部实例只允许通过明确接管授权")
		}
		if err := AdoptExternalHosts(adopt); err != nil {
			return out, err
		}
	}
	if err := RequireManagedHosts(); err != nil {
		return out, err
	}
	if command == "stop" {
		err = stopManagedHosts()
	} else {
		err = launchHostInMode(mode, path, email)
	}
	if err != nil {
		return out, err
	}
	facts := RuntimeModeFactsNow(mode)
	if command != "stop" && (facts.Effective != mode || facts.HostPID <= 0 || !CanManageHostPID(facts.HostPID)) {
		return out, fmt.Errorf("宿主运行形态或进程所有权未确认，操作未完成")
	}
	out.PID, out.Running, out.Mode = facts.HostPID, facts.Effective != "none", facts.Effective
	for _, proc := range HostProcesses() {
		if proc.PID == out.PID {
			out.Ownership = proc.Ownership
		}
	}
	out.CredentialOwner, _ = ReadHostLoginEmail()
	if raw, readErr := readAntigravityCredentialRaw(); readErr == nil && out.CredentialOwner != "" {
		if credential, validateErr := StoredCredentialValidation(raw, out.CredentialOwner); validateErr == nil {
			out.CredentialState, out.CredentialRecovery = credential.State, credential.Recovery
		}
	}
	// Current Antigravity has no proven internal identity source. Credentials,
	// profile paths and arbitrary DOM emails cannot populate active account.
	out.ActiveAccount = GetActiveAccountEmail()
	out.IdentityVerified = out.ActiveAccount != "" && out.Running
	if out.IdentityVerified {
		out.IdentityStatus = "confirmed"
	}
	if command == "stop" {
		out.Message = "宿主已停止"
	} else {
		out.Message = "宿主已启动；系统凭据归属与宿主内部登录身份分别报告，身份未确认时不提交 active account"
		if out.CredentialRecovery != "" {
			out.Message += "；短期 token 待宿主恢复，尚未确认刷新成功"
		}
	}
	return out, nil
}
