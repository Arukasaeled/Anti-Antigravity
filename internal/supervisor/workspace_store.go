package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Workspace data contains user-authored text only; credentials remain in Vault.
var workspaceMu sync.Mutex

var workspaceCollections = []string{"drafts", "snippets", "pins", "capsules", "recent", "extension-state"}

func workspaceCollectionPath(collection string) (string, error) {
	valid := false
	for _, name := range workspaceCollections {
		valid = valid || collection == name
	}
	if !valid {
		return "", errors.New("unknown workspace collection")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".2ag", "workspace", collection+".json"), nil
}

func loadWorkspaceCollection(collection string) ([]map[string]any, error) {
	path, err := workspaceCollectionPath(collection)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("read %s: %w", collection, err)
	}
	if items == nil {
		items = []map[string]any{}
	}
	return items, nil
}

func LoadWorkspace() (map[string][]map[string]any, error) {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	state := make(map[string][]map[string]any, len(workspaceCollections))
	for _, name := range workspaceCollections {
		items, err := loadWorkspaceCollection(name)
		if err != nil {
			return nil, err
		}
		state[name] = items
	}
	return state, nil
}

func LoadWorkspaceCollection(collection string) ([]map[string]any, error) {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	return loadWorkspaceCollection(collection)
}

// Mutations read the current file while holding the same lock as writes, so
// saving one item never replaces another window's entire collection snapshot.
func SaveWorkspaceItem(collection, action, id string, item map[string]any) ([]map[string]any, map[string]any, error) {
	workspaceMu.Lock()
	defer workspaceMu.Unlock()
	path, err := workspaceCollectionPath(collection)
	if err != nil {
		return nil, nil, err
	}
	if action != "save" && action != "delete" {
		return nil, nil, errors.New("action must be save or delete")
	}
	if action == "save" {
		id, _ = item["id"].(string)
	}
	if strings.TrimSpace(id) == "" || len(id) > 160 {
		return nil, nil, errors.New("item id is required (maximum 160 characters)")
	}
	items, err := loadWorkspaceCollection(collection)
	if err != nil {
		return nil, nil, err
	}
	index := -1
	for i, existing := range items {
		if existing["id"] == id {
			index = i
			break
		}
	}
	if action == "delete" {
		if index >= 0 {
			items = append(items[:index], items[index+1:]...)
		}
		item = nil
	} else {
		fields := map[string][]string{
			"drafts":   {"title", "text"},
			"snippets": {"title", "text"},
			"pins":     {"title", "text", "conversation_key", "role", "locator"},
			"capsules": {"title", "goal", "decisions", "notes", "current_state", "remaining_work", "markdown"},
			"recent":   {"kind", "title", "target_id"},
		}
		for _, field := range fields[collection] {
			if _, ok := item[field].(string); !ok {
				return nil, nil, fmt.Errorf("%s must be text", field)
			}
		}
		if collection == "extension-state" {
			if enabled, exists := item["enabled"]; exists {
				if _, ok := enabled.(bool); !ok {
					return nil, nil, errors.New("enabled must be a boolean")
				}
			}
		}
		now := nowRFC3339()
		item["updated_at"] = now
		item["created_at"] = now
		if index >= 0 {
			item["created_at"] = items[index]["created_at"]
		}
		if collection == "drafts" {
			versions := []any{}
			version := float64(0)
			if index >= 0 {
				versions, _ = items[index]["versions"].([]any)
				version, _ = items[index]["version"].(float64)
				// A queued autosave with unchanged text is not a new revision.
				if items[index]["text"] == item["text"] {
					item["versions"], item["version"] = versions, version
				} else {
					item["version"] = version + 1
				}
			} else {
				item["version"] = float64(1)
			}
			if _, unchanged := item["versions"]; !unchanged {
				versions = append(versions, map[string]any{"version": item["version"], "text": item["text"], "saved_at": now})
				if len(versions) > 20 {
					versions = versions[len(versions)-20:]
				}
				item["versions"] = versions
			}
		}
		if index >= 0 {
			items[index] = item
		} else {
			items = append(items, item)
		}
		if collection == "recent" {
			// Move every just-used entry to the end before trimming history.
			if index >= 0 {
				items = append(append(items[:index], items[index+1:]...), item)
			}
			if len(items) > 60 {
				items = items[len(items)-60:]
			}
		}
	}
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(path, append(raw, '\n'), 0o600); err != nil {
		return nil, nil, err
	}
	return items, item, nil
}
