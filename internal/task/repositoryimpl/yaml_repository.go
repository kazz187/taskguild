package repositoryimpl

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const projectsPrefix = "projects"

// knownSubdirs contains directory names that are NOT task directories.
var knownSubdirs = map[string]bool{
	"agents":                   true,
	"workflows":                true,
	"skills":                   true,
	"scripts":                  true,
	"singlecommandpermissions": true,
	"archived":                 true,
}

type YAMLRepository struct {
	storage storage.Storage
	claimMu sync.Mutex

	// In-memory cache for active tasks, lazily loaded on first access.
	cacheOnce sync.Once
	cacheMu   sync.RWMutex
	tasks     map[string]*task.Task // id -> cached task
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func taskPath(projectID, taskID string) string {
	return fmt.Sprintf("%s/%s/%s/task.yaml", projectsPrefix, projectID, taskID)
}

func archivedTaskPath(projectID, taskID string) string {
	return fmt.Sprintf("%s/%s/archived/%s/task.yaml", projectsPrefix, projectID, taskID)
}

func taskDirPath(projectID, taskID string) string {
	return fmt.Sprintf("%s/%s/%s", projectsPrefix, projectID, taskID)
}

func archivedTaskDirPath(projectID, taskID string) string {
	return fmt.Sprintf("%s/%s/archived/%s", projectsPrefix, projectID, taskID)
}

// ensureCache lazily loads all active tasks from disk into the in-memory cache.
func (r *YAMLRepository) ensureCache(ctx context.Context) {
	r.cacheOnce.Do(func() {
		r.cacheMu.Lock()
		defer r.cacheMu.Unlock()

		r.tasks = make(map[string]*task.Task)

		projectDirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			slog.Error("failed to list project dirs for task cache", "error", err)
			return
		}

		for _, pd := range projectDirs {
			pid := filepath.Base(pd)

			subdirs, err := r.storage.ListDirs(ctx, pd)
			if err != nil {
				continue
			}

			for _, sd := range subdirs {
				name := filepath.Base(sd)
				if knownSubdirs[name] {
					continue
				}

				p := taskPath(pid, name)

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
		}

		slog.Info("task cache initialized", "count", len(r.tasks))
	})
}

// copyTask returns a deep copy of the task so callers cannot mutate cached data.
func copyTask(t *task.Task) *task.Task {
	cp := *t
	if t.Metadata != nil {
		cp.Metadata = make(map[string]string, len(t.Metadata))
		maps.Copy(cp.Metadata, t.Metadata)
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

	if err := r.storage.Write(ctx, taskPath(t.ProjectID, t.ID), data); err != nil {
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

	return nil, cerr.NewError(cerr.NotFound, "task not found", nil)
}

func (r *YAMLRepository) List(ctx context.Context, projectID, workflowID, statusID string, limit, offset int) ([]*task.Task, int, error) {
	r.ensureCache(ctx)

	r.cacheMu.RLock()

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
		return cerr.NewError(cerr.NotFound, "task not found", nil)
	}

	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task: %w", err))
	}

	if err := r.storage.Write(ctx, taskPath(t.ProjectID, t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}

	r.cacheMu.Lock()
	r.tasks[t.ID] = copyTask(t)
	r.cacheMu.Unlock()

	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	cached, ok := r.tasks[id]
	r.cacheMu.RUnlock()

	if !ok {
		return cerr.NewError(cerr.NotFound, "task not found", nil)
	}

	if err := r.storage.Delete(ctx, taskPath(cached.ProjectID, id)); err != nil {
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
	task.ClearPendingReason(t.Metadata)

	if err := r.Update(ctx, t); err != nil {
		return nil, err
	}

	return t, nil
}

func (r *YAMLRepository) Archive(ctx context.Context, id string) error {
	r.ensureCache(ctx)

	r.cacheMu.RLock()
	cached, ok := r.tasks[id]
	r.cacheMu.RUnlock()

	if !ok {
		return cerr.NewError(cerr.NotFound, "task not found", nil)
	}

	pid := cached.ProjectID
	src := taskDirPath(pid, id)
	dst := archivedTaskDirPath(pid, id)

	if err := r.storage.MoveDir(ctx, src, dst); err != nil {
		return cerr.WrapStorageWriteError("archived task", err)
	}

	r.cacheMu.Lock()
	delete(r.tasks, id)
	r.cacheMu.Unlock()

	return nil
}

func (r *YAMLRepository) Unarchive(ctx context.Context, id string) error {
	// We need to find the project that has this archived task.
	// Scan all projects' archived directories.
	projectDirs, err := r.storage.ListDirs(ctx, projectsPrefix)
	if err != nil {
		return cerr.WrapStorageReadError("archived task", err)
	}

	var foundPID string

	for _, pd := range projectDirs {
		pid := filepath.Base(pd)
		archivedDir := fmt.Sprintf("%s/%s/archived", projectsPrefix, pid)

		taskDirs, err := r.storage.ListDirs(ctx, archivedDir)
		if err != nil {
			continue
		}

		for _, td := range taskDirs {
			if filepath.Base(td) == id {
				foundPID = pid
				break
			}
		}

		if foundPID != "" {
			break
		}
	}

	if foundPID == "" {
		return cerr.NewError(cerr.NotFound, "archived task not found", nil)
	}

	src := archivedTaskDirPath(foundPID, id)
	dst := taskDirPath(foundPID, id)

	if err := r.storage.MoveDir(ctx, src, dst); err != nil {
		return cerr.WrapStorageWriteError("task", err)
	}

	// Read the task and add to cache.
	data, err := r.storage.Read(ctx, taskPath(foundPID, id))
	if err == nil {
		var t task.Task
		if err := yaml.Unmarshal(data, &t); err == nil {
			r.cacheMu.Lock()
			r.tasks[t.ID] = &t
			r.cacheMu.Unlock()
		}
	}

	return nil
}

func (r *YAMLRepository) GetArchived(ctx context.Context, id string) (*task.Task, error) {
	projectDirs, err := r.storage.ListDirs(ctx, projectsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("archived task", err)
	}

	for _, pd := range projectDirs {
		pid := filepath.Base(pd)

		data, err := r.storage.Read(ctx, archivedTaskPath(pid, id))
		if err != nil {
			continue
		}

		var t task.Task
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal archived task: %w", err))
		}

		return &t, nil
	}

	return nil, cerr.NewError(cerr.NotFound, "archived task not found", nil)
}

func (r *YAMLRepository) ListArchived(ctx context.Context, projectID, workflowID string, limit, offset int) ([]*task.Task, int, error) {
	var projectIDs []string
	if projectID != "" {
		projectIDs = []string{projectID}
	} else {
		dirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			return nil, 0, cerr.WrapStorageReadError("archived tasks", err)
		}

		for _, d := range dirs {
			projectIDs = append(projectIDs, filepath.Base(d))
		}
	}

	var all []*task.Task

	for _, pid := range projectIDs {
		archivedDir := fmt.Sprintf("%s/%s/archived", projectsPrefix, pid)

		taskDirs, err := r.storage.ListDirs(ctx, archivedDir)
		if err != nil {
			continue
		}

		for _, td := range taskDirs {
			tid := filepath.Base(td)

			data, err := r.storage.Read(ctx, archivedTaskPath(pid, tid))
			if err != nil {
				continue
			}

			var t task.Task
			if err := yaml.Unmarshal(data, &t); err != nil {
				continue
			}

			if workflowID != "" && t.WorkflowID != workflowID {
				continue
			}

			all = append(all, &t)
		}
	}

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
