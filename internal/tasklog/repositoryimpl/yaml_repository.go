package repositoryimpl

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const projectsPrefix = "projects"

// knownSubdirs contains directory names under a project that are NOT task directories.
var knownSubdirs = map[string]bool{
	"agents":                   true,
	"workflows":                true,
	"skills":                   true,
	"scripts":                  true,
	"singlecommandpermissions": true,
	"archived":                 true,
}

type entityLocation struct {
	projectID string
	taskID    string
}

// YAMLRepository implements tasklog.Repository using YAML files on Storage.
type YAMLRepository struct {
	storage storage.Storage

	indexOnce     sync.Once
	indexMu       sync.RWMutex
	taskIndex     map[string][]string       // taskID -> sorted []logID
	locationIndex map[string]entityLocation // logID -> {projectID, taskID}
	allIDs        []string                  // all log IDs in sorted order
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func logPath(projectID, taskID, id string) string {
	return fmt.Sprintf("%s/%s/%s/logs/%s.yaml", projectsPrefix, projectID, taskID, id)
}

func logPrefix(projectID, taskID string) string {
	return fmt.Sprintf("%s/%s/%s/logs", projectsPrefix, projectID, taskID)
}

func pathToID(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".yaml")
}

// extractField does a fast text scan for a top-level YAML scalar field.
func extractField(data []byte, field string) string {
	prefix := []byte("\n" + field + ": ")
	prefixStart := []byte(field + ": ")

	var start int
	if len(data) >= len(prefixStart) && string(data[:len(prefixStart)]) == string(prefixStart) {
		start = len(prefixStart)
	} else {
		idx := -1
		for i := 0; i <= len(data)-len(prefix); i++ {
			if string(data[i:i+len(prefix)]) == string(prefix) {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ""
		}
		start = idx + len(prefix)
	}

	end := -1
	for i := start; i < len(data); i++ {
		if data[i] == '\n' {
			end = i
			break
		}
	}
	if end < 0 {
		return strings.TrimSpace(string(data[start:]))
	}
	return strings.TrimSpace(string(data[start:end]))
}

// scanTaskDirs returns all task directory paths (active + archived) for a project.
func (r *YAMLRepository) scanTaskDirs(ctx context.Context, pid string) []struct{ projectID, taskID string } {
	var result []struct{ projectID, taskID string }

	projectDir := fmt.Sprintf("%s/%s", projectsPrefix, pid)
	subdirs, err := r.storage.ListDirs(ctx, projectDir)
	if err != nil {
		return result
	}
	for _, sd := range subdirs {
		name := filepath.Base(sd)
		if name == "archived" {
			archivedDirs, err := r.storage.ListDirs(ctx, sd)
			if err != nil {
				continue
			}
			for _, ad := range archivedDirs {
				tid := filepath.Base(ad)
				result = append(result, struct{ projectID, taskID string }{pid, tid})
			}
		} else if !knownSubdirs[name] {
			result = append(result, struct{ projectID, taskID string }{pid, name})
		}
	}
	return result
}

// ensureIndex lazily builds the in-memory index on first access.
func (r *YAMLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()
		r.taskIndex = make(map[string][]string)
		r.locationIndex = make(map[string]entityLocation)

		projectDirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			return
		}

		for _, pd := range projectDirs {
			pid := filepath.Base(pd)
			taskDirs := r.scanTaskDirs(ctx, pid)
			for _, td := range taskDirs {
				prefix := logPrefix(td.projectID, td.taskID)
				files, err := r.storage.List(ctx, prefix)
				if err != nil {
					continue
				}
				for _, f := range files {
					id := pathToID(f)
					if id == "" {
						continue
					}
					r.locationIndex[id] = entityLocation{projectID: td.projectID, taskID: td.taskID}
					r.taskIndex[td.taskID] = append(r.taskIndex[td.taskID], id)
					r.allIDs = append(r.allIDs, id)
				}
			}
		}

		sort.Strings(r.allIDs)
		for tid := range r.taskIndex {
			sort.Strings(r.taskIndex[tid])
		}
	})
}

