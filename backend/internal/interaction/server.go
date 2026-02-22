package interaction

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.InteractionServiceHandler = (*Server)(nil)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) ListInteractions(ctx context.Context, req *connect.Request[taskguildv1.ListInteractionsRequest]) (*connect.Response[taskguildv1.ListInteractionsResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) RespondToInteraction(ctx context.Context, req *connect.Request[taskguildv1.RespondToInteractionRequest]) (*connect.Response[taskguildv1.RespondToInteractionResponse], error) {
	return nil, cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}

func (s *Server) SubscribeInteractions(ctx context.Context, req *connect.Request[taskguildv1.SubscribeInteractionsRequest], stream *connect.ServerStream[taskguildv1.InteractionEvent]) error {
	return cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}
