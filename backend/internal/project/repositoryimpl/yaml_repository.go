package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/project"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const projectsPrefix = "projects"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", projectsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, p *project.Project) error {
	exists, err := r.storage.Exists(ctx, path(p.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("project", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "project already exists", nil)
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal project: %w", err))
	}
	if err := r.storage.Write(ctx, path(p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("project", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*project.Project, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("project", err)
	}
	var p project.Project
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal project: %w", err))
	}
	return &p, nil
}

func (r *YAMLRepository) FindByName(ctx context.Context, name string) (*project.Project, error) {
	paths, err := r.storage.List(ctx, projectsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("project", err)
	}
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var proj project.Project
		if err := yaml.Unmarshal(data, &proj); err != nil {
			continue
		}
		if proj.Name == name {
			return &proj, nil
		}
	}
	return nil, cerr.NewError(cerr.NotFound, "project not found", nil)
}

func (r *YAMLRepository) List(ctx context.Context, limit, offset int) ([]*project.Project, int, error) {
	paths, err := r.storage.List(ctx, projectsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("projects", err)
	}
	total := len(paths)

	// Sort by filename for consistent ordering.
	sort.Strings(paths)

	// Apply pagination.
	if offset >= len(paths) {
		return nil, total, nil
	}
	paths = paths[offset:]
	if limit > 0 && len(paths) > limit {
		paths = paths[:limit]
	}

	projects := make([]*project.Project, 0, len(paths))
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var proj project.Project
		if err := yaml.Unmarshal(data, &proj); err != nil {
			continue
		}
		projects = append(projects, &proj)
	}
	return projects, total, nil
}

func (r *YAMLRepository) Update(ctx context.Context, p *project.Project) error {
	exists, err := r.storage.Exists(ctx, path(p.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("project", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "project not found", nil)
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal project: %w", err))
	}
	if err := r.storage.Write(ctx, path(p.ID), data); err != nil {
		return cerr.WrapStorageWriteError("project", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("project", err)
	}
	return nil
}
