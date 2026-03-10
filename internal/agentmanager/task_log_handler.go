package agentmanager

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/gen/proto/taskguild/v1"
)

func (s *Server) ReportTaskLog(ctx context.Context, req *connect.Request[taskguildv1.ReportTaskLogRequest]) (*connect.Response[taskguildv1.ReportTaskLogResponse], error) {
	if req.Msg.TaskId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id is required", nil).ConnectError()
	}

	now := time.Now()
	l := &tasklog.TaskLog{
		ID:        ulid.Make().String(),
		TaskID:    req.Msg.TaskId,
		Level:     int32(req.Msg.Level),
		Category:  int32(req.Msg.Category),
		Message:   req.Msg.Message,
		Metadata:  req.Msg.Metadata,
		CreatedAt: now,
	}

	if err := s.taskLogRepo.Create(ctx, l); err != nil {
		return nil, err
	}

	// Resolve project_id from the task for event metadata.
	eventMeta := map[string]string{"task_id": req.Msg.TaskId}
	if t, err := s.taskRepo.Get(ctx, req.Msg.TaskId); err == nil {
		eventMeta["project_id"] = t.ProjectID
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_LOG,
		l.ID,
		"",
		eventMeta,
	)

	return connect.NewResponse(&taskguildv1.ReportTaskLogResponse{}), nil
}
