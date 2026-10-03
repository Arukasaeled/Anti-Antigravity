package supervisor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type SessionItem struct {
	Store             string        `json:"store"`
	Storage           SessionSource `json:"storage"`
	Provider          string        `json:"provider"`
	Summary           string        `json:"summary,omitempty"`
	ProjectDir        string        `json:"project_dir,omitempty"`
	CreatedAt         string        `json:"created_at,omitempty"`
	MessagesAvailable bool          `json:"messages_available"`
	CanDelete         bool          `json:"can_delete"`
	ID                string        `json:"id"`
	Project           string        `json:"project"`
	Title             string        `json:"title"`
	UpdatedAt         string        `json:"updated_at"`
	Turns             int           `json:"turns"`
	Source            string        `json:"source"`
	Path              string        `json:"path,omitempty"`
}

type SessionsResult struct {
	Total    int           `json:"total"`
	Projects []string      `json:"projects"`
	Sessions []SessionItem `json:"sessions"`
}

type transcriptLine struct {
	StepIndex int    `json:"step_index"`
	Source    string `json:"source"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	Content   string `json:"content"`
	Thinking  string `json:"thinking,omitempty"`
}

var (
	userReqRegex = regexp.MustCompile(`(?s)<USER_REQUEST>([\s\S]*?)</USER_REQUEST>`)
	cwdRegex     = regexp.MustCompile(`(?i)"Cwd"\s*:\s*"?\\?"?([a-zA-Z]:(?:\\\\|\\|/)[^",]+)`)
	fileRegex    = regexp.MustCompile(`(?i)"(?:TargetFile|AbsolutePath|path)"\s*:\s*"?\\?"?([a-zA-Z]:(?:\\\\|\\|/)[^",]+)`)
)

// CleanSessionTitle 提取并清洗用户输入的第一句作为真实标题
func CleanSessionTitle(raw string) string {
	raw = strings.ReplaceAll(raw, "<USER_REQUEST>", "")
	raw = strings.ReplaceAll(raw, "</USER_REQUEST>", "")
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimLeft(l, "#>*-~`\"' \t\r")
		l = strings.TrimRight(l, "\"': \t\r")
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "<!--") {
			runes := []rune(l)
			if len(runes) > 55 {
				return string(runes[:55]) + "..."
			}
			return l
		}
	}
	return "无标题会话"
}

