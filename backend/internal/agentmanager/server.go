package agentmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
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
	"github.com/kazz187/taskguild/backend/internal/version"
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

	// scriptBroker manages streaming script execution output.
	scriptBroker *script.ScriptExecutionBroker

	// worktreeCache stores the latest worktree list per project_id,
	// populated by ReportWorktreeList and read by GetWorktreeList.
	worktreeMu    sync.RWMutex
	worktreeCache map[string][]*taskguildv1.WorktreeInfo // project_id -> worktrees

	// scriptDiffCache stores the latest script comparison per project_id,
	// populated by ReportScriptComparison and read by GetScriptComparison.
	scriptDiffMu    sync.RWMutex
	scriptDiffCache map[string][]*taskguildv1.ScriptDiff // project_id -> diffs
}

func NewServer(registry *Registry, taskRepo task.Repository, workflowRepo workflow.Repository, agentRepo agent.Repository, interactionRepo interaction.Repository, projectRepo project.Repository, skillRepo skill.Repository, scriptRepo script.Repository, taskLogRepo tasklog.Repository, permissionRepo permission.Repository, eventBus *eventbus.Bus, scriptBroker *script.ScriptExecutionBroker) *Server {
	return &Server{
		registry:        registry,
		taskRepo:        taskRepo,
		workflowRepo:    workflowRepo,
		agentRepo:       agentRepo,
		interactionRepo: interactionRepo,
		projectRepo:     projectRepo,
		skillRepo:       skillRepo,
		scriptRepo:      scriptRepo,
		taskLogRepo:     taskLogRepo,
		permissionRepo:  permissionRepo,
		eventBus:        eventBus,
		scriptBroker:    scriptBroker,
		worktreeCache:   make(map[string][]*taskguildv1.WorktreeInfo),
		scriptDiffCache: make(map[string][]*taskguildv1.ScriptDiff),
	}
}

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[taskguildv1.AgentManagerSubscribeRequest], stream *connect.ServerStream[taskguildv1.AgentCommand]) error {
	agentManagerID := req.Msg.AgentManagerId
	if agentManagerID == "" {
		return cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}

	projectName := req.Msg.ProjectName
	activeTaskIDs := req.Msg.ActiveTaskIds
	agentVersion := req.Msg.AgentVersion
	serverVersion := version.Short()

	slog.Info("agent-manager connected",
		"agent_manager_id", agentManagerID,
		"agent_version", agentVersion,
		"server_version", serverVersion,
		"max_concurrent_tasks", req.Msg.MaxConcurrentTasks,
		"project_name", projectName,
		"active_tasks", len(activeTaskIDs),
	)

	if agentVersion != "" && agentVersion != serverVersion {
		slog.Warn("agent version mismatch: agent may need rebuild",
			"agent_manager_id", agentManagerID,
			"agent_version", agentVersion,
			"server_version", serverVersion,
		)
	}

	// On (re-)connect, release tasks that are no longer active on this agent.
	// If the agent sends active_task_ids, only tasks NOT in that list are released.
	// This prevents disrupting tasks that are still running locally after a
	// transient stream disconnection.
	s.releaseAgentTasksExcept(ctx, agentManagerID, activeTaskIDs)

	commandCh := s.registry.Register(agentManagerID, req.Msg.MaxConcurrentTasks, projectName)
	defer func() {
		s.registry.Unregister(agentManagerID)
		// On disconnect, release assigned tasks so other agents can pick them up.
		// We release ALL tasks here (no exceptions) because the agent is actually
		// disconnecting. If it reconnects, it will send active_task_ids to reclaim.
		s.releaseAgentTasks(context.Background(), agentManagerID)
		slog.Info("agent-manager disconnected", "agent_manager_id", agentManagerID)
	}()

	// Send existing PENDING tasks to this agent so it can pick them up
	// immediately. This covers tasks that were pending before this agent
	// connected and tasks released during reconnection whose broadcast
	// was sent before the agent was registered.
	s.sendPendingTasksToStream(ctx, projectName, stream)

	// Server-side keepalive: send a PingCommand every 30 seconds to keep the
	// HTTP/2 stream active and detect dead connections faster. This prevents
	// intermediaries (proxies, load balancers) and OS-level TCP timeouts from
	// silently closing the stream.
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	pingCmd := &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_Ping{
			Ping: &taskguildv1.PingCommand{},
		},
	}

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
		case <-pingTicker.C:
			if err := stream.Send(pingCmd); err != nil {
				return err
			}
		}
	}
}

