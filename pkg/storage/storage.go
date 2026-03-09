package storage

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a requested path does not exist in storage.
var ErrNotFound = errors.New("not found")

// Storage provides an abstraction over key-value style file storage.
type Storage interface {
	Read(ctx context.Context, path string) ([]byte, error)
	Write(ctx context.Context, path string, data []byte) error
	Delete(ctx context.Context, path string) error
	List(ctx context.Context, prefix string) ([]string, error)
	Exists(ctx context.Context, path string) (bool, error)
}
