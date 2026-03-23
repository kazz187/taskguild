package repositoryimpl

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/claudesettings"
	"github.com/kazz187/taskguild/pkg/cerr"
	"github.com/kazz187/taskguild/pkg/storage"
)

const claudeSettingsPrefix = "claude_settings"

// YAMLRepository stores Claude Code settings as YAML files keyed by project ID.
type YAMLRepository struct {
	storage storage.Storage
}

// NewYAMLRepository creates a new YAML-backed Claude Code settings repository.
func NewYAMLRepository(s storage.Storage) *YAMLRepository {
	return &YAMLRepository{storage: s}
}

func path(projectID string) string {
	return fmt.Sprintf("%s/%s.yaml", claudeSettingsPrefix, projectID)
}

// Get returns the settings for a project.
// Returns an empty ClaudeSettings (not an error) if none exists yet.
func (r *YAMLRepository) Get(ctx context.Context, projectID string) (*claudesettings.ClaudeSettings, error) {
	exists, err := r.storage.Exists(ctx, path(projectID))
	if err != nil {
		return nil, cerr.WrapStorageReadError("claude_settings", err)
	}
	if !exists {
		return &claudesettings.ClaudeSettings{
			ProjectID: projectID,
		}, nil
	}
	data, err := r.storage.Read(ctx, path(projectID))
	if err != nil {
		return nil, cerr.WrapStorageReadError("claude_settings", err)
	}
	var cs claudesettings.ClaudeSettings
	if err := yaml.Unmarshal(data, &cs); err != nil {
		return nil, cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to unmarshal claude_settings: %w", err))
	}
	return &cs, nil
}

// Upsert creates or replaces the settings for a project.
func (r *YAMLRepository) Upsert(ctx context.Context, cs *claudesettings.ClaudeSettings) error {
	data, err := yaml.Marshal(cs)
	if err != nil {
		return cerr.NewError(cerr.Internal, "server error", fmt.Errorf("failed to marshal claude_settings: %w", err))
	}
	if err := r.storage.Write(ctx, path(cs.ProjectID), data); err != nil {
		return cerr.WrapStorageWriteError("claude_settings", err)
	}
	return nil
}