// releaseAgentTasksExcept releases tasks assigned to the agent EXCEPT those
// in the keepTaskIDs set. This is used during reconnection to avoid disrupting
// tasks that are still actively running on the agent.
func (s *Server) releaseAgentTasksExcept(ctx context.Context, agentManagerID string, keepTaskIDs []string) {
	if len(keepTaskIDs) == 0 {
		// No active tasks — release everything (original behavior).
		s.releaseAgentTasks(ctx, agentManagerID)
		return
	}

	keepSet := make(map[string]struct{}, len(keepTaskIDs))
	for _, id := range keepTaskIDs {
		keepSet[id] = struct{}{}
	}

	released, err := s.taskRepo.ReleaseByAgentExcept(ctx, agentManagerID, keepSet)
	if err != nil {
		slog.Error("failed to release tasks for agent (except active)",
			"agent_manager_id", agentManagerID, "error", err)
		return
	}

	slog.Info("reconnection: released orphaned tasks, kept active tasks",
		"agent_manager_id", agentManagerID,
		"released", len(released),
		"kept", len(keepTaskIDs),
	)

	for _, t := range released {
		s.handleReleasedTask(ctx, agentManagerID, t)
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
		s.handleReleasedTask(ctx, agentManagerID, t)
	}
}

// handleReleasedTask handles a single released task: expires orphaned interactions,
// re-broadcasts the task, and publishes events.
func (s *Server) handleReleasedTask(ctx context.Context, agentManagerID string, t *task.Task) {
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
		return
	}
	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)

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

