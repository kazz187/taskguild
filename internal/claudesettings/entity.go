package claudesettings

import "time"

// ClaudeSettings represents project-scoped Claude Code settings
// (e.g. language) that are synced to .claude/settings.json on agents.
// One ClaudeSettings per project.
type ClaudeSettings struct {
	ProjectID string    `yaml:"project_id"`
	Language  string    `yaml:"language"`
	UpdatedAt time.Time `yaml:"updated_at"`
}
