package repositoryimpl

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"
	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const (
	projectsPrefix      = "projects"
	listLoadConcurrency = 8
)

type entityLocation struct {
	projectID string
	taskID    string
	archived  bool
}

func (loc entityLocation) interactionPath(id string) string {
	if loc.archived {
		return fmt.Sprintf("%s/%s/archived/%s/interactions/%s.yaml", projectsPrefix, loc.projectID, loc.taskID, id)
	}

	return fmt.Sprintf("%s/%s/%s/interactions/%s.yaml", projectsPrefix, loc.projectID, loc.taskID, id)
}

func (loc entityLocation) interactionPrefix() string {
	if loc.archived {
		return fmt.Sprintf("%s/%s/archived/%s/interactions", projectsPrefix, loc.projectID, loc.taskID)
	}

	return fmt.Sprintf("%s/%s/%s/interactions", projectsPrefix, loc.projectID, loc.taskID)
}

// loadState tracks lazy per-task load progress. It uses sync.Once to guarantee
// the task's interactions are only read from disk once per repository lifetime;
// callers after the first wait on the same Once and then observe the cached err.
type loadState struct {
	once sync.Once
	err  error
}

// YAMLRepository implements interaction.Repository using YAML files on Storage.
//
// Instead of scanning the entire filesystem at startup, interactions are loaded
// lazily per task on first access. Decoded *Interaction values are cached in
// dataCache so List / Get serve from memory after the initial load. Active
// tasks are enumerated via the injected task.Repository (which maintains its
// own in-memory cache); archived tasks are only touched when explicitly
// requested, so routine operations never pay for archived I/O.
type YAMLRepository struct {
	storage  storage.Storage
	taskRepo task.Repository

	mu            sync.RWMutex
	taskIndex     map[string][]string                 // taskID -> sorted []interactionID
	locationIndex map[string]entityLocation           // interactionID -> location
	tokenIndex    map[string]string                   // responseToken -> interactionID (pending only)
	dataCache     map[string]*interaction.Interaction // interactionID -> decoded interaction

	loadStates sync.Map // taskID -> *loadState (lazy per-task load)

	// warmActiveOnce ensures token-lookup fallbacks only warm active tasks once.
	warmActiveOnce sync.Once
	warmActiveErr  error
}

// NewYAMLRepository constructs a YAMLRepository backed by the given storage
// and the provided task repository. The task repository is used to enumerate
// active / archived tasks on demand; it is required for correct per-task
// lazy loading.
func NewYAMLRepository(s storage.Storage, taskRepo task.Repository) *YAMLRepository {
	return &YAMLRepository{
		storage:       s,
		taskRepo:      taskRepo,
		taskIndex:     make(map[string][]string),
		locationIndex: make(map[string]entityLocation),
		tokenIndex:    make(map[string]string),
		dataCache:     make(map[string]*interaction.Interaction),
	}
}

func pathToID(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".yaml")
}

// cloneInteraction returns a shallow copy so callers cannot mutate cached data.
func cloneInteraction(i *interaction.Interaction) *interaction.Interaction {
	if i == nil {
		return nil
	}

	cp := *i
	if i.Options != nil {
		cp.Options = make([]interaction.Option, len(i.Options))
		copy(cp.Options, i.Options)
	}

	if i.RespondedAt != nil {
		t := *i.RespondedAt
		cp.RespondedAt = &t
	}

	return &cp
}

// locateTask resolves taskID → (projectID, archived). If the task exists
// neither actively nor archived, the zero value + false is returned and
// callers should treat the task as having no interactions.
func (r *YAMLRepository) locateTask(ctx context.Context, taskID string) (projectID string, archived bool, found bool) {
	if r.taskRepo == nil {
		return "", false, false
	}

	if t, err := r.taskRepo.Get(ctx, taskID); err == nil && t != nil {
		return t.ProjectID, false, true
	}

	if t, err := r.taskRepo.GetArchived(ctx, taskID); err == nil && t != nil {
		return t.ProjectID, true, true
	}

	return "", false, false
}

