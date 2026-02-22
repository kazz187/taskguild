package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const tasksPrefix = "tasks"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", tasksPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, t *task.Task) error {
	exists, err := r.storage.Exists(ctx, path(t.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "task already exists", nil)
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task: %w", err))
	}
	if err := r.storage.Write(ctx, path(t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*task.Task, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("task", err)
	}
	var t task.Task
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal task: %w", err))
	}
	return &t, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID, workflowID, statusID string, limit, offset int) ([]*task.Task, int, error) {
	paths, err := r.storage.List(ctx, tasksPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("tasks", err)
	}

	sort.Strings(paths)

	var all []*task.Task
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var t task.Task
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		if workflowID != "" && t.WorkflowID != workflowID {
			continue
		}
		if statusID != "" && t.StatusID != statusID {
			continue
		}
		all = append(all, &t)
	}

	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	all = all[offset:]
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, total, nil
}

func (r *YAMLRepository) Update(ctx context.Context, t *task.Task) error {
	exists, err := r.storage.Exists(ctx, path(t.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "task not found", nil)
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task: %w", err))
	}
	if err := r.storage.Write(ctx, path(t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("task", err)
	}
	return nil
}
