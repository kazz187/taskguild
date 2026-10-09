package agentmanager

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) ReportTaskLog(ctx context.Context, req *taskguildv1.ReportTaskLogRequest) (*taskguildv1.ReportTaskLogResponse, error) {
	if req.GetTaskId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id is required", nil)
	}

	// Look up the task to get ProjectID for storage path construction.
	t, err := s.taskRepo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	now := time.Now()
	l := &tasklog.TaskLog{
		ID:        ulid.Make().String(),
		ProjectID: t.ProjectID,
		TaskID:    req.GetTaskId(),
		Level:     int32(req.GetLevel()),
		Category:  int32(req.GetCategory()),
		Message:   req.GetMessage(),
		Metadata:  req.GetMetadata(),
		CreatedAt: now,
	}

	if err := s.taskLogRepo.Create(ctx, l); err != nil {
		return nil, err
	}

	eventMeta := map[string]string{eventbus.MetaTaskID: req.GetTaskId(), eventbus.MetaProjectID: t.ProjectID}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_LOG,
		l.ID,
		"",
		eventMeta,
	)

	return &taskguildv1.ReportTaskLogResponse{}, nil
}
