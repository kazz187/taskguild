package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/agent"
	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/permission"
	"github.com/kazz187/taskguild/backend/internal/project"
	"github.com/kazz187/taskguild/backend/internal/script"
	"github.com/kazz187/taskguild/backend/internal/skill"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/tasklog"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentManagerServiceHandler = (*Server)(nil)

type Server struct {
	registry        *Registry
	taskRepo        task.Repository
	workflowRepo    workflow.Repository
	agentRepo       agent.Repository
	interactionRepo interaction.Repository
	projectRepo     project.Repository
	skillRepo       skill.Repository
	scriptRepo      script.Repository
	taskLogRepo     tasklog.Repository
	permissionRepo  permission.Repository
	eventBus        *eventbus.Bus

	// scriptResultStore stores execution results keyed by request_id.
	scriptResultStore script.ExecutionResultStore

	// worktreeCache stores the latest worktree list per project_id,
	// populated by ReportWorktreeList and read by GetWorktreeList.
	worktreeMu    sync.RWMutex
	worktreeCache map[string][]*taskguildv1.WorktreeInfo // project_id -> worktrees
}

func NewServer(registry *Registry, taskRepo task.Repository, workflowRepo workflow.Repository, agentRepo agent.Repository, interactionRepo interaction.Repository, projectRepo project.Repository, skillRepo skill.Repository, scriptRepo script.Repository, taskLogRepo tasklog.Repository, permissionRepo permission.Repository, eventBus *eventbus.Bus, scriptResultStore script.ExecutionResultStore) *Server {
	return &Server{
		registry:          registry,
		taskRepo:          taskRepo,
		workflowRepo:      workflowRepo,
		agentRepo:         agentRepo,
		interactionRepo:   interactionRepo,
		projectRepo:       projectRepo,
		skillRepo:         skillRepo,
		scriptRepo:        scriptRepo,
		taskLogRepo:       taskLogRepo,
		permissionRepo:    permissionRepo,
		eventBus:          eventBus,
		scriptResultStore: scriptResultStore,
		worktreeCache:     make(map[string][]*taskguildv1.WorktreeInfo),
	}
}

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[taskguildv1.AgentManagerSubscribeRequest], stream *connect.ServerStream[taskguildv1.AgentCommand]) error {
	agentManagerID := req.Msg.AgentManagerId
	if agentManagerID == "" {
		return cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}

	projectName := req.Msg.ProjectName
	slog.Info("agent-manager connected", "agent_manager_id", agentManagerID, "max_concurrent_tasks", req.Msg.MaxConcurrentTasks, "project_name", projectName)

	// On (re-)connect, release any tasks still assigned to this agent.
	// The agent starts fresh and does not retain tasks from a previous session.
	s.releaseAgentTasks(ctx, agentManagerID)

	commandCh := s.registry.Register(agentManagerID, req.Msg.MaxConcurrentTasks, projectName)
	defer func() {
		s.registry.Unregister(agentManagerID)
		// On disconnect, release assigned tasks so other agents can pick them up.
		s.releaseAgentTasks(context.Background(), agentManagerID)
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

// releaseAgentTasks unassigns all tasks held by the given agent and
// re-broadcasts them so other agents can pick them up.
func (s *Server) releaseAgentTasks(ctx context.Context, agentManagerID string) {
	released, err := s.taskRepo.ReleaseByAgent(ctx, agentManagerID)
	if err != nil {
		slog.Error("failed to release tasks for agent", "agent_manager_id", agentManagerID, "error", err)
		return
	}
	for _, t := range released {
		slog.Info("released task from agent",
			"task_id", t.ID,
			"agent_manager_id", agentManagerID,
		)

		// Expire any orphaned PENDING interactions for the released task
		// so they no longer show in the UI.
		if expired, err := s.interactionRepo.ExpirePendingByTask(ctx, t.ID); err != nil {
			slog.Error("failed to expire pending interactions for released task",
				"task_id", t.ID, "error", err)
		} else if expired > 0 {
			slog.Info("expired orphaned pending interactions",
				"task_id", t.ID, "count", expired)
			// Publish events so the frontend removes them from the pending list.
			s.eventBus.PublishNew(
				taskguildv1.EventType_EVENT_TYPE_INTERACTION_RESPONDED,
				t.ID,
				"",
				map[string]string{
					"task_id": t.ID,
					"reason":  "agent_released",
				},
			)
		}

		// Look up agent config to build the broadcast command.
		wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
		if err != nil {
			slog.Error("failed to get workflow for released task", "task_id", t.ID, "error", err)
			continue
		}
		var agentConfigID string
		// Try new approach: status-level agent_id.
		for _, st := range wf.Statuses {
			if st.ID == t.StatusID && st.AgentID != "" {
				agentConfigID = st.AgentID
				break
			}
		}
		// Fall back to legacy AgentConfig list.
		if agentConfigID == "" {
			for _, cfg := range wf.AgentConfigs {
				if cfg.WorkflowStatusID == t.StatusID {
					agentConfigID = cfg.ID
					break
				}
			}
		}

		// Resolve project name for filtered broadcast.
		var projectName string
		if p, pErr := s.projectRepo.Get(ctx, t.ProjectID); pErr == nil {
			projectName = p.Name
		}

		// Broadcast so other connected agents (same project) can claim the task.
		s.registry.BroadcastCommandToProject(projectName, &taskguildv1.AgentCommand{
			Command: &taskguildv1.AgentCommand_TaskAvailable{
				TaskAvailable: &taskguildv1.TaskAvailableCommand{
					TaskId:        t.ID,
					AgentConfigId: agentConfigID,
					Title:         t.Title,
					Metadata:      t.Metadata,
				},
			},
		})

		s.eventBus.PublishNew(
			taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
			t.ID,
			"",
			map[string]string{
				"project_id":  t.ProjectID,
				"workflow_id": t.WorkflowID,
				"reason":      "agent_released",
			},
		)
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
			"project_id":  t.ProjectID,
			"workflow_id": t.WorkflowID,
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

	// Validate project name: if the agent declared a project, verify it matches.
	if agentProject, ok := s.registry.GetProjectName(req.Msg.AgentManagerId); ok && agentProject != "" {
		var taskProjectName string
		if p, pErr := s.projectRepo.Get(ctx, t.ProjectID); pErr == nil {
			taskProjectName = p.Name
		}
		if taskProjectName != "" && agentProject != taskProjectName {
			// Mismatch: unclaim the task and reject.
			t.AssignedAgentID = ""
			t.AssignmentStatus = task.AssignmentStatusPending
			t.UpdatedAt = time.Now()
			_ = s.taskRepo.Update(ctx, t)
			slog.Warn("agent claimed task from wrong project",
				"task_id", t.ID,
				"agent_project", agentProject,
				"task_project", taskProjectName,
			)
			return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
				Success: false,
			}), nil
		}
	}

	// Find agent config for the task's current status.
	wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var instructions string
	var agentConfigID string

	// Try new approach: status-level agent_id referencing Agent entity.
	var currentAgentID string
	for _, st := range wf.Statuses {
		if st.ID == t.StatusID && st.AgentID != "" {
			currentAgentID = st.AgentID
			break
		}
	}

	if currentAgentID != "" {
		// New approach: fetch Agent entity.
		ag, err := s.agentRepo.Get(ctx, currentAgentID)
		if err == nil {
			instructions = ag.Prompt
			agentConfigID = ag.ID
		}
	}

	// Fall back to legacy AgentConfig list.
	if instructions == "" {
		for _, cfg := range wf.AgentConfigs {
			if cfg.WorkflowStatusID == t.StatusID {
				instructions = cfg.Instructions
				agentConfigID = cfg.ID
				break
			}
		}
	}

	// Prepend workflow custom prompt to agent instructions (if non-empty).
	if wf.CustomPrompt != "" && instructions != "" {
		instructions = wf.CustomPrompt + "\n\n" + instructions
	} else if wf.CustomPrompt != "" {
		instructions = wf.CustomPrompt
	}

	// Build enriched metadata with task info and available transitions.
	enrichedMetadata := make(map[string]string)
	for k, v := range t.Metadata {
		enrichedMetadata[k] = v
	}
	enrichedMetadata["_task_title"] = t.Title
	enrichedMetadata["_task_description"] = t.Description
	enrichedMetadata["_current_status_id"] = t.StatusID
	enrichedMetadata["_project_id"] = t.ProjectID
	enrichedMetadata["_workflow_id"] = t.WorkflowID
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

	// Inject all workflow statuses so agents can create tasks with any status.
	{
		type statusInfo struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var allStatuses []statusInfo
		for _, st := range wf.Statuses {
			allStatuses = append(allStatuses, statusInfo{ID: st.ID, Name: st.Name})
		}
		if b, err := json.Marshal(allStatuses); err == nil {
			enrichedMetadata["_workflow_statuses"] = string(b)
		}
	}

	// Resolve hooks for the current status and inject into metadata.
	for _, st := range wf.Statuses {
		if st.ID == t.StatusID && len(st.Hooks) > 0 {
			type hookEntry struct {
				ID         string `json:"id"`
				SkillID    string `json:"skill_id"`
				ActionType string `json:"action_type"`
				ActionID   string `json:"action_id"`
				Trigger    string `json:"trigger"`
				Order      int32  `json:"order"`
				Name       string `json:"name"`
				Content    string `json:"content"`
			}
			var hooks []hookEntry
			for _, h := range st.Hooks {
				entry := hookEntry{
					ID:         h.ID,
					SkillID:    h.SkillID,
					ActionType: string(h.ActionType),
					ActionID:   h.ActionID,
					Trigger:    string(h.Trigger),
					Order:      h.Order,
					Name:       h.Name,
				}

				// Resolve content based on action type.
				// New approach: use action_type + action_id.
				if h.ActionType == workflow.HookActionTypeSkill && h.ActionID != "" {
					if s.skillRepo != nil {
						if sk, err := s.skillRepo.Get(ctx, h.ActionID); err == nil {
							entry.Content = sk.Content
						} else {
							slog.Warn("failed to resolve hook skill", "hook_id", h.ID, "action_id", h.ActionID, "error", err)
						}
					}
				} else if h.ActionType == workflow.HookActionTypeScript && h.ActionID != "" {
					if s.scriptRepo != nil {
						if sc, err := s.scriptRepo.Get(ctx, h.ActionID); err == nil {
							entry.Content = sc.Content
						} else {
							slog.Warn("failed to resolve hook script", "hook_id", h.ID, "action_id", h.ActionID, "error", err)
						}
					}
				} else if h.SkillID != "" {
					// Legacy: use skill_id directly.
					if s.skillRepo != nil {
						if sk, err := s.skillRepo.Get(ctx, h.SkillID); err == nil {
							entry.Content = sk.Content
						} else {
							slog.Warn("failed to resolve hook skill", "hook_id", h.ID, "skill_id", h.SkillID, "error", err)
						}
					}
				}

				hooks = append(hooks, entry)
			}
			if len(hooks) > 0 {
				if b, err := json.Marshal(hooks); err == nil {
					enrichedMetadata["_hooks"] = string(b)
				}
			}
			break
		}
	}

	// Build sub-agents from project's Agent definitions.
	// All agents in the project (except the current one) are passed as sub-agents.
	if s.agentRepo != nil {
		agents, _, err := s.agentRepo.List(ctx, t.ProjectID, 100, 0)
		if err == nil && len(agents) > 0 {
			type subAgentDef struct {
				Description string   `json:"description"`
				Prompt      string   `json:"prompt"`
				Tools       []string `json:"tools,omitempty"`
				Model       string   `json:"model,omitempty"`
			}
			subAgents := make(map[string]subAgentDef)
			for _, ag := range agents {
				if ag.ID == agentConfigID {
					continue // skip the current agent
				}
				subAgents[ag.Name] = subAgentDef{
					Description: ag.Description,
					Prompt:      ag.Prompt,
					Tools:       ag.Tools,
					Model:       ag.Model,
				}
			}
			if len(subAgents) > 0 {
				if b, err := json.Marshal(subAgents); err == nil {
					enrichedMetadata["_sub_agents"] = string(b)
				}
			}
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

func (s *Server) SyncAgents(ctx context.Context, req *connect.Request[taskguildv1.SyncAgentsRequest]) (*connect.Response[taskguildv1.SyncAgentsResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	agents, _, err := s.agentRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.AgentDefinition, len(agents))
	for i, ag := range agents {
		protos[i] = agentToProto(ag)
	}

	return connect.NewResponse(&taskguildv1.SyncAgentsResponse{
		Agents: protos,
	}), nil
}

func (s *Server) SyncPermissions(ctx context.Context, req *connect.Request[taskguildv1.SyncPermissionsRequest]) (*connect.Response[taskguildv1.SyncPermissionsResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Get stored permissions for the project.
	stored, err := s.permissionRepo.Get(ctx, proj.ID)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Merge local permissions with stored (union strategy).
	merged := permission.Merge(stored, req.Msg.LocalAllow, req.Msg.LocalAsk, req.Msg.LocalDeny)

	// Save merged result.
	if err := s.permissionRepo.Upsert(ctx, merged); err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	return connect.NewResponse(&taskguildv1.SyncPermissionsResponse{
		Permissions: &taskguildv1.PermissionSet{
			ProjectId: proj.ID,
			Allow:     merged.Allow,
			Ask:       merged.Ask,
			Deny:      merged.Deny,
			UpdatedAt: timestamppb.New(merged.UpdatedAt),
		},
	}), nil
}

func agentToProto(a *agent.Agent) *taskguildv1.AgentDefinition {
	return &taskguildv1.AgentDefinition{
		Id:              a.ID,
		ProjectId:       a.ProjectID,
		Name:            a.Name,
		Description:     a.Description,
		Prompt:          a.Prompt,
		Tools:           a.Tools,
		DisallowedTools: a.DisallowedTools,
		Model:           a.Model,
		PermissionMode:  a.PermissionMode,
		Skills:          a.Skills,
		Memory:          a.Memory,
		IsSynced:        a.IsSynced,
		CreatedAt:       timestamppb.New(a.CreatedAt),
		UpdatedAt:       timestamppb.New(a.UpdatedAt),
	}
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

func (s *Server) ReportTaskLog(ctx context.Context, req *connect.Request[taskguildv1.ReportTaskLogRequest]) (*connect.Response[taskguildv1.ReportTaskLogResponse], error) {
	if req.Msg.TaskId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id is required", nil).ConnectError()
	}

	now := time.Now()
	l := &tasklog.TaskLog{
		ID:        ulid.Make().String(),
		TaskID:    req.Msg.TaskId,
		Level:     int32(req.Msg.Level),
		Category:  int32(req.Msg.Category),
		Message:   req.Msg.Message,
		Metadata:  req.Msg.Metadata,
		CreatedAt: now,
	}

	if err := s.taskLogRepo.Create(ctx, l); err != nil {
		return nil, err
	}

	// Resolve project_id from the task for event metadata.
	eventMeta := map[string]string{"task_id": req.Msg.TaskId}
	if t, err := s.taskRepo.Get(ctx, req.Msg.TaskId); err == nil {
		eventMeta["project_id"] = t.ProjectID
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_LOG,
		l.ID,
		"",
		eventMeta,
	)

	return connect.NewResponse(&taskguildv1.ReportTaskLogResponse{}), nil
}

// --- Worktree management RPCs ---

func (s *Server) RequestWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.RequestWorktreeListRequest]) (*connect.Response[taskguildv1.RequestWorktreeListResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send ListWorktreesCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_ListWorktrees{
			ListWorktrees: &taskguildv1.ListWorktreesCommand{
				RequestId: requestID,
			},
		},
	})

	slog.Info("worktree list requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestWorktreeListResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.ReportWorktreeListRequest]) (*connect.Response[taskguildv1.ReportWorktreeListResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the worktree list for this project.
	s.worktreeMu.Lock()
	s.worktreeCache[proj.ID] = req.Msg.Worktrees
	s.worktreeMu.Unlock()

	// Publish event so frontend can pick up the update.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_WORKTREE_LIST,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":  proj.ID,
			"request_id":  req.Msg.RequestId,
		},
	)

	slog.Info("worktree list reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.RequestId,
		"count", len(req.Msg.Worktrees),
	)

	return connect.NewResponse(&taskguildv1.ReportWorktreeListResponse{}), nil
}

func (s *Server) GetWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.GetWorktreeListRequest]) (*connect.Response[taskguildv1.GetWorktreeListResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.worktreeMu.RLock()
	worktrees := s.worktreeCache[req.Msg.ProjectId]
	s.worktreeMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetWorktreeListResponse{
		Worktrees: worktrees,
	}), nil
}

