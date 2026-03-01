package repositoryimpl

import (
	"context"
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const interactionsPrefix = "interactions"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", interactionsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, i *interaction.Interaction) error {
	exists, err := r.storage.Exists(ctx, path(i.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "interaction already exists", nil)
	}
	data, err := yaml.Marshal(i)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal interaction: %w", err))
	}
	if err := r.storage.Write(ctx, path(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*interaction.Interaction, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("interaction", err)
	}
	var i interaction.Interaction
	if err := yaml.Unmarshal(data, &i); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal interaction: %w", err))
	}
	return &i, nil
}

func (r *YAMLRepository) List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*interaction.Interaction, int, error) {
	paths, err := r.storage.List(ctx, interactionsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("interactions", err)
	}

	sort.Strings(paths)

	// Build a set for fast lookup when filtering by multiple task IDs.
	var taskIDSet map[string]struct{}
	if len(taskIDs) > 0 {
		taskIDSet = make(map[string]struct{}, len(taskIDs))
		for _, id := range taskIDs {
			taskIDSet[id] = struct{}{}
		}
	}

	var all []*interaction.Interaction
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var i interaction.Interaction
		if err := yaml.Unmarshal(data, &i); err != nil {
			continue
		}
		if taskID != "" && i.TaskID != taskID {
			continue
		}
		if taskIDSet != nil {
			if _, ok := taskIDSet[i.TaskID]; !ok {
				continue
			}
		}
		all = append(all, &i)
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

func (r *YAMLRepository) Update(ctx context.Context, i *interaction.Interaction) error {
	exists, err := r.storage.Exists(ctx, path(i.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "interaction not found", nil)
	}
	data, err := yaml.Marshal(i)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal interaction: %w", err))
	}
	if err := r.storage.Write(ctx, path(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	return nil
}

func (r *YAMLRepository) ExpirePendingByTask(ctx context.Context, taskID string) (int, error) {
	// List all interactions and find PENDING ones for the given task.
	all, _, err := r.List(ctx, taskID, nil, 0, 0)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	count := 0
	for _, i := range all {
		if i.Status != interaction.StatusPending {
			continue
		}
		i.Status = interaction.StatusExpired
		i.RespondedAt = &now
		if err := r.Update(ctx, i); err != nil {
			return count, fmt.Errorf("failed to expire interaction %s: %w", i.ID, err)
		}
		count++
	}
	return count, nil
}
