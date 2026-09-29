package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type InstallPaths struct {
	Executable    string
	Root          string
	EntryHTML     string
	UserData      string
	Credentials   string
	MCPConfigPath string
}

func FindAntigravity() (InstallPaths, error) {
	if runtime.GOOS != "windows" {
		return InstallPaths{}, errors.New("Antigravity path discovery is supported on Windows only")
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	appData := os.Getenv("APPDATA")
	roots := make([]string, 0, 8)
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Join(cwd, "app"))
	}
	if executable, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Join(filepath.Dir(executable), "app"))
	}
	if localAppData != "" {
		roots = append(roots,
			filepath.Join(localAppData, "Programs", "Antigravity"),
			filepath.Join(localAppData, "Antigravity"),
		)
	}
	if programFiles := os.Getenv("ProgramFiles"); programFiles != "" {
		roots = append(roots, filepath.Join(programFiles, "Antigravity"))
	}
	if programFilesX86 := os.Getenv("ProgramFiles(x86)"); programFilesX86 != "" {
		roots = append(roots, filepath.Join(programFilesX86, "Antigravity"))
	}
	exeCandidates := []string{"antigravity.exe", "Antigravity.exe"}
	var executable string
	var root string
	for _, candidateRoot := range roots {
		if candidateRoot == "" {
			continue
		}
		for _, name := range exeCandidates {
			candidate := filepath.Join(candidateRoot, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				executable, root = candidate, candidateRoot
				break
			}
		}
		if executable != "" {
			break
		}
	}
	if executable == "" {
		if localAppData == "" {
			return InstallPaths{}, errors.New("Antigravity executable not found and LOCALAPPDATA is not set")
		}
		return InstallPaths{}, fmt.Errorf("Antigravity executable not found under %s", localAppData)
	}
	entry := findEntryHTML(root)
	userData := filepath.Join(appData, "Antigravity")
	if appData == "" {
		userData = filepath.Join(localAppData, "Antigravity")
	}
	return InstallPaths{
		Executable:    executable,
		Root:          root,
		EntryHTML:     entry,
		UserData:      userData,
		Credentials:   firstExisting(filepath.Join(userData, "User", "globalStorage", "state.vscdb"), filepath.Join(userData, "User", "globalStorage", "storage.json"), filepath.Join(userData, "credentials.json")),
		MCPConfigPath: firstExisting(filepath.Join(userData, "mcp.json"), filepath.Join(userData, "User", "mcp.json"), filepath.Join(userData, "config.json")),
	}, nil
}

func firstExisting(candidates ...string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func findEntryHTML(root string) string {
	candidates := []string{
		filepath.Join(root, "resources", "app", "index.html"),
		filepath.Join(root, "resources", "app", "dist", "index.html"),
		filepath.Join(root, "resources", "app", "out", "index.html"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
