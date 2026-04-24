package task

import "context"

// ResolveTitles returns a map of taskID → title for the given IDs.
// It checks active tasks first, then falls back to archived tasks.
// Tasks that cannot be found are silently skipped.
func ResolveTitles(ctx context.Context, repo Repository, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}

	titles, _ := ResolveAll(ctx, repo, ids)

	return titles
}

// ResolveProjectIDs returns a map of taskID → projectID for the given IDs.
// It checks active tasks first, then falls back to archived tasks.
// Tasks that cannot be found are silently skipped.
func ResolveProjectIDs(ctx context.Context, repo Repository, ids []string) map[string]string {
	if len(ids) == 0 {
		return nil
	}

	_, projectIDs := ResolveAll(ctx, repo, ids)

	return projectIDs
}

// ResolveAll returns both taskID → title and taskID → projectID maps.
//
// Performance: the repo's active task cache is used first in a single bulk
// List call. Archived tasks are only scanned once (via a single ListArchived
// call) if some IDs are still missing, avoiding the N+1 GetArchived pattern
// which previously did a full project directory scan per missing ID.
func ResolveAll(ctx context.Context, repo Repository, ids []string) (titles map[string]string, projectIDs map[string]string) {
	if len(ids) == 0 {
		return nil, nil
	}

	titles = make(map[string]string, len(ids))
	projectIDs = make(map[string]string, len(ids))

	// Build a set of wanted IDs for quick membership tests.
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}

	// First pass: pull everything from the active task cache in one shot.
	// The YAML task repo caches active tasks in memory, so this is cheap.
	activeTasks, _, err := repo.List(ctx, "", "", "", 0, 0)
	if err == nil {
		for _, t := range activeTasks {
			if _, ok := wanted[t.ID]; !ok {
				continue
			}

			titles[t.ID] = t.Title
			projectIDs[t.ID] = t.ProjectID
		}
	}

	// Fast path: if all wanted IDs are resolved, skip the archived scan.
	if len(titles) == len(wanted) {
		return titles, projectIDs
	}

	// Second pass: one archived scan covers all remaining IDs at once.
	archivedTasks, _, err := repo.ListArchived(ctx, "", "", 0, 0)
	if err == nil {
		for _, t := range archivedTasks {
			if _, ok := wanted[t.ID]; !ok {
				continue
			}

			if _, done := titles[t.ID]; done {
				continue
			}

			titles[t.ID] = t.Title
			projectIDs[t.ID] = t.ProjectID
		}
	}

	return titles, projectIDs
}
