package profile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Manager struct {
	DataDir     string
	ProfilesDir string
}

func NewManager(dataDir, profilesDir string) *Manager {
	return &Manager{DataDir: dataDir, ProfilesDir: profilesDir}
}

func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.ProfilesDir)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	profiles := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && validName(entry.Name()) {
			profiles = append(profiles, entry.Name())
		}
	}
	sort.Strings(profiles)
	return profiles, nil
}

func (m *Manager) SaveProfile(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := os.MkdirAll(m.ProfilesDir, 0700); err != nil {
		return fmt.Errorf("create profiles directory: %w", err)
	}
	destination := filepath.Join(m.ProfilesDir, name)
	if err := removePath(destination); err != nil {
		return fmt.Errorf("replace profile %q: %w", name, err)
	}
	if err := copyDirectory(m.DataDir, destination); err != nil {
		_ = removePath(destination)
		return fmt.Errorf("save profile %q: %w", name, err)
	}
	return nil
}

func (m *Manager) SwitchProfile(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	source := filepath.Join(m.ProfilesDir, name)
	if info, err := os.Stat(source); err != nil || !info.IsDir() {
		if err != nil {
			return fmt.Errorf("profile %q not found: %w", name, err)
		}
		return fmt.Errorf("profile %q is not a directory", name)
	}
	parent := filepath.Dir(m.DataDir)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("create profile data parent: %w", err)
	}
	staging, err := os.MkdirTemp(parent, ".2ag-profile-staging-")
	if err != nil {
		return fmt.Errorf("create profile staging directory: %w", err)
	}
	defer removePath(staging)
	if err := copyDirectory(source, staging); err != nil {
		return fmt.Errorf("stage profile %q: %w", name, err)
	}
	backup := m.DataDir + ".2ag-before-switch-" + time.Now().UTC().Format("20060102-150405.000000000")
	if _, err := os.Stat(m.DataDir); err == nil {
		if err := os.Rename(m.DataDir, backup); err != nil {
			return fmt.Errorf("backup current profile: %w", err)
		}
	}
	if err := os.Rename(staging, m.DataDir); err != nil {
		_ = os.Rename(backup, m.DataDir)
		return fmt.Errorf("activate profile %q: %w", name, err)
	}
	return nil
}

func validateName(name string) error {
	if !validName(name) {
		return errors.New("profile name must contain only letters, numbers, '.', '_' or '-'")
	}
	return nil
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 64 {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func copyDirectory(source, destination string) error {
	if err := os.MkdirAll(destination, 0700); err != nil {
		return err
	}
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", source)
	}
	return filepath.Walk(source, func(path string, entry os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, entry.Mode().Perm())
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("unsupported profile entry: %s", path)
		}
		return copyFile(path, target, entry.Mode().Perm())
	})
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}

func removePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("empty path")
	}
	return os.RemoveAll(path)
}
