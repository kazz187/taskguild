package repositoryimpl

import (
	"context"
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/backend/internal/template"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	"github.com/kazz187/taskguild/backend/pkg/storage"
)

const templatesPrefix = "templates"

type YAMLRepository struct {
	storage storage.Storage
}

func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(id string) string {
	return fmt.Sprintf("%s/%s.yaml", templatesPrefix, id)
}

func (r *YAMLRepository) Create(ctx context.Context, t *template.Template) error {
	exists, err := r.storage.Exists(ctx, path(t.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("template", err)
	}
	if exists {
		return cerr.NewError(cerr.AlreadyExists, "template already exists", nil)
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal template: %w", err))
	}
	if err := r.storage.Write(ctx, path(t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("template", err)
	}
	return nil
}

func (r *YAMLRepository) Get(ctx context.Context, id string) (*template.Template, error) {
	data, err := r.storage.Read(ctx, path(id))
	if err != nil {
		return nil, cerr.WrapStorageReadError("template", err)
	}
	var t template.Template
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal template: %w", err))
	}
	return &t, nil
}

func (r *YAMLRepository) List(ctx context.Context, entityType string, limit, offset int) ([]*template.Template, int, error) {
	paths, err := r.storage.List(ctx, templatesPrefix)
	if err != nil {
		return nil, 0, cerr.WrapStorageReadError("templates", err)
	}

	sort.Strings(paths)

	var all []*template.Template
	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var t template.Template
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		if entityType != "" && t.EntityType != entityType {
			continue
		}
		all = append(all, &t)
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

func (r *YAMLRepository) Update(ctx context.Context, t *template.Template) error {
	exists, err := r.storage.Exists(ctx, path(t.ID))
	if err != nil {
		return cerr.WrapStorageWriteError("template", err)
	}
	if !exists {
		return cerr.NewError(cerr.NotFound, "template not found", nil)
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal template: %w", err))
	}
	if err := r.storage.Write(ctx, path(t.ID), data); err != nil {
		return cerr.WrapStorageWriteError("template", err)
	}
	return nil
}

func (r *YAMLRepository) Delete(ctx context.Context, id string) error {
	if err := r.storage.Delete(ctx, path(id)); err != nil {
		return cerr.WrapStorageDeleteError("template", err)
	}
	return nil
}

// configName extracts the entity name stored inside a template's config.
func configName(t *template.Template) string {
	switch t.EntityType {
	case "agent":
		if t.AgentConfig != nil {
			return t.AgentConfig.Name
		}
	case "skill":
		if t.SkillConfig != nil {
			return t.SkillConfig.Name
		}
	case "script":
		if t.ScriptConfig != nil {
			return t.ScriptConfig.Name
		}
	}
	return ""
}

func (r *YAMLRepository) FindByConfigName(ctx context.Context, entityType, name string) (*template.Template, error) {
	paths, err := r.storage.List(ctx, templatesPrefix)
	if err != nil {
		return nil, cerr.WrapStorageReadError("templates", err)
	}

	for _, p := range paths {
		data, err := r.storage.Read(ctx, p)
		if err != nil {
			continue
		}
		var t template.Template
		if err := yaml.Unmarshal(data, &t); err != nil {
			continue
		}
		if t.EntityType == entityType && configName(&t) == name {
			return &t, nil
		}
	}
	return nil, cerr.NewError(cerr.NotFound, "template not found", nil)
}
