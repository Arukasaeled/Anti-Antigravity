//go:build windows

package supervisor

import (
	"bytes"
	"os"
	"testing"
)

func TestPendingRefreshIsEncryptedBoundAndExcludedFromVault(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	parent := fixtureCredential("target@example.com", false)
	fresh, err := mergeNativeRefresh(parent, credentialRefreshResponse{Access: "fixture-new-access", Refresh: "fixture-rotated-refresh", Expires: 3600}, quotaNow())
	if err != nil {
		t.Fatal(err)
	}
	if err = saveRefreshCandidate("target@example.com", parent, fresh); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(refreshCandidatePath("target@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if !isEncryptedVaultBlob(sealed) || bytes.Contains(sealed, []byte("fixture-rotated-refresh")) {
		t.Fatal("candidate must be DPAPI encrypted")
	}
	if len(ListVaultAccountEntries()) != 0 {
		t.Fatal("unverified candidate must not appear as a Vault account")
	}
	got, err := readRefreshCandidate("target@example.com", parent)
	if err != nil || !bytes.Equal(got, fresh) {
		t.Fatal("rotation not recoverable across a second read")
	}
	got, err = readRefreshCandidate("target@example.com", fresh)
	if err != nil || len(got) != 0 {
		t.Fatal("candidate from a different parent grant must not override a new login")
	}
}
