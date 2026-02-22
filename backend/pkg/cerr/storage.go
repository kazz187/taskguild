package cerr

import (
	"errors"
	"fmt"

	"github.com/kazz187/taskguild/backend/pkg/storage"
)

func WrapStorageReadError(target string, err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return NewError(NotFound, fmt.Sprintf("%s not found", target), err)
	}
	return NewError(Internal, "server error", fmt.Errorf("failed to read %s: %w", target, err))
}

func WrapStorageWriteError(target string, err error) error {
	return NewError(Internal, "server error", fmt.Errorf("failed to write %s: %w", target, err))
}

func WrapStorageDeleteError(target string, err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return NewError(NotFound, fmt.Sprintf("%s not found", target), err)
	}
	return NewError(Internal, "server error", fmt.Errorf("failed to delete %s: %w", target, err))
}
