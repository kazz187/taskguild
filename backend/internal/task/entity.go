package task

import "time"

type Task struct {
	ID              string            `yaml:"id"`
	ProjectID       string            `yaml:"project_id"`
	WorkflowID      string            `yaml:"workflow_id"`
	Title           string            `yaml:"title"`
	Description     string            `yaml:"description"`
	StatusID        string            `yaml:"status_id"`
	AssignedAgentID string            `yaml:"assigned_agent_id"`
	Metadata        map[string]string `yaml:"metadata"`
	CreatedAt       time.Time         `yaml:"created_at"`
	UpdatedAt       time.Time         `yaml:"updated_at"`
}
