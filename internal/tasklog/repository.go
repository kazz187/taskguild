package tasklog

import (
	"context"
	"time"
)

type Repository interface {
	Create(ctx context.Context, log *TaskLog) error
	List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*TaskLog, int, error)
	// DeleteByTaskID removes all task logs belonging to the given task.
	DeleteByTaskID(ctx context.Context, taskID string) (int, error)
	// CleanupOlderThan removes task log entries older than maxAge.
	// Returns the number of deleted entries.
	CleanupOlderThan(ctx context.Context, maxAge time.Duration) (int, error)
}
