package supervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/2ag/2ag/plugins/examples"
)

// Only local, manifest-declared single-file factories are loaded here. Invalid
// entries become individual error rows, never a failure of the whole catalog.
func LocalUIExtensions() ([]examples.Extension, error) {
	entries := map[string]examples.Extension{}
	for _, entry := range examples.Catalog() {
		entries[entry.ID] = entry
	}
	roots := []string{}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Join(cwd, "plugins"))
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Join(filepath.Dir(exe), "plugins"))
	}
	for _, root := range roots {
		dirs, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("cannot read local extension directory: %w", err)
		}
		for _, dir := range dirs {
			if !dir.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(root, dir.Name(), "manifest.json"))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("cannot read local extension manifest: %w", err)
			}
			var manifest PluginManifest
			if err := json.Unmarshal(raw, &manifest); err != nil {
				id := "invalid." + dir.Name()
				entries[id] = examples.Extension{ID: id, Title: dir.Name(), Error: "manifest: " + err.Error()}
				continue
			}
			if manifest.UI == nil || manifest.UI.Type != "factory" {
				continue
			}
			entry := examples.Extension{ID: manifest.ID, Title: manifest.Name, Version: manifest.Version}
			path := filepath.Clean(filepath.FromSlash(manifest.UI.Entry))
			if entry.ID == "" || strings.ContainsAny(entry.ID, "/\\?#%\r\n") {
				continue
			}
			if manifest.UI.Entry == "" || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || strings.ContainsAny(manifest.UI.Entry, "\\\r\n") {
				entry.Error = "invalid local extension entry"
			} else if source, err := os.ReadFile(filepath.Join(root, dir.Name(), path)); err != nil {
				entry.Error = err.Error()
			} else {
				entry.Source = string(source)
			}
			if _, exists := entries[entry.ID]; !exists {
				entries[entry.ID] = entry
			}
		}
	}
	result := make([]examples.Extension, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
