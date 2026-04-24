package interaction

import "context"

type Repository interface {
	Create(ctx context.Context, i *Interaction) error
	Get(ctx context.Context, id string) (*Interaction, error)
	// GetByResponseToken finds a PENDING interaction by its single-use response token.
	// Returns nil and an error if no matching interaction is found.
	GetByResponseToken(ctx context.Context, token string) (*Interaction, error)
	// List returns interactions matching the given filters.
	//
	// - taskID / taskIDs: restrict results to the given task(s). If both are
	//   empty, all active tasks' interactions are returned.
	// - statusFilter: when StatusUnspecified, no status filtering is applied.
	// - limit / offset: pagination. limit == 0 means "no limit".
	List(ctx context.Context, taskID string, taskIDs []string, statusFilter InteractionStatus, limit, offset int) ([]*Interaction, int, error)
	Update(ctx context.Context, i *Interaction) error
	// ExpirePendingByTask sets all PENDING interactions for the given task to EXPIRED.
	// Returns the number of interactions expired.
	ExpirePendingByTask(ctx context.Context, taskID string) (int, error)
	// DeleteByTaskID removes all interactions belonging to the given task.
	DeleteByTaskID(ctx context.Context, taskID string) (int, error)
}