// sendPendingTasksToStream scans for PENDING tasks in the given project and
// sends TaskAvailableCommand for each directly on the agent's stream. This
// ensures that tasks pending before an agent connects (or tasks released
// during reconnection before the agent was registered) are picked up.
func (s *Server) sendPendingTasksToStream(ctx context.Context, projectName string, stream *connect.ServerStream[taskguildv1.AgentCommand]) {
	if projectName == "" {
		return
	}

	p, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		slog.Error("sendPendingTasks: failed to find project", "project_name", projectName, "error", err)
		return
	}

	tasks, _, err := s.taskRepo.List(ctx, p.ID, "", "", 0, 0)
	if err != nil {
		slog.Error("sendPendingTasks: failed to list tasks", "project_id", p.ID, "error", err)
		return
	}

	// Cache workflows to avoid repeated lookups.
	wfCache := make(map[string]*workflow.Workflow)
	sentCount := 0

	for _, t := range tasks {
		if t.AssignmentStatus != task.AssignmentStatusPending {
			continue
		}

		wf, ok := wfCache[t.WorkflowID]
		if !ok {
			wf, err = s.workflowRepo.Get(ctx, t.WorkflowID)
			if err != nil {
				slog.Error("sendPendingTasks: failed to get workflow",
					"workflow_id", t.WorkflowID, "error", err)
				continue
			}
			wfCache[t.WorkflowID] = wf
		}

		agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
		if agentConfigID == "" {
			continue // no agent configured for this status
		}

		cmd := &taskguildv1.AgentCommand{
			Command: &taskguildv1.AgentCommand_TaskAvailable{
				TaskAvailable: &taskguildv1.TaskAvailableCommand{
					TaskId:        t.ID,
					AgentConfigId: agentConfigID,
					Title:         t.Title,
					Metadata:      t.Metadata,
				},
			},
		}
		if err := stream.Send(cmd); err != nil {
			slog.Error("sendPendingTasks: failed to send command",
				"task_id", t.ID, "error", err)
			return // stream broken, abort
		}
		sentCount++
	}

	if sentCount > 0 {
		slog.Info("sent existing pending tasks to agent",
			"count", sentCount, "project_name", projectName)
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

// Retry constants for failed task auto-retry.
const (
	retryMetadataKey = "_retry_count"
	maxRetries       = 5
	retryBaseDelay   = 30 * time.Second
)

func (s *Server) ReportTaskResult(ctx context.Context, req *connect.Request[taskguildv1.ReportTaskResultRequest]) (*connect.Response[taskguildv1.ReportTaskResultResponse], error) {
	t, err := s.taskRepo.Get(ctx, req.Msg.TaskId)
	if err != nil {
		return nil, err
	}

	// Clear assigned agent.
	t.AssignedAgentID = ""
	t.UpdatedAt = time.Now()

	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}

	// Store result summary/error in metadata.
	if req.Msg.Summary != "" {
		t.Metadata["result_summary"] = req.Msg.Summary
	}
	if req.Msg.ErrorMessage != "" {
		t.Metadata["result_error"] = req.Msg.ErrorMessage
	}

	eventMeta := map[string]string{
		"project_id":  t.ProjectID,
		"workflow_id": t.WorkflowID,
	}

	if req.Msg.ErrorMessage != "" {
		// Task failed — check if we should retry.
		retryCount := 0
		if rc, ok := t.Metadata[retryMetadataKey]; ok {
			retryCount, _ = strconv.Atoi(rc)
		}

		if retryCount < maxRetries {
			retryCount++
			t.Metadata[retryMetadataKey] = strconv.Itoa(retryCount)
			t.AssignmentStatus = task.AssignmentStatusPending

			if err := s.taskRepo.Update(ctx, t); err != nil {
				return nil, err
			}

			// Calculate exponential backoff: 30s, 1m, 2m, 4m, 8m
			delay := retryBaseDelay * time.Duration(1<<uint(retryCount-1))

			slog.Info("scheduling task retry",
				"task_id", t.ID,
				"retry_count", retryCount,
				"max_retries", maxRetries,
				"delay", delay,
			)

			// Schedule delayed re-broadcast in a goroutine.
			go s.delayedRebroadcast(t.ID, t.ProjectID, t.WorkflowID, delay)

			eventMeta["reason"] = "retry_scheduled"
			eventMeta["retry_count"] = strconv.Itoa(retryCount)
			s.eventBus.PublishNew(
				taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
				t.ID, "", eventMeta,
			)

			return connect.NewResponse(&taskguildv1.ReportTaskResultResponse{}), nil
		}

		// Max retries reached — leave as UNASSIGNED.
		slog.Warn("max retries reached for task",
			"task_id", t.ID,
			"retry_count", retryCount,
		)
		t.AssignmentStatus = task.AssignmentStatusUnassigned
	} else {
		// Task succeeded — reset retry count and set UNASSIGNED.
		delete(t.Metadata, retryMetadataKey)
		t.AssignmentStatus = task.AssignmentStatusUnassigned
	}

	if err := s.taskRepo.Update(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
		t.ID, "", eventMeta,
	)

	return connect.NewResponse(&taskguildv1.ReportTaskResultResponse{}), nil
}

