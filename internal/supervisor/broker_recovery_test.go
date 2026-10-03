//go:build windows

package supervisor

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"testing"
)

func TestCredentialOperationCrossProcessLock(t *testing.T) {
	if os.Getenv("TWOAG_LOCK_CHILD") == "1" {
		release, err := lockCredentialOperation()
		if err == nil {
			release()
			t.Fatal("second process acquired active broker lock")
		}
		return
	}
	isolateHome(t)
	release, err := lockCredentialOperation()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCredentialOperationCrossProcessLock$")
	cmd.Env = append(os.Environ(), "TWOAG_LOCK_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cross-process lock failed: %s", out)
	}
}

func TestBrokerRecoveryAfterProcessLoss(t *testing.T) {
	isolateHome(t)
	original := []byte(`{"token":{"refresh_token":"fixture-original"},"email":"original@example.com"}`)
	if err := saveBrokerRecovery(original, "original@example.com"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(brokerRecoveryPath())
	if bytes.Contains(data, []byte("fixture-original")) {
		t.Fatal("plaintext credential leaked")
	}
	var current []byte // a new process no longer has origRaw
	recovered, err := restoreBrokerRecovery(func() error { return nil }, func(raw []byte) error { current = append([]byte(nil), raw...); return nil }, func() ([]byte, error) { return current, nil })
	if err != nil || !recovered || !bytes.Equal(current, original) {
		t.Fatal("original credential did not survive process loss")
	}
	if _, err := os.Stat(brokerRecoveryPath()); !os.IsNotExist(err) {
		t.Fatal("recovery record not cleared")
	}
}

func TestBrokerRecoveryRetainedOnFailure(t *testing.T) {
	for _, phase := range []string{"stop", "write", "readback"} {
		t.Run(phase, func(t *testing.T) {
			isolateHome(t)
			raw := []byte(`{"email":"original@example.com"}`)
			if err := saveBrokerRecovery(raw, "original@example.com"); err != nil {
				t.Fatal(err)
			}
			reads := 0
			_, err := restoreBrokerRecovery(func() error {
				if phase == "stop" {
					return errors.New("busy")
				}
				return nil
			}, func([]byte) error {
				if phase == "write" {
					return errors.New("denied")
				}
				return nil
			}, func() ([]byte, error) {
				reads++
				if phase == "readback" && reads > 1 {
					return nil, nil
				}
				return nil, nil
			})
			if err == nil {
				t.Fatal("failure must be reported")
			}
			if _, err := os.Stat(brokerRecoveryPath()); err != nil {
				t.Fatal("retry record lost")
			}
			if err := saveBrokerRecovery(raw, "other@example.com"); err == nil {
				t.Fatal("pending recovery overwritten")
			}
		})
	}
}
