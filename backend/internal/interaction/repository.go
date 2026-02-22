package interaction

import "context"

type Repository interface {
	Create(ctx context.Context, i *Interaction) error
	Get(ctx context.Context, id string) (*Interaction, error)
	List(ctx context.Context, taskID string, limit, offset int) ([]*Interaction, int, error)
	Update(ctx context.Context, i *Interaction) error
}
