package repositoryimpl

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/tasklog"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const taskLogsPrefix = "task_logs"

// YAMLRepository implements tasklog.Repository using YAML files on Storage.
//
// An in-memory index (taskID → sorted []logID) is built lazily on first
// access and maintained incrementally on Create/Delete. This avoids the
// need to read and YAML-parse every file on each List call.
type YAMLRepository struct {
	storage storage.Storage

	indexOnce sync.Once
	indexMu   sync.RWMutex
	taskIndex map[string][]string // taskID -> sorted []logID
	allIDs    []string            // all log IDs in sorted order
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func logPath(id string) string {
	return fmt.Sprintf("%s/%s.yaml", taskLogsPrefix, id)
}

func pathToID(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".yaml")
}

// extractField does a fast text scan for a top-level YAML scalar field
// (e.g. "task_id: VALUE") without full YAML parsing.
func extractField(data []byte, field string) string {
	prefix := []byte("\n" + field + ": ")
	// Also check if the field is at the very beginning of the data.
	prefixStart := []byte(field + ": ")

	var start int
	if bytes.HasPrefix(data, prefixStart) {
		start = len(prefixStart)
	} else {
		idx := bytes.Index(data, prefix)
		if idx < 0 {
			return ""
		}
		start = idx + len(prefix)
	}

	end := bytes.IndexByte(data[start:], '\n')
	if end < 0 {
		return strings.TrimSpace(string(data[start:]))
	}
	return strings.TrimSpace(string(data[start : start+end]))
}

// ensureIndex lazily builds the in-memory index on first access by
// scanning all files once. Subsequent calls are no-ops.
func (r *YAMLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()
		r.taskIndex = make(map[string][]string)

		paths, err := r.storage.List(ctx, taskLogsPrefix)
		if err != nil {
			return
		}
		sort.Strings(paths)

		r.allIDs = make([]string, 0, len(paths))
		for _, p := range paths {
			data, err := r.storage.Read(ctx, p)
			if err != nil {
				continue
			}
			id := pathToID(p)
			taskID := extractField(data, "task_id")
			if id == "" || taskID == "" {
				continue
			}
			r.taskIndex[taskID] = append(r.taskIndex[taskID], id)
			r.allIDs = append(r.allIDs, id)
		}
	})
}

func (r *YAMLRepository) addToIndex(id, taskID string) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

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

	exists, err := r.storage.Exists(ctx, logPath(l.ID))
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
	if err := r.storage.Write(ctx, logPath(l.ID), data); err != nil {
		return cerr.WrapStorageWriteError("task_log", err)
	}
	r.addToIndex(l.ID, l.TaskID)
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
		data, err := r.storage.Read(ctx, logPath(id))
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
		if err := r.storage.Delete(ctx, logPath(id)); err != nil {
			return count, cerr.WrapStorageDeleteError("task_log", err)
		}
		r.removeFromIndex(id, taskID)
		count++
	}
	return count, nil
}