// delayedRebroadcast waits for the specified delay, then re-checks the task
// state. If the task is still PENDING, it broadcasts a TaskAvailableCommand
// so agents can pick it up for retry. The re-check guards against manual
// user intervention during the delay window.
func (s *Server) delayedRebroadcast(taskID, projectID, workflowID string, delay time.Duration) {
	time.Sleep(delay)

	ctx := context.Background()

	// Re-read task to check current state.
	t, err := s.taskRepo.Get(ctx, taskID)
	if err != nil {
		slog.Error("retry rebroadcast: failed to get task",
			"task_id", taskID, "error", err)
		return
	}

	// Only broadcast if still PENDING (user might have manually changed it).
	if t.AssignmentStatus != task.AssignmentStatusPending {
		slog.Info("retry rebroadcast: task no longer pending, skipping",
			"task_id", taskID,
			"assignment_status", string(t.AssignmentStatus),
		)
		return
	}

	// Look up workflow to find agent config.
	wf, err := s.workflowRepo.Get(ctx, workflowID)
	if err != nil {
		slog.Error("retry rebroadcast: failed to get workflow",
			"workflow_id", workflowID, "error", err)
		return
	}

	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
	if agentConfigID == "" {
		slog.Info("retry rebroadcast: no agent config for status, skipping",
			"task_id", taskID, "status_id", t.StatusID)
		return
	}

	// Resolve project name for filtered broadcast.
	var projectName string
	if p, pErr := s.projectRepo.Get(ctx, projectID); pErr == nil {
		projectName = p.Name
	}

	cmd := &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_TaskAvailable{
			TaskAvailable: &taskguildv1.TaskAvailableCommand{
				TaskId:        t.ID,
				AgentConfigId: agentConfigID,
				Title:         t.Title,
				Metadata:      t.Metadata,
			},
		},
	}
	s.registry.BroadcastCommandToProject(projectName, cmd)

	slog.Info("retry rebroadcast: task available broadcast sent",
		"task_id", taskID,
		"retry_count", t.Metadata[retryMetadataKey],
		"agent_config_id", agentConfigID,
		"project_name", projectName,
	)
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

	// Generate a single-use response token for push notification actions.
	// This allows the Service Worker to respond to interactions without
	// exposing the main API key.
	interType := interaction.InteractionType(req.Msg.Type)
	if interType == interaction.TypePermissionRequest || interType == interaction.TypeQuestion {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err == nil {
			inter.ResponseToken = hex.EncodeToString(tokenBytes)
		}
	}

	if err := s.interactionRepo.Create(ctx, inter); err != nil {
		return nil, err
	}

	interProto := interaction.ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		interaction.MarshalInteractionPayload(interProto),
		map[string]string{"task_id": inter.TaskID, "agent_id": inter.AgentID},
	)

	return connect.NewResponse(&taskguildv1.CreateInteractionResponse{
		Interaction: interProto,
	}), nil
}

func (s *Server) GetInteractionResponse(ctx context.Context, req *connect.Request[taskguildv1.GetInteractionResponseRequest]) (*connect.Response[taskguildv1.GetInteractionResponseResponse], error) {
	inter, err := s.interactionRepo.Get(ctx, req.Msg.InteractionId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetInteractionResponseResponse{
		Interaction: interaction.ToProto(inter),
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

	// Complete execution in the broker — this sends the completion event
	// to all streaming subscribers and closes their channels.
	if s.scriptBroker != nil {
		s.scriptBroker.CompleteExecution(
			req.Msg.RequestId,
			req.Msg.Success,
			req.Msg.ExitCode,
			req.Msg.Stdout,
			req.Msg.Stderr,
			req.Msg.ErrorMessage,
		)
	}

	// Publish event so other consumers (e.g. notifications) can react.
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

func (s *Server) ReportScriptOutputChunk(ctx context.Context, req *connect.Request[taskguildv1.ReportScriptOutputChunkRequest]) (*connect.Response[taskguildv1.ReportScriptOutputChunkResponse], error) {
	if req.Msg.ProjectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	if s.scriptBroker != nil {
		s.scriptBroker.PushOutput(req.Msg.RequestId, req.Msg.StdoutChunk, req.Msg.StderrChunk)
	}

	return connect.NewResponse(&taskguildv1.ReportScriptOutputChunkResponse{}), nil
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

// --- Script comparison & conflict resolution RPCs ---

// RequestScriptComparison sends a CompareScriptsCommand to connected agent-managers
// so they compare local scripts with server versions.
func (s *Server) RequestScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.RequestScriptComparisonRequest]) (*connect.Response[taskguildv1.RequestScriptComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Fetch all scripts for this project so the agent can compare.
	scripts, _, err := s.scriptRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = scriptToProto(sc)
	}

	requestID := ulid.Make().String()

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_CompareScripts{
			CompareScripts: &taskguildv1.CompareScriptsCommand{
				RequestId: requestID,
				Scripts:   protos,
			},
		},
	})

	slog.Info("script comparison requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
		"script_count", len(scripts),
	)

	return connect.NewResponse(&taskguildv1.RequestScriptComparisonResponse{
		RequestId: requestID,
	}), nil
}

