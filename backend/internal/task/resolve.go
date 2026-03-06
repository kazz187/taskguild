package task

import "context"

// ResolveTitles returns a map of taskID → title for the given IDs.
// It checks active tasks first, then falls back to archived tasks.
// Tasks that cannot be found are silently skipped.
func ResolveTitles(ctx context.Context, repo Repository, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	titles := make(map[string]string, len(ids))
	for _, id := range ids {
		t, err := repo.Get(ctx, id)
		if err == nil {
			titles[id] = t.Title
			continue
		}
		// Fall back to archived.
		t, err = repo.GetArchived(ctx, id)
		if err == nil {
			titles[id] = t.Title
		}
	}
	return titles
}
