package permission

import "time"

// PermissionSet represents project-scoped permission rules for Claude Code tools
// and Bash command patterns. One PermissionSet per project.
type PermissionSet struct {
	ProjectID string    `yaml:"project_id"`
	Allow     []string  `yaml:"allow"`
	Ask       []string  `yaml:"ask"`
	Deny      []string  `yaml:"deny"`
	UpdatedAt time.Time `yaml:"updated_at"`
}