func (r *YAMLRepository) addToIndex(id, projectID, taskID string) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	r.locationIndex[id] = entityLocation{projectID: projectID, taskID: taskID}
	r.taskIndex[taskID] = append(r.taskIndex[taskID], id)
	sort.Strings(r.taskIndex[taskID])

	i := sort.SearchStrings(r.allIDs, id)
	r.allIDs = append(r.allIDs, "")
	copy(r.allIDs[i+1:], r.allIDs[i:])
	r.allIDs[i] = id
}

func (r *YAMLRepository) removeFromIndex(id, taskID string) {
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

func (r *YAMLRepository) Create(ctx context.Context, l *tasklog.TaskLog) error {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	_, exists := r.locationIndex[l.ID]
	r.indexMu.RUnlock()
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "task log already exists", nil)
	}

	data, err := yaml.Marshal(l)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal task log: %w", err))
	}
	if err := r.storage.Write(ctx, logPath(l.ProjectID, l.TaskID, l.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task_log", err)
	}
	r.addToIndex(l.ID, l.ProjectID, l.TaskID)
	return nil
}

func (r *YAMLRepository) List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*tasklog.TaskLog, int, error) {
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

	result := make([]*tasklog.TaskLog, 0, len(paginated))
	for _, id := range paginated {
		r.indexMu.RLock()
		loc, ok := r.locationIndex[id]
		r.indexMu.RUnlock()
		if !ok {
			continue
		}
		data, err := r.storage.Read(ctx, logPath(loc.projectID, loc.taskID, id))
		if err != nil {
			continue
		}
		var l tasklog.TaskLog
		if err := yaml.Unmarshal(data, &l); err != nil {
			continue
		}
		result = append(result, &l)
	}
	return result, total, nil
}

func (r *YAMLRepository) DeleteByTaskID(ctx context.Context, taskID string) (int, error) {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	ids := make([]string, len(r.taskIndex[taskID]))
	copy(ids, r.taskIndex[taskID])
	r.indexMu.RUnlock()

	count := 0
	for _, id := range ids {
		r.indexMu.RLock()
		loc, ok := r.locationIndex[id]
		r.indexMu.RUnlock()
		if !ok {
			continue
		}
		if err := r.storage.Delete(ctx, logPath(loc.projectID, loc.taskID, id)); err != nil {
			return count, cerr.WrapStorageDeleteError("task_log", err)
		}
		r.removeFromIndex(id, taskID)
		count++
	}
	return count, nil
}

// CleanupOlderThan removes task log entries older than maxAge.
func (r *YAMLRepository) CleanupOlderThan(ctx context.Context, maxAge time.Duration) (int, error) {
	r.ensureIndex(ctx)

	cutoff := time.Now().Add(-maxAge)

	r.indexMu.RLock()
	ids := make([]string, len(r.allIDs))
	copy(ids, r.allIDs)
	r.indexMu.RUnlock()

	deleted := 0
	for _, id := range ids {
		r.indexMu.RLock()
		loc, ok := r.locationIndex[id]
		r.indexMu.RUnlock()
		if !ok {
			continue
		}

		data, err := r.storage.Read(ctx, logPath(loc.projectID, loc.taskID, id))
		if err != nil {
			continue
		}

		createdStr := extractField(data, "created_at")
		if createdStr == "" {
			continue
		}
		createdAt, err := time.Parse(time.RFC3339Nano, createdStr)
		if err != nil {
			createdAt, err = time.Parse(time.RFC3339, createdStr)
			if err != nil {
				continue
			}
		}

		if createdAt.Before(cutoff) {
			taskID := extractField(data, "task_id")
			if delErr := r.storage.Delete(ctx, logPath(loc.projectID, loc.taskID, id)); delErr != nil {
				continue
			}
			if taskID != "" {
				r.removeFromIndex(id, taskID)
			}
			deleted++
		}
	}

	if deleted > 0 {
		slog.Info("task log cleanup completed", "deleted", deleted, "max_age", maxAge)
	}
	return deleted, nil
}
