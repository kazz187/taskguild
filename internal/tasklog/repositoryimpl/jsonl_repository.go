package repositoryimpl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
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

// JSONLRepository implements tasklog.Repository using JSONL files on the local filesystem.
type JSONLRepository struct {
	baseDir string

	mu        sync.Mutex
	turnFiles map[string]*turnFileState // taskID -> current turn file

	indexOnce     sync.Once
	indexMu       sync.RWMutex
	taskIndex     map[string][]string    // taskID -> sorted []logID
	locationIndex map[string]logLocation // logID -> location info
	allIDs        []string               // all log IDs in sorted order
}

func NewJSONLRepository(baseDir string) *JSONLRepository {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		abs = baseDir
	}

	return &JSONLRepository{
		baseDir:   abs,
		turnFiles: make(map[string]*turnFileState),
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

func (r *JSONLRepository) Create(ctx context.Context, l *tasklog.TaskLog) error {
	r.ensureIndex(ctx)

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
	r.ensureIndex(ctx)

	r.indexMu.RLock()

	var matchIDs []string

	switch {
	case taskID != "":
		matchIDs = make([]string, len(r.taskIndex[taskID]))
		copy(matchIDs, r.taskIndex[taskID])
	case len(taskIDs) > 0:
		for _, tid := range taskIDs {
			matchIDs = append(matchIDs, r.taskIndex[tid]...)
		}

		sort.Strings(matchIDs)
	default:
		matchIDs = make([]string, len(r.allIDs))
		copy(matchIDs, r.allIDs)
	}

	r.indexMu.RUnlock()

	total := len(matchIDs)
	if offset >= total {
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

	var needed []idAndLoc

	r.indexMu.RLock()

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

	for fp, loc := range fileGroups {
		absPath := filepath.Join(r.baseDir, fp)

		entries, err := r.readJSONLFile(absPath, loc.projectID, loc.taskID)
		if err != nil {
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

func (r *JSONLRepository) DeleteByTaskID(ctx context.Context, taskID string) (int, error) {
	r.ensureIndex(ctx)

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
	r.ensureIndex(ctx)

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

// ensureIndex lazily builds the in-memory index on first access.
func (r *JSONLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()

		r.taskIndex = make(map[string][]string)
		r.locationIndex = make(map[string]logLocation)

		projectsDir := filepath.Join(r.baseDir, "projects")

		projectDirs, err := os.ReadDir(projectsDir)
		if err != nil {
			return
		}

		for _, pd := range projectDirs {
			if !pd.IsDir() {
				continue
			}

			projectID := pd.Name()
			for _, td := range r.listTaskDirs(filepath.Join(projectsDir, projectID)) {
				r.scanTaskLogs(projectID, td.taskID, td.dir)
			}
		}

		sort.Strings(r.allIDs)

		for tid := range r.taskIndex {
			sort.Strings(r.taskIndex[tid])
		}
	})
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

// scanTaskLogs reads all .jsonl files under a task's logs directory and indexes their entries.
func (r *JSONLRepository) scanTaskLogs(projectID, taskID, dir string) {
	logsPath := filepath.Join(r.baseDir, "projects", projectID, dir, "logs")

	entries, err := os.ReadDir(logsPath)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		absPath := filepath.Join(logsPath, entry.Name())
		relPath := filepath.Join("projects", projectID, dir, "logs", entry.Name())

		ids := r.scanJSONLIDs(absPath)
		for _, id := range ids {
			r.locationIndex[id] = logLocation{
				projectID: projectID,
				taskID:    taskID,
				filePath:  relPath,
			}
			r.taskIndex[taskID] = append(r.taskIndex[taskID], id)
			r.allIDs = append(r.allIDs, id)
		}
	}
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
