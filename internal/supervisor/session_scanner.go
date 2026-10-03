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
	Provider          string `json:"provider"`
	Summary           string `json:"summary,omitempty"`
	ProjectDir        string `json:"project_dir,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	MessagesAvailable bool   `json:"messages_available"`
	CanDelete         bool   `json:"can_delete"`
	ID                string `json:"id"`
	Project           string `json:"project"`
	Title             string `json:"title"`
	UpdatedAt         string `json:"updated_at"`
	Turns             int    `json:"turns"`
	Source            string `json:"source"`
	Path              string `json:"path,omitempty"`
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
	environment := DetectAntigravityEnvironment()
	sessionMap := make(map[string]SessionItem)
	if environment.BrainRoot != "" {
		if entries, err := os.ReadDir(environment.BrainRoot); err == nil {
			for _, entry := range entries {
				id := entry.Name()
				if !entry.IsDir() || !validSessionID(id) || id == "tempmediaStorage" {
					continue
				}
				path := filepath.Join(environment.BrainRoot, id)
				info, err := entry.Info()
				if err != nil {
					continue
				}
				item := SessionItem{ID: id, Provider: "antigravity", Project: "未分类项目", Title: "Antigravity #" + id[:min(8, len(id))], UpdatedAt: info.ModTime().Format("2006-01-02 15:04:05"), Source: "Brain metadata", Path: path}
				logPath := filepath.Join(path, ".system_generated", "logs", "transcript.jsonl")
				if logInfo, err := os.Stat(logPath); err == nil {
					item.MessagesAvailable = true
					item.UpdatedAt = logInfo.ModTime().Format("2006-01-02 15:04:05")
				}
				// Native metadata is optional. Never scan transcript bodies during list loading.
				if raw, err := os.ReadFile(filepath.Join(path, ".system_generated", "metadata.json")); err == nil && len(raw) < 65536 {
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
						if meta.ProjectDir != "" {
							item.Project = resolveProjectName(meta.ProjectDir)
						}
					}
				}
				sessionMap[id] = item
			}
		}
	}
	if environment.ConversationsRoot != "" {
		if files, err := os.ReadDir(environment.ConversationsRoot); err == nil {
			for _, file := range files {
				extension := strings.ToLower(filepath.Ext(file.Name()))
				if file.IsDir() || extension != ".pb" && extension != ".db" && extension != ".jsonl" {
					continue
				}
				id := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))
				if !validSessionID(id) {
					continue
				}
				info, err := file.Info()
				if err != nil {
					continue
				}
				item, exists := sessionMap[id]
				if !exists {
					item = SessionItem{ID: id, Provider: "antigravity", Project: "未分类项目", Title: "Antigravity #" + id[:min(8, len(id))], Source: "Conversations " + extension, Path: filepath.Join(environment.ConversationsRoot, file.Name())}
				}
				updated := info.ModTime().Format("2006-01-02 15:04:05")
				if updated > item.UpdatedAt {
					item.UpdatedAt = updated
				}
				sessionMap[id] = item
			}
		}
	}
	targets, _ := RuntimeManager.Snapshot()
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
	projectNames := []string{}
	for project := range projects {
		if project != "" {
			projectNames = append(projectNames, project)
		}
	}
	sort.Strings(projectNames)
	return SessionsResult{Total: len(sessions), Projects: projectNames, Sessions: sessions}
}

// DeleteSession 物理删除指定会话目录与数据
func DeleteSession(id string) error {
	if !validSessionID(id) {
		return fmt.Errorf("无效会话 ID；当前仅支持 Antigravity")
	}
	if ProbeRealHost().ProcessFound {
		return fmt.Errorf("宿主运行期间禁用会话删除，以保护正在进行的任务")
	}
	targets, _ := RuntimeManager.Snapshot()
	for _, target := range targets {
		if target.Connected || target.Context != nil && time.Now().UnixMilli()-target.Context.SampledAt < 30000 {
			return fmt.Errorf("检测到活动宿主页面，禁用会话删除")
		}
	}
	environment := DetectAntigravityEnvironment()
	if environment.BrainRoot == "" || environment.ConversationsRoot == "" {
		return fmt.Errorf("存储目录不可用")
	}
	brainPath := filepath.Join(environment.BrainRoot, id)
	// A symlink/junction must never make recursive deletion escape the discovered storage root.
	resolvedRoot, rootErr := filepath.EvalSymlinks(environment.BrainRoot)
	resolvedPath, pathErr := filepath.EvalSymlinks(brainPath)
	if rootErr == nil && pathErr == nil {
		relative, err := filepath.Rel(resolvedRoot, resolvedPath)
		if err != nil || relative != id {
			return fmt.Errorf("会话路径超出存储根目录")
		}
		if err := os.RemoveAll(brainPath); err != nil {
			return err
		}
	} else if pathErr != nil && !os.IsNotExist(pathErr) {
		return pathErr
	}
	for _, suffix := range []string{".pb", ".db", ".db-wal", ".db-shm"} {
		if err := os.Remove(filepath.Join(environment.ConversationsRoot, id+suffix)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ExportSessionMarkdown 将指定会话日志导出为 Markdown 内容
func ExportSessionMarkdown(id string) (string, error) {
	if strings.HasPrefix(id, "codex:") {
		return exportCodexSession(strings.TrimPrefix(id, "codex:"))
	}
	if !validSessionID(id) {
		return "", fmt.Errorf("无效会话 ID")
	}
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return "", fmt.Errorf("无法获取用户主目录")
	}
	logPath := filepath.Join(DetectAntigravityEnvironment().BrainRoot, id, ".system_generated", "logs", "transcript.jsonl")
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
