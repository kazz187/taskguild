package script

import "context"

type Repository interface {
	Create(ctx context.Context, s *Script) error
	Get(ctx context.Context, id string) (*Script, error)
	List(ctx context.Context, projectID string, limit, offset int) ([]*Script, int, error)
	FindByName(ctx context.Context, projectID, name string) (*Script, error)
	Update(ctx context.Context, s *Script) error
	Delete(ctx context.Context, id string) error
}
