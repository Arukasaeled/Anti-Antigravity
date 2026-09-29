package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Store struct {
	Path string
}

func NewStore(path string) *Store { return &Store{Path: path} }

func (s *Store) Load() (map[string]any, error) {
	if s == nil || s.Path == "" {
		return nil, errors.New("MCP config path is empty")
	}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MCP config: %w", err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode MCP config: %w", err)
	}
	if value == nil {
		value = map[string]any{}
	}
	return value, nil
}

func (s *Store) Backup() (string, error) {
	if s == nil || s.Path == "" {
		return "", errors.New("MCP config path is empty")
	}
	if _, err := os.Stat(s.Path); err != nil {
		return "", fmt.Errorf("inspect MCP config: %w", err)
	}
	backup := s.Path + ".2ag-backup-" + time.Now().UTC().Format("20060102-150405") + ".json"
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return "", fmt.Errorf("read MCP config for backup: %w", err)
	}
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", fmt.Errorf("write MCP backup: %w", err)
	}
	return backup, nil
}

func (s *Store) Update(update func(map[string]any) error) error {
	if update == nil {
		return errors.New("MCP update function is nil")
	}
	value, err := s.Load()
	if err != nil {
		return err
	}
	if err := update(value); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return fmt.Errorf("create MCP config directory: %w", err)
	}
	if _, err := os.Stat(s.Path); err == nil {
		if _, err := s.Backup(); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".2ag-mcp-*.tmp")
	if err != nil {
		return fmt.Errorf("create MCP temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	_ = tmp.Chmod(0600)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write MCP temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush MCP temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.Path); err == nil {
		return nil
	}
	backup := s.Path + ".2ag-before-update"
	_ = os.Remove(backup)
	if err := os.Rename(s.Path, backup); err != nil {
		return fmt.Errorf("replace MCP config: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		_ = os.Rename(backup, s.Path)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
