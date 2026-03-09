package repositoryimpl

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/singlecommandpermission"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const storagePrefix = "singlecommandpermissions"

// YAMLRepository stores single-command permission rules as individual YAML
// files, one per rule, keyed by rule ID.
type YAMLRepository struct {
	storage storage.Storage
}

// NewYAMLRepository creates a new YAML-backed single-command permission repository.
func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", storagePrefix, id)
}

func prefixPath() string {
	return storagePrefix + "/"
}

// Create stores a new permission rule.
func (r *YAMLRepository) Create(ctx context.Context, p *singlecommandpermission.SingleCommandPermission) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal single command permission: %w", err))
	}
	if err := r.storage.Write(ctx, path(p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}
	return nil
}

// Get returns a single permission rule by ID.
func (r *YAMLRepository) Get(ctx context.Context, id string) (*singlecommandpermission.SingleCommandPermission, error) {
	exists, err := r.storage.Exists(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("single_command_permission", err)
	}
	if !exists {
		return nil, cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("single_command_permission", err)
	}
	var p singlecommandpermission.SingleCommandPermission
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal single command permission: %w", err))
	}
	return &p, nil
}

// List returns all permission rules for a project.
func (r *YAMLRepository) List(ctx context.Context, projectID string) ([]*singlecommandpermission.SingleCommandPermission, error) {
	keys, err := r.storage.List(ctx, prefixPath())
	if err != nil {
		return nil, cerr.WrapStorageReadError("single_command_permission", err)
	}

	var result []*singlecommandpermission.SingleCommandPermission
	for _, key := range keys {
		if !strings.HasSuffix(key, ".yaml") {
			continue
		}
		data, err := r.storage.Read(ctx, key)
		if err != nil {
			return nil, cerr.WrapStorageReadError("single_command_permission", err)
		}
		var p singlecommandpermission.SingleCommandPermission
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue // skip malformed entries
		}
		if p.ProjectID == projectID {
			result = append(result, &p)
		}
	}

	// Sort by creation time (newest first).
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result, nil
}

// FindByPatternAndType returns all permission rules matching the given
// projectID, pattern, and type. Results are sorted by CreatedAt ascending
// (oldest first) so that callers can keep the oldest entry when deduplicating.
func (r *YAMLRepository) FindByPatternAndType(ctx context.Context, projectID, pattern, permType string) ([]*singlecommandpermission.SingleCommandPermission, error) {
	keys, err := r.storage.List(ctx, prefixPath())
	if err != nil {
		return nil, cerr.WrapStorageReadError("single_command_permission", err)
	}

	var result []*singlecommandpermission.SingleCommandPermission
	for _, key := range keys {
		if !strings.HasSuffix(key, ".yaml") {
			continue
		}
		data, err := r.storage.Read(ctx, key)
		if err != nil {
			return nil, cerr.WrapStorageReadError("single_command_permission", err)
		}
		var p singlecommandpermission.SingleCommandPermission
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue // skip malformed entries
		}
		if p.ProjectID == projectID && p.Pattern == pattern && p.Type == permType {
			result = append(result, &p)
		}
	}

	// Sort by creation time ascending (oldest first).
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})

	return result, nil
}

// Update replaces an existing permission rule.
func (r *YAMLRepository) Update(ctx context.Context, p *singlecommandpermission.SingleCommandPermission) error {
	exists, err := r.storage.Exists(ctx, path(p.ID))
	if err != nil {
		return cerr.WrapStorageReadError("single_command_permission", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}
	data, merr := yaml.Marshal(p)
	if merr != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal single command permission: %w", merr))
	}
	if err := r.storage.Write(ctx, path(p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}
	return nil
}

// Delete removes a permission rule by ID.
func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	exists, err := r.storage.Exists(ctx, path(id))
	if err != nil {
		return cerr.WrapStorageReadError("single_command_permission", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}
	return nil
}
