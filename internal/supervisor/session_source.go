package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
)

// Store is a discovered root identifier, never a caller-supplied filesystem path.
type SessionSource struct {
	Store            string `json:"store"`
	Root             string `json:"root"`
	BrainPath        string `json:"brain_path,omitempty"`
	ConversationPath string `json:"conversation_path,omitempty"`
	TranscriptPath   string `json:"transcript_path,omitempty"`
}

func sessionStoreRoots() []string {
	roots := []string{}
	for _, conversations := range discoverConversationStores() {
		roots = append(roots, filepath.Dir(conversations))
	}
	primary := DetectPrimarySessionRoot()
	if primary != "" {
		found := false
		for _, root := range roots {
			if root == primary {
				found = true
			}
		}
		if !found {
			if info, err := os.Stat(filepath.Join(primary, "brain")); err == nil && info.IsDir() {
				roots = append([]string{primary}, roots...)
			}
		}
	}
	return roots
}

func DetectPrimarySessionRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity")
}

func sessionPathWithin(root, path string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(realRoot, realPath)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) && len(relative) > 0 && relative != "." && (len(relative) < 3 || relative[:3] != ".."+string(filepath.Separator))
}

func sessionSourceInRoot(id, root string) (SessionSource, error) {
	source := SessionSource{Store: filepath.Base(root), Root: root}
	brain := filepath.Join(root, "brain", id)
	if info, err := os.Stat(brain); err == nil && info.IsDir() {
		if !sessionPathWithin(root, brain) {
			return source, fmt.Errorf("会话 Brain 路径超出存储根目录")
		}
		source.BrainPath = brain
		transcript := filepath.Join(brain, ".system_generated", "logs", "transcript.jsonl")
		if info, err := os.Stat(transcript); err == nil && info.Mode().IsRegular() && sessionPathWithin(root, transcript) {
			source.TranscriptPath = transcript
		}
	}
	for _, extension := range []string{".db", ".pb", ".jsonl"} {
		path := filepath.Join(root, "conversations", id+extension)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			if !sessionPathWithin(root, path) {
				return source, fmt.Errorf("会话文件超出存储根目录")
			}
			if source.ConversationPath == "" {
				source.ConversationPath = path
			}
		}
	}
	if source.BrainPath == "" && source.ConversationPath == "" {
		return source, fmt.Errorf("该来源中找不到会话")
	}
	return source, nil
}

func resolveSessionSource(id, store string) (SessionSource, error) {
	if !validSessionID(id) {
		return SessionSource{}, fmt.Errorf("无效 Antigravity 会话 ID")
	}
	for _, root := range sessionStoreRoots() {
		if store != "" && store != filepath.Base(root) {
			continue
		}
		if source, err := sessionSourceInRoot(id, root); err == nil {
			return source, nil
		} else if store != "" {
			return source, err
		}
	}
	return SessionSource{}, fmt.Errorf("会话或存储来源不可用")
}
