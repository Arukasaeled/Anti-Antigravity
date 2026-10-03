//go:build windows

package supervisor

import (
	"bytes"
	"fmt"
	"log"
	"time"
)

// Update only existing, complete logins. Native logout and transient OAuth
// placeholders never recreate accounts or replace a healthy encrypted backup.
func CheckpointCurrentCredential() error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	raw, err := readAntigravityCredentialRaw()
	if err != nil {
		return err
	}
	email := nativeCredentialOwner(raw)
	if email == "" {
		return nil
	}
	old, err := ReadVaultCredential(email)
	if err != nil {
		return nil
	}
	if bytes.Equal(old, raw) {
		return nil
	}
	complete, err := withArchivedCredentialIdentity(raw, old, email)
	if err != nil || bytes.Equal(old, complete) || !nativeCredentialReady(complete, ReadNativeAuthState(), email) {
		return nil
	}
	if _, err := StoreVaultCredential(email, displayNameFromCredentialPayload(complete), complete); err != nil {
		return fmt.Errorf("更新原生刷新凭据失败: %w", err)
	}
	return nil
}

func StartCredentialCheckpoint(stop <-chan struct{}) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := CheckpointCurrentCredential(); err != nil {
				log.Printf("[2ag] 登录凭据同步暂缓: %v", err)
			}
		}
	}
}
