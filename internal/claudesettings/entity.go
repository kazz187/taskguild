package claudesettings

import "time"

// Attribution holds optional attribution messages appended to commits and PRs.
// nil = not configured, pointer to "" = explicitly off, pointer to value = custom text.
type Attribution struct {
	Commit *string `yaml:"commit,omitempty"`
	Pr     *string `yaml:"pr,omitempty"`
}

// ClaudeSettings represents project-scoped Claude Code settings
// (e.g. language) that are synced to .claude/settings.json on agents.
// One ClaudeSettings per project.
type ClaudeSettings struct {
	ProjectID   string       `yaml:"project_id"`
	Language    *string      `yaml:"language,omitempty"`
	Attribution *Attribution `yaml:"attribution,omitempty"`
	UpdatedAt   time.Time    `yaml:"updated_at"`
}
