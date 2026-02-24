package project

import "context"

type Repository interface {
	Create(ctx context.Context, p *Project) error
	Get(ctx context.Context, id string) (*Project, error)
	FindByName(ctx context.Context, name string) (*Project, error)
	List(ctx context.Context, limit, offset int) ([]*Project, int, error)
	Update(ctx context.Context, p *Project) error
	Delete(ctx context.Context, id string) error
}
