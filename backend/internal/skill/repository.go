package skill

import "context"

type Repository interface {
	Create(ctx context.Context, s *Skill) error
	Get(ctx context.Context, id string) (*Skill, error)
	List(ctx context.Context, projectID string, limit, offset int) ([]*Skill, int, error)
	FindByName(ctx context.Context, projectID, name string) (*Skill, error)
	Update(ctx context.Context, s *Skill) error
	Delete(ctx context.Context, id string) error
}
