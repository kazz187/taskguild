package task

import "time"

type AssignmentStatus string

const (
	AssignmentStatusUnassigned AssignmentStatus = "unassigned"
	AssignmentStatusPending    AssignmentStatus = "pending"
	AssignmentStatusAssigned   AssignmentStatus = "assigned"
)

// Pending reason metadata keys and values.
const (
	MetaPendingReason          = "_pending_reason"
	MetaPendingBlockerTaskID   = "_pending_blocker_task_id"
	MetaPendingBlockerTaskTitle = "_pending_blocker_task_title"
	MetaPendingRetryAfter      = "_pending_retry_after"

	PendingReasonWorktreeOccupied = "worktree_occupied"
	PendingReasonWaitingAgent     = "waiting_agent"
	PendingReasonRetryBackoff     = "retry_backoff"
)

// ClearPendingReason removes all pending-reason metadata keys from the map.
func ClearPendingReason(metadata map[string]string) {
	delete(metadata, MetaPendingReason)
	delete(metadata, MetaPendingBlockerTaskID)
	delete(metadata, MetaPendingBlockerTaskTitle)
	delete(metadata, MetaPendingRetryAfter)
}

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
	// Effort overrides WorkflowStatus.effort when non-empty.
	// Valid values: "low", "medium", "high", "max". Empty = inherit from WorkflowStatus.
	Effort    string    `yaml:"effort,omitempty"`
	CreatedAt time.Time `yaml:"created_at"`
	UpdatedAt time.Time `yaml:"updated_at"`
}
