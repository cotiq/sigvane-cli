// Package state reads and writes the CLI cursor state file.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// File is the on-disk state document keyed by inbox slug.
type File map[string]Entry

// Entry stores the resolved inbox id and last handled item id for one slug.
type Entry struct {
	InboxID    string `json:"inbox_id"`
	LastItemID string `json:"last_item_id,omitempty"`
}

// ResolvePath returns the state file path using the documented default location.
func ResolvePath(overridePath string) (string, error) {
	if overridePath != "" {
		return overridePath, nil
	}

	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for state path: %w", err)
		}
		base = filepath.Join(home, ".local", "state")
	}

	return filepath.Join(base, "sigvane", "state.json"), nil
}

// Load reads the state file from disk, treating a missing file as empty state.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return nil, fmt.Errorf("read state %q: %w", path, err)
	}

	var state File
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse state %q: %w", path, err)
	}
	if state == nil {
		return File{}, nil
	}

	return state, nil
}

// Save atomically rewrites the state file on disk.
func Save(path string, state File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state directory for %q: %w", path, err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state %q: %w", path, err)
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp state file for %q: %w", path, err)
	}

	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("chmod temp state file for %q: %w", path, err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp state file for %q: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp state file for %q: %w", path, err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp state file for %q: %w", path, err)
	}

	return nil
}
