package agent

import "time"

type Agent struct {
	ID             string   `yaml:"id"`
	ProjectID      string   `yaml:"project_id"`
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	Prompt         string   `yaml:"prompt"`
	Tools          []string `yaml:"tools"`
	Model          string   `yaml:"model"`
	MaxTurns       int32    `yaml:"max_turns"`
	PermissionMode string   `yaml:"permission_mode"`
	Isolation      string   `yaml:"isolation"`
	IsSynced       bool     `yaml:"is_synced"`
	CreatedAt      time.Time `yaml:"created_at"`
	UpdatedAt      time.Time `yaml:"updated_at"`
}
