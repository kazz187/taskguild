package event

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.EventServiceHandler = (*Server)(nil)

type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) SubscribeEvents(ctx context.Context, req *connect.Request[taskguildv1.SubscribeEventsRequest], stream *connect.ServerStream[taskguildv1.Event]) error {
	return cerr.NewError(cerr.Unimplemented, "not implemented", nil).ConnectError()
}