// resolveProjectName 从文件系统路径解析项目名
func resolveProjectName(rawPath string) string {
	rawPath = strings.ReplaceAll(rawPath, `\\`, `/`)
	rawPath = strings.ReplaceAll(rawPath, `\`, `/`)
	rawPath = strings.Trim(rawPath, `"' `)
	rawPath = strings.TrimRight(rawPath, `/`)

	if strings.Contains(strings.ToLower(rawPath), "/desktop") {
		return "Desktop"
	}
	// 当前 Windows 用户的用户名必须**动态取**，不能写死。
	// 历史实现把某个具体用户的用户名当成通用噪音段跳过 ——
	// 后果是任何用户名不是它的安装上，项目归属过滤里都会多出一项
	// 以用户名命名的假项目（路径 C:/Users/<me>/... 的最后一段被当成项目名）。
	// 这里改成把当前用户目录名加入噪音表；取不到就说明这张表少一项，
	// 但不会伪造出一个项目名。
	home, _ := os.UserHomeDir()
	homeLeaf := ""
	if home != "" {
		homeLeaf = strings.ToLower(filepath.Base(filepath.Clean(home)))
	}
	parts := strings.Split(rawPath, "/")
	for i := len(parts) - 1; i >= 1; i-- {
		p := parts[i]
		lower := strings.ToLower(p)
		if p == "" || strings.HasPrefix(lower, "@") || lower == "node_modules" || lower == ".git" ||
			lower == ".gemini" || lower == "antigravity" || lower == "scratch" || lower == "brain" ||
			lower == "users" || lower == "appdata" || lower == "roaming" || lower == "local" ||
			lower == "locallow" || lower == "programs" {
			continue
		}
		// 当前用户的主目录名同样跳过（它是路径噪音，不是项目名）。
		if homeLeaf != "" && lower == homeLeaf {
			continue
		}
		// 跳过 UUID 格式目录
		if len(p) == 36 && strings.Count(p, "-") == 4 {
			continue
		}
		// 若带文件后缀则跳过当前项取上级
		if strings.Contains(p, ".") {
			continue
		}
		return p
	}
	return ""
}

// ScanLocalSessions 扫描 Antigravity 宿主真实本地持久化数据
func ScanLocalSessions() SessionsResult {
	sessionMap := make(map[string]SessionItem)
	processes, processErr := CheckedHostProcesses()
	canDelete := processErr == nil && len(processes) == 0
	targets, _ := RuntimeManager.Snapshot()
	for _, target := range targets {
		if target.Connected {
			canDelete = false
		}
	}
	for _, root := range sessionStoreRoots() {
		ids := make(map[string]bool)
		if entries, err := os.ReadDir(filepath.Join(root, "brain")); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && validSessionID(entry.Name()) && entry.Name() != "tempmediaStorage" {
					ids[entry.Name()] = true
				}
			}
		}
		if entries, err := os.ReadDir(filepath.Join(root, "conversations")); err == nil {
			for _, entry := range entries {
				extension := strings.ToLower(filepath.Ext(entry.Name()))
				id := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
				if !entry.IsDir() && validSessionID(id) && (extension == ".db" || extension == ".pb" || extension == ".jsonl") {
					ids[id] = true
				}
			}
		}
		for id := range ids {
			// Pick a whole authoritative source. Never combine Brain from one root
			// with metadata/tokens from a backup that happens to share the same ID.
			if _, exists := sessionMap[id]; exists {
				continue
			}
			source, err := sessionSourceInRoot(id, root)
			if err != nil {
				continue
			}
			path := source.ConversationPath
			if path == "" {
				path = source.BrainPath
			}
			item := SessionItem{ID: id, Provider: "antigravity", Store: source.Store, Storage: source,
				Project: "未分类项目", Title: "Antigravity #" + id[:min(8, len(id))], Source: source.Store, Path: path,
				MessagesAvailable: source.TranscriptPath != "", CanDelete: canDelete}
			if info, err := os.Stat(path); err == nil {
				item.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
			}
			if source.BrainPath != "" {
				metadata := filepath.Join(source.BrainPath, ".system_generated", "metadata.json")
				if sessionPathWithin(root, metadata) {
					if raw, err := os.ReadFile(metadata); err == nil && len(raw) < 65536 {
						var meta struct {
							Title      string `json:"title"`
							Summary    string `json:"summary"`
							ProjectDir string `json:"project_dir"`
							CreatedAt  string `json:"created_at"`
						}
						if json.Unmarshal(raw, &meta) == nil {
							if meta.Title != "" {
								item.Title = CleanSessionTitle(meta.Title)
							}
							item.Summary, item.ProjectDir, item.CreatedAt = meta.Summary, meta.ProjectDir, meta.CreatedAt
						}
					}
				}
			}
			if filepath.Ext(source.ConversationPath) == ".db" {
				project, created, updated := readNativeSessionMetadata(source.ConversationPath)
				if project != "" {
					item.ProjectDir = project
				}
				if created != nil {
					item.CreatedAt = time.UnixMilli(*created).UTC().Format(time.RFC3339Nano)
				}
				if updated != nil {
					item.UpdatedAt = time.UnixMilli(*updated).UTC().Format(time.RFC3339Nano)
				}
			}
			if item.ProjectDir != "" {
				item.Project = resolveProjectName(item.ProjectDir)
			}
			sessionMap[id] = item
		}
	}
	for _, target := range targets {
		if target.Context == nil {
			continue
		}
		if item, ok := sessionMap[target.Context.SessionID]; ok {
			item.Title = strings.TrimSuffix(target.Title, " - Antigravity")
			if len(target.Context.WorkspaceDirs) > 0 {
				item.ProjectDir = workspaceURIPath(target.Context.WorkspaceDirs[0])
				item.Project = resolveProjectName(item.ProjectDir)
			}
			sessionMap[item.ID] = item
		}
	}
	sessions, projects := []SessionItem{}, make(map[string]bool)
	for _, item := range sessionMap {
		sessions = append(sessions, item)
		projects[item.Project] = true
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt > sessions[j].UpdatedAt })
	names := []string{}
	for project := range projects {
		if project != "" {
			names = append(names, project)
		}
	}
	sort.Strings(names)
	return SessionsResult{Total: len(sessions), Projects: names, Sessions: sessions}
}

