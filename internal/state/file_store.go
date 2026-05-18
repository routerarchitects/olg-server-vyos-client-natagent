package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore stores state in a local JSON file.
type FileStore struct {
	path string
}

// NewFileStore creates a JSON-file state store.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Load reads state from JSON file. Missing file is treated as empty state.
func (s *FileStore) Load(ctx context.Context) (State, error) {
	if ctx == nil {
		return State{}, errors.New("load state: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return State{}, fmt.Errorf("load state: %w", err)
	}
	if strings.TrimSpace(s.path) == "" {
		return State{}, errors.New("load state: file path is empty")
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("load state file %q: %w", s.path, err)
	}
	if err := ctx.Err(); err != nil {
		return State{}, fmt.Errorf("load state: %w", err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("decode state file %q: %w", s.path, err)
	}
	return st, nil
}

// Save writes state atomically to JSON file.
func (s *FileStore) Save(ctx context.Context, st State) error {
	if ctx == nil {
		return errors.New("save state: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if strings.TrimSpace(s.path) == "" {
		return errors.New("save state: file path is empty")
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state directory %q: %w", dir, err)
	}

	payload, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state file in %q: %w", dir, err)
	}
	tmpName := tmpFile.Name()

	cleanup := func() {
		_ = os.Remove(tmpName)
	}

	if _, err := tmpFile.Write(payload); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return fmt.Errorf("write temp state file %q: %w", tmpName, err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return fmt.Errorf("sync temp state file %q: %w", tmpName, err)
	}
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return fmt.Errorf("chmod temp state file %q: %w", tmpName, err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp state file %q: %w", tmpName, err)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return fmt.Errorf("save state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp state file %q to %q: %w", tmpName, s.path, err)
	}

	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state directory %q for sync: %w", dir, err)
	}
	defer dirFile.Close()

	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("sync state directory %q: %w", dir, err)
	}

	return nil
}
