package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/tasklog"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const taskLogsPrefix = "task_logs"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", taskLogsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, l *tasklog.TaskLog) error {
	exists, err := r.storage.Exists(ctx, path(l.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("task_log", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "task log already exists", nil)
	}
	data, err := yaml.Marshal(l)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task log: %w", err))
	}
	if err := r.storage.Write(ctx, path(l.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task_log", err)
	}
	return nil
}

func (r *YAMLRepository) List(ctx context.Context, taskID string, limit, offset int) ([]*tasklog.TaskLog, int, error) {
	paths, err := r.storage.List(ctx, taskLogsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("task_logs", err)
	}

	sort.Strings(paths)

	var all []*tasklog.TaskLog
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var l tasklog.TaskLog
		if err := yaml.Unmarshal(data, &l); err != nil {
			continue
		}
		if taskID != "" && l.TaskID != taskID {
			continue
		}
		all = append(all, &l)
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
