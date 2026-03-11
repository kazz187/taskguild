package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/version"
	"github.com/kazz187/taskguild/internal/workflow"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

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
		// If stopped by user, skip retry and go straight to UNASSIGNED.
		if t.Metadata["_stopped_by_user"] == "true" {
			slog.Info("task stopped by user, skipping retry",
				"task_id", t.ID,
			)
			delete(t.Metadata, "_stopped_by_user")
			delete(t.Metadata, retryMetadataKey)
			t.AssignmentStatus = task.AssignmentStatusUnassigned

			if err := s.taskRepo.Update(ctx, t); err != nil {
				return nil, err
			}

			eventMeta["reason"] = "stopped_by_user"
			s.eventBus.PublishNew(
				taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
				t.ID, "", eventMeta,
			)

			return connect.NewResponse(&taskguildv1.ReportTaskResultResponse{}), nil
		}

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
	var agentName string

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
		// The agent's prompt is provided by the .claude/agents/<name>.md file
		// via the --agent CLI flag, so we don't include ag.Prompt in instructions.
		ag, err := s.agentRepo.Get(ctx, currentAgentID)
		if err == nil {
			agentConfigID = ag.ID
			agentName = ag.Name
		}
	}

	// Fall back to legacy AgentConfig list.
	if agentName == "" {
		for _, cfg := range wf.AgentConfigs {
			if cfg.WorkflowStatusID == t.StatusID {
				instructions = cfg.Instructions
				agentConfigID = cfg.ID
				break
			}
		}
	}

	// Prepend workflow custom prompt to agent instructions.
	// For named agents, CustomPrompt is passed as append-system-prompt via metadata.
	if agentName != "" {
		// Named agent: pass CustomPrompt separately (not merged into instructions).
		instructions = wf.CustomPrompt
	} else if wf.CustomPrompt != "" && instructions != "" {
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
	// Resolve permission mode from workflow status, falling back to workflow default.
	for _, st := range wf.Statuses {
		if st.ID == t.StatusID && st.PermissionMode != "" {
			enrichedMetadata["_permission_mode"] = st.PermissionMode
			break
		}
	}
	if _, ok := enrichedMetadata["_permission_mode"]; !ok && wf.DefaultPermissionMode != "" {
		enrichedMetadata["_permission_mode"] = wf.DefaultPermissionMode
	}
	if agentName != "" {
		enrichedMetadata["_agent_name"] = agentName
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

	// Inject AGENT.md harness flag for the current status.
	// Default is enabled (true) unless explicitly disabled.
	for _, st := range wf.Statuses {
		if st.ID == t.StatusID {
			harnessEnabled := !st.AgentMDHarnessExplicitlyDisabled
			if st.AgentMDHarnessExplicitlyDisabled {
				harnessEnabled = st.EnableAgentMDHarness
			}
			if harnessEnabled {
				enrichedMetadata["_enable_agent_md_harness"] = "true"
			} else {
				enrichedMetadata["_enable_agent_md_harness"] = "false"
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

// RequestTaskStop sends a CancelTaskCommand to the agent running the given task.
func (s *Server) RequestTaskStop(taskID string, assignedAgentID string) error {
	sent := s.registry.SendCommand(assignedAgentID, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_CancelTask{
			CancelTask: &taskguildv1.CancelTaskCommand{
				TaskId: taskID,
				Reason: "stopped by user",
			},
		},
	})
	if !sent {
		return fmt.Errorf("agent %s not connected", assignedAgentID)
	}
	slog.Info("task stop command sent",
		"task_id", taskID,
		"agent_id", assignedAgentID,
	)
	return nil
}

// RequestTaskResume re-triggers orchestration for a stopped task by setting it
// to PENDING and broadcasting a TaskAvailableCommand.
func (s *Server) RequestTaskResume(ctx context.Context, t *task.Task) error {
	t.AssignmentStatus = task.AssignmentStatusPending
	t.UpdatedAt = time.Now()
	if err := s.taskRepo.Update(ctx, t); err != nil {
		return err
	}

	wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		return err
	}
	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
	if agentConfigID == "" {
		return fmt.Errorf("no agent configured for status %s", t.StatusID)
	}

	var projectName string
	if p, pErr := s.projectRepo.Get(ctx, t.ProjectID); pErr == nil {
		projectName = p.Name
	}

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

	slog.Info("task resume broadcast sent",
		"task_id", t.ID,
		"agent_config_id", agentConfigID,
		"project_name", projectName,
	)
	return nil
}
