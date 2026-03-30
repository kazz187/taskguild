package tasklog

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// DescriptionLoggerAdapter implements task.DescriptionLogger using the tasklog repository.
type DescriptionLoggerAdapter struct {
	repo     Repository
	eventBus *eventbus.Bus
}

func NewDescriptionLoggerAdapter(repo Repository, eventBus *eventbus.Bus) *DescriptionLoggerAdapter {
	return &DescriptionLoggerAdapter{repo: repo, eventBus: eventBus}
}

func (a *DescriptionLoggerAdapter) LogDescriptionChange(ctx context.Context, projectID, taskID, newDescription string) error {
	preview := newDescription
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	l := &TaskLog{
		ID:        ulid.Make().String(),
		ProjectID: projectID,
		TaskID:    taskID,
		Level:     int32(taskguildv1.TaskLogLevel_TASK_LOG_LEVEL_INFO),
		Category:  int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_RESULT),
		Message:   preview,
		Metadata: map[string]string{
			"full_text":   newDescription,
			"result_type": "description",
			"source":      "user",
		},
		CreatedAt: time.Now(),
	}

	if err := a.repo.Create(ctx, l); err != nil {
		return err
	}

	a.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_LOG,
		l.ID,
		"",
		map[string]string{"task_id": taskID, "project_id": projectID},
	)

	return nil
}
