package repositoryimpl

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/singlecommandpermission"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const (
	projectsPrefix = "projects"
	entityType     = "singlecommandpermissions"
)

// YAMLRepository stores single-command permission rules as individual YAML
// files, one per rule, scoped under project directories.
type YAMLRepository struct {
	storage     storage.Storage
	indexOnce   sync.Once
	indexMu     sync.RWMutex
	idToProject map[string]string
}

// NewYAMLRepository creates a new YAML-backed single-command permission repository.
func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func entityPath(projectID, id string) string {
	return fmt.Sprintf("%s/%s/%s/%s.yaml", projectsPrefix, projectID, entityType, id)
}

func entityPrefix(projectID string) string {
	return fmt.Sprintf("%s/%s/%s", projectsPrefix, projectID, entityType)
}

func (r *YAMLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()
		r.idToProject = make(map[string]string)

		dirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			return
		}
		for _, d := range dirs {
			pid := filepath.Base(d)
			files, err := r.storage.List(ctx, entityPrefix(pid))
			if err != nil {
				continue
			}
			for _, f := range files {
				id := strings.TrimSuffix(filepath.Base(f), ".yaml")
				r.idToProject[id] = pid
			}
		}
	})
}

// Create stores a new permission rule.
func (r *YAMLRepository) Create(ctx context.Context, p *singlecommandpermission.SingleCommandPermission) error {
	r.ensureIndex(ctx)

	data, err := yaml.Marshal(p)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal single command permission: %w", err))
	}
	if err := r.storage.Write(ctx, entityPath(p.ProjectID, p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}

	r.indexMu.Lock()
	r.idToProject[p.ID] = p.ProjectID
	r.indexMu.Unlock()
	return nil
}

// Get returns a single permission rule by ID.
func (r *YAMLRepository) Get(ctx context.Context, id string) (*singlecommandpermission.SingleCommandPermission, error) {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[id]
	r.indexMu.RUnlock()
	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}

	data, err := r.storage.Read(ctx, entityPath(pid, id))
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
	r.ensureIndex(ctx)

	keys, err := r.storage.List(ctx, entityPrefix(projectID))
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
			continue
		}
		result = append(result, &p)
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
	r.ensureIndex(ctx)

	keys, err := r.storage.List(ctx, entityPrefix(projectID))
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
			continue
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
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[p.ID]
	r.indexMu.RUnlock()
	if !ok {
		return cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}

	if pid != p.ProjectID {
		_ = r.storage.Delete(ctx, entityPath(pid, p.ID))
	}

	data, merr := yaml.Marshal(p)
	if merr != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal single command permission: %w", merr))
	}
	if err := r.storage.Write(ctx, entityPath(p.ProjectID, p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}

	r.indexMu.Lock()
	r.idToProject[p.ID] = p.ProjectID
	r.indexMu.Unlock()
	return nil
}

// Delete removes a permission rule by ID.
func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[id]
	r.indexMu.RUnlock()
	if !ok {
		return cerr.NewError(cerr.NotFound, "single command permission not found", nil)
	}

	if err := r.storage.Delete(ctx, entityPath(pid, id)); err != nil {
		return cerr.WrapStorageWriteError("single_command_permission", err)
	}

	r.indexMu.Lock()
	delete(r.idToProject, id)
	r.indexMu.Unlock()
	return nil
}
