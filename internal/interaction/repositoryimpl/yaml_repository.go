package repositoryimpl

import (
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

// YAMLRepository implements interaction.Repository using YAML files on Storage.
type YAMLRepository struct {
	storage storage.Storage

	indexOnce     sync.Once
	indexMu       sync.RWMutex
	taskIndex     map[string][]string       // taskID -> sorted []interactionID
	tokenIndex    map[string]string         // responseToken -> interactionID (pending only)
	locationIndex map[string]entityLocation // interactionID -> {projectID, taskID}
	allIDs        []string                  // all interaction IDs in sorted order
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
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

type scannedTask struct {
	projectID string
	taskID    string
	archived  bool
}

// scanTaskDirs returns all task directory paths (active + archived) for a project.
func (r *YAMLRepository) scanTaskDirs(ctx context.Context, pid string) []scannedTask {
	var result []scannedTask

	projectDir := fmt.Sprintf("%s/%s", projectsPrefix, pid)

	subdirs, err := r.storage.ListDirs(ctx, projectDir)
	if err != nil {
		return result
	}

	for _, sd := range subdirs {
		name := filepath.Base(sd)
		if name == "archived" {
			// Scan archived subdirectories.
			archivedDirs, err := r.storage.ListDirs(ctx, sd)
			if err != nil {
				continue
			}

			for _, ad := range archivedDirs {
				tid := filepath.Base(ad)
				result = append(result, scannedTask{pid, tid, true})
			}
		} else if !knownSubdirs[name] {
			result = append(result, scannedTask{pid, name, false})
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
		r.tokenIndex = make(map[string]string)
		r.locationIndex = make(map[string]entityLocation)

		projectDirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			return
		}

		for _, pd := range projectDirs {
			pid := filepath.Base(pd)

			taskDirs := r.scanTaskDirs(ctx, pid)
			for _, td := range taskDirs {
				loc := entityLocation(td)
				prefix := loc.interactionPrefix()

				files, err := r.storage.List(ctx, prefix)
				if err != nil {
					continue
				}

				for _, f := range files {
					id := pathToID(f)
					if id == "" {
						continue
					}

					r.locationIndex[id] = loc
					r.taskIndex[td.taskID] = append(r.taskIndex[td.taskID], id)
					r.allIDs = append(r.allIDs, id)

					// Index response tokens for pending interactions.
					data, err := r.storage.Read(ctx, f)
					if err != nil {
						continue
					}

					token := extractField(data, "response_token")

					statusStr := extractField(data, "status")
					if token != "" && statusStr == "1" { // StatusPending = 1
						r.tokenIndex[token] = id
					}
				}
			}
		}

		sort.Strings(r.allIDs)

		for tid := range r.taskIndex {
			sort.Strings(r.taskIndex[tid])
		}
	})
}

func (r *YAMLRepository) addToIndex(i *interaction.Interaction) {
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	r.locationIndex[i.ID] = entityLocation{projectID: i.ProjectID, taskID: i.TaskID}
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

	r.indexMu.RLock()
	_, exists := r.locationIndex[i.ID]
	r.indexMu.RUnlock()

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

	r.addToIndex(i)

	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*interaction.Interaction, error) {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	loc, ok := r.locationIndex[id]
	r.indexMu.RUnlock()

	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "interaction not found", nil)
	}

	data, err := r.storage.Read(ctx, loc.interactionPath(id))
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
		r.indexMu.RLock()
		loc, ok := r.locationIndex[id]
		r.indexMu.RUnlock()

		if !ok {
			continue
		}

		data, err := r.storage.Read(ctx, loc.interactionPath(id))
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

	r.indexMu.RLock()
	loc, ok := r.locationIndex[i.ID]
	r.indexMu.RUnlock()

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
		r.indexMu.RLock()
		loc, ok := r.locationIndex[id]
		r.indexMu.RUnlock()

		if !ok {
			continue
		}

		err := r.storage.Delete(ctx, loc.interactionPath(id))
		if err != nil {
			return count, cerr.WrapStorageDeleteError("interaction", err)
		}

		r.removeFromIndex(id, taskID)

		count++
	}

	return count, nil
}

func (r *YAMLRepository) ExpirePendingByTask(ctx context.Context, taskID string) (int, error) {
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
		err := r.Update(ctx, i)
		if err != nil {
			return count, fmt.Errorf("failed to expire interaction %s: %w", i.ID, err)
		}

		count++
	}

	return count, nil
}

// NotifyTaskArchived updates the in-memory index so that interactions for the
// given task resolve to the archived path.
func (r *YAMLRepository) NotifyTaskArchived(ctx context.Context, projectID, taskID string) error {
	r.ensureIndex(ctx)
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	for _, id := range r.taskIndex[taskID] {
		if loc, ok := r.locationIndex[id]; ok {
			loc.archived = true
			r.locationIndex[id] = loc
		}
	}

	return nil
}

// NotifyTaskUnarchived updates the in-memory index so that interactions for the
// given task resolve to the active path.
func (r *YAMLRepository) NotifyTaskUnarchived(ctx context.Context, projectID, taskID string) error {
	r.ensureIndex(ctx)
	r.indexMu.Lock()
	defer r.indexMu.Unlock()

	for _, id := range r.taskIndex[taskID] {
		if loc, ok := r.locationIndex[id]; ok {
			loc.archived = false
			r.locationIndex[id] = loc
		}
	}

	return nil
}