// ReportScriptComparison receives comparison results from the agent and caches them.
func (s *Server) ReportScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.ReportScriptComparisonRequest]) (*connect.Response[taskguildv1.ReportScriptComparisonResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the diffs for this project.
	s.scriptDiffMu.Lock()
	s.scriptDiffCache[proj.ID] = req.Msg.Diffs
	s.scriptDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SCRIPT_COMPARISON,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id": proj.ID,
			"request_id": req.Msg.RequestId,
			"diff_count": fmt.Sprintf("%d", len(req.Msg.Diffs)),
		},
	)

	slog.Info("script comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.RequestId,
		"diff_count", len(req.Msg.Diffs),
	)

	return connect.NewResponse(&taskguildv1.ReportScriptComparisonResponse{}), nil
}

// GetScriptComparison returns the cached script diffs for a project.
func (s *Server) GetScriptComparison(ctx context.Context, req *connect.Request[taskguildv1.GetScriptComparisonRequest]) (*connect.Response[taskguildv1.GetScriptComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.scriptDiffMu.RLock()
	diffs := s.scriptDiffCache[req.Msg.ProjectId]
	s.scriptDiffMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetScriptComparisonResponse{
		Diffs: diffs,
	}), nil
}

// ResolveScriptConflict resolves a single script conflict between server and agent versions.
func (s *Server) ResolveScriptConflict(ctx context.Context, req *connect.Request[taskguildv1.ResolveScriptConflictRequest]) (*connect.Response[taskguildv1.ResolveScriptConflictResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var resultScript *script.Script

	switch req.Msg.Choice {
	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncScriptsCommand.
		if req.Msg.ScriptId != "" {
			resultScript, err = s.scriptRepo.Get(ctx, req.Msg.ScriptId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncScripts{
					SyncScripts: &taskguildv1.SyncScriptsCommand{
						ForceOverwriteScriptIds: []string{req.Msg.ScriptId},
					},
				},
			})
		}

	case taskguildv1.ScriptResolutionChoice_SCRIPT_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		if req.Msg.ScriptId != "" {
			// Update existing script.
			resultScript, err = s.scriptRepo.Get(ctx, req.Msg.ScriptId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
			resultScript.Content = req.Msg.AgentContent
			if req.Msg.Filename != "" {
				resultScript.Filename = req.Msg.Filename
			}
			resultScript.IsSynced = true
			resultScript.UpdatedAt = time.Now()
			if err := s.scriptRepo.Update(ctx, resultScript); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		} else {
			// Agent-only script — create new in DB.
			now := time.Now()
			filename := req.Msg.Filename
			if filename == "" {
				filename = req.Msg.ScriptName + ".sh"
			}
			resultScript = &script.Script{
				ID:        ulid.Make().String(),
				ProjectID: req.Msg.ProjectId,
				Name:      req.Msg.ScriptName,
				Filename:  filename,
				Content:   req.Msg.AgentContent,
				IsSynced:  true,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := s.scriptRepo.Create(ctx, resultScript); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil).ConnectError()
	}

	// Remove the resolved diff from cache.
	s.removeScriptDiff(req.Msg.ProjectId, req.Msg.ScriptId, req.Msg.Filename)

	var proto *taskguildv1.ScriptDefinition
	if resultScript != nil {
		proto = scriptToProto(resultScript)
	}

	slog.Info("script conflict resolved",
		"project_id", req.Msg.ProjectId,
		"script_id", req.Msg.ScriptId,
		"script_name", req.Msg.ScriptName,
		"choice", req.Msg.Choice.String(),
	)

	return connect.NewResponse(&taskguildv1.ResolveScriptConflictResponse{
		Script: proto,
	}), nil
}

// removeScriptDiff removes a specific diff entry from the cache.
// It matches by script_id if non-empty, otherwise by filename.
func (s *Server) removeScriptDiff(projectID, scriptID, filename string) {
	s.scriptDiffMu.Lock()
	defer s.scriptDiffMu.Unlock()

	diffs := s.scriptDiffCache[projectID]
	if len(diffs) == 0 {
		return
	}

	filtered := make([]*taskguildv1.ScriptDiff, 0, len(diffs))
	for _, d := range diffs {
		if scriptID != "" && d.ScriptId == scriptID {
			continue // remove this diff
		}
		if scriptID == "" && filename != "" && d.Filename == filename {
			continue // remove agent-only diff by filename
		}
		filtered = append(filtered, d)
	}
	s.scriptDiffCache[projectID] = filtered
}

