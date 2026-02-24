package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/agent"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const agentsPrefix = "agents"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", agentsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, a *agent.Agent) error {
	exists, err := r.storage.Exists(ctx, path(a.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("agent", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "agent already exists", nil)
	}
	data, err := yaml.Marshal(a)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal agent: %w", err))
	}
	if err := r.storage.Write(ctx, path(a.ID), data); err != nil {
		return cerr.WrapStorageWriteError("agent", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*agent.Agent, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("agent", err)
	}
	var a agent.Agent
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal agent: %w", err))
	}
	return &a, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID string, limit, offset int) ([]*agent.Agent, int, error) {
	paths, err := r.storage.List(ctx, agentsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("agents", err)
	}

	sort.Strings(paths)

	var all []*agent.Agent
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var a agent.Agent
		if err := yaml.Unmarshal(data, &a); err != nil {
			continue
		}
		if projectID != "" && a.ProjectID != projectID {
			continue
		}
		all = append(all, &a)
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

func (r *YAMLRepository) FindByName(ctx context.Context, projectID, name string) (*agent.Agent, error) {
	paths, err := r.storage.List(ctx, agentsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("agents", err)
	}

	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var a agent.Agent
		if err := yaml.Unmarshal(data, &a); err != nil {
			continue
		}
		if a.ProjectID == projectID && a.Name == name {
			return &a, nil
		}
	}
	return nil, cerr.NewError(cerr.NotFound, "agent not found", nil)
}

func (r *YAMLRepository) Update(ctx context.Context, a *agent.Agent) error {
	exists, err := r.storage.Exists(ctx, path(a.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("agent", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "agent not found", nil)
	}
	data, err := yaml.Marshal(a)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal agent: %w", err))
	}
	if err := r.storage.Write(ctx, path(a.ID), data); err != nil {
		return cerr.WrapStorageWriteError("agent", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("agent", err)
	}
	return nil
}
