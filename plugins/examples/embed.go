package examples

import (
	"embed"
	"encoding/json"
	"strings"
)

// Bundled examples are local UI extensions, independent of sidecar processes.
//
//go:embed */manifest.json */index.js
var files embed.FS

type Extension struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version string `json:"version"`
	Source  string `json:"source"`
	Bundled bool   `json:"bundled"`
	Error   string `json:"error,omitempty"`
}

func Catalog() []Extension {
	result := []Extension{}
	for _, id := range []string{"prompt-toolkit", "conversation-map", "project-context"} {
		raw, _ := files.ReadFile(id + "/manifest.json")
		var manifest struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		_ = json.Unmarshal(raw, &manifest)
		source, _ := files.ReadFile(id + "/index.js")
		result = append(result, Extension{ID: manifest.ID, Title: manifest.Name, Version: manifest.Version, Source: string(source), Bundled: true})
	}
	return result
}

// Definitions are compiled with the injected Hub instead of depending on eval
// or a remotely loaded script to initialize the official examples.
func Expression() string {
	sources := []string{}
	for _, extension := range Catalog() {
		sources = append(sources, extension.Source)
	}
	return "[" + strings.Join(sources, ",\n") + "]"
}