func (s *Server) RequestWorktreeDelete(ctx context.Context, req *connect.Request[taskguildv1.RequestWorktreeDeleteRequest]) (*connect.Response[taskguildv1.RequestWorktreeDeleteResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}
	if req.Msg.WorktreeName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "worktree_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send DeleteWorktreeCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_DeleteWorktree{
			DeleteWorktree: &taskguildv1.DeleteWorktreeCommand{
				RequestId:    requestID,
				WorktreeName: req.Msg.WorktreeName,
				Force:        req.Msg.Force,
			},
		},
	})

	slog.Info("worktree delete requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"worktree_name", req.Msg.WorktreeName,
		"force", req.Msg.Force,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestWorktreeDeleteResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportWorktreeDeleteResult(ctx context.Context, req *connect.Request[taskguildv1.ReportWorktreeDeleteResultRequest]) (*connect.Response[taskguildv1.ReportWorktreeDeleteResultResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// If deletion was successful, remove the worktree from the cache.
	if req.Msg.Success {
		s.worktreeMu.Lock()
		if cached, ok := s.worktreeCache[proj.ID]; ok {
			filtered := make([]*taskguildv1.WorktreeInfo, 0, len(cached))
			for _, wt := range cached {
				if wt.Name != req.Msg.WorktreeName {
					filtered = append(filtered, wt)
				}
			}
			s.worktreeCache[proj.ID] = filtered
		}
		s.worktreeMu.Unlock()
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_WORKTREE_DELETED,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":    proj.ID,
			"request_id":    req.Msg.RequestId,
			"worktree_name": req.Msg.WorktreeName,
			"success":       fmt.Sprintf("%v", req.Msg.Success),
			"error_message": req.Msg.ErrorMessage,
		},
	)

	slog.Info("worktree delete result reported",
		"project_id", proj.ID,
		"worktree_name", req.Msg.WorktreeName,
		"success", req.Msg.Success,
		"error_message", req.Msg.ErrorMessage,
	)

	return connect.NewResponse(&taskguildv1.ReportWorktreeDeleteResultResponse{}), nil
}

// --- Git pull main RPCs ---

func (s *Server) RequestGitPullMain(ctx context.Context, req *connect.Request[taskguildv1.RequestGitPullMainRequest]) (*connect.Response[taskguildv1.RequestGitPullMainResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send GitPullMainCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_GitPullMain{
			GitPullMain: &taskguildv1.GitPullMainCommand{
				RequestId: requestID,
			},
		},
	})

	slog.Info("git pull main requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestGitPullMainResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportGitPullMainResult(ctx context.Context, req *connect.Request[taskguildv1.ReportGitPullMainResultRequest]) (*connect.Response[taskguildv1.ReportGitPullMainResultResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_GIT_PULL_MAIN_RESULT,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":    proj.ID,
			"request_id":    req.Msg.RequestId,
			"success":       fmt.Sprintf("%v", req.Msg.Success),
			"output":        req.Msg.Output,
			"error_message": req.Msg.ErrorMessage,
		},
	)

	slog.Info("git pull main result reported",
		"project_id", proj.ID,
		"success", req.Msg.Success,
		"request_id", req.Msg.RequestId,
	)

	return connect.NewResponse(&taskguildv1.ReportGitPullMainResultResponse{}), nil
}

