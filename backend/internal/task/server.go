package task

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.TaskServiceHandler = (*Server)(nil)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) CreateTask(ctx context.Context, req *connect.Request[taskguildv1.CreateTaskRequest]) (*connect.Response[taskguildv1.CreateTaskResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) GetTask(ctx context.Context, req *connect.Request[taskguildv1.GetTaskRequest]) (*connect.Response[taskguildv1.GetTaskResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) ListTasks(ctx context.Context, req *connect.Request[taskguildv1.ListTasksRequest]) (*connect.Response[taskguildv1.ListTasksResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) UpdateTask(ctx context.Context, req *connect.Request[taskguildv1.UpdateTaskRequest]) (*connect.Response[taskguildv1.UpdateTaskResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) DeleteTask(ctx context.Context, req *connect.Request[taskguildv1.DeleteTaskRequest]) (*connect.Response[taskguildv1.DeleteTaskResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) UpdateTaskStatus(ctx context.Context, req *connect.Request[taskguildv1.UpdateTaskStatusRequest]) (*connect.Response[taskguildv1.UpdateTaskStatusResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}
