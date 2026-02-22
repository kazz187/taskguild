package task

import "context"

type Repository interface {
	Create(ctx context.Context, t *Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, projectID, workflowID, statusID string, limit, offset int) ([]*Task, int, error)
	Update(ctx context.Context, t *Task) error
	Delete(ctx context.Context, id string) error
}
