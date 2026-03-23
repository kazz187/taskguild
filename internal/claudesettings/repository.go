package claudesettings

import "context"

// Repository provides persistence for project-scoped Claude Code settings.
type Repository interface {
	// Get returns the settings for a project.
	// Returns an empty ClaudeSettings (not an error) if none exists yet.
	Get(ctx context.Context, projectID string) (*ClaudeSettings, error)

	// Upsert creates or replaces the settings for a project.
	Upsert(ctx context.Context, cs *ClaudeSettings) error
}
