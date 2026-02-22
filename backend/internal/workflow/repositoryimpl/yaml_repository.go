package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const workflowsPrefix = "workflows"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", workflowsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, w *workflow.Workflow) error {
	exists, err := r.storage.Exists(ctx, path(w.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("workflow", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "workflow already exists", nil)
	}
	data, err := yaml.Marshal(w)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal workflow: %w", err))
	}
	if err := r.storage.Write(ctx, path(w.ID), data); err != nil {
		return cerr.WrapStorageWriteError("workflow", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*workflow.Workflow, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("workflow", err)
	}
	var w workflow.Workflow
	if err := yaml.Unmarshal(data, &w); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal workflow: %w", err))
	}
	return &w, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID string, limit, offset int) ([]*workflow.Workflow, int, error) {
	paths, err := r.storage.List(ctx, workflowsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("workflows", err)
	}

	sort.Strings(paths)

	// Read all and filter by projectID in memory.
	var all []*workflow.Workflow
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var w workflow.Workflow
		if err := yaml.Unmarshal(data, &w); err != nil {
			continue
		}
		if projectID != "" && w.ProjectID != projectID {
			continue
		}
		all = append(all, &w)
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

func (r *YAMLRepository) Update(ctx context.Context, w *workflow.Workflow) error {
	exists, err := r.storage.Exists(ctx, path(w.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("workflow", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "workflow not found", nil)
	}
	data, err := yaml.Marshal(w)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal workflow: %w", err))
	}
	if err := r.storage.Write(ctx, path(w.ID), data); err != nil {
		return cerr.WrapStorageWriteError("workflow", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("workflow", err)
	}
	return nil
}
