package tasklog

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.TaskLogServiceHandler = (*Server)(nil)

type Server struct {
	repo Repository
}

func NewServer(repo Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) ListTaskLogs(ctx context.Context, req *connect.Request[taskguildv1.ListTaskLogsRequest]) (*connect.Response[taskguildv1.ListTaskLogsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		limit = req.Msg.Pagination.Limit
		offset = req.Msg.Pagination.Offset
	}

	logs, total, err := s.repo.List(ctx, req.Msg.TaskId, int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.TaskLog, len(logs))
	for i, l := range logs {
		protos[i] = toProto(l)
	}

	return connect.NewResponse(&taskguildv1.ListTaskLogsResponse{
		Logs: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func toProto(l *TaskLog) *taskguildv1.TaskLog {
	return &taskguildv1.TaskLog{
		Id:        l.ID,
		TaskId:    l.TaskID,
		Level:     taskguildv1.TaskLogLevel(l.Level),
		Category:  taskguildv1.TaskLogCategory(l.Category),
		Message:   l.Message,
		Metadata:  l.Metadata,
		CreatedAt: timestamppb.New(l.CreatedAt),
	}
}
