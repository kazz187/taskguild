package task

import "time"

type AssignmentStatus string

const (
	AssignmentStatusUnassigned AssignmentStatus = "unassigned"
	AssignmentStatusPending    AssignmentStatus = "pending"
	AssignmentStatusAssigned   AssignmentStatus = "assigned"
)

type Task struct {
	ID               string            `yaml:"id"`
	ProjectID        string            `yaml:"project_id"`
	WorkflowID       string            `yaml:"workflow_id"`
	Title            string            `yaml:"title"`
	Description      string            `yaml:"description"`
	StatusID         string            `yaml:"status_id"`
	AssignedAgentID  string            `yaml:"assigned_agent_id"`
	AssignmentStatus AssignmentStatus  `yaml:"assignment_status"`
	Metadata         map[string]string `yaml:"metadata"`
	UseWorktree      bool              `yaml:"use_worktree"`
	PermissionMode   string            `yaml:"permission_mode"`
	CreatedAt        time.Time         `yaml:"created_at"`
	UpdatedAt        time.Time         `yaml:"updated_at"`
}
