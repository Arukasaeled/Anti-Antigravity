package assets

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed inject.js
var InjectJS []byte

//go:embed dream-skin.css
var DreamSkinCSS []byte

//go:embed logo.png
var LogoPNG []byte

func Materialize(dir string) (string, string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("create embedded asset directory: %w", err)
	}
	injectPath := filepath.Join(dir, "inject.js")
	cssPath := filepath.Join(dir, "dream-skin.css")
	if err := writeAsset(injectPath, InjectJS); err != nil {
		return "", "", err
	}
	if err := writeAsset(cssPath, DreamSkinCSS); err != nil {
		return "", "", err
	}
	return injectPath, cssPath, nil
}

func writeAsset(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write embedded asset %s: %w", path, err)
	}
	return nil
}
