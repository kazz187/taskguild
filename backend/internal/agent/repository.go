package agent

import "context"

type Repository interface {
	Create(ctx context.Context, a *Agent) error
	Get(ctx context.Context, id string) (*Agent, error)
	List(ctx context.Context, projectID string, limit, offset int) ([]*Agent, int, error)
	FindByName(ctx context.Context, projectID, name string) (*Agent, error)
	Update(ctx context.Context, a *Agent) error
	Delete(ctx context.Context, id string) error
}
