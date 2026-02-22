package agentmanager

import (
	"context"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentManagerServiceHandler = (*Server)(nil)

type Server struct {
	registry        *Registry
	taskRepo        task.Repository
	interactionRepo interaction.Repository
	eventBus        *eventbus.Bus
}

func NewServer(registry *Registry, taskRepo task.Repository, interactionRepo interaction.Repository, eventBus *eventbus.Bus) *Server {
	return &Server{
		registry:        registry,
		taskRepo:        taskRepo,
		interactionRepo: interactionRepo,
		eventBus:        eventBus,
	}
}

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[taskguildv1.AgentManagerSubscribeRequest], stream *connect.ServerStream[taskguildv1.AgentCommand]) error {
	agentManagerID := req.Msg.AgentManagerId
	if agentManagerID == "" {
		return cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}

	slog.Info("agent-manager connected", "agent_manager_id", agentManagerID, "max_concurrent_tasks", req.Msg.MaxConcurrentTasks)

	commandCh := s.registry.Register(agentManagerID, req.Msg.MaxConcurrentTasks)
	defer func() {
		s.registry.Unregister(agentManagerID)
		slog.Info("agent-manager disconnected", "agent_manager_id", agentManagerID)
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-commandCh:
			if !ok {
				return nil
			}
			if err := stream.Send(cmd); err != nil {
				return err
			}
		}
	}
}

func (s *Server) Heartbeat(ctx context.Context, req *connect.Request[taskguildv1.HeartbeatRequest]) (*connect.Response[taskguildv1.HeartbeatResponse], error) {
	if req.Msg.AgentManagerId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}
	if !s.registry.UpdateHeartbeat(req.Msg.AgentManagerId, req.Msg.ActiveTasks) {
		return nil, cerr.NewError(cerr.NotFound, "agent-manager not connected", nil).ConnectError()
	}
	return connect.NewResponse(&taskguildv1.HeartbeatResponse{}), nil
}

func (s *Server) ReportTaskResult(ctx context.Context, req *connect.Request[taskguildv1.ReportTaskResultRequest]) (*connect.Response[taskguildv1.ReportTaskResultResponse], error) {
	t, err := s.taskRepo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	// Clear assigned agent.
	t.AssignedAgentID = ""
	t.UpdatedAt = time.Now()

	// Store result summary in metadata.
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}
	t.Metadata["result_status"] = req.Msg.Status.String()
	if req.Msg.Summary != "" {
		t.Metadata["result_summary"] = req.Msg.Summary
	}
	if req.Msg.ErrorMessage != "" {
		t.Metadata["result_error"] = req.Msg.ErrorMessage
	}

	if err := s.taskRepo.Update(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
		t.ID,
		"",
		map[string]string{
			"project_id":    t.ProjectID,
			"workflow_id":   t.WorkflowID,
			"result_status": req.Msg.Status.String(),
		},
	)

	return connect.NewResponse(&taskguildv1.ReportTaskResultResponse{}), nil
}

func (s *Server) CreateInteraction(ctx context.Context, req *connect.Request[taskguildv1.CreateInteractionRequest]) (*connect.Response[taskguildv1.CreateInteractionResponse], error) {
	now := time.Now()
	inter := &interaction.Interaction{
		ID:          ulid.Make().String(),
		TaskID:      req.Msg.TaskId,
		AgentID:     req.Msg.AgentId,
		Type:        interaction.InteractionType(req.Msg.Type),
		Status:      interaction.StatusPending,
		Title:       req.Msg.Title,
		Description: req.Msg.Description,
		CreatedAt:   now,
	}
	for _, opt := range req.Msg.Options {
		inter.Options = append(inter.Options, interaction.Option{
			Label:       opt.Label,
			Value:       opt.Value,
			Description: opt.Description,
		})
	}

	if err := s.interactionRepo.Create(ctx, inter); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		"",
		map[string]string{"task_id": inter.TaskID, "agent_id": inter.AgentID},
	)

	return connect.NewResponse(&taskguildv1.CreateInteractionResponse{
		Interaction: interactionToProto(inter),
	}), nil
}

func (s *Server) GetInteractionResponse(ctx context.Context, req *connect.Request[taskguildv1.GetInteractionResponseRequest]) (*connect.Response[taskguildv1.GetInteractionResponseResponse], error) {
	inter, err := s.interactionRepo.Get(ctx, req.Msg.InteractionId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetInteractionResponseResponse{
		Interaction: interactionToProto(inter),
	}), nil
}

func (s *Server) ReportAgentStatus(ctx context.Context, req *connect.Request[taskguildv1.ReportAgentStatusRequest]) (*connect.Response[taskguildv1.ReportAgentStatusResponse], error) {
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_STATUS_CHANGED,
		req.Msg.TaskId,
		"",
		map[string]string{
			"agent_manager_id": req.Msg.AgentManagerId,
			"agent_status":     req.Msg.Status.String(),
			"message":          req.Msg.Message,
		},
	)
	return connect.NewResponse(&taskguildv1.ReportAgentStatusResponse{}), nil
}

func interactionToProto(i *interaction.Interaction) *taskguildv1.Interaction {
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
