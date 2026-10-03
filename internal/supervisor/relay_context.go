package supervisor

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// An archived login may have an expired access/id token. The native host owns
// refresh; unlike broker completion, restore requires identity and refresh
// material, not a just-issued ID token. Quota routing separately requires live
// reads, so expired/unavailable quota is never treated as remaining capacity.
func credentialRestorableEmail(raw []byte) (string, bool) {
	view, ok := parseCredentialBlobView(raw)
	if !ok || view.Token == nil || strings.TrimSpace(view.Token.AccessToken) == "" || strings.TrimSpace(view.Token.RefreshToken) == "" {
		return "", false
	}
	claims := decodeJWTPayload(view.IDToken)
	email, _ := claims["email"].(string)
	email = strings.TrimSpace(email)
	if !strings.Contains(email, "@") {
		return "", false
	}
	if view.Email != "" && !strings.EqualFold(strings.TrimSpace(view.Email), email) {
		return "", false
	}
	return email, true
}

func AccountCredentialReady(email string) bool {
	raw, err := ReadVaultCredential(email)
	if err != nil {
		return false
	}
	owner, ok := credentialRestorableEmail(raw)
	return ok && strings.EqualFold(owner, strings.TrimSpace(email))
}

// Only return paths actually declared by the host's workspace metadata. A
// project name is a match key, never a substitute filesystem path.
func RelayWorkspacePath(email, explicit, projectName string) string {
	valid := func(path string) bool {
		info, err := os.Stat(path)
		return filepath.IsAbs(path) && err == nil && info.IsDir()
	}
	if explicit != "" && valid(explicit) {
		return explicit
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, ".2ag", "profiles", sanitizeEmail(email), "User", "workspaceStorage")
	dirs, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	paths := make(map[string]string)
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, dir.Name(), "workspace.json"))
		if err != nil {
			continue
		}
		var workspace struct {
			Folder string `json:"folder"`
		}
		if json.Unmarshal(raw, &workspace) != nil {
			continue
		}
		uri, err := url.Parse(workspace.Folder)
		if err != nil || uri.Scheme != "file" {
			continue
		}
		path, err := url.PathUnescape(uri.EscapedPath())
		if err != nil {
			continue
		}
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:]
		}
		path = filepath.FromSlash(path)
		if uri.Host != "" && uri.Host != "localhost" {
			path = `\\` + uri.Host + path
		}
		if !valid(path) || (projectName != "" && !strings.EqualFold(filepath.Base(path), projectName)) {
			continue
		}
		paths[strings.ToLower(filepath.Clean(path))] = path
	}
	if len(paths) == 1 {
		for _, path := range paths {
			return path
		}
	}
	return ""
}
