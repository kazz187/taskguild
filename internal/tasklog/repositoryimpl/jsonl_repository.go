package repositoryimpl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/sourcegraph/conc/pool"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

const (
	projectsPrefix      = "projects"
	listLoadConcurrency = 8
)

// knownSubdirs contains directory names under a project that are NOT task directories.
var knownSubdirs = map[string]bool{
	"agents":                   true,
	"workflows":                true,
	"skills":                   true,
	"scripts":                  true,
	"singlecommandpermissions": true,
	"archived":                 true,
}

var categoryNames = map[int32]string{
	0: "unspecified", 1: "turn_start", 2: "turn_end", 3: "status_change",
	4: "hook", 5: "stderr", 6: "error", 7: "system",
	8: "tool_use", 9: "agent_output", 10: "directive", 11: "result",
}

var categoryValues map[string]int32

var levelNames = map[int32]string{
	0: "unspecified", 1: "info", 2: "debug", 3: "warn", 4: "error",
}

var levelValues map[string]int32

func init() {
	categoryValues = make(map[string]int32, len(categoryNames))
	for k, v := range categoryNames {
		categoryValues[v] = k
	}
	levelValues = make(map[string]int32, len(levelNames))
	for k, v := range levelNames {
		levelValues[v] = k
	}
}

