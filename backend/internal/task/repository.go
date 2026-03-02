package task

import "context"

type Repository interface {
	Create(ctx context.Context, t *Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, projectID, workflowID, statusID string, limit, offset int) ([]*Task, int, error)
	Update(ctx context.Context, t *Task) error
	Delete(ctx context.Context, id string) error
	Claim(ctx context.Context, taskID string, agentID string) (*Task, error)
	// ReleaseByAgent unassigns all tasks currently assigned to the given agent,
	// resetting them to Pending so other agents can claim them.
	ReleaseByAgent(ctx context.Context, agentID string) ([]*Task, error)

	// Archive moves a task from tasks/ to tasks/archived/.
	Archive(ctx context.Context, id string) error
	// Unarchive moves a task from tasks/archived/ back to tasks/.
	Unarchive(ctx context.Context, id string) error
	// GetArchived retrieves a single archived task by ID.
	GetArchived(ctx context.Context, id string) (*Task, error)
	// ListArchived lists archived tasks, optionally filtered by project and workflow.
	ListArchived(ctx context.Context, projectID, workflowID string, limit, offset int) ([]*Task, int, error)
}
