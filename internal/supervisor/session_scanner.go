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
	ID        string `json:"id"`
	Project   string `json:"project"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
	Turns     int    `json:"turns"`
	Source    string `json:"source"`
	Path      string `json:"path,omitempty"`
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
	parts := strings.Split(rawPath, "/")
	for i := len(parts) - 1; i >= 1; i-- {
		p := parts[i]
		lower := strings.ToLower(p)
		if p == "" || strings.HasPrefix(lower, "@") || lower == "node_modules" || lower == ".git" ||
			lower == ".gemini" || lower == "antigravity" || lower == "scratch" || lower == "brain" ||
			lower == "users" || lower == "user" || lower == "appdata" || lower == "roaming" || lower == "local" || lower == "locallow" || lower == "programs" {
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

	// 1. 扫描 %USERPROFILE%\.gemini\antigravity\brain
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		brainDir := filepath.Join(homeDir, ".gemini", "antigravity", "brain")
		if entries, err := os.ReadDir(brainDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() || entry.Name() == "tempmediaStorage" || strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				id := entry.Name()
				fullPath := filepath.Join(brainDir, id)
				logPath := filepath.Join(fullPath, ".system_generated", "logs", "transcript.jsonl")

				title := "新会话"
				turns := 0
				project := ""
				modTime := time.Now()
				if fi, err := entry.Info(); err == nil {
					modTime = fi.ModTime()
				}

				if f, err := os.Open(logPath); err == nil {
					if lfi, err := f.Stat(); err == nil {
						modTime = lfi.ModTime()
					}
					scanner := bufio.NewScanner(f)
					buf := make([]byte, 1024*1024)
					scanner.Buffer(buf, 10*1024*1024)

					foundTitle := false
					for scanner.Scan() {
						line := scanner.Bytes()
						if len(line) == 0 {
							continue
						}
						turns++
						lineStr := string(line)

						// 尝试提取项目路径
						if project == "" {
							if m := cwdRegex.FindStringSubmatch(lineStr); len(m) > 1 {
								project = resolveProjectName(m[1])
							} else if m := fileRegex.FindStringSubmatch(lineStr); len(m) > 1 {
								project = resolveProjectName(m[1])
							}
						}

						if !foundTitle {
							var item transcriptLine
							if err := json.Unmarshal(line, &item); err == nil {
								if item.Type == "USER_INPUT" && item.Content != "" {
									title = CleanSessionTitle(item.Content)
									foundTitle = true
									if item.CreatedAt != "" {
										if t, err := time.Parse(time.RFC3339, item.CreatedAt); err == nil {
											modTime = t
										}
									}
								}
							}
						}
					}
					_ = f.Close()
				}

				if project == "" {
					project = "未分类项目"
				}

				sessionMap[id] = SessionItem{
					ID:        id,
					Project:   project,
					Title:     title,
					UpdatedAt: modTime.Format("2006-01-02 15:04:05"),
					Turns:     turns,
					Source:    "Brain持久化",
					Path:      fullPath,
				}
			}
		}

		// 2. 扫描 %USERPROFILE%\.gemini\antigravity\conversations\*.db
		convoDir := filepath.Join(homeDir, ".gemini", "antigravity", "conversations")
		if files, err := os.ReadDir(convoDir); err == nil {
			for _, file := range files {
				if !file.IsDir() && strings.HasSuffix(file.Name(), ".db") && !strings.Contains(file.Name(), "-wal") && !strings.Contains(file.Name(), "-shm") {
					id := strings.TrimSuffix(file.Name(), ".db")
					if _, exists := sessionMap[id]; !exists {
						var modTime time.Time
						if fi, err := file.Info(); err == nil {
							modTime = fi.ModTime()
						} else {
							modTime = time.Now()
						}
						sessionMap[id] = SessionItem{
							ID:        id,
							Project:   "未分类项目",
							Title:     fmt.Sprintf("会话 #%s", id[:min(8, len(id))]),
							UpdatedAt: modTime.Format("2006-01-02 15:04:05"),
							Turns:     1,
							Source:    "Conversations DB",
							Path:      filepath.Join(convoDir, file.Name()),
						}
					}
				}
			}
		}
	}

	// 3. 扫描 %APPDATA%\Antigravity\User\workspaceStorage
	appData := os.Getenv("APPDATA")
	if appData != "" {
		possibleWsDirs := []string{
			filepath.Join(appData, "Antigravity", "User", "workspaceStorage"),
			filepath.Join(appData, "Antigravity", "workspaceStorage"),
		}
		for _, wsDir := range possibleWsDirs {
			if entries, err := os.ReadDir(wsDir); err == nil {
				for _, entry := range entries {
					if entry.IsDir() {
						id := entry.Name()
						if _, exists := sessionMap[id]; !exists {
							var modTime time.Time
							if fi, err := entry.Info(); err == nil {
								modTime = fi.ModTime()
							} else {
								modTime = time.Now()
							}

							proj := "未分类项目"
							wsJSONPath := filepath.Join(wsDir, id, "workspace.json")
							if wsData, err := os.ReadFile(wsJSONPath); err == nil {
								var wsMeta struct {
									Folder string `json:"folder"`
								}
								if err := json.Unmarshal(wsData, &wsMeta); err == nil && wsMeta.Folder != "" {
									if resolved := resolveProjectName(wsMeta.Folder); resolved != "" {
										proj = resolved
									}
								}
							}

							sessionMap[id] = SessionItem{
								ID:        id,
								Project:   proj,
								Title:     fmt.Sprintf("工作区会话 #%s", id[:min(8, len(id))]),
								UpdatedAt: modTime.Format("2006-01-02 15:04:05"),
								Turns:     1,
								Source:    "WorkspaceStorage",
								Path:      filepath.Join(wsDir, id),
							}
						}
					}
				}
			}
		}
	}

	// 转换为数组并按修改时间降序排序
	sessions := make([]SessionItem, 0, len(sessionMap))
	projectSet := make(map[string]struct{})
	for _, item := range sessionMap {
		sessions = append(sessions, item)
		if item.Project != "" {
			projectSet[item.Project] = struct{}{}
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt > sessions[j].UpdatedAt
	})

	projects := make([]string, 0, len(projectSet))
	for p := range projectSet {
		if p != "未分类项目" {
			projects = append(projects, p)
		}
	}
	sort.Strings(projects)
	if _, ok := projectSet["未分类项目"]; ok || len(projects) == 0 {
		projects = append(projects, "未分类项目")
	}

	return SessionsResult{
		Total:    len(sessions),
		Projects: projects,
		Sessions: sessions,
	}
}

// DeleteSession 物理删除指定会话目录与数据
func DeleteSession(id string) error {
	if id == "" {
		return fmt.Errorf("会话 ID 不能为空")
	}
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return fmt.Errorf("无法获取用户主目录")
	}

	// 1. 删除 brain/<id>
	brainPath := filepath.Join(homeDir, ".gemini", "antigravity", "brain", id)
	if _, err := os.Stat(brainPath); err == nil {
		_ = os.RemoveAll(brainPath)
	}

	// 2. 删除 conversations/<id>.db
	convoPath := filepath.Join(homeDir, ".gemini", "antigravity", "conversations", id+".db")
	_ = os.Remove(convoPath)
	_ = os.Remove(convoPath + "-wal")
	_ = os.Remove(convoPath + "-shm")

	// 3. 删除 workspaceStorage/<id>
	appData := os.Getenv("APPDATA")
	if appData != "" {
		_ = os.RemoveAll(filepath.Join(appData, "Antigravity", "User", "workspaceStorage", id))
		_ = os.RemoveAll(filepath.Join(appData, "Antigravity", "workspaceStorage", id))
	}
	return nil
}

// ExportSessionMarkdown 将指定会话日志导出为 Markdown 内容
func ExportSessionMarkdown(id string) (string, error) {
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return "", fmt.Errorf("无法获取用户主目录")
	}
	logPath := filepath.Join(homeDir, ".gemini", "antigravity", "brain", id, ".system_generated", "logs", "transcript.jsonl")
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
	return sb.String(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
