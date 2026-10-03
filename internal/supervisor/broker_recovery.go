//go:build windows

package supervisor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// All account actions share this gate with the broker's entire login lifetime.
var credentialOperationMu sync.Mutex

// Windows releases this lock on process exit. A second Manager/run process
// must never mistake an active broker's record for a crashed transaction.
func lockCredentialOperation() (func(), error) {
	if !credentialOperationMu.TryLock() {
		return nil, fmt.Errorf("账号操作正在进行，请稍后重试")
	}
	dir := filepath.Dir(vaultDirPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		credentialOperationMu.Unlock()
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "credential-operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		credentialOperationMu.Unlock()
		return nil, err
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		f.Close()
		credentialOperationMu.Unlock()
		return nil, fmt.Errorf("另一个 2Ag 正在操作账号，请稍后重试")
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
		f.Close()
		credentialOperationMu.Unlock()
	}, nil
}

// Independent UI host controls must not interrupt a login/switch transaction.
// Transaction-owned launches use their internal path while holding the same lock.
// A nil callback is a read-only admission check before queueing a UI command.
func RunIndependentHostOperation(action func() error) error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		return fmt.Errorf("存在未完成的账号恢复，请先重启 2Ag 恢复")
	}
	if action == nil {
		return nil
	}
	return action()
}

type brokerRecovery struct {
	Version    int             `json:"version"`
	Email      string          `json:"email"`
	CreatedAt  string          `json:"created_at"`
	Credential json.RawMessage `json:"credential"` // DPAPI envelope, never plaintext
	SignedOut  bool            `json:"signed_out,omitempty"`
}

func brokerRecoveryPath() string {
	return filepath.Join(filepath.Dir(vaultDirPath()), "broker-recovery.json")
}

func saveBrokerRecovery(raw []byte, email string) error {
	return writeBrokerRecovery(raw, email, false)
}

// Only the credential-lock owner may replace its own pre-stop checkpoint.
func updateBrokerRecovery(raw []byte, email string) error {
	return writeBrokerRecovery(raw, email, true)
}

func writeBrokerRecovery(raw []byte, email string, update bool) error {
	var sealed []byte
	if len(raw) > 0 {
		var err error
		sealed, err = encryptVaultPayload(raw)
		if err != nil {
			return err
		}
	} else if email != "" {
		return fmt.Errorf("缺少原账号凭据，无法保存恢复记录")
	}
	data, err := json.Marshal(brokerRecovery{Version: 1, Email: email, CreatedAt: time.Now().UTC().Format(time.RFC3339), Credential: sealed, SignedOut: len(raw) == 0})
	if err != nil {
		return err
	}
	path := brokerRecoveryPath()
	if update {
		data, err := os.ReadFile(path)
		var prior brokerRecovery
		if err != nil || json.Unmarshal(data, &prior) != nil || prior.Version != 1 || !strings.EqualFold(prior.Email, email) || prior.SignedOut != (len(raw) == 0) {
			return fmt.Errorf("切号恢复记录归属不一致，拒绝覆盖")
		}
	} else if _, err := os.Stat(path); !os.IsNotExist(err) {
		return fmt.Errorf("存在未完成的账号恢复记录，请先恢复")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".broker-recovery-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func clearBrokerRecovery() error {
	err := os.Remove(brokerRecoveryPath())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// A byte-for-byte readback confirms restoration even for expired identity tokens.
func restoreBrokerRecovery(stop func() error, write func([]byte) error, read func() ([]byte, error)) (bool, error) {
	data, err := os.ReadFile(brokerRecoveryPath())
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var record brokerRecovery
	if json.Unmarshal(data, &record) != nil || record.Version != 1 {
		return false, fmt.Errorf("账号恢复记录损坏，已保留；请从保险库恢复原账号")
	}
	var raw []byte
	if record.SignedOut {
		if record.Email != "" || (len(record.Credential) != 0 && string(record.Credential) != "null") {
			return false, fmt.Errorf("退出登录恢复记录损坏，已保留")
		}
	} else {
		if !isEncryptedVaultBlob(record.Credential) {
			return false, fmt.Errorf("账号恢复记录损坏，已保留")
		}
		var legacy bool
		raw, legacy, err = decryptVaultPayload(record.Credential)
		if err != nil || legacy || len(raw) == 0 {
			return false, fmt.Errorf("账号恢复记录无法解密，已保留")
		}
	}
	if err := stop(); err != nil {
		return false, err
	}
	// Preserve a newly completed login before returning the original credential.
	current, err := read()
	if err != nil {
		return false, err
	}
	if !bytes.Equal(current, raw) {
		if owner, ok := credentialUsableEmail(current); ok {
			if _, err := StoreVaultCredential(owner, displayNameFromCredentialPayload(current), current); err != nil {
				return false, fmt.Errorf("保存中断前的新账号失败，恢复记录已保留")
			}
			if err := SaveOwnedAccountEntry(owner, displayNameFromCredentialPayload(current)); err != nil {
				return false, err
			}
		}
	}
	if err := write(raw); err != nil {
		return false, err
	}
	got, err := read()
	if err != nil || !bytes.Equal(got, raw) {
		return false, fmt.Errorf("原凭据恢复校验失败，恢复记录已保留")
	}
	if err := clearBrokerRecovery(); err != nil {
		return false, err
	}
	return true, nil
}

// RecoverPendingLoginBroker must complete before API handlers or host startup run.
func RecoverPendingLoginBroker() (bool, error) {
	release, err := lockCredentialOperation()
	if err != nil {
		return false, err
	}
	defer release()
	return restoreBrokerRecovery(func() error {
		if stopAllAntigravityProcesses("恢复上次中断的登录") != 0 {
			return fmt.Errorf("官方宿主仍在运行，无法恢复原凭据")
		}
		return nil
	}, func(raw []byte) error {
		if len(raw) == 0 {
			if err := deleteAntigravityCredentialRaw(); err != nil {
				return err
			}
			return ClearActiveAccount()
		}
		if err := writeAntigravityCredentialRaw(raw); err != nil {
			return err
		}
		email := emailFromCredentialPayload(raw)
		SetActiveAccount(email)
		SetPersistedActiveAccount(email)
		return nil
	}, readAntigravityCredentialRaw)
}
