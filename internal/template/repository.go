package template

import "context"

// Repository provides data access for Template entities.
type Repository interface {
	Create(ctx context.Context, t *Template) error
	Get(ctx context.Context, id string) (*Template, error)
	List(ctx context.Context, entityType string, limit, offset int) ([]*Template, int, error)
	Update(ctx context.Context, t *Template) error
	Delete(ctx context.Context, id string) error
	// FindByConfigName searches for a template by entity type and the name stored
	// inside the config (e.g., AgentConfig.Name, SkillConfig.Name).
	FindByConfigName(ctx context.Context, entityType, configName string) (*Template, error)
}
