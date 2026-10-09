package interaction

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/cerr"
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

func (s *Server) ListInteractions(ctx context.Context, req *taskguildv1.ListInteractionsRequest) (*taskguildv1.ListInteractionsResponse, error) {
	limit, offset := int32(0), int32(0)
	if req.GetPagination() != nil {
		limit = req.GetPagination().GetLimit() // 0 means no limit
		offset = req.GetPagination().GetOffset()
	}

	// When project_id is provided, resolve to task IDs for filtering.
	var taskIDs []string

	if req.GetProjectId() != "" {
		tasks, _, err := s.taskRepo.List(ctx, req.GetProjectId(), "", "", 0, 0)
		if err != nil {
			return nil, err
		}

		taskIDs = make([]string, len(tasks))
		for i, t := range tasks {
			taskIDs[i] = t.ID
		}
	}

	statusFilter := InteractionStatus(req.GetStatusFilter())

	interactions, total, err := s.repo.List(ctx, req.GetTaskId(), taskIDs, statusFilter, int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	// Collect unique task IDs to resolve titles.
	uniqueTaskIDs := make(map[string]struct{}, len(interactions))
	for _, inter := range interactions {
		uniqueTaskIDs[inter.TaskID] = struct{}{}
	}

	ids := make([]string, 0, len(uniqueTaskIDs))
	for id := range uniqueTaskIDs {
		ids = append(ids, id)
	}

	taskTitles, taskProjectIDs := task.ResolveAll(ctx, s.taskRepo, ids)

	protos := make([]*taskguildv1.Interaction, len(interactions))
	for i, inter := range interactions {
		protos[i] = ToProto(inter)
	}

	return &taskguildv1.ListInteractionsResponse{
		Interactions: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
		TaskTitles:     taskTitles,
		TaskProjectIds: taskProjectIDs,
	}, nil
}

func (s *Server) RespondToInteraction(ctx context.Context, req *taskguildv1.RespondToInteractionRequest) (*taskguildv1.RespondToInteractionResponse, error) {
	inter, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if inter.Status != StatusPending {
		return nil, cerr.NewError(cerr.FailedPrecondition, "interaction is not pending", nil).ConnectError()
	}

	now := time.Now()
	inter.Response = req.GetResponse()
	inter.Status = StatusResponded
	inter.RespondedAt = &now

	if err := s.repo.Update(ctx, inter); err != nil {
		return nil, err
	}

	interProto := ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED,
		inter.ID,
		MarshalInteractionPayload(interProto),
		map[string]string{eventbus.MetaTaskID: inter.TaskID, eventbus.MetaAgentID: inter.AgentID},
	)

	return &taskguildv1.RespondToInteractionResponse{
		Interaction: interProto,
	}, nil
}

func (s *Server) RespondToInteractionByToken(ctx context.Context, req *taskguildv1.RespondToInteractionByTokenRequest) (*taskguildv1.RespondToInteractionByTokenResponse, error) {
	if req.GetToken() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "token is required", nil).ConnectError()
	}

	if req.GetResponse() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "response is required", nil).ConnectError()
	}

	inter, err := s.repo.GetByResponseToken(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}

	if inter.Status != StatusPending {
		return nil, cerr.NewError(cerr.FailedPrecondition, "interaction is not pending", nil).ConnectError()
	}

	now := time.Now()
	inter.Response = req.GetResponse()
	inter.Status = StatusResponded
	inter.RespondedAt = &now
	// Invalidate the token after use.
	inter.ResponseToken = ""

	if err := s.repo.Update(ctx, inter); err != nil {
		return nil, err
	}

	interProto := ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED,
		inter.ID,
		MarshalInteractionPayload(interProto),
		map[string]string{eventbus.MetaTaskID: inter.TaskID, eventbus.MetaAgentID: inter.AgentID},
	)

	return &taskguildv1.RespondToInteractionByTokenResponse{
		Interaction: interProto,
	}, nil
}

func (s *Server) ExpireInteraction(ctx context.Context, req *taskguildv1.ExpireInteractionRequest) (*taskguildv1.ExpireInteractionResponse, error) {
	inter, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if inter.Status != StatusPending {
		return nil, cerr.NewError(cerr.FailedPrecondition, "interaction is not pending", nil).ConnectError()
	}

	now := time.Now()
	inter.Status = StatusExpired
	inter.RespondedAt = &now

	if err := s.repo.Update(ctx, inter); err != nil {
		return nil, err
	}

	interProto := ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED,
		inter.ID,
		MarshalInteractionPayload(interProto),
		map[string]string{eventbus.MetaTaskID: inter.TaskID, eventbus.MetaAgentID: inter.AgentID},
	)

	return &taskguildv1.ExpireInteractionResponse{
		Interaction: interProto,
	}, nil
}

func (s *Server) SendMessage(ctx context.Context, req *taskguildv1.SendMessageRequest) (*taskguildv1.SendMessageResponse, error) {
	if req.GetTaskId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id is required", nil).ConnectError()
	}

	if req.GetMessage() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "message is required", nil).ConnectError()
	}

	t, err := s.taskRepo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	now := time.Now()
	inter := &Interaction{
		ID:          ulid.Make().String(),
		ProjectID:   t.ProjectID,
		TaskID:      req.GetTaskId(),
		Type:        TypeUserMessage,
		Status:      StatusResponded,
		Title:       req.GetMessage(),
		CreatedAt:   now,
		RespondedAt: &now,
	}

	if err := s.repo.Create(ctx, inter); err != nil {
		return nil, err
	}

	interProto := ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		MarshalInteractionPayload(interProto),
		map[string]string{eventbus.MetaTaskID: inter.TaskID, eventbus.MetaProjectID: t.ProjectID},
	)

	return &taskguildv1.SendMessageResponse{
		Interaction: interProto,
	}, nil
}

func (s *Server) SubscribeInteractions(ctx context.Context, req *taskguildv1.SubscribeInteractionsRequest, stream taskguildv1connect.InteractionServiceSubscribeInteractionsServerStream) error {
	subID, ch := s.eventBus.Subscribe(64)
	defer s.eventBus.Unsubscribe(subID)

	taskID := req.GetTaskId()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			// Filter to interaction events only.
			if event.GetType() != taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED &&
				event.GetType() != taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED {
				continue
			}
			// Filter by task_id if specified.
			if taskID != "" {
				if eventTaskID, ok := event.GetMetadata()[eventbus.MetaTaskID]; ok && eventTaskID != taskID {
					continue
				}
			}
			// Use interaction data from event payload if available;
			// fall back to fetching from the repository.
			interProto := UnmarshalInteractionPayload(event.GetPayload())
			if interProto == nil {
				inter, err := s.repo.Get(ctx, event.GetResourceId())
				if err != nil {
					slog.Warn("failed to get interaction for stream", "id", event.GetResourceId(), "error", err)
					continue
				}

				interProto = ToProto(inter)
			}

			err := stream.Send(&taskguildv1.InteractionEvent{
				Interaction: interProto,
			})
			if err != nil {
				return err
			}
		}
	}
}

// ToProto converts a domain Interaction to its protobuf representation.
func ToProto(i *Interaction) *taskguildv1.Interaction {
	pb := &taskguildv1.Interaction{
		Id:          i.ID,
		TaskId:      i.TaskID,
		AgentId:     i.AgentID,
		Type:        taskguildv1.InteractionType(i.Type),
		Status:      taskguildv1.InteractionStatus(i.Status),
		Title:       i.Title,
		Description: i.Description,
		Response:    i.Response,
		Metadata:    i.Metadata,
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
