package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/project"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const projectsPrefix = "projects"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s/project.yaml", projectsPrefix, id)
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
	projects, err := r.readAll(ctx)
	if err != nil {
		return nil, err
	}

	for _, proj := range projects {
		if proj.Name == name {
			return proj, nil
		}
	}

	return nil, cerr.NewError(cerr.NotFound, "project not found", nil)
}

// readAll reads all project YAML files and returns them unsorted.
func (r *YAMLRepository) readAll(ctx context.Context) ([]*project.Project, error) {
	dirs, err := r.storage.ListDirs(ctx, projectsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("projects", err)
	}

	projects := make([]*project.Project, 0, len(dirs))
	for _, d := range dirs {
		projectPath := d + "/project.yaml"

		data, err := r.storage.Read(ctx, projectPath)
		if err != nil {
			continue
		}

		var proj project.Project
		if err := yaml.Unmarshal(data, &proj); err != nil {
			continue
		}

		projects = append(projects, &proj)
	}

	return projects, nil
}

func (r *YAMLRepository) ListAll(ctx context.Context) ([]*project.Project, error) {
	projects, err := r.readAll(ctx)
	if err != nil {
		return nil, err
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Order < projects[j].Order
	})

	return projects, nil
}

func (r *YAMLRepository) List(ctx context.Context, limit, offset int) ([]*project.Project, int, error) {
	projects, err := r.readAll(ctx)
	if err != nil {
		return nil, 0, err
	}

	total := len(projects)

	// Sort by Order field.
	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Order < projects[j].Order
	})

	// Apply pagination.
	if offset >= len(projects) {
		return nil, total, nil
	}

	projects = projects[offset:]
	if limit > 0 && len(projects) > limit {
		projects = projects[:limit]
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
	err := r.storage.Delete(ctx, path(id))
	if err != nil {
		return cerr.WrapStorageDeleteError("project", err)
	}

	return nil
}
