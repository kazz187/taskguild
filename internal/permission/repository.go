package permission

import "context"

// Repository provides persistence for project-scoped permission sets.
type Repository interface {
	// Get returns the permission set for a project.
	// Returns an empty PermissionSet (not an error) if none exists yet.
	Get(ctx context.Context, projectID string) (*PermissionSet, error)

	// Upsert creates or replaces the permission set for a project.
	Upsert(ctx context.Context, ps *PermissionSet) error
}
