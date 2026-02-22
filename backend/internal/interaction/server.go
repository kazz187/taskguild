package interaction

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.InteractionServiceHandler = (*Server)(nil)

type Server struct {
	repo     Repository
	taskRepo task.Repository
	eventBus *eventbus.Bus
}

func NewServer(repo Repository, taskRepo task.Repository, eventBus *eventbus.Bus) *Server {
	return &Server{
		repo:     repo,
		taskRepo: taskRepo,
		eventBus: eventBus,
	}
}

func (s *Server) ListInteractions(ctx context.Context, req *connect.Request[taskguildv1.ListInteractionsRequest]) (*connect.Response[taskguildv1.ListInteractionsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	interactions, total, err := s.repo.List(ctx, req.Msg.TaskId, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.Interaction, len(interactions))
	for i, inter := range interactions {
		protos[i] = toProto(inter)
	}
	return connect.NewResponse(&taskguildv1.ListInteractionsResponse{
		Interactions: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) RespondToInteraction(ctx context.Context, req *connect.Request[taskguildv1.RespondToInteractionRequest]) (*connect.Response[taskguildv1.RespondToInteractionResponse], error) {
	inter, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	if inter.Status != StatusPending {
		return nil, cerr.NewError(cerr.FailedPrecondition, "interaction is not pending", nil).ConnectError()
	}

	now := time.Now()
	inter.Response = req.Msg.Response
	inter.Status = StatusResponded
	inter.RespondedAt = &now

	if err := s.repo.Update(ctx, inter); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED,
		inter.ID,
		"",
		map[string]string{"task_id": inter.TaskID, "agent_id": inter.AgentID},
	)

	return connect.NewResponse(&taskguildv1.RespondToInteractionResponse{
		Interaction: toProto(inter),
	}), nil
}

func (s *Server) SendMessage(ctx context.Context, req *connect.Request[taskguildv1.SendMessageRequest]) (*connect.Response[taskguildv1.SendMessageResponse], error) {
	if req.Msg.TaskId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id is required", nil).ConnectError()
	}
	if req.Msg.Message == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "message is required", nil).ConnectError()
	}

	t, err := s.taskRepo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	inter := &Interaction{
		ID:          ulid.Make().String(),
		TaskID:      req.Msg.TaskId,
		Type:        TypeUserMessage,
		Status:      StatusResponded,
		Title:       req.Msg.Message,
		CreatedAt:   now,
		RespondedAt: &now,
	}

	if err := s.repo.Create(ctx, inter); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		"",
		map[string]string{"task_id": inter.TaskID, "project_id": t.ProjectID},
	)

	return connect.NewResponse(&taskguildv1.SendMessageResponse{
		Interaction: toProto(inter),
	}), nil
}

func (s *Server) SubscribeInteractions(ctx context.Context, req *connect.Request[taskguildv1.SubscribeInteractionsRequest], stream *connect.ServerStream[taskguildv1.InteractionEvent]) error {
	subID, ch := s.eventBus.Subscribe(64)
	defer s.eventBus.Unsubscribe(subID)

	taskID := req.Msg.TaskId

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			// Filter to interaction events only.
			if event.Type != taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED &&
				event.Type != taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED {
				continue
			}
			// Filter by task_id if specified.
			if taskID != "" {
				if eventTaskID, ok := event.Metadata["task_id"]; ok && eventTaskID != taskID {
					continue
				}
			}
			// Fetch latest interaction state.
			inter, err := s.repo.Get(ctx, event.ResourceId)
			if err != nil {
				slog.Warn("failed to get interaction for stream", "id", event.ResourceId, "error", err)
				continue
			}
			if err := stream.Send(&taskguildv1.InteractionEvent{
				Interaction: toProto(inter),
			}); err != nil {
				return err
			}
		}
	}
}

func toProto(i *Interaction) *taskguildv1.Interaction {
	pb := &taskguildv1.Interaction{
		Id:          i.ID,
		TaskId:      i.TaskID,
		AgentId:     i.AgentID,
		Type:        taskguildv1.InteractionType(i.Type),
		Status:      taskguildv1.InteractionStatus(i.Status),
		Title:       i.Title,
		Description: i.Description,
		Response:    i.Response,
		CreatedAt:   timestamppb.New(i.CreatedAt),
	}
	for _, opt := range i.Options {
		pb.Options = append(pb.Options, &taskguildv1.InteractionOption{
			Label:       opt.Label,
			Value:       opt.Value,
			Description: opt.Description,
		})
	}
	if i.RespondedAt != nil {
		pb.RespondedAt = timestamppb.New(*i.RespondedAt)
	}
	return pb
}
