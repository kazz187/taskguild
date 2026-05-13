package schedule

import "time"

// Schedule defines a cron-based recurring task creation.
type Schedule struct {
	ID             string `yaml:"id"`
	ProjectID      string `yaml:"project_id"`
	WorkflowID     string `yaml:"workflow_id"`
	Name           string `yaml:"name"`
	Description    string `yaml:"description"`
	CronExpression string `yaml:"cron_expression"`
	Enabled        bool   `yaml:"enabled"`

	// Task content (template fields expanded at fire time).
	TaskTitle       string            `yaml:"task_title"`
	TaskDescription string            `yaml:"task_description"`
	StatusID        string            `yaml:"status_id,omitempty"`
	UseWorktree     bool              `yaml:"use_worktree"`
	Effort          string            `yaml:"effort,omitempty"`
	TaskMetadata    map[string]string `yaml:"task_metadata,omitempty"`

	// State updated by the scheduler.
	LastRunAt time.Time `yaml:"last_run_at,omitempty"`
	NextRunAt time.Time `yaml:"next_run_at,omitempty"`
	LastError string    `yaml:"last_error,omitempty"`

	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
}
