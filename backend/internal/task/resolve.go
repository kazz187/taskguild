package task

import "context"

// resolveTask fetches a task by ID, checking active tasks first,
// then falling back to archived tasks.
func resolveTask(ctx context.Context, repo Repository, id string) *Task {
	t, err := repo.Get(ctx, id)
	if err == nil {
		return t
	}
	t, err = repo.GetArchived(ctx, id)
	if err == nil {
		return t
	}
	return nil
}

// ResolveTitles returns a map of taskID → title for the given IDs.
// It checks active tasks first, then falls back to archived tasks.
// Tasks that cannot be found are silently skipped.
func ResolveTitles(ctx context.Context, repo Repository, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	titles := make(map[string]string, len(ids))
	for _, id := range ids {
		if t := resolveTask(ctx, repo, id); t != nil {
			titles[id] = t.Title
		}
	}
	return titles
}

// ResolveProjectIDs returns a map of taskID → projectID for the given IDs.
// It checks active tasks first, then falls back to archived tasks.
// Tasks that cannot be found are silently skipped.
func ResolveProjectIDs(ctx context.Context, repo Repository, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}
	projectIDs := make(map[string]string, len(ids))
	for _, id := range ids {
		if t := resolveTask(ctx, repo, id); t != nil {
			projectIDs[id] = t.ProjectID
		}
	}
	return projectIDs
}

// ResolveAll returns both taskID → title and taskID → projectID maps
// with a single pass over the task IDs (avoiding duplicate lookups).
func ResolveAll(ctx context.Context, repo Repository, ids []string) (titles map[string]string, projectIDs map[string]string) {
	if len(ids) == 0 {
		return nil, nil
	}
	titles = make(map[string]string, len(ids))
	projectIDs = make(map[string]string, len(ids))
	for _, id := range ids {
		if t := resolveTask(ctx, repo, id); t != nil {
			titles[id] = t.Title
			projectIDs[id] = t.ProjectID
		}
	}
	return titles, projectIDs
}
