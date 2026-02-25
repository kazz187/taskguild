package tasklog

import "context"

type Repository interface {
	Create(ctx context.Context, log *TaskLog) error
	List(ctx context.Context, taskID string, limit, offset int) ([]*TaskLog, int, error)
}
