//go:build windows

package supervisor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type refreshCandidate struct {
	Parent     string          `json:"parent"`
	Credential json.RawMessage `json:"credential"`
}

func refreshParentHash(raw []byte) string {
	v, ok := parseCredentialBlobView(raw)
	if !ok || v.Token == nil {
		return ""
	}
	h := sha256.Sum256([]byte(v.Token.RefreshToken))
	return hex.EncodeToString(h[:])
}

func saveRefreshCandidate(email string, parent, raw []byte) error {
	data, err := json.Marshal(refreshCandidate{refreshParentHash(parent), raw})
	if err != nil {
		return err
	}
	sealed, err := encryptVaultPayload(data)
	if err != nil {
		return err
	}
	path := refreshCandidatePath(email)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeFileAtomic(path, sealed, 0600)
}

func readRefreshCandidate(email string, parent []byte) ([]byte, error) {
	sealed, err := os.ReadFile(refreshCandidatePath(email))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data, legacy, err := decryptVaultPayload(sealed)
	if err != nil {
		return nil, err
	}
	if legacy {
		return nil, fmt.Errorf("unencrypted refresh candidate")
	}
	var candidate refreshCandidate
	if json.Unmarshal(data, &candidate) != nil {
		return nil, fmt.Errorf("invalid refresh candidate")
	}
	// A newer manual import replaces the original grant. Do not resurrect a
	// pending attempt from before that re-login.
	if candidate.Parent != refreshParentHash(parent) {
		return nil, clearRefreshCandidate(email)
	}
	if len(candidate.Credential) == 0 {
		return nil, fmt.Errorf("empty refresh candidate")
	}
	return candidate.Credential, nil
}
