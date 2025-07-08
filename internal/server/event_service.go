package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/event"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// EventServiceHandler implements the EventService Connect handler
type EventServiceHandler struct {
	eventBus *event.EventBus
}

// NewEventServiceHandler creates a new EventService handler
func NewEventServiceHandler(eventBus *event.EventBus) *EventServiceHandler {
	return &EventServiceHandler{
		eventBus: eventBus,
	}
}

// PathAndHandler returns the Connect path and handler
func (h *EventServiceHandler) PathAndHandler() (string, http.Handler) {
	return taskguildv1connect.NewEventServiceHandler(h)
}

// GetEventLogs gets event logs
func (h *EventServiceHandler) GetEventLogs(
	ctx context.Context,
	req *connect.Request[taskguildv1.GetEventLogsRequest],
) (*connect.Response[taskguildv1.GetEventLogsResponse], error) {
	// This is a placeholder implementation
	// In a real implementation, you would query the event store/database

	return connect.NewResponse(&taskguildv1.GetEventLogsResponse{
		Events: []*taskguildv1.Event{},
		Total:  0,
	}), nil
}

// SubscribeEvents subscribes to events (streaming)
func (h *EventServiceHandler) SubscribeEvents(
	ctx context.Context,
	req *connect.Request[taskguildv1.SubscribeEventsRequest],
	stream *connect.ServerStream[taskguildv1.EventMessage],
) error {
	// This is a placeholder implementation
	// In a real implementation, you would:
	// 1. Subscribe to the event bus for the requested event types
	// 2. Stream events to the client as they arrive
	// 3. Handle context cancellation for cleanup

	// For now, just wait for context cancellation
	<-ctx.Done()
	return ctx.Err()
}

// PublishEvent publishes an event
func (h *EventServiceHandler) PublishEvent(
	ctx context.Context,
	req *connect.Request[taskguildv1.PublishEventRequest],
) (*connect.Response[taskguildv1.PublishEventResponse], error) {
	// Create event
	evt := &event.EventMessage{
		ID:        fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Type:      event.EventType(req.Msg.Type),
		Timestamp: time.Now(),
		Source:    "grpc-client",
		Data:      []byte(req.Msg.Payload),
	}

	// Publish event
	if err := h.eventBus.Publish(ctx, "eventservice", evt); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to publish event: %w", err))
	}

	// Convert to proto event
	protoEvent := &taskguildv1.Event{
		Id:        evt.ID,
		Type:      string(evt.Type),
		Payload:   string(evt.Data),
		Metadata:  make(map[string]string), // Initialize empty metadata
		CreatedAt: timestamppb.New(evt.Timestamp),
	}

	return connect.NewResponse(&taskguildv1.PublishEventResponse{
		Event: protoEvent,
	}), nil
}
