package repositoryimpl

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const tasksPrefix = "tasks"
const archivedPrefix = "tasks/archived"

type YAMLRepository struct {
	storage storage.Storage
	claimMu sync.Mutex

	// In-memory cache for active tasks, lazily loaded on first access.
	// Eliminates repeated full-directory scans that caused excessive disk IO.
	cacheOnce sync.Once
	cacheMu   sync.RWMutex
	tasks     map[string]*task.Task // id -> cached task
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", tasksPrefix, id)
}

func archivedPath(id string) string {
	return fmt.Sprintf("%s/%s.yaml", archivedPrefix, id)
}

// ensureCache lazily loads all active tasks from disk into the in-memory cache.
// Uses sync.Once so the disk scan happens only once per process lifetime.
func (r *YAMLRepository) ensureCache(ctx context.Context) {
	r.cacheOnce.Do(func() {
		r.cacheMu.Lock()
		defer r.cacheMu.Unlock()
		r.tasks = make(map[string]*task.Task)

		paths, err := r.storage.List(ctx, tasksPrefix)
		if err != nil {
			slog.Error("failed to list tasks for cache", "error", err)
			return
		}

		for _, p := range paths {
			data, err := r.storage.Read(ctx, p)
			if err != nil {
				continue
			}
			var t task.Task
			if err := yaml.Unmarshal(data, &t); err != nil {
				continue
			}
			r.tasks[t.ID] = &t
		}
		slog.Info("task cache initialized", "count", len(r.tasks))
	})
}

// copyTask returns a deep copy of the task so callers cannot mutate cached data.
func copyTask(t *task.Task) *task.Task {
	cp := *t
	if t.Metadata != nil {
		cp.Metadata = make(map[string]string, len(t.Metadata))
		for k, v := range t.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

func (r *YAMLRepository) Create(ctx context.Context, t *task.Task) error {
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	_, exists := r.tasks[t.ID]
	r.cacheMu.RUnlock()
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

	r.cacheMu.Lock()
	r.tasks[t.ID] = copyTask(t)
	r.cacheMu.Unlock()
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*task.Task, error) {
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	cached, ok := r.tasks[id]
	r.cacheMu.RUnlock()
	if ok {
		return copyTask(cached), nil
	}

	// Not in cache — might be a race or not loaded. Fall back to disk.
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("task", err)
	}
	var t task.Task
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal task: %w", err))
	}

	// Add to cache for future access.
	r.cacheMu.Lock()
	r.tasks[t.ID] = copyTask(&t)
	r.cacheMu.Unlock()
	return &t, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID, workflowID, statusID string, limit, offset int) ([]*task.Task, int, error) {
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	// Collect matching tasks from cache.
	var all []*task.Task
	for _, t := range r.tasks {
		if projectID != "" && t.ProjectID != projectID {
			continue
		}
		if workflowID != "" && t.WorkflowID != workflowID {
			continue
		}
		if statusID != "" && t.StatusID != statusID {
			continue
		}
		all = append(all, copyTask(t))
	}
	r.cacheMu.RUnlock()

	// Sort by ID for deterministic ordering.
	sort.Slice(all, func(i, j int) bool {
		return all[i].ID < all[j].ID
	})

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
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	_, exists := r.tasks[t.ID]
	r.cacheMu.RUnlock()
	if !exists {
		// Fall back to disk check for tasks not in cache.
		diskExists, err := r.storage.Exists(ctx, path(t.ID))
		if err != nil {
			return cerr.WrapStorageWriteError("task", err)
		}
		if !diskExists {
			return cerr.NewError(cerr.NotFound, "task not found", nil)
		}
	}

	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task: %w", err))
	}
	if err := r.storage.Write(ctx, path(t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}

	r.cacheMu.Lock()
	r.tasks[t.ID] = copyTask(t)
	r.cacheMu.Unlock()
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("task", err)
	}

	r.cacheMu.Lock()
	delete(r.tasks, id)
	r.cacheMu.Unlock()
	return nil
}

