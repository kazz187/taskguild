package agentmanager

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentManagerServiceHandler = (*Server)(nil)

type Server struct {
	registry        *Registry
	taskRepo        task.Repository
	workflowRepo    workflow.Repository
	interactionRepo interaction.Repository
	eventBus        *eventbus.Bus
}

func NewServer(registry *Registry, taskRepo task.Repository, workflowRepo workflow.Repository, interactionRepo interaction.Repository, eventBus *eventbus.Bus) *Server {
	return &Server{
		registry:        registry,
		taskRepo:        taskRepo,
		workflowRepo:    workflowRepo,
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

	// Clear assigned agent and reset assignment status.
	t.AssignedAgentID = ""
	t.AssignmentStatus = task.AssignmentStatusUnassigned
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

func (s *Server) ClaimTask(ctx context.Context, req *connect.Request[taskguildv1.ClaimTaskRequest]) (*connect.Response[taskguildv1.ClaimTaskResponse], error) {
	if req.Msg.TaskId == "" || req.Msg.AgentManagerId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id and agent_manager_id are required", nil).ConnectError()
	}

	t, err := s.taskRepo.Claim(ctx, req.Msg.TaskId, req.Msg.AgentManagerId)
	if err != nil {
		if cerr.IsCode(err, cerr.FailedPrecondition) {
			return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
				Success: false,
			}), nil
		}
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Find agent config for the task's current status.
	wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var instructions string
	var agentConfigID string
	for _, cfg := range wf.AgentConfigs {
		if cfg.WorkflowStatusID == t.StatusID {
			instructions = cfg.Instructions
			agentConfigID = cfg.ID
			break
		}
	}

	// Build enriched metadata with task info and available transitions.
	enrichedMetadata := make(map[string]string)
	for k, v := range t.Metadata {
		enrichedMetadata[k] = v
	}
	enrichedMetadata["_task_title"] = t.Title
	enrichedMetadata["_task_description"] = t.Description
	enrichedMetadata["_current_status_id"] = t.StatusID
	if t.UseWorktree {
		enrichedMetadata["_use_worktree"] = "true"
	}
	if t.PermissionMode != "" {
		enrichedMetadata["_permission_mode"] = t.PermissionMode
	}

	// Resolve current status name and available transitions from workflow.
	statusMap := make(map[string]string) // id -> name
	for _, st := range wf.Statuses {
		statusMap[st.ID] = st.Name
	}
	if name, ok := statusMap[t.StatusID]; ok {
		enrichedMetadata["_current_status_name"] = name
	}
	for _, st := range wf.Statuses {
		if st.ID == t.StatusID {
			type transitionEntry struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			var transitions []transitionEntry
			for _, targetID := range st.TransitionsTo {
				transitions = append(transitions, transitionEntry{
					ID:   targetID,
					Name: statusMap[targetID],
				})
			}
			if b, err := json.Marshal(transitions); err == nil {
				enrichedMetadata["_available_transitions"] = string(b)
			}
			break
		}
	}

	// Publish agent assigned event.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_ASSIGNED,
		t.ID,
		"",
		map[string]string{
			"agent_manager_id": req.Msg.AgentManagerId,
			"agent_config_id":  agentConfigID,
			"project_id":       t.ProjectID,
			"workflow_id":      t.WorkflowID,
		},
	)

	slog.Info("agent claimed task",
		"task_id", t.ID,
		"agent_manager_id", req.Msg.AgentManagerId,
		"agent_config_id", agentConfigID,
	)

	return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
		Success:       true,
		Instructions:  instructions,
		AgentConfigId: agentConfigID,
		Metadata:      enrichedMetadata,
	}), nil
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