// jsonlEntry is the on-disk representation of a single log line.
type jsonlEntry struct {
	ID        string            `json:"id"`
	Level     string            `json:"level"`
	Category  string            `json:"category"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

type turnFileState struct {
	file      *os.File
	projectID string
	taskID    string
	turnID    string
}

type logLocation struct {
	projectID string
	taskID    string
	filePath  string // relative path to the .jsonl file within baseDir
}

// loadState guards per-task lazy index population. Once Done, it is never
// re-run so the first observation of a task is the authoritative one for the
// lifetime of the repository. Create() maintains the index incrementally from
// that point onwards.
type loadState struct {
	once sync.Once
}

// JSONLRepository implements tasklog.Repository using JSONL files on the local filesystem.
//
// The in-memory index is built lazily on a per-task basis: the first time a
// task's logs are requested, its directory is scanned and the IDs are cached.
// Subsequent reads serve from the index without another directory walk. A
// task.Repository is used to enumerate active tasks when a global List is
// issued; archived tasks are not scanned by default. The task.Repository is
// optional — tests and edge cases (orphaned log directories) fall back to a
// filesystem search.
type JSONLRepository struct {
	baseDir  string
	taskRepo task.Repository

	mu        sync.Mutex
	turnFiles map[string]*turnFileState // taskID -> current turn file

	indexMu       sync.RWMutex
	taskIndex     map[string][]string    // taskID -> sorted []logID
	locationIndex map[string]logLocation // logID -> location info
	allIDs        []string               // all log IDs in sorted order

	loadStates sync.Map // taskID -> *loadState
}

// NewJSONLRepository constructs a JSONLRepository rooted at baseDir. The
// taskRepo is used to enumerate active tasks and resolve taskID → projectID;
// nil is accepted for tests and orphan-log scenarios (in which case the repo
// falls back to directory scanning).
func NewJSONLRepository(baseDir string, taskRepo task.Repository) *JSONLRepository {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		abs = baseDir
	}
	return &JSONLRepository{
		baseDir:       abs,
		taskRepo:      taskRepo,
		turnFiles:     make(map[string]*turnFileState),
		taskIndex:     make(map[string][]string),
		locationIndex: make(map[string]logLocation),
	}
}

// Close closes all open turn file handles.
func (r *JSONLRepository) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for taskID, ts := range r.turnFiles {
		ts.file.Close()
		delete(r.turnFiles, taskID)
	}
}

func (r *JSONLRepository) logsDir(projectID, taskID string) string {
	return filepath.Join(r.baseDir, "projects", projectID, taskID, "logs")
}

func (r *JSONLRepository) archivedLogsDir(projectID, taskID string) string {
	return filepath.Join(r.baseDir, "projects", projectID, "archived", taskID, "logs")
}

// openTurnFile creates a new JSONL file for a turn and registers it. Caller must hold r.mu.
func (r *JSONLRepository) openTurnFile(projectID, taskID, turnID string) (*os.File, error) {
	p := filepath.Join(r.logsDir(projectID, taskID), turnID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create logs dir: %w", err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", turnID, err)
	}
	r.turnFiles[taskID] = &turnFileState{
		file:      f,
		projectID: projectID,
		taskID:    taskID,
		turnID:    turnID,
	}
	return f, nil
}

// closeTurnFile closes and removes the turn file for a task. Caller must hold r.mu.
func (r *JSONLRepository) closeTurnFile(taskID string) {
	if ts, ok := r.turnFiles[taskID]; ok {
		ts.file.Close()
		delete(r.turnFiles, taskID)
	}
}

func toJSONLEntry(l *tasklog.TaskLog) jsonlEntry {
	cat, ok := categoryNames[l.Category]
	if !ok {
		cat = fmt.Sprintf("unknown_%d", l.Category)
	}
	lvl, ok := levelNames[l.Level]
	if !ok {
		lvl = fmt.Sprintf("unknown_%d", l.Level)
	}
	return jsonlEntry{
		ID:        l.ID,
		Level:     lvl,
		Category:  cat,
		Message:   l.Message,
		Metadata:  l.Metadata,
		CreatedAt: l.CreatedAt,
	}
}

func fromJSONLEntry(e *jsonlEntry, projectID, taskID string) *tasklog.TaskLog {
	return &tasklog.TaskLog{
		ID:        e.ID,
		ProjectID: projectID,
		TaskID:    taskID,
		Level:     levelValues[e.Level],
		Category:  categoryValues[e.Category],
		Message:   e.Message,
		Metadata:  e.Metadata,
		CreatedAt: e.CreatedAt,
	}
}

// locateTaskDir resolves taskID → logs directory path. It consults the task
// repository first (cache-friendly), then falls back to scanning the base
// projects directory. Returns ("", false) if the task's log directory cannot
// be found.
func (r *JSONLRepository) locateTaskDir(ctx context.Context, taskID string) (logsDir string, ok bool) {
	// Prefer the task repository if available: cheap and authoritative.
	if r.taskRepo != nil {
		if t, err := r.taskRepo.Get(ctx, taskID); err == nil && t != nil {
			return r.logsDir(t.ProjectID, taskID), true
		}
		if t, err := r.taskRepo.GetArchived(ctx, taskID); err == nil && t != nil {
			return r.archivedLogsDir(t.ProjectID, taskID), true
		}
	}

	// Fallback: scan projects on disk. Used for orphan log directories and
	// tests that don't wire up a taskRepo.
	projectsDir := filepath.Join(r.baseDir, projectsPrefix)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", false
	}
	for _, pd := range entries {
		if !pd.IsDir() {
			continue
		}
		pid := pd.Name()
		if info, err := os.Stat(filepath.Join(projectsDir, pid, taskID)); err == nil && info.IsDir() {
			return r.logsDir(pid, taskID), true
		}
		if info, err := os.Stat(filepath.Join(projectsDir, pid, "archived", taskID)); err == nil && info.IsDir() {
			return r.archivedLogsDir(pid, taskID), true
		}
	}
	return "", false
}

// loadTaskLogs scans a single task's logs directory and populates indexes.
// The work is gated by sync.Once so the directory is read at most once per
// task per repository lifetime.
func (r *JSONLRepository) loadTaskLogs(ctx context.Context, taskID string) {
	if taskID == "" {
		return
	}
	v, _ := r.loadStates.LoadOrStore(taskID, &loadState{})
	state := v.(*loadState)
	state.once.Do(func() {
		logsPath, ok := r.locateTaskDir(ctx, taskID)
		if !ok {
			return
		}
		entries, err := os.ReadDir(logsPath)
		if err != nil {
			return
		}

		type fileIDs struct {
			relPath string
			ids     []string
		}
		var files []fileIDs
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			absPath := filepath.Join(logsPath, entry.Name())
			rel, err := filepath.Rel(r.baseDir, absPath)
			if err != nil {
				continue
			}
			files = append(files, fileIDs{relPath: rel, ids: r.scanJSONLIDs(absPath)})
		}

		// Determine projectID for index entries — from the first file's path.
		// logsPath is under baseDir/projects/<pid>/[archived/]<taskID>/logs.
		var projectID string
		rel, err := filepath.Rel(filepath.Join(r.baseDir, projectsPrefix), logsPath)
		if err == nil {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			if len(parts) > 0 {
				projectID = parts[0]
			}
		}

		r.indexMu.Lock()
		defer r.indexMu.Unlock()
		var taskIDs []string
		for _, fi := range files {
			for _, id := range fi.ids {
				r.locationIndex[id] = logLocation{
					projectID: projectID,
					taskID:    taskID,
					filePath:  fi.relPath,
				}
				taskIDs = append(taskIDs, id)
				r.allIDs = append(r.allIDs, id)
			}
		}
		sort.Strings(taskIDs)
		r.taskIndex[taskID] = taskIDs
		sort.Strings(r.allIDs)
	})
}

// loadManyTaskLogs loads logs for many tasks in parallel with bounded concurrency.
func (r *JSONLRepository) loadManyTaskLogs(ctx context.Context, taskIDs []string) {
	if len(taskIDs) == 0 {
		return
	}
	p := pool.New().WithContext(ctx).WithMaxGoroutines(listLoadConcurrency)
	for _, tid := range taskIDs {
		p.Go(func(ctx context.Context) error {
			r.loadTaskLogs(ctx, tid)
			return nil
		})
	}
	_ = p.Wait()
}

func (r *JSONLRepository) Create(ctx context.Context, l *tasklog.TaskLog) error {
	r.loadTaskLogs(ctx, l.TaskID)

	r.indexMu.RLock()
	_, exists := r.locationIndex[l.ID]
	r.indexMu.RUnlock()
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "task log already exists", nil)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// On TURN_START, open a new JSONL file.
	if l.Category == int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START) {
		r.closeTurnFile(l.TaskID)
		if _, err := r.openTurnFile(l.ProjectID, l.TaskID, ulid.Make().String()); err != nil {
			return cerr.NewError(cerr.Internal, "server error", err)
		}
	}

	// Get or create the file to write to (falls back to _default.jsonl).
	if _, ok := r.turnFiles[l.TaskID]; !ok {
		if _, err := r.openTurnFile(l.ProjectID, l.TaskID, "_default"); err != nil {
			return cerr.NewError(cerr.Internal, "server error", err)
		}
	}
	ts := r.turnFiles[l.TaskID]

	// Serialize and append.
	entry := toJSONLEntry(l)
	data, err := json.Marshal(entry)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task log: %w", err))
	}
	data = append(data, '\n')
	if _, err := ts.file.Write(data); err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to write task log: %w", err))
	}

	relPath := filepath.Join("projects", l.ProjectID, l.TaskID, "logs", ts.turnID+".jsonl")
	r.addToIndex(l.ID, l.ProjectID, l.TaskID, relPath)

	// On TURN_END, close the turn file.
	if l.Category == int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END) {
		r.closeTurnFile(l.TaskID)
	}

	return nil
}

func (r *JSONLRepository) List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*tasklog.TaskLog, int, error) {
	// Resolve the set of tasks to query.
	var targetTaskIDs []string
	switch {
	case taskID != "":
		targetTaskIDs = []string{taskID}
	case len(taskIDs) > 0:
		targetTaskIDs = taskIDs
	default:
		// Global list: active tasks only. Archived tasks are excluded to
		// keep this call bounded even with large histories on disk.
		if r.taskRepo != nil {
			tasks, _, err := r.taskRepo.List(ctx, "", "", "", 0, 0)
			if err != nil {
				return nil, 0, err
			}
			targetTaskIDs = make([]string, 0, len(tasks))
			for _, t := range tasks {
				targetTaskIDs = append(targetTaskIDs, t.ID)
			}
		} else {
			// No task repo wired: fall back to a filesystem scan of project
			// directories for their active task subdirectories.
			targetTaskIDs = r.discoverActiveTaskIDs()
		}
	}

	r.loadManyTaskLogs(ctx, targetTaskIDs)

	// Gather matching IDs from the index.
	r.indexMu.RLock()
	var matchIDs []string
	if taskID != "" {
		matchIDs = append([]string(nil), r.taskIndex[taskID]...)
	} else {
		for _, tid := range targetTaskIDs {
			matchIDs = append(matchIDs, r.taskIndex[tid]...)
		}
	}
	sort.Strings(matchIDs)

	total := len(matchIDs)
	if offset >= total {
		r.indexMu.RUnlock()
		return nil, total, nil
	}
	paginated := matchIDs[offset:]
	if limit > 0 && len(paginated) > limit {
		paginated = paginated[:limit]
	}

	type idAndLoc struct {
		id  string
		loc logLocation
	}
	needed := make([]idAndLoc, 0, len(paginated))
	for _, id := range paginated {
		if loc, ok := r.locationIndex[id]; ok {
			needed = append(needed, idAndLoc{id, loc})
		}
	}
	r.indexMu.RUnlock()

	// Group by file path to avoid re-reading the same file.
	fileEntries := make(map[string]map[string]*tasklog.TaskLog)
	fileGroups := make(map[string]logLocation)
	neededIDs := make(map[string]bool, len(needed))
	for _, n := range needed {
		neededIDs[n.id] = true
		fileGroups[n.loc.filePath] = n.loc
	}

	var missingPaths []string
	for fp, loc := range fileGroups {
		absPath := filepath.Join(r.baseDir, fp)
		entries, err := r.readJSONLFile(absPath, loc.projectID, loc.taskID)
		if err != nil {
			// Missing files are treated as a stale index and silently
			// evicted. Other I/O errors (permission denied etc.) are still
			// logged so operators notice them.
			if errors.Is(err, os.ErrNotExist) {
				missingPaths = append(missingPaths, fp)
				continue
			}
			slog.Warn("failed to read JSONL file during List, skipping", "path", absPath, "error", err)
			continue
		}
		m := make(map[string]*tasklog.TaskLog, len(entries))
		for _, e := range entries {
			if neededIDs[e.ID] {
				m[e.ID] = e
			}
		}
		fileEntries[fp] = m
	}

	// Evict stale index entries pointing at missing files so the next List
	// does not retry them.
	if len(missingPaths) > 0 {
		r.evictMissingPaths(missingPaths)
	}

	result := make([]*tasklog.TaskLog, 0, len(paginated))
	for _, n := range needed {
		if m, ok := fileEntries[n.loc.filePath]; ok {
			if l, ok := m[n.id]; ok {
				result = append(result, l)
			}
		}
	}

	return result, total, nil
}

// evictMissingPaths removes all index entries for the given stale relative
// file paths so that subsequent List calls no longer surface the ENOENT
// error for the same files.
func (r *JSONLRepository) evictMissingPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	stale := make(map[string]bool, len(paths))
	for _, p := range paths {
		stale[p] = true
	}

	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	// Collect ids to remove.
	var removeIDs []string
	byTask := make(map[string][]string)
	for id, loc := range r.locationIndex {
		if stale[loc.filePath] {
			removeIDs = append(removeIDs, id)
			byTask[loc.taskID] = append(byTask[loc.taskID], id)
		}
	}
	if len(removeIDs) == 0 {
		return
	}
	removeSet := make(map[string]bool, len(removeIDs))
	for _, id := range removeIDs {
		delete(r.locationIndex, id)
		removeSet[id] = true
	}

	// Prune taskIndex per-task.
	for tid, ids := range byTask {
		cur := r.taskIndex[tid]
		filtered := cur[:0]
		rm := make(map[string]bool, len(ids))
		for _, id := range ids {
			rm[id] = true
		}
		for _, id := range cur {
			if !rm[id] {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) == 0 {
			delete(r.taskIndex, tid)
		} else {
			r.taskIndex[tid] = filtered
		}
	}

	// Prune allIDs in-place (preserving order).
	filteredAll := r.allIDs[:0]
	for _, id := range r.allIDs {
		if !removeSet[id] {
			filteredAll = append(filteredAll, id)
		}
	}
	r.allIDs = filteredAll
}

func (r *JSONLRepository) DeleteByTaskID(ctx context.Context, taskID string) (int, error) {
	r.loadTaskLogs(ctx, taskID)

	r.mu.Lock()
	r.closeTurnFile(taskID)
	r.mu.Unlock()

	r.indexMu.RLock()
	ids := make([]string, len(r.taskIndex[taskID]))
	copy(ids, r.taskIndex[taskID])
	var projectID string
	if len(ids) > 0 {
		if loc, ok := r.locationIndex[ids[0]]; ok {
			projectID = loc.projectID
		}
	}
	r.indexMu.RUnlock()

	count := len(ids)
	if count == 0 {
		return 0, nil
	}

	if err := os.RemoveAll(r.logsDir(projectID, taskID)); err != nil {
		return 0, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to remove logs dir: %w", err))
	}

	r.indexMu.Lock()
	for _, id := range ids {
		delete(r.locationIndex, id)
	}
	delete(r.taskIndex, taskID)
	deletedSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		deletedSet[id] = true
	}
	newAllIDs := make([]string, 0, len(r.allIDs)-len(ids))
	for _, id := range r.allIDs {
		if !deletedSet[id] {
			newAllIDs = append(newAllIDs, id)
		}
	}
	r.allIDs = newAllIDs
	r.indexMu.Unlock()

	return count, nil
}

func (r *JSONLRepository) CleanupOlderThan(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge)
	deleted := 0

	projectsDir := filepath.Join(r.baseDir, "projects")
	projectDirs, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to read projects dir: %w", err)
	}

	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		projectID := pd.Name()
		for _, td := range r.listTaskDirs(filepath.Join(projectsDir, projectID)) {
			// Ensure the task's logs are indexed before we touch files under
			// its logs directory, so the index stays in sync with deletions.
			r.loadTaskLogs(ctx, td.taskID)

			logsPath := filepath.Join(projectsDir, projectID, td.dir, "logs")
			logFiles, err := os.ReadDir(logsPath)
			if err != nil {
				continue
			}
			for _, lf := range logFiles {
				if lf.IsDir() || !strings.HasSuffix(lf.Name(), ".jsonl") {
					continue
				}
				info, err := lf.Info()
				if err != nil {
					continue
				}
				if info.ModTime().Before(cutoff) {
					absPath := filepath.Join(logsPath, lf.Name())
					ids := r.scanJSONLIDs(absPath)
					if err := os.Remove(absPath); err != nil {
						continue
					}
					for _, id := range ids {
						r.removeFromIndex(id, td.taskID)
						deleted++
					}
				}
			}
		}
	}

	if deleted > 0 {
		slog.Info("task log cleanup completed", "deleted", deleted, "max_age", maxAge)
	}
	return deleted, nil
}

// readJSONLFile reads all entries from a JSONL file and converts them to TaskLog entities.
func (r *JSONLRepository) readJSONLFile(absPath, projectID, taskID string) ([]*tasklog.TaskLog, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var result []*tasklog.TaskLog
	scanner := bufio.NewScanner(f)
	// Large buffer for agent_output entries that can contain full LLM responses.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e jsonlEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		result = append(result, fromJSONLEntry(&e, projectID, taskID))
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error reading JSONL file", "path", absPath, "error", err)
	}
	return result, nil
}

// scanJSONLIDs extracts only the IDs from a JSONL file without full deserialization.
func (r *JSONLRepository) scanJSONLIDs(absPath string) []string {
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var ids []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var partial struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(line, &partial); err != nil {
			continue
		}
		if partial.ID != "" {
			ids = append(ids, partial.ID)
		}
	}
	return ids
}

// discoverActiveTaskIDs enumerates active task directories on disk. Used as a
// fallback when no taskRepo is wired (primarily in tests).
func (r *JSONLRepository) discoverActiveTaskIDs() []string {
	projectsDir := filepath.Join(r.baseDir, projectsPrefix)
	projectDirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}
	var result []string
	for _, pd := range projectDirs {
		if !pd.IsDir() {
			continue
		}
		projectPath := filepath.Join(projectsDir, pd.Name())
		subdirs, err := os.ReadDir(projectPath)
		if err != nil {
			continue
		}
		for _, sd := range subdirs {
			if !sd.IsDir() {
				continue
			}
			name := sd.Name()
			if knownSubdirs[name] {
				continue
			}
			result = append(result, name)
		}
	}
	return result
}

type taskDirEntry struct {
	taskID string
	dir    string // relative path from project dir (e.g., "TASKID" or "archived/TASKID")
}

// listTaskDirs returns all task directory entries (active + archived) for a project path.
func (r *JSONLRepository) listTaskDirs(projectPath string) []taskDirEntry {
	subdirs, err := os.ReadDir(projectPath)
	if err != nil {
		return nil
	}
	var result []taskDirEntry
	for _, sd := range subdirs {
		if !sd.IsDir() {
			continue
		}
		name := sd.Name()
		if name == "archived" {
			archivedPath := filepath.Join(projectPath, "archived")
			archivedDirs, err := os.ReadDir(archivedPath)
			if err != nil {
				continue
			}
			for _, ad := range archivedDirs {
				if ad.IsDir() {
					result = append(result, taskDirEntry{taskID: ad.Name(), dir: filepath.Join("archived", ad.Name())})
				}
			}
		} else if !knownSubdirs[name] {
			result = append(result, taskDirEntry{taskID: name, dir: name})
		}
	}
	return result
}

func (r *JSONLRepository) addToIndex(id, projectID, taskID, relPath string) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	r.locationIndex[id] = logLocation{projectID: projectID, taskID: taskID, filePath: relPath}
	r.taskIndex[taskID] = append(r.taskIndex[taskID], id)
	// ULIDs are monotonically increasing, so the new ID is always the largest.
	r.allIDs = append(r.allIDs, id)
}

func (r *JSONLRepository) removeFromIndex(id, taskID string) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	delete(r.locationIndex, id)

	if ids, ok := r.taskIndex[taskID]; ok {
		for i, eid := range ids {
			if eid == id {
				r.taskIndex[taskID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(r.taskIndex[taskID]) == 0 {
			delete(r.taskIndex, taskID)
		}
	}

	for i, eid := range r.allIDs {
		if eid == id {
			r.allIDs = append(r.allIDs[:i], r.allIDs[i+1:]...)
			break
		}
	}
}
