package tasklog

import "context"

type Repository interface {
	Create(ctx context.Context, log *TaskLog) error
	List(ctx context.Context, taskID string, taskIDs []string, limit, offset int) ([]*TaskLog, int, error)
	// DeleteByTaskID removes all task logs belonging to the given task.
	DeleteByTaskID(ctx context.Context, taskID string) (int, error)
}
