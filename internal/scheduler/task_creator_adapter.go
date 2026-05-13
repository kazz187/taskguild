package scheduler

import (
	"context"
	"time"

	"github.com/kazz187/taskguild/internal/schedule"
	"github.com/kazz187/taskguild/internal/task"
)

// TaskCreatorAdapter bridges scheduler.TaskCreator to task.Server. It expands
// title/description placeholders and attaches schedule provenance metadata.
type TaskCreatorAdapter struct {
	TaskServer *task.Server
}

// NewTaskCreatorAdapter constructs an adapter wired to the provided task
// server.
func NewTaskCreatorAdapter(ts *task.Server) *TaskCreatorAdapter {
	return &TaskCreatorAdapter{TaskServer: ts}
}

func (a *TaskCreatorAdapter) CreateTaskFromSchedule(ctx context.Context, s *schedule.Schedule, firedAt time.Time) (*task.Task, error) {
	title := schedule.ExpandTemplate(s.TaskTitle, firedAt)
	desc := schedule.ExpandTemplate(s.TaskDescription, firedAt)

	metadata := mergeMetadata(s.TaskMetadata, map[string]string{
		"created_by":        "schedule",
		"schedule_id":       s.ID,
		"schedule_fired_at": firedAt.Format(time.RFC3339),
	})

	return a.TaskServer.CreateTaskInternal(ctx, task.CreateTaskInput{
		ProjectID:   s.ProjectID,
		WorkflowID:  s.WorkflowID,
		Title:       title,
		Description: desc,
		StatusID:    s.StatusID,
		UseWorktree: s.UseWorktree,
		Effort:      s.Effort,
		Metadata:    metadata,
	})
}

// mergeMetadata returns a new map containing all entries from base, with
// overrides taking precedence on key collisions. base is not mutated.
func mergeMetadata(base, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		out[k] = v
	}

	for k, v := range overrides {
		out[k] = v
	}

	return out
}
