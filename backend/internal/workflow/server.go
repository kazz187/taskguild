package workflow

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.WorkflowServiceHandler = (*Server)(nil)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) CreateWorkflow(ctx context.Context, req *connect.Request[taskguildv1.CreateWorkflowRequest]) (*connect.Response[taskguildv1.CreateWorkflowResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) GetWorkflow(ctx context.Context, req *connect.Request[taskguildv1.GetWorkflowRequest]) (*connect.Response[taskguildv1.GetWorkflowResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) ListWorkflows(ctx context.Context, req *connect.Request[taskguildv1.ListWorkflowsRequest]) (*connect.Response[taskguildv1.ListWorkflowsResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) UpdateWorkflow(ctx context.Context, req *connect.Request[taskguildv1.UpdateWorkflowRequest]) (*connect.Response[taskguildv1.UpdateWorkflowResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) DeleteWorkflow(ctx context.Context, req *connect.Request[taskguildv1.DeleteWorkflowRequest]) (*connect.Response[taskguildv1.DeleteWorkflowResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}