// --- Script sync & execution RPCs ---

func (s *Server) SyncScripts(ctx context.Context, req *connect.Request[taskguildv1.SyncScriptsRequest]) (*connect.Response[taskguildv1.SyncScriptsResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	scripts, _, err := s.scriptRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = scriptToProto(sc)
	}

	return connect.NewResponse(&taskguildv1.SyncScriptsResponse{
		Scripts: protos,
	}), nil
}

func (s *Server) ReportScriptExecutionResult(ctx context.Context, req *connect.Request[taskguildv1.ReportScriptExecutionResultRequest]) (*connect.Response[taskguildv1.ReportScriptExecutionResultResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Store result for polling.
	if s.scriptResultStore != nil {
		s.scriptResultStore.StoreResult(req.Msg.RequestId, &taskguildv1.GetScriptExecutionResultResponse{
			Completed:    true,
			Success:      req.Msg.Success,
			ExitCode:     req.Msg.ExitCode,
			Stdout:       req.Msg.Stdout,
			Stderr:       req.Msg.Stderr,
			ErrorMessage: req.Msg.ErrorMessage,
		})
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SCRIPT_EXECUTION_RESULT,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":    proj.ID,
			"request_id":    req.Msg.RequestId,
			"script_id":     req.Msg.ScriptId,
			"success":       fmt.Sprintf("%v", req.Msg.Success),
			"exit_code":     fmt.Sprintf("%d", req.Msg.ExitCode),
			"error_message": req.Msg.ErrorMessage,
		},
	)

	slog.Info("script execution result reported",
		"project_id", proj.ID,
		"script_id", req.Msg.ScriptId,
		"success", req.Msg.Success,
		"exit_code", req.Msg.ExitCode,
		"request_id", req.Msg.RequestId,
	)

	return connect.NewResponse(&taskguildv1.ReportScriptExecutionResultResponse{}), nil
}

