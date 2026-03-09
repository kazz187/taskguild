package workflow

import "context"

type Repository interface {
	Create(ctx context.Context, w *Workflow) error
	Get(ctx context.Context, id string) (*Workflow, error)
	List(ctx context.Context, projectID string, limit, offset int) ([]*Workflow, int, error)
	Update(ctx context.Context, w *Workflow) error
	Delete(ctx context.Context, id string) error
}
