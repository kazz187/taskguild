package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/script"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const scriptsPrefix = "scripts"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", scriptsPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, s *script.Script) error {
	exists, err := r.storage.Exists(ctx, path(s.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("script", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "script already exists", nil)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal script: %w", err))
	}
	if err := r.storage.Write(ctx, path(s.ID), data); err != nil {
		return cerr.WrapStorageWriteError("script", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*script.Script, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("script", err)
	}
	var s script.Script
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal script: %w", err))
	}
	return &s, nil
}

func (r *YAMLRepository) List(ctx context.Context, projectID string, limit, offset int) ([]*script.Script, int, error) {
	paths, err := r.storage.List(ctx, scriptsPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("scripts", err)
	}

	sort.Strings(paths)

	var all []*script.Script
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var s script.Script
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}
		if projectID != "" && s.ProjectID != projectID {
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

func (r *YAMLRepository) FindByName(ctx context.Context, projectID, name string) (*script.Script, error) {
	paths, err := r.storage.List(ctx, scriptsPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("scripts", err)
	}

	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var s script.Script
		if err := yaml.Unmarshal(data, &s); err != nil {
			continue
		}
		if s.ProjectID == projectID && s.Name == name {
			return &s, nil
		}
	}
	return nil, cerr.NewError(cerr.NotFound, "script not found", nil)
}

func (r *YAMLRepository) Update(ctx context.Context, s *script.Script) error {
	exists, err := r.storage.Exists(ctx, path(s.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("script", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "script not found", nil)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal script: %w", err))
	}
	if err := r.storage.Write(ctx, path(s.ID), data); err != nil {
		return cerr.WrapStorageWriteError("script", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("script", err)
	}
	return nil
}