// loadTaskInteractions reads the interactions directory for a single task and
// populates in-memory indexes. Subsequent calls for the same taskID are no-ops.
func (r *YAMLRepository) loadTaskInteractions(ctx context.Context, taskID string) error {
	if taskID == "" {
		return nil
	}

	v, _ := r.loadStates.LoadOrStore(taskID, &loadState{})
	state := v.(*loadState)
	state.once.Do(func() {
		state.err = r.doLoadTaskInteractions(ctx, taskID)
	})

	return state.err
}

func (r *YAMLRepository) doLoadTaskInteractions(ctx context.Context, taskID string) error {
	projectID, archived, found := r.locateTask(ctx, taskID)
	if !found {
		// Task is unknown. Leave the task as "loaded with no interactions"
		// so callers don't retry forever.
		return nil
	}

	loc := entityLocation{projectID: projectID, taskID: taskID, archived: archived}

	files, err := r.storage.List(ctx, loc.interactionPrefix())
	if err != nil {
		return cerr.WrapStorageReadError("interaction", err)
	}

	type loaded struct {
		id    string
		inter *interaction.Interaction
	}

	entries := make([]loaded, 0, len(files))
	for _, f := range files {
		id := pathToID(f)
		if id == "" {
			continue
		}

		data, err := r.storage.Read(ctx, f)
		if err != nil {
			// Individual read failures are tolerated: stale / partially
			// written files should not prevent loading the rest.
			continue
		}

		var i interaction.Interaction
		if err := yaml.Unmarshal(data, &i); err != nil {
			continue
		}

		entries = append(entries, loaded{id, &i})
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		r.locationIndex[e.id] = loc
		r.dataCache[e.id] = e.inter

		ids = append(ids, e.id)
		if e.inter.ResponseToken != "" && e.inter.Status == interaction.StatusPending {
			r.tokenIndex[e.inter.ResponseToken] = e.id
		}
	}

	sort.Strings(ids)
	r.taskIndex[taskID] = ids

	return nil
}

// loadTasksInteractions loads multiple tasks' interactions in parallel with
// bounded concurrency. Individual task failures are logged at caller.
func (r *YAMLRepository) loadTasksInteractions(ctx context.Context, taskIDs []string) {
	if len(taskIDs) == 0 {
		return
	}

	p := pool.New().WithContext(ctx).WithMaxGoroutines(listLoadConcurrency)
	for _, tid := range taskIDs {
		p.Go(func(ctx context.Context) error {
			// Intentionally ignore per-task errors so one bad task does not
			// fail the whole list.
			_ = r.loadTaskInteractions(ctx, tid)
			return nil
		})
	}

	_ = p.Wait()
}

// warmActiveTasks loads all currently active tasks' interactions. Used as a
// fallback for token lookups after a server restart.
func (r *YAMLRepository) warmActiveTasks(ctx context.Context) {
	r.warmActiveOnce.Do(func() {
		if r.taskRepo == nil {
			return
		}

		tasks, _, err := r.taskRepo.List(ctx, "", "", "", 0, 0)
		if err != nil {
			r.warmActiveErr = err
			return
		}

		ids := make([]string, 0, len(tasks))
		for _, t := range tasks {
			ids = append(ids, t.ID)
		}

		r.loadTasksInteractions(ctx, ids)
	})
}