// RequestScriptExecution sends an ExecuteScriptCommand to connected agent-managers
// for the project and returns a request_id.
func (s *Server) RequestScriptExecution(projectID string, sc *script.Script) (string, error) {
	proj, err := s.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		return "", fmt.Errorf("failed to look up project: %w", err)
	}

	requestID := ulid.Make().String()

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_ExecuteScript{
			ExecuteScript: &taskguildv1.ExecuteScriptCommand{
				RequestId: requestID,
				ScriptId:  sc.ID,
				Filename:  sc.Filename,
				Content:   sc.Content,
			},
		},
	})

	slog.Info("script execution requested",
		"project_id", projectID,
		"project_name", proj.Name,
		"script_id", sc.ID,
		"request_id", requestID,
	)

	return requestID, nil
}

func scriptToProto(s *script.Script) *taskguildv1.ScriptDefinition {
	return &taskguildv1.ScriptDefinition{
		Id:          s.ID,
		ProjectId:   s.ProjectID,
		Name:        s.Name,
		Description: s.Description,
		Filename:    s.Filename,
		Content:     s.Content,
		IsSynced:    s.IsSynced,
		CreatedAt:   timestamppb.New(s.CreatedAt),
		UpdatedAt:   timestamppb.New(s.UpdatedAt),
	}
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
