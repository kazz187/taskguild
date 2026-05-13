package schedule

import "context"

type Repository interface {
	Create(ctx context.Context, s *Schedule) error
	Get(ctx context.Context, id string) (*Schedule, error)
	List(ctx context.Context, projectID string, limit, offset int) ([]*Schedule, int, error)
	// ListAll returns every schedule across all projects. Used by the scheduler
	// at server startup to register all enabled schedules.
	ListAll(ctx context.Context) ([]*Schedule, error)
	Update(ctx context.Context, s *Schedule) error
	Delete(ctx context.Context, id string) error
}