// DeleteSession 物理删除指定会话目录与数据
func DeleteSession(id string, stores ...string) error {
	release, err := lockCredentialOperation()
	if err != nil {
		return err
	}
	defer release()
	if !validSessionID(id) {
		return fmt.Errorf("无效会话 ID；当前仅支持 Antigravity")
	}
	processes, processErr := CheckedHostProcesses()
	if processErr != nil {
		return processErr
	}
	if len(processes) > 0 {
		return fmt.Errorf("宿主运行期间禁用会话删除，以保护正在进行的任务")
	}
	targets, _ := RuntimeManager.Snapshot()
	for _, target := range targets {
		if target.Connected || target.Context != nil && time.Now().UnixMilli()-target.Context.SampledAt < 30000 {
			return fmt.Errorf("检测到活动宿主页面，禁用会话删除")
		}
	}
	store := ""
	if len(stores) > 0 {
		store = stores[0]
	}
	source, err := resolveSessionSource(id, store)
	if err != nil {
		return err
	}
	if source.BrainPath != "" {
		if !sessionPathWithin(source.Root, source.BrainPath) {
			return fmt.Errorf("会话路径超出存储根目录")
		}
		if err := os.RemoveAll(source.BrainPath); err != nil {
			return err
		}
	}
	for _, suffix := range []string{".pb", ".db", ".db-wal", ".db-shm", ".jsonl"} {
		path := filepath.Join(source.Root, "conversations", id+suffix)
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if !sessionPathWithin(source.Root, path) {
			return fmt.Errorf("会话文件超出存储根目录")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	verify := []string{filepath.Join(source.Root, "brain", id)}
	for _, suffix := range []string{".pb", ".db", ".db-wal", ".db-shm", ".jsonl"} {
		verify = append(verify, filepath.Join(source.Root, "conversations", id+suffix))
	}
	for _, path := range verify {
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("会话删除未完成")
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	nativeUsageCache.Lock()
	delete(nativeUsageCache.entries, id+"|"+source.Store)
	nativeUsageCache.Unlock()
	return nil
}

// ExportSessionMarkdown 将指定会话日志导出为 Markdown 内容
func ExportSessionMarkdown(id string, stores ...string) (string, error) {
	if strings.HasPrefix(id, "codex:") {
		return "", fmt.Errorf("主会话审计仅支持 Antigravity")
	}
	if !validSessionID(id) {
		return "", fmt.Errorf("无效会话 ID")
	}
	store := ""
	if len(stores) > 0 {
		store = stores[0]
	}
	source, err := resolveSessionSource(id, store)
	if err != nil {
		return "", err
	}
	if source.TranscriptPath == "" {
		return "", fmt.Errorf("此来源没有可导出的消息记录")
	}
	logPath := source.TranscriptPath
	f, err := os.Open(logPath)
	if err != nil {
		return "", fmt.Errorf("找不到该会话日志: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# 2Ag Session Export: %s\n\n", id))
	sb.WriteString(fmt.Sprintf("- **UUID**: `%s`\n", id))
	sb.WriteString(fmt.Sprintf("- **Exported At**: `%s`\n\n---\n\n", time.Now().Format("2006-01-02 15:04:05")))

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	step := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item transcriptLine
		if err := json.Unmarshal(line, &item); err == nil {
			step++
			role := "System"
			if item.Source == "USER_EXPLICIT" || item.Type == "USER_INPUT" {
				role = "User"
			} else if item.Source == "MODEL" || item.Type == "PLANNER_RESPONSE" {
				role = "Assistant"
			}
			sb.WriteString(fmt.Sprintf("### %d. %s\n", step, role))
			if item.CreatedAt != "" {
				sb.WriteString(fmt.Sprintf("*Time: %s*\n\n", item.CreatedAt))
			}
			content := item.Content
			if match := userReqRegex.FindStringSubmatch(content); len(match) > 1 {
				content = strings.TrimSpace(match[1])
			}
			if content != "" {
				sb.WriteString(content + "\n\n")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
