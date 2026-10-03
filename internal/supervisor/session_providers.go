package supervisor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type SessionProvider interface {
	Name() string
	List() []SessionItem
	Preview(string) (SessionPreview, error)
}

type SessionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	At      string `json:"at,omitempty"`
}
type SessionPreview struct {
	ID        string           `json:"id"`
	Provider  string           `json:"provider"`
	Messages  []SessionMessage `json:"messages"`
	Truncated bool             `json:"truncated"`
}

type antigravitySessionProvider struct{}

func (antigravitySessionProvider) Name() string        { return "antigravity" }
func (antigravitySessionProvider) List() []SessionItem { return ScanLocalSessions().Sessions }
func (antigravitySessionProvider) Preview(id string) (SessionPreview, error) {
	if !validSessionID(id) {
		return SessionPreview{}, fmt.Errorf("invalid session ID")
	}
	return previewAntigravitySession(id, "")
}

type codexSessionProvider struct{}

func (codexSessionProvider) Name() string { return "codex" }

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validSessionID(id string) bool { return sessionIDPattern.MatchString(id) }

var codexUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func codexHomePath() string {
	if path := os.Getenv("CODEX_HOME"); path != "" {
		if absolute, err := filepath.Abs(path); err == nil {
			return absolute
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".codex")
	}
	return ""
}

// Directory entries and the native session_index only. No JSONL message bodies in List.
func codexSessionFiles() map[string]string {
	result := make(map[string]string)
	root := codexHomePath()
	if root == "" {
		return result
	}
	for _, directory := range []string{"sessions", "archived_sessions"} {
		_ = filepath.WalkDir(filepath.Join(root, directory), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
				return nil
			}
			id := codexUUIDPattern.FindString(strings.TrimSuffix(entry.Name(), ".jsonl"))
			if id != "" {
				result[id] = path
			}
			return nil
		})
	}
	return result
}

func (codexSessionProvider) List() []SessionItem {
	files := codexSessionFiles()
	metadata := make(map[string]SessionItem)
	if root := codexHomePath(); root != "" {
		if file, err := os.Open(filepath.Join(root, "session_index.jsonl")); err == nil {
			scanner := bufio.NewScanner(io.LimitReader(file, 16<<20))
			scanner.Buffer(make([]byte, 4096), 1<<20)
			for scanner.Scan() {
				var row struct {
					ID         string `json:"id"`
					ThreadName string `json:"thread_name"`
					UpdatedAt  string `json:"updated_at"`
					Cwd        string `json:"cwd"`
					CreatedAt  string `json:"created_at"`
				}
				if json.Unmarshal(scanner.Bytes(), &row) == nil && validSessionID(row.ID) {
					metadata[row.ID] = SessionItem{Title: row.ThreadName, UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt, ProjectDir: row.Cwd}
				}
			}
			_ = file.Close()
		}
	}
	result := []SessionItem{}
	for id, path := range files {
		item := metadata[id]
		item.ID, item.Provider, item.Source, item.Path = "codex:"+id, "codex", "Codex native metadata", path
		item.MessagesAvailable = true
		if item.Title == "" {
			item.Title = "Codex #" + id[:8]
		}
		if info, err := os.Stat(path); err == nil && item.UpdatedAt == "" {
			item.UpdatedAt = info.ModTime().Format("2006-01-02 15:04:05")
		}
		if item.ProjectDir != "" {
			item.Project = resolveProjectName(item.ProjectDir)
		} else {
			item.Project = "未分类项目"
		}
		result = append(result, item)
	}
	return result
}

func (codexSessionProvider) Preview(id string) (SessionPreview, error) {
	if !validSessionID(id) {
		return SessionPreview{}, fmt.Errorf("invalid session ID")
	}
	path := codexSessionFiles()[id]
	if path == "" {
		return SessionPreview{}, fmt.Errorf("session file unavailable")
	}
	return readSessionMessages(path, "codex:"+id, "codex", 120)
}

func ScanAISessions() SessionsResult {
	items, projects := []SessionItem{}, make(map[string]bool)
	for _, provider := range []SessionProvider{antigravitySessionProvider{}} {
		for _, item := range provider.List() {
			if updated := sessionTimestamp(item.UpdatedAt); !updated.IsZero() {
				item.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
			}
			if created := sessionTimestamp(item.CreatedAt); !created.IsZero() {
				item.CreatedAt = created.UTC().Format(time.RFC3339Nano)
			}
			items = append(items, item)
			projects[item.Project] = true
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return sessionTimestamp(items[i].UpdatedAt).After(sessionTimestamp(items[j].UpdatedAt))
	})
	projectNames := []string{}
	for project := range projects {
		if project != "" {
			projectNames = append(projectNames, project)
		}
	}
	sort.Strings(projectNames)
	return SessionsResult{Total: len(items), Projects: projectNames, Sessions: items}
}