func (r *YAMLRepository) Create(ctx context.Context, i *interaction.Interaction) error {
	if err := r.loadTaskInteractions(ctx, i.TaskID); err != nil {
		return err
	}

	r.mu.RLock()
	_, exists := r.locationIndex[i.ID]
	r.mu.RUnlock()

	if exists {
		return cerr.NewError(cerr.AlreadyExists, "interaction already exists", nil)
	}

	data, err := yaml.Marshal(i)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal interaction: %w", err))
	}

	loc := entityLocation{projectID: i.ProjectID, taskID: i.TaskID}
	if err := r.storage.Write(ctx, loc.interactionPath(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}

	r.mu.Lock()
	r.locationIndex[i.ID] = loc
	r.dataCache[i.ID] = cloneInteraction(i)
	ids := r.taskIndex[i.TaskID]
	ids = append(ids, i.ID)
	sort.Strings(ids)

	r.taskIndex[i.TaskID] = ids
	if i.ResponseToken != "" && i.Status == interaction.StatusPending {
		r.tokenIndex[i.ResponseToken] = i.ID
	}
	r.mu.Unlock()

	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*interaction.Interaction, error) {
	// Fast path: already cached.
	r.mu.RLock()

	if cached, ok := r.dataCache[id]; ok {
		out := cloneInteraction(cached)

		r.mu.RUnlock()

		return out, nil
	}

	r.mu.RUnlock()

	// The interaction may exist under a task we haven't loaded yet. Since the
	// id alone is not enough to locate the task, Get is only guaranteed to
	// succeed after its task has been listed or the active-task set has been
	// warmed. Try warming; return NotFound if still absent.
	r.warmActiveTasks(ctx)

	r.mu.RLock()
	defer r.mu.RUnlock()

	cached, ok := r.dataCache[id]
	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "interaction not found", nil)
	}

	return cloneInteraction(cached), nil
}

// List returns interactions matching the provided filters. See
// interaction.Repository for semantics. statusFilter = StatusUnspecified means
// "no filter".
func (r *YAMLRepository) List(ctx context.Context, taskID string, taskIDs []string, statusFilter interaction.InteractionStatus, limit, offset int) ([]*interaction.Interaction, int, error) {
	// Decide which tasks to load.
	var targetTaskIDs []string

	switch {
	case taskID != "":
		targetTaskIDs = []string{taskID}
	case len(taskIDs) > 0:
		targetTaskIDs = taskIDs
	default:
		// Global list: active tasks only. Archived tasks are skipped to avoid
		// unbounded I/O; add an explicit flag in the future if needed.
		if r.taskRepo != nil {
			tasks, _, err := r.taskRepo.List(ctx, "", "", "", 0, 0)
			if err != nil {
				return nil, 0, err
			}

			targetTaskIDs = make([]string, 0, len(tasks))
			for _, t := range tasks {
				targetTaskIDs = append(targetTaskIDs, t.ID)
			}
		}
	}

	r.loadTasksInteractions(ctx, targetTaskIDs)

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Gather candidate interactions (by decoded cache) applying status filter.
	results := make([]*interaction.Interaction, 0, 64)
	seen := make(map[string]struct{})

	for _, tid := range targetTaskIDs {
		ids := r.taskIndex[tid]
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				continue
			}

			seen[id] = struct{}{}

			cached, ok := r.dataCache[id]
			if !ok {
				continue
			}

			if statusFilter != interaction.StatusUnspecified && cached.Status != statusFilter {
				continue
			}

			results = append(results, cached)
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })

	total := len(results)
	if offset >= total {
		return nil, total, nil
	}

	results = results[offset:]
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	cloned := make([]*interaction.Interaction, len(results))
	for i, v := range results {
		cloned[i] = cloneInteraction(v)
	}

	return cloned, total, nil
}

func (r *YAMLRepository) Update(ctx context.Context, i *interaction.Interaction) error {
	if err := r.loadTaskInteractions(ctx, i.TaskID); err != nil {
		return err
	}

	r.mu.RLock()
	loc, ok := r.locationIndex[i.ID]

	var prevToken string
	if prev, exists := r.dataCache[i.ID]; exists {
		prevToken = prev.ResponseToken
	}

	r.mu.RUnlock()

	if !ok {
		return cerr.NewError(cerr.NotFound, "interaction not found", nil)
	}

	data, err := yaml.Marshal(i)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal interaction: %w", err))
	}

	if err := r.storage.Write(ctx, loc.interactionPath(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}

	r.mu.Lock()
	r.dataCache[i.ID] = cloneInteraction(i)
	// Maintain tokenIndex: a pending interaction's token is valid; once the
	// status transitions away from pending (or the token changes) the old
	// token entry must be evicted so it cannot be reused.
	if prevToken != "" && (prevToken != i.ResponseToken || i.Status != interaction.StatusPending) {
		if mapped, ok := r.tokenIndex[prevToken]; ok && mapped == i.ID {
			delete(r.tokenIndex, prevToken)
		}
	}

	if i.ResponseToken != "" && i.Status == interaction.StatusPending {
		r.tokenIndex[i.ResponseToken] = i.ID
	}
	r.mu.Unlock()

	return nil
}

