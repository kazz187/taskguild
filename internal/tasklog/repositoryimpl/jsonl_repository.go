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

	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/oklog/ulid/v2"
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

// categoryNames maps proto int32 values to human-readable strings.
var categoryNames = map[int32]string{
	0: "unspecified", 1: "turn_start", 2: "turn_end", 3: "status_change",
	4: "hook", 5: "stderr", 6: "error", 7: "system",
	8: "tool_use", 9: "agent_output", 10: "directive", 11: "result",
}

// categoryValues is the reverse map of categoryNames.
var categoryValues map[string]int32

// levelNames maps proto int32 values to human-readable strings.
var levelNames = map[int32]string{
	0: "unspecified", 1: "info", 2: "debug", 3: "warn", 4: "error",
}

// levelValues is the reverse map of levelNames.
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

const (
	categoryTurnStart int32 = 1
	categoryTurnEnd   int32 = 2
)

// jsonlEntry is the on-disk representation of a single log line.
type jsonlEntry struct {
	ID        string            `json:"id"`
	Level     string            `json:"level"`
	Category  string            `json:"category"`
	Message   string            `json:"message"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

// turnFileState tracks an open JSONL file for a task's current turn.
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
	taskIndex     map[string][]string      // taskID -> sorted []logID
	locationIndex map[string]logLocation   // logID -> location info
	allIDs        []string                 // all log IDs in sorted order
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

func (r *JSONLRepository) logsDir(projectID, taskID string) string {
	return filepath.Join(r.baseDir, "projects", projectID, taskID, "logs")
}

func (r *JSONLRepository) turnFilePath(projectID, taskID, turnID string) string {
	return filepath.Join(r.logsDir(projectID, taskID), turnID+".jsonl")
}

func (r *JSONLRepository) defaultFilePath(projectID, taskID string) string {
	return filepath.Join(r.logsDir(projectID, taskID), "_default.jsonl")
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

// getOrCreateTurnFile returns the file handle for the current turn of the given task.
// Caller must hold r.mu.
func (r *JSONLRepository) getOrCreateTurnFile(projectID, taskID string) (*os.File, error) {
	if ts, ok := r.turnFiles[taskID]; ok {
		return ts.file, nil
	}
	// No active turn; use _default.jsonl.
	p := r.defaultFilePath(projectID, taskID)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create logs dir: %w", err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open default log file: %w", err)
	}
	r.turnFiles[taskID] = &turnFileState{
		file:      f,
		projectID: projectID,
		taskID:    taskID,
		turnID:    "_default",
	}
	return f, nil
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
	if l.Category == categoryTurnStart {
		// Close any existing turn file for this task.
		if ts, ok := r.turnFiles[l.TaskID]; ok {
			ts.file.Close()
			delete(r.turnFiles, l.TaskID)
		}

		turnID := ulid.Make().String()
		p := r.turnFilePath(l.ProjectID, l.TaskID, turnID)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to create logs dir: %w", err))
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to create turn log file: %w", err))
		}
		r.turnFiles[l.TaskID] = &turnFileState{
			file:      f,
			projectID: l.ProjectID,
			taskID:    l.TaskID,
			turnID:    turnID,
		}
	}

	// Get the file to write to.
	f, err := r.getOrCreateTurnFile(l.ProjectID, l.TaskID)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", err)
	}

	// Serialize and append.
	entry := toJSONLEntry(l)
	data, err := json.Marshal(entry)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task log: %w", err))
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to write task log: %w", err))
	}

	// Determine the relative file path for the index.
	ts := r.turnFiles[l.TaskID]
	relPath := filepath.Join("projects", l.ProjectID, l.TaskID, "logs", ts.turnID+".jsonl")
	r.addToIndex(l.ID, l.ProjectID, l.TaskID, relPath)

	// On TURN_END, close the turn file.
	if l.Category == categoryTurnEnd {
		if ts, ok := r.turnFiles[l.TaskID]; ok {
			ts.file.Close()
			delete(r.turnFiles, l.TaskID)
		}
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

	// Collect unique file paths to read.
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
	fileEntries := make(map[string]map[string]*tasklog.TaskLog) // filePath -> logID -> TaskLog
	fileGroups := make(map[string]logLocation)                   // filePath -> location (for projectID/taskID)
	neededIDs := make(map[string]bool, len(needed))
	for _, n := range needed {
		neededIDs[n.id] = true
		fileGroups[n.loc.filePath] = n.loc
	}

	for fp, loc := range fileGroups {
		absPath := filepath.Join(r.baseDir, fp)
		entries, err := r.readJSONLFile(absPath, loc.projectID, loc.taskID)
		if err != nil {
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

	// Build result in order.
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

	// Close any open turn file for this task.
	r.mu.Lock()
	if ts, ok := r.turnFiles[taskID]; ok {
		ts.file.Close()
		delete(r.turnFiles, taskID)
	}
	r.mu.Unlock()

	// Get count and projectID from index.
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

	// Remove the logs directory.
	logsDir := r.logsDir(projectID, taskID)
	if err := os.RemoveAll(logsDir); err != nil {
		return 0, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to remove logs dir: %w", err))
	}

	// Remove from index.
	r.indexMu.Lock()
	for _, id := range ids {
		delete(r.locationIndex, id)
	}
	delete(r.taskIndex, taskID)
	// Rebuild allIDs without deleted IDs.
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
		projectPath := filepath.Join(projectsDir, projectID)
		taskDirs, err := os.ReadDir(projectPath)
		if err != nil {
			continue
		}
		for _, td := range taskDirs {
			if !td.IsDir() || knownSubdirs[td.Name()] {
				continue
			}
			taskID := td.Name()
			logsPath := filepath.Join(projectPath, taskID, "logs")
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
					// Read entries to get IDs for index cleanup.
					entries, _ := r.readJSONLFile(absPath, projectID, taskID)
					if err := os.Remove(absPath); err != nil {
						continue
					}
					for _, e := range entries {
						r.removeFromIndex(e.ID, taskID)
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
	// Increase buffer for large log lines (e.g., agent output with full text).
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
	return result, nil
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
			r.scanProjectTaskDirs(projectID)
		}

		sort.Strings(r.allIDs)
		for tid := range r.taskIndex {
			sort.Strings(r.taskIndex[tid])
		}
	})
}

// scanProjectTaskDirs scans all task directories (active + archived) for a project.
func (r *JSONLRepository) scanProjectTaskDirs(projectID string) {
	projectPath := filepath.Join(r.baseDir, "projects", projectID)
	subdirs, err := os.ReadDir(projectPath)
	if err != nil {
		return
	}
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
					r.scanTaskLogs(projectID, ad.Name())
				}
			}
		} else if !knownSubdirs[name] {
			r.scanTaskLogs(projectID, name)
		}
	}
}

// scanTaskLogs reads all .jsonl files under a task's logs directory and indexes their entries.
func (r *JSONLRepository) scanTaskLogs(projectID, taskID string) {
	logsPath := filepath.Join(r.baseDir, "projects", projectID, taskID, "logs")
	entries, err := os.ReadDir(logsPath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		absPath := filepath.Join(logsPath, entry.Name())
		relPath := filepath.Join("projects", projectID, taskID, "logs", entry.Name())
		logs, err := r.readJSONLFile(absPath, projectID, taskID)
		if err != nil {
			continue
		}
		for _, l := range logs {
			r.locationIndex[l.ID] = logLocation{
				projectID: projectID,
				taskID:    taskID,
				filePath:  relPath,
			}
			r.taskIndex[taskID] = append(r.taskIndex[taskID], l.ID)
			r.allIDs = append(r.allIDs, l.ID)
		}
	}
}

func (r *JSONLRepository) addToIndex(id, projectID, taskID, relPath string) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	r.locationIndex[id] = logLocation{projectID: projectID, taskID: taskID, filePath: relPath}
	r.taskIndex[taskID] = append(r.taskIndex[taskID], id)
	sort.Strings(r.taskIndex[taskID])

	i := sort.SearchStrings(r.allIDs, id)
	r.allIDs = append(r.allIDs, "")
	copy(r.allIDs[i+1:], r.allIDs[i:])
	r.allIDs[i] = id
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
