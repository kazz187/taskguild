package singlecommandpermission

import "time"

// SingleCommandPermission represents a wildcard-based permission rule that matches
// against individual shell commands (not full one-liners). The pattern uses
// wildcard syntax where `*` matches zero or more arbitrary characters.
type SingleCommandPermission struct {
	ID        string    `yaml:"id"`
	ProjectID string    `yaml:"project_id"`
	Pattern   string    `yaml:"pattern"` // wildcard pattern (e.g. "git status" or "git *")
	Type      string    `yaml:"type"`    // "command" or "redirect"
	CreatedAt time.Time `yaml:"created_at"`
}

// Permission types.
const (
	TypeCommand  = "command"
	TypeRedirect = "redirect"
)
