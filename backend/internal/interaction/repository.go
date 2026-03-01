package interaction

import "context"

type Repository interface {
	Create(ctx context.Context, i *Interaction) error
	Get(ctx context.Context, id string) (*Interaction, error)
	List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*Interaction, int, error)
	Update(ctx context.Context, i *Interaction) error
	// ExpirePendingByTask sets all PENDING interactions for the given task to EXPIRED.
	// Returns the number of interactions expired.
	ExpirePendingByTask(ctx context.Context, taskID string) (int, error)
}