func (r *YAMLRepository) GetByResponseToken(ctx context.Context, token string) (*interaction.Interaction, error) {
	if token == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "token is required", nil)
	}

	r.mu.RLock()
	id, ok := r.tokenIndex[token]
	r.mu.RUnlock()

	if ok {
		return r.Get(ctx, id)
	}

	// Token not yet indexed. This typically happens after a server restart
	// before any List call has warmed the active task set. Trigger warm-up
	// and retry.
	r.warmActiveTasks(ctx)

	r.mu.RLock()
	id, ok = r.tokenIndex[token]
	r.mu.RUnlock()

	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "interaction not found for token", nil)
	}

	return r.Get(ctx, id)
}

func (r *YAMLRepository) DeleteByTaskID(ctx context.Context, taskID string) (int, error) {
	err := r.loadTaskInteractions(ctx, taskID)
	if err != nil {
		return 0, err
	}

	r.mu.RLock()
	ids := make([]string, len(r.taskIndex[taskID]))
	copy(ids, r.taskIndex[taskID])

	locs := make(map[string]entityLocation, len(ids))
	for _, id := range ids {
		if loc, ok := r.locationIndex[id]; ok {
			locs[id] = loc
		}
	}

	r.mu.RUnlock()

	count := 0

	for _, id := range ids {
		loc, ok := locs[id]
		if !ok {
			continue
		}

		err := r.storage.Delete(ctx, loc.interactionPath(id))
		if err != nil {
			return count, cerr.WrapStorageDeleteError("interaction", err)
		}

		r.mu.Lock()
		if cached, ok := r.dataCache[id]; ok {
			if cached.ResponseToken != "" {
				if mapped, ok2 := r.tokenIndex[cached.ResponseToken]; ok2 && mapped == id {
					delete(r.tokenIndex, cached.ResponseToken)
				}
			}
		}

		delete(r.dataCache, id)
		delete(r.locationIndex, id)
		r.mu.Unlock()

		count++
	}

	r.mu.Lock()
	delete(r.taskIndex, taskID)
	r.mu.Unlock()

	return count, nil
}

func (r *YAMLRepository) ExpirePendingByTask(ctx context.Context, taskID string) (int, error) {
	all, _, err := r.List(ctx, taskID, nil, interaction.StatusPending, 0, 0)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	count := 0

	for _, i := range all {
		i.Status = interaction.StatusExpired

		i.RespondedAt = &now
		err := r.Update(ctx, i)
		if err != nil {
			return count, fmt.Errorf("failed to expire interaction %s: %w", i.ID, err)
		}

		count++
	}

	return count, nil
}

// NotifyTaskArchived flips the cached location for the given task's
// interactions so subsequent writes use the archived path.
func (r *YAMLRepository) NotifyTaskArchived(ctx context.Context, projectID, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range r.taskIndex[taskID] {
		if loc, ok := r.locationIndex[id]; ok {
			loc.archived = true
			r.locationIndex[id] = loc
		}
	}

	return nil
}

// NotifyTaskUnarchived flips the cached location for the given task's
// interactions back to the active path.
func (r *YAMLRepository) NotifyTaskUnarchived(ctx context.Context, projectID, taskID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, id := range r.taskIndex[taskID] {
		if loc, ok := r.locationIndex[id]; ok {
			loc.archived = false
			r.locationIndex[id] = loc
		}
	}

	return nil
}
