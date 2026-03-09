package pushsubscription

import "context"

type Repository interface {
	Create(ctx context.Context, s *Subscription) error
	Get(ctx context.Context, id string) (*Subscription, error)
	List(ctx context.Context) ([]*Subscription, error)
	Delete(ctx context.Context, id string) error
	FindByEndpoint(ctx context.Context, endpoint string) (*Subscription, error)
	DeleteByEndpoint(ctx context.Context, endpoint string) error
}
