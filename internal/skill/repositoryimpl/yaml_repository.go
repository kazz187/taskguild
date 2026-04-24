package repositoryimpl

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const (
	projectsPrefix = "projects"
	entityType     = "skills"
)

type YAMLRepository struct {
	storage     storage.Storage
	indexOnce   sync.Once
	indexMu     sync.RWMutex
	idToProject map[string]string
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func entityPath(projectID, id string) string {
	return fmt.Sprintf("%s/%s/%s/%s.yaml", projectsPrefix, projectID, entityType, id)
}

func entityPrefix(projectID string) string {
	return fmt.Sprintf("%s/%s/%s", projectsPrefix, projectID, entityType)
}

func (r *YAMLRepository) ensureIndex(ctx context.Context) {
	r.indexOnce.Do(func() {
		r.indexMu.Lock()
		defer r.indexMu.Unlock()

		r.idToProject = make(map[string]string)

		dirs, err := r.storage.ListDirs(ctx, projectsPrefix)
		if err != nil {
			return
		}

		for _, d := range dirs {
			pid := filepath.Base(d)

			files, err := r.storage.List(ctx, entityPrefix(pid))
			if err != nil {
				continue
			}

			for _, f := range files {
				id := strings.TrimSuffix(filepath.Base(f), ".yaml")
				r.idToProject[id] = pid
			}
		}
	})
}

func (r *YAMLRepository) Create(ctx context.Context, s *skill.Skill) error {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	_, exists := r.idToProject[s.ID]
	r.indexMu.RUnlock()

	if exists {
		return cerr.NewError(cerr.AlreadyExists, "skill already exists", nil)
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal skill: %w", err))
	}

	if err := r.storage.Write(ctx, entityPath(s.ProjectID, s.ID), data); err != nil {
		return cerr.WrapStorageWriteError("skill", err)
	}

	r.indexMu.Lock()
	r.idToProject[s.ID] = s.ProjectID
	r.indexMu.Unlock()

	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*skill.Skill, error) {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[id]
	r.indexMu.RUnlock()

	if !ok {
		return nil, cerr.NewError(cerr.NotFound, "skill not found", nil)
	}

	data, err := r.storage.Read(ctx, entityPath(pid, id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("skill", err)
	}

	var s skill.Skill
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal skill: %w", err))
	}

	return &s, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID string, limit, offset int) ([]*skill.Skill, int, error) {
	r.ensureIndex(ctx)

	var filePaths []string

	if projectID != "" {
		paths, err := r.storage.List(ctx, entityPrefix(projectID))
		if err != nil {
			return nil, 0, cerr.WrapStorageReadError("skills", err)
		}

		filePaths = paths
	} else {
		r.indexMu.RLock()

		for id, pid := range r.idToProject {
			filePaths = append(filePaths, entityPath(pid, id))
		}

		r.indexMu.RUnlock()
	}

	sort.Strings(filePaths)

	var all []*skill.Skill

	for _, p := range filePaths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}

		var s skill.Skill
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}

		all = append(all, &s)
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

func (r *YAMLRepository) FindByName(ctx context.Context, projectID, name string) (*skill.Skill, error) {
	r.ensureIndex(ctx)

	paths, err := r.storage.List(ctx, entityPrefix(projectID))
	if err != nil {
		return nil, cerr.WrapStorageReadError("skills", err)
	}

	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}

		var s skill.Skill
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}

		if s.ProjectID == projectID && s.Name == name {
			return &s, nil
		}
	}

	return nil, cerr.NewError(cerr.NotFound, "skill not found", nil)
}

func (r *YAMLRepository) Update(ctx context.Context, s *skill.Skill) error {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[s.ID]
	r.indexMu.RUnlock()

	if !ok {
		return cerr.NewError(cerr.NotFound, "skill not found", nil)
	}

	if pid != s.ProjectID {
		_ = r.storage.Delete(ctx, entityPath(pid, s.ID))
	}

	data, err := yaml.Marshal(s)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal skill: %w", err))
	}

	if err := r.storage.Write(ctx, entityPath(s.ProjectID, s.ID), data); err != nil {
		return cerr.WrapStorageWriteError("skill", err)
	}

	r.indexMu.Lock()
	r.idToProject[s.ID] = s.ProjectID
	r.indexMu.Unlock()

	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	r.ensureIndex(ctx)

	r.indexMu.RLock()
	pid, ok := r.idToProject[id]
	r.indexMu.RUnlock()

	if !ok {
		return cerr.NewError(cerr.NotFound, "skill not found", nil)
	}

	err := r.storage.Delete(ctx, entityPath(pid, id))
	if err != nil {
		return cerr.WrapStorageDeleteError("skill", err)
	}

	r.indexMu.Lock()
	delete(r.idToProject, id)
	r.indexMu.Unlock()

	return nil
}
