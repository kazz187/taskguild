package agentmanager

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentManagerServiceHandler = (*Server)(nil)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[taskguildv1.AgentManagerSubscribeRequest], stream *connect.ServerStream[taskguildv1.AgentCommand]) error {
	return cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) Heartbeat(ctx context.Context, req *connect.Request[taskguildv1.HeartbeatRequest]) (*connect.Response[taskguildv1.HeartbeatResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) ReportTaskResult(ctx context.Context, req *connect.Request[taskguildv1.ReportTaskResultRequest]) (*connect.Response[taskguildv1.ReportTaskResultResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) CreateInteraction(ctx context.Context, req *connect.Request[taskguildv1.CreateInteractionRequest]) (*connect.Response[taskguildv1.CreateInteractionResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) GetInteractionResponse(ctx context.Context, req *connect.Request[taskguildv1.GetInteractionResponseRequest]) (*connect.Response[taskguildv1.GetInteractionResponseResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) ReportAgentStatus(ctx context.Context, req *connect.Request[taskguildv1.ReportAgentStatusRequest]) (*connect.Response[taskguildv1.ReportAgentStatusResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}
