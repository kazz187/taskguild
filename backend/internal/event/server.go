package event

import (
	"context"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.EventServiceHandler = (*Server)(nil)

type Server struct {
	eventBus *eventbus.Bus
}

func NewServer(eventBus *eventbus.Bus) *Server {
	return &Server{eventBus: eventBus}
}

func (s *Server) SubscribeEvents(ctx context.Context, req *connect.Request[taskguildv1.SubscribeEventsRequest], stream *connect.ServerStream[taskguildv1.Event]) error {
	subID, ch := s.eventBus.Subscribe(64)
	defer s.eventBus.Unsubscribe(subID)

	// Send an initial event to signal that the stream connection is established.
	// This allows the client to immediately transition from "connecting" to "connected".
	if err := stream.Send(&taskguildv1.Event{
		Type: taskguildv1.EventType_EVENT_TYPE_UNSPECIFIED,
	}); err != nil {
		return err
	}

	// Build event type filter set.
	typeFilter := make(map[taskguildv1.EventType]struct{}, len(req.Msg.EventTypes))
	for _, et := range req.Msg.EventTypes {
		typeFilter[et] = struct{}{}
	}

	projectID := req.Msg.ProjectId

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			// Filter by event type if specified.
			if len(typeFilter) > 0 {
				if _, match := typeFilter[event.Type]; !match {
					continue
				}
			}
			// Filter by project_id if specified.
			if projectID != "" {
				if eventProjectID, ok := event.Metadata["project_id"]; ok && eventProjectID != projectID {
					continue
				}
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}
