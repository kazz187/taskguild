package agent

import "time"

type Agent struct {
	ID              string    `yaml:"id"`
	ProjectID       string    `yaml:"project_id"`
	Name            string    `yaml:"name"`
	Description     string    `yaml:"description"`
	Prompt          string    `yaml:"prompt"`
	Tools           []string  `yaml:"tools"`
	DisallowedTools []string  `yaml:"disallowed_tools"`
	Model           string    `yaml:"model"`
	PermissionMode  string    `yaml:"permission_mode"`
	Skills          []string  `yaml:"skills"`
	Memory          string    `yaml:"memory"`
	IsSynced        bool      `yaml:"is_synced"`
	CreatedAt       time.Time `yaml:"created_at"`
	UpdatedAt       time.Time `yaml:"updated_at"`
}