func sessionTimestamp(value string) time.Time {
	if timestamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return timestamp
	}
	timestamp, _ := time.ParseInLocation("2006-01-02 15:04:05", value, time.Local)
	return timestamp
}

func PreviewAISession(id string, stores ...string) (SessionPreview, error) {
	if strings.HasPrefix(id, "codex:") {
		return SessionPreview{}, fmt.Errorf("主会话审计仅支持 Antigravity")
	}
	store := ""
	if len(stores) > 0 {
		store = stores[0]
	}
	return previewAntigravitySession(id, store)
}

func previewAntigravitySession(id, store string) (SessionPreview, error) {
	source, err := resolveSessionSource(id, store)
	if err != nil {
		return SessionPreview{}, err
	}
	if source.TranscriptPath == "" {
		return SessionPreview{}, fmt.Errorf("此来源没有可预览的消息记录")
	}
	return readSessionMessages(source.TranscriptPath, id, "antigravity", 120)
}

func readSessionMessages(path, id, provider string, limit int) (SessionPreview, error) {
	result := SessionPreview{ID: id, Provider: provider, Messages: []SessionMessage{}}
	file, err := os.Open(path)
	if err != nil {
		return result, fmt.Errorf("可读消息不可用；此版本可能仅保存 protobuf / SQLite")
	}
	defer file.Close()
	info, _ := file.Stat()
	if info != nil && info.Size() > 16<<20 {
		result.Truncated = true
	}
	scanner := bufio.NewScanner(io.LimitReader(file, 16<<20))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var message SessionMessage
		if provider == "antigravity" {
			var row transcriptLine
			if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Content == "" {
				continue
			}
			role := "tool"
			if row.Type == "USER_INPUT" {
				role = "user"
			} else if row.Type == "PLANNER_RESPONSE" {
				role = "assistant"
			} else if row.Type == "SYSTEM_MESSAGE" {
				role = "system"
			}
			message = SessionMessage{Role: role, Content: row.Content, At: row.CreatedAt}
		} else {
			var row struct {
				Type      string `json:"type"`
				Timestamp string `json:"timestamp"`
				Payload   struct {
					Type    string `json:"type"`
					Role    string `json:"role"`
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
					Output    json.RawMessage `json:"output"`
					Name      string          `json:"name"`
					Arguments string          `json:"arguments"`
				} `json:"payload"`
			}
			if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Type != "response_item" {
				continue
			}
			message.Role, message.At = row.Payload.Role, row.Timestamp
			for _, part := range row.Payload.Content {
				message.Content += part.Text + "\n"
			}
			if row.Payload.Type == "function_call" {
				message.Role, message.Content = "tool call", row.Payload.Name+"\n"+row.Payload.Arguments
			}
			if row.Payload.Type == "function_call_output" {
				message.Role = "tool"
				if json.Unmarshal(row.Payload.Output, &message.Content) != nil {
					message.Content = string(row.Payload.Output)
				}
			}
			if message.Content == "" {
				continue
			}
		}
		if len(result.Messages) >= limit {
			result.Truncated = true
			break
		}
		if len(message.Content) > 64000 {
			message.Content = message.Content[:64000] + "\n[预览截断]"
			result.Truncated = true
		}
		result.Messages = append(result.Messages, message)
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func exportCodexSession(id string) (string, error) {
	if !validSessionID(id) {
		return "", fmt.Errorf("invalid session ID")
	}
	path := codexSessionFiles()[id]
	if path == "" {
		return "", fmt.Errorf("session file unavailable")
	}
	preview, err := readSessionMessages(path, "codex:"+id, "codex", 10000)
	if err != nil {
		return "", err
	}
	if preview.Truncated {
		return "", fmt.Errorf("该会话超出当前导出上限；不会导出不完整记录")
	}
	var text strings.Builder
	fmt.Fprintf(&text, "# Codex Session %s\n\n", id)
	for _, message := range preview.Messages {
		fmt.Fprintf(&text, "## %s\n\n%s\n\n", message.Role, message.Content)
	}
	return text.String(), nil
}
