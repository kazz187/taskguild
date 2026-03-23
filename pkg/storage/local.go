package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements Storage using the local filesystem.
//
// Individual file operations (Read, Write, Delete, Exists) are inherently
// safe for concurrent use: each operates on a single file, and Write uses
// an atomic temp-file-then-rename pattern. Multi-operation atomicity (e.g.
// check-then-write for Claim) is handled by repository-level mutexes
// (e.g. claimMu in the task repo). Removing the previous global RWMutex
// eliminates cross-repository lock contention that caused disk IO spikes
// when a long-running scan (e.g. task log index build) blocked all storage
// operations system-wide.
type LocalStorage struct {
	basePath string
}

// NewLocalStorage creates a new LocalStorage rooted at basePath.
func NewLocalStorage(basePath string) (*LocalStorage, error) {
	abs, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}
	return &LocalStorage{basePath: abs}, nil
}

func (s *LocalStorage) resolve(path string) string {
	return filepath.Join(s.basePath, filepath.Clean(path))
}

func (s *LocalStorage) Read(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(s.resolve(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return data, nil
}

func (s *LocalStorage) Write(_ context.Context, path string, data []byte) error {
	full := s.resolve(path)
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Atomic write: write to temp file then rename.
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

func (s *LocalStorage) Delete(_ context.Context, path string) error {
	full := s.resolve(path)
	if err := os.Remove(full); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return fmt.Errorf("failed to delete %s: %w", path, err)
	}
	return nil
}

func (s *LocalStorage) List(_ context.Context, prefix string) ([]string, error) {
	dir := s.resolve(prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list %s: %w", prefix, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		paths = append(paths, strings.TrimPrefix(filepath.Join(prefix, entry.Name()), "/"))
	}
	return paths, nil
}

func (s *LocalStorage) ListDirs(_ context.Context, prefix string) ([]string, error) {
	dir := s.resolve(prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list dirs %s: %w", prefix, err)
	}

	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirs = append(dirs, strings.TrimPrefix(filepath.Join(prefix, entry.Name()), "/"))
	}
	return dirs, nil
}

func (s *LocalStorage) MoveDir(_ context.Context, oldPrefix, newPrefix string) error {
	oldFull := s.resolve(oldPrefix)
	newFull := s.resolve(newPrefix)

	// Ensure parent directory of destination exists.
	if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", newPrefix, err)
	}

	if err := os.Rename(oldFull, newFull); err != nil {
		return fmt.Errorf("failed to move %s to %s: %w", oldPrefix, newPrefix, err)
	}
	return nil
}

func (s *LocalStorage) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(s.resolve(path))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat %s: %w", path, err)
	}
	return true, nil
}
