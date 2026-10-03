package supervisor

import (
	"os"
	"path/filepath"
)

func refreshCandidatePath(email string) string {
	return filepath.Join(vaultDirPath(), ".renewal-"+vaultAccountID(email)+".pending")
}

func clearRefreshCandidate(email string) error {
	err := os.Remove(refreshCandidatePath(email))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
