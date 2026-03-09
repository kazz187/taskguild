package repositoryimpl

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const interactionsPrefix = "interactions"

// YAMLRepository implements interaction.Repository using YAML files on Storage.
//
// An in-memory index is built lazily on first access and maintained
// incrementally on Create/Update/Delete. This avoids reading and
// YAML-parsing every file on each List or token-lookup call.
type YAMLRepository struct {
	storage storage.Storage

	indexOnce  sync.Once
	indexMu    sync.RWMutex
	taskIndex  map[string][]string // taskID -> sorted []interactionID
	tokenIndex map[string]string   // responseToken -> interactionID (pending only)
	allIDs     []string            // all interaction IDs in sorted order
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func interactionPath(id string) string {
	return fmt.Sprintf("%s/%s.yaml", interactionsPrefix, id)
}

func pathToID(p string) string {
	return strings.TrimSuffix(filepath.Base(p), ".yaml")
}

// extractField does a fast text scan for a top-level YAML scalar field
// (e.g. "task_id: VALUE") without full YAML parsing.
func extractField(data []byte, field string) string {
	prefix := []byte("\n" + field + ": ")
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

// ensureIndex lazily builds the in-memory index on first access.
func (r *YAMLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()
		r.taskIndex = make(map[string][]string)
		r.tokenIndex = make(map[string]string)

		paths, err := r.storage.List(ctx, interactionsPrefix)
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

			// Index response tokens for pending interactions.
			token := extractField(data, "response_token")
			statusStr := extractField(data, "status")
			if token != "" && statusStr == "1" { // StatusPending = 1
				r.tokenIndex[token] = id
			}
		}
	})
}

func (r *YAMLRepository) addToIndex(i *interaction.Interaction) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	r.taskIndex[i.TaskID] = append(r.taskIndex[i.TaskID], i.ID)
	sort.Strings(r.taskIndex[i.TaskID])

	idx := sort.SearchStrings(r.allIDs, i.ID)
	r.allIDs = append(r.allIDs, "")
	copy(r.allIDs[idx+1:], r.allIDs[idx:])
	r.allIDs[idx] = i.ID

	if i.ResponseToken != "" && i.Status == interaction.StatusPending {
		r.tokenIndex[i.ResponseToken] = i.ID
	}
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

	// Remove any token index entry for this ID.
	for token, tid := range r.tokenIndex {
		if tid == id {
			delete(r.tokenIndex, token)
			break
		}
	}
}

func (r *YAMLRepository) updateTokenIndex(i *interaction.Interaction) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	if i.ResponseToken == "" {
		return
	}
	if i.Status == interaction.StatusPending {
		r.tokenIndex[i.ResponseToken] = i.ID
	} else {
		delete(r.tokenIndex, i.ResponseToken)
	}
}

func (r *YAMLRepository) Create(ctx context.Context, i *interaction.Interaction) error {
	r.ensureIndex(ctx)

	exists, err := r.storage.Exists(ctx, interactionPath(i.ID))
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
	if err := r.storage.Write(ctx, interactionPath(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	r.addToIndex(i)
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*interaction.Interaction, error) {
	data, err := r.storage.Read(ctx, interactionPath(id))
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

	result := make([]*interaction.Interaction, 0, len(paginated))
	for _, id := range paginated {
		data, err := r.storage.Read(ctx, interactionPath(id))
		if err != nil {
			continue
		}
		var i interaction.Interaction
		if err := yaml.Unmarshal(data, &i); err != nil {
			continue
		}
		result = append(result, &i)
	}
	return result, total, nil
}

func (r *YAMLRepository) Update(ctx context.Context, i *interaction.Interaction) error {
	r.ensureIndex(ctx)

	// Skip the separate Exists() check — callers always Get() first, so
	// the interaction is already verified to exist. If it was deleted in
	// the meantime, Write() will simply create it again (acceptable for
	// YAML-file storage).
	data, err := yaml.Marshal(i)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal interaction: %w", err))
	}
	if err := r.storage.Write(ctx, interactionPath(i.ID), data); err != nil {
		return cerr.WrapStorageWriteError("interaction", err)
	}
	r.updateTokenIndex(i)
	return nil
}

func (r *YAMLRepository) GetByResponseToken(ctx context.Context, token string) (*interaction.Interaction, error) {
	if token == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "token is required", nil)
	}
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	id, ok := r.tokenIndex[token]
	r.indexMu.RUnlock()

	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "interaction not found for token", nil)
	}
	return r.Get(ctx, id)
}

func (r *YAMLRepository) DeleteByTaskID(ctx context.Context, taskID string) (int, error) {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	ids := make([]string, len(r.taskIndex[taskID]))
	copy(ids, r.taskIndex[taskID])
	r.indexMu.RUnlock()

	count := 0
	for _, id := range ids {
		if err := r.storage.Delete(ctx, interactionPath(id)); err != nil {
			return count, cerr.WrapStorageDeleteError("interaction", err)
		}
		r.removeFromIndex(id, taskID)
		count++
	}
	return count, nil
}

func (r *YAMLRepository) ExpirePendingByTask(ctx context.Context, taskID string) (int, error) {
	// List all interactions for this task (uses index, reads only matching files).
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
