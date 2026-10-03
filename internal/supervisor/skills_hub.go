package supervisor

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SkillItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Scope           string `json:"scope"`
	Path            string `json:"path"`
	Resources       bool   `json:"resources"`
	Examples        bool   `json:"examples"`
	Scripts         bool   `json:"scripts"`
	MetadataWarning string `json:"metadata_warning,omitempty"`
}

func workspaceURIPath(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		if parsed, err := url.Parse(uri); err == nil {
			path, _ := url.PathUnescape(parsed.EscapedPath())
			if len(path) > 2 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			return filepath.FromSlash(path)
		}
	}
	return filepath.Clean(uri)
}

func workspaceFolderFromJSON(raw []byte) string {
	var metadata struct {
		Folder string `json:"folder"`
	}
	if json.Unmarshal(raw, &metadata) != nil {
		return ""
	}
	return metadata.Folder
}

func discoveredWorkspaces() []string {
	seen := make(map[string]bool)
	targets, _ := RuntimeManager.Snapshot()
	for _, target := range targets {
		if target.Context == nil {
			continue
		}
		for _, uri := range target.Context.WorkspaceDirs {
			path := workspaceURIPath(uri)
			if filepath.IsAbs(path) {
				seen[path] = true
			}
		}
	}
	// Profiles' workspace.json contains only project metadata, not conversation bodies.
	for _, root := range DetectAntigravityEnvironment().WorkspaceStorage {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, entry.Name(), "workspace.json"))
			if err != nil || len(raw) > 65536 {
				continue
			}
			// Extract with JSON decoding, never text-search the database or WAL.
			folder := workspaceFolderFromJSON(raw)
			if folder != "" {
				path := workspaceURIPath(folder)
				if filepath.IsAbs(path) {
					seen[path] = true
				}
			}
		}
	}
	paths := []string{}
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func skillFrontmatter(text string) (string, string, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", "未检测到 YAML frontmatter"
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", "", "Frontmatter 未闭合"
	}
	lines := strings.Split(text[4:4+end], "\n")
	name, description := "", ""
	for i, line := range lines {
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "\"'")
		}
		if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			if description == "|" || description == ">" || description == "|-" || description == ">-" {
				description = ""
				for _, continuation := range lines[i+1:] {
					if continuation != "" && !strings.HasPrefix(continuation, " ") && !strings.HasPrefix(continuation, "\t") {
						break
					}
					description += strings.TrimSpace(continuation) + " "
				}
			} else {
				description = strings.Trim(description, "\"'")
			}
		}
	}
	return name, strings.TrimSpace(description), ""
}

func ScanInstalledSkills() []SkillItem {
	result := []SkillItem{}
	roots := []struct{ path, scope string }{}
	if root := DetectAntigravityEnvironment().GlobalSkillsRoot; root != "" {
		roots = append(roots, struct{ path, scope string }{root, "global"})
	}
	for _, project := range discoveredWorkspaces() {
		roots = append(roots, struct{ path, scope string }{filepath.Join(project, ".agent", "skills"), "workspace"})
	}
	seen := make(map[string]bool)
	for _, root := range roots {
		entries, err := os.ReadDir(root.path)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(root.path, entry.Name(), "SKILL.md")
			if seen[path] {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				continue
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			seen[path] = true
			name, description, warning := skillFrontmatter(string(raw))
			if name == "" {
				name = entry.Name()
			}
			hasDir := func(name string) bool {
				info, err := os.Stat(filepath.Join(filepath.Dir(path), name))
				return err == nil && info.IsDir()
			}
			result = append(result, SkillItem{ID: base64.RawURLEncoding.EncodeToString([]byte(path)), Name: name, Description: description, Scope: root.scope, Path: filepath.Dir(path), Resources: hasDir("resources"), Examples: hasDir("examples"), Scripts: hasDir("scripts"), MetadataWarning: warning})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Scope != result[j].Scope {
			return result[i].Scope < result[j].Scope
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func ReadInstalledSkill(id string) (string, error) {
	// Resolve only entries from currently discovered install roots. No arbitrary file-read route.
	for _, skill := range ScanInstalledSkills() {
		if skill.ID == id {
			raw, err := os.ReadFile(filepath.Join(skill.Path, "SKILL.md"))
			return string(raw), err
		}
	}
	return "", fmt.Errorf("Skill 不在当前安装索引中")
}
