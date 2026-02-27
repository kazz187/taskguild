package repositoryimpl

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/permission"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const permissionsPrefix = "permissions"

// YAMLRepository stores permission sets as YAML files keyed by project ID.
type YAMLRepository struct {
	storage storage.Storage
}

// NewYAMLRepository creates a new YAML-backed permission repository.
func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(projectID string) string {
	return fmt.Sprintf("%s/%s.yaml", permissionsPrefix, projectID)
}

// Get returns the permission set for a project.
// Returns an empty PermissionSet (not an error) if none exists yet.
func (r *YAMLRepository) Get(ctx context.Context, projectID string) (*permission.PermissionSet, error) {
	exists, err := r.storage.Exists(ctx, path(projectID))
	if err != nil {
		return nil, cerr.WrapStorageReadError("permission", err)
	}
	if !exists {
		return &permission.PermissionSet{
			ProjectID: projectID,
		}, nil
	}
	data, err := r.storage.Read(ctx, path(projectID))
	if err != nil {
		return nil, cerr.WrapStorageReadError("permission", err)
	}
	var ps permission.PermissionSet
	if err := yaml.Unmarshal(data, &ps); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal permission: %w", err))
	}
	return &ps, nil
}

// Upsert creates or replaces the permission set for a project.
func (r *YAMLRepository) Upsert(ctx context.Context, ps *permission.PermissionSet) error {
	data, err := yaml.Marshal(ps)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal permission: %w", err))
	}
	if err := r.storage.Write(ctx, path(ps.ProjectID), data); err != nil {
		return cerr.WrapStorageWriteError("permission", err)
	}
	return nil
}
