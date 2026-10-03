package supervisor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The read-only discovery boundary shared by sessions, skills, CDP and Doctor.
type AntigravityEnvironment struct {
	ConversationStores  []string `json:"conversation_stores"`
	StorageRoot         string   `json:"storage_root"`
	BrainRoot           string   `json:"brain_root"`
	ConversationsRoot   string   `json:"conversations_root"`
	UserDataRoot        string   `json:"user_data_root"`
	ProfileRoots        []string `json:"profile_roots"`
	WorkspaceStorage    []string `json:"workspace_storage"`
	CredentialSlot      string   `json:"credential_slot"`
	CredentialStorage   string   `json:"credential_storage"`
	GlobalSkillsRoot    string   `json:"global_skills_root"`
	ConversationFormats []string `json:"conversation_formats"`
	LanguageServer      string   `json:"language_server"`
	Version             string   `json:"version"`
}

func DetectAntigravityEnvironment() AntigravityEnvironment {
	env := AntigravityEnvironment{ConversationStores: discoverConversationStores(), CredentialSlot: "gemini:antigravity", ProfileRoots: []string{}, WorkspaceStorage: []string{}, ConversationFormats: []string{}}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		env.StorageRoot = filepath.Join(home, ".gemini", "antigravity")
		env.BrainRoot = filepath.Join(env.StorageRoot, "brain")
		env.ConversationsRoot = filepath.Join(env.StorageRoot, "conversations")
		env.GlobalSkillsRoot = filepath.Join(env.StorageRoot, "skills")
		if entries, err := os.ReadDir(filepath.Join(home, ".2ag", "profiles")); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					env.ProfileRoots = append(env.ProfileRoots, filepath.Join(home, ".2ag", "profiles", entry.Name()))
				}
			}
		}
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		env.UserDataRoot = filepath.Join(appData, "Antigravity")
	}
	if paths, err := FindAntigravity(); err == nil {
		env.UserDataRoot, env.CredentialStorage = paths.UserData, paths.Credentials
		candidate := filepath.Join(paths.Root, "resources", "bin", "language_server.exe")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			env.LanguageServer = candidate
		}
		env.Version = detectEnvironmentVersion(paths.Executable)
	}
	for _, root := range append([]string{env.UserDataRoot}, env.ProfileRoots...) {
		if root == "" {
			continue
		}
		for _, candidate := range []string{filepath.Join(root, "User", "workspaceStorage"), filepath.Join(root, "workspaceStorage")} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				env.WorkspaceStorage = append(env.WorkspaceStorage, candidate)
			}
		}
	}
	formats := make(map[string]bool)
	for _, conversationRoot := range env.ConversationStores {
		if entries, err := os.ReadDir(conversationRoot); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				switch strings.ToLower(filepath.Ext(entry.Name())) {
				case ".pb":
					formats["protobuf"] = true
				case ".db":
					formats["sqlite"] = true
				case ".jsonl":
					formats["jsonl"] = true
				}
			}
		}
	}
	if info, err := os.Stat(env.BrainRoot); env.BrainRoot != "" && err == nil && info.IsDir() {
		formats["brain/transcript.jsonl"] = true
	}
	for format := range formats {
		env.ConversationFormats = append(env.ConversationFormats, format)
	}
	sort.Strings(env.ConversationFormats)
	return env
}
