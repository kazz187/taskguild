package singlecommandpermission

import "context"

// Repository provides persistence for single-command permission rules.
type Repository interface {
	// Create stores a new permission rule.
	Create(ctx context.Context, p *SingleCommandPermission) error

	// Get returns a single permission rule by ID.
	// Returns an error if the rule does not exist.
	Get(ctx context.Context, id string) (*SingleCommandPermission, error)

	// List returns all permission rules for a project.
	List(ctx context.Context, projectID string) ([]*SingleCommandPermission, error)

	// FindByPatternAndType returns all permission rules that match the given
	// projectID, pattern, and type combination. Returns an empty slice if none
	// found. Results are sorted by CreatedAt ascending (oldest first).
	FindByPatternAndType(ctx context.Context, projectID, pattern, permType string) ([]*SingleCommandPermission, error)

	// Update replaces an existing permission rule.
	Update(ctx context.Context, p *SingleCommandPermission) error

	// Delete removes a permission rule by ID.
	Delete(ctx context.Context, id string) error
}
