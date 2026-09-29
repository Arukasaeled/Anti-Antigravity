package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/2ag/2ag/internal/config"
)

type PluginManifest struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Author      string               `json:"author"`
	Description string               `json:"description"`
	Entrypoint  *ManifestEntrypoint  `json:"entrypoint,omitempty"`
	UI          *ManifestUI          `json:"ui,omitempty"`
	HealthCheck *ManifestHealthCheck `json:"health_check,omitempty"`
	Directory   string               `json:"-"`
}

type ManifestEntrypoint struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type ManifestUI struct {
	Type   string         `json:"type"`
	Schema map[string]any `json:"schema,omitempty"`
	Entry  string         `json:"entry,omitempty"`
}

type ManifestHealthCheck struct {
	Type       string `json:"type"`
	URL        string `json:"url"`
	IntervalMs int    `json:"interval_ms"`
}

func DiscoverPlugins(root string) ([]PluginManifest, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return []PluginManifest{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read plugin directory: %w", err)
	}
	manifests := make([]PluginManifest, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "manifest.json")
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read plugin manifest %s: %w", path, err)
		}
		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode plugin manifest %s: %w", path, err)
		}
		if manifest.ID == "" || manifest.Name == "" || manifest.Version == "" {
			return nil, fmt.Errorf("plugin manifest %s is missing id, name, or version", path)
		}
		if strings.ContainsAny(manifest.ID, "/\\?#%\r\n") {
			return nil, fmt.Errorf("invalid plugin id %q", manifest.ID)
		}
		manifest.Directory = filepath.Join(root, entry.Name())
		if manifest.Entrypoint != nil && manifest.Entrypoint.Type == "sidecar" {
			if manifest.Entrypoint.Command == "" || filepath.IsAbs(manifest.Entrypoint.Command) || strings.ContainsAny(manifest.Entrypoint.Command, "\r\n") || strings.Contains(manifest.Entrypoint.Command, "..") {
				return nil, fmt.Errorf("invalid entrypoint command in %s", path)
			}
			manifest.Entrypoint.Command = filepath.Join(manifest.Directory, filepath.FromSlash(manifest.Entrypoint.Command))
		}
		if manifest.UI != nil && manifest.UI.Type == "iframe" && (manifest.UI.Entry == "" || filepath.IsAbs(manifest.UI.Entry) || strings.ContainsAny(manifest.UI.Entry, "\\\r\n") || strings.Contains(manifest.UI.Entry, "..")) {
			return nil, fmt.Errorf("invalid UI entry in %s", path)
		}
		manifests = append(manifests, manifest)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].ID < manifests[j].ID })
	return manifests, nil
}

func MergePluginConfig(manifests []PluginManifest, configured []config.Plugin) []config.Plugin {
	configuredByID := make(map[string]config.Plugin, len(configured))
	for _, plugin := range configured {
		configuredByID[plugin.Name] = plugin
	}
	merged := make([]config.Plugin, 0, len(manifests)+len(configured))
	seen := make(map[string]bool)
	for _, manifest := range manifests {
		plugin := configuredByID[manifest.ID]
		plugin.Name = manifest.ID
		plugin.DisplayName, plugin.Version, plugin.Author, plugin.Description, plugin.Directory = manifest.Name, manifest.Version, manifest.Author, manifest.Description, manifest.Directory
		if manifest.Entrypoint != nil && manifest.Entrypoint.Type == "sidecar" {
			plugin.Executable = manifest.Entrypoint.Command
			plugin.Args = append([]string(nil), manifest.Entrypoint.Args...)
		}
		if manifest.UI != nil && manifest.UI.Type == "iframe" {
			plugin.UIEntry = filepath.Join(manifest.Directory, filepath.FromSlash(manifest.UI.Entry))
		}
		if plugin.Executable == "" {
			plugin.Enabled = false
		}
		merged = append(merged, plugin)
		seen[manifest.ID] = true
	}
	for _, plugin := range configured {
		if !seen[plugin.Name] {
			merged = append(merged, plugin)
		}
	}
	return merged
}