func (r *YAMLRepository) ReleaseByAgent(ctx context.Context, agentID string) ([]*task.Task, error) {
	r.ensureCache(ctx)
	r.claimMu.Lock()
	defer r.claimMu.Unlock()

	r.cacheMu.RLock()
	var toRelease []*task.Task
	for _, t := range r.tasks {
		if t.AssignedAgentID == agentID && t.AssignmentStatus == task.AssignmentStatusAssigned {
			toRelease = append(toRelease, copyTask(t))
		}
	}
	r.cacheMu.RUnlock()

	var released []*task.Task
	now := time.Now()
	for _, t := range toRelease {
		t.AssignedAgentID = ""
		t.AssignmentStatus = task.AssignmentStatusPending
		t.UpdatedAt = now
		if err := r.Update(ctx, t); err != nil {
			continue
		}
		released = append(released, t)
	}
	return released, nil
}

func (r *YAMLRepository) ReleaseByAgentExcept(ctx context.Context, agentID string, keepSet map[string]struct{}) ([]*task.Task, error) {
	r.ensureCache(ctx)
	r.claimMu.Lock()
	defer r.claimMu.Unlock()

	r.cacheMu.RLock()
	var toRelease []*task.Task
	for _, t := range r.tasks {
		if t.AssignedAgentID != agentID || t.AssignmentStatus != task.AssignmentStatusAssigned {
			continue
		}
		if _, keep := keepSet[t.ID]; keep {
			continue
		}
		toRelease = append(toRelease, copyTask(t))
	}
	r.cacheMu.RUnlock()

	var released []*task.Task
	now := time.Now()
	for _, t := range toRelease {
		t.AssignedAgentID = ""
		t.AssignmentStatus = task.AssignmentStatusPending
		t.UpdatedAt = now
		if err := r.Update(ctx, t); err != nil {
			continue
		}
		released = append(released, t)
	}
	return released, nil
}

func (r *YAMLRepository) Claim(ctx context.Context, taskID, agentID string) (*task.Task, error) {
	r.claimMu.Lock()
	defer r.claimMu.Unlock()

	t, err := r.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if t.AssignmentStatus != task.AssignmentStatusPending {
		return nil, cerr.NewError(cerr.FailedPrecondition, "task is not pending assignment", nil)
	}

	t.AssignmentStatus = task.AssignmentStatusAssigned
	t.AssignedAgentID = agentID
	t.UpdatedAt = time.Now()

	if err := r.Update(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (r *YAMLRepository) Archive(ctx context.Context, id string) error {
	// Read the task from active path.
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return cerr.WrapStorageReadError("task", err)
	}

	// Write to archived path.
	if err := r.storage.Write(ctx, archivedPath(id), data); err != nil {
		return cerr.WrapStorageWriteError("archived task", err)
	}

	// Delete from active path.
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		// Try to clean up archived copy on failure.
		_ = r.storage.Delete(ctx, archivedPath(id))
		return cerr.WrapStorageDeleteError("task", err)
	}

	// Remove from cache.
	r.cacheMu.Lock()
	delete(r.tasks, id)
	r.cacheMu.Unlock()
	return nil
}

func (r *YAMLRepository) Unarchive(ctx context.Context, id string) error {
	// Read the task from archived path.
	data, err := r.storage.Read(ctx, archivedPath(id))
	if err != nil {
		return cerr.WrapStorageReadError("archived task", err)
	}

	// Write to active path.
	if err := r.storage.Write(ctx, path(id), data); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}

	// Delete from archived path.
	if err := r.storage.Delete(ctx, archivedPath(id)); err != nil {
		// Try to clean up active copy on failure.
		_ = r.storage.Delete(ctx, path(id))
		return cerr.WrapStorageDeleteError("archived task", err)
	}

	// Add to cache.
	var t task.Task
	if err := yaml.Unmarshal(data, &t); err == nil {
		r.cacheMu.Lock()
		r.tasks[t.ID] = &t
		r.cacheMu.Unlock()
	}
	return nil
}

func (r *YAMLRepository) GetArchived(ctx context.Context, id string) (*task.Task, error) {
	data, err := r.storage.Read(ctx, archivedPath(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("archived task", err)
	}
	var t task.Task
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal archived task: %w", err))
	}
	return &t, nil
}

func (r *YAMLRepository) ListArchived(ctx context.Context, projectID, workflowID string, limit, offset int) ([]*task.Task, int, error) {
	paths, err := r.storage.List(ctx, archivedPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("archived tasks", err)
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
