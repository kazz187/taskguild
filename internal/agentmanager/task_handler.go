package agentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/tasklog"
	"github.com/kazz187/taskguild/internal/version"
	"github.com/kazz187/taskguild/internal/workflow"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) Subscribe(ctx context.Context, req *connect.Request[taskguildv1.AgentManagerSubscribeRequest], stream *connect.ServerStream[taskguildv1.AgentCommand]) error {
	agentManagerID := req.Msg.GetAgentManagerId()
	if agentManagerID == "" {
		return cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}

	projectName := req.Msg.GetProjectName()
	activeTaskIDs := req.Msg.GetActiveTaskIds()
	agentVersion := req.Msg.GetAgentVersion()
	serverVersion := version.Short()

	slog.Info("agent-manager connected",
		"agent_manager_id", agentManagerID,
		"agent_version", agentVersion,
		"server_version", serverVersion,
		"max_concurrent_tasks", req.Msg.GetMaxConcurrentTasks(),
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

	commandCh := s.registry.Register(agentManagerID, req.Msg.GetMaxConcurrentTasks(), projectName, req.Msg.GetWorkDir())

	defer func() {
		wasActive := s.registry.UnregisterIfMatch(agentManagerID, commandCh)
		if wasActive {
			// This handler was the active one — the agent truly disconnected.
			// Release all tasks so other agents can pick them up.
			s.releaseAgentTasks(context.Background(), agentManagerID)
			slog.Info("agent-manager disconnected", "agent_manager_id", agentManagerID)
		} else {
			// A newer Subscribe handler has replaced us via Register. Do NOT
			// release tasks — the new handler already called releaseAgentTasksExcept
			// on connect and is now the authoritative connection for this agent.
			slog.Info("agent-manager handler superseded, skipping task release",
				"agent_manager_id", agentManagerID)
		}
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

		// agentConfigID may be empty when no agent is configured for the
		// status (e.g. comment-triggered launch). ClaimTask handles this
		// gracefully by falling back to a plain agent.
		agentConfigID := wf.FindAgentIDForStatus(t.StatusID)

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
	if req.Msg.GetAgentManagerId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "agent_manager_id is required", nil).ConnectError()
	}

	if !s.registry.UpdateHeartbeat(req.Msg.GetAgentManagerId(), req.Msg.GetActiveTasks()) {
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
	t, err := s.taskRepo.Get(ctx, req.Msg.GetTaskId())
	if err != nil {
		return nil, err
	}

	// If the task is already unassigned (e.g. stopped by user via StopTask),
	// just emit the result log without triggering retry logic.
	if t.AssignmentStatus == task.AssignmentStatusUnassigned && t.AssignedAgentID == "" {
		if t.Metadata == nil {
			t.Metadata = make(map[string]string)
		}

		delete(t.Metadata, "_stopped_by_user")

		t.UpdatedAt = time.Now()
		if err := s.taskRepo.Update(ctx, t); err != nil {
			return nil, err
		}

		slog.Info("task already unassigned, updated metadata only", "task_id", t.ID)
		s.emitResultLog(ctx, t, req.Msg.GetSummary(), req.Msg.GetErrorMessage())

		return connect.NewResponse(&taskguildv1.ReportTaskResultResponse{}), nil
	}

	// Clear assigned agent.
	t.AssignedAgentID = ""
	t.UpdatedAt = time.Now()

	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}

	// Emit a chronological RESULT log entry (append-only).
	// Result data is no longer stored in metadata to avoid overwrites.
	s.emitResultLog(ctx, t, req.Msg.GetSummary(), req.Msg.GetErrorMessage())

	eventMeta := map[string]string{
		"project_id":  t.ProjectID,
		"workflow_id": t.WorkflowID,
	}

	if req.Msg.GetErrorMessage() != "" {
		// If stopped by user, skip retry and go straight to UNASSIGNED.
		if t.Metadata["_stopped_by_user"] == "true" {
			slog.Info("task stopped by user, skipping retry",
				"task_id", t.ID,
			)
			delete(t.Metadata, "_stopped_by_user")
			delete(t.Metadata, retryMetadataKey)
			task.ClearPendingReason(t.Metadata)
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

			// Calculate exponential backoff: 30s, 1m, 2m, 4m, 8m
			delay := retryBaseDelay * time.Duration(1<<uint(retryCount-1))

			task.ClearPendingReason(t.Metadata)
			t.Metadata[task.MetaPendingReason] = task.PendingReasonRetryBackoff
			t.Metadata[task.MetaPendingRetryAfter] = time.Now().Add(delay).Format(time.RFC3339)

			if err := s.taskRepo.Update(ctx, t); err != nil {
				return nil, err
			}

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
		task.ClearPendingReason(t.Metadata)
	} else {
		// Task succeeded — reset retry count and set UNASSIGNED.
		delete(t.Metadata, retryMetadataKey)
		t.AssignmentStatus = task.AssignmentStatusUnassigned
		task.ClearPendingReason(t.Metadata)
	}

	if err := s.taskRepo.Update(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
		t.ID, "", eventMeta,
	)

	// If this task had a worktree, re-broadcast any PENDING tasks waiting
	// for the same worktree so they can be claimed now.
	if worktreeName := t.Metadata["worktree"]; worktreeName != "" {
		s.rebroadcastWorktreeWaiters(ctx, t.ProjectID, worktreeName, t.ID)
	}

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

	skillIDs := wf.FindSkillIDsForStatus(t.StatusID)
	if agentConfigID == "" && len(skillIDs) == 0 {
		slog.Info("retry rebroadcast: no executor (skill or agent) for status, skipping",
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

// emitResultLog creates a chronological RESULT TaskLog entry so that results
// are preserved across status transitions instead of being overwritten.
func (s *Server) emitResultLog(ctx context.Context, t *task.Task, summary, errMsg string) {
	if summary == "" && errMsg == "" {
		return
	}

	now := time.Now()
	level := int32(taskguildv1.TaskLogLevel_TASK_LOG_LEVEL_INFO)
	resultType := "summary"
	text := summary

	if errMsg != "" {
		level = int32(taskguildv1.TaskLogLevel_TASK_LOG_LEVEL_ERROR)
		resultType = "error"
		text = errMsg
	}

	preview := text
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}

	meta := map[string]string{
		"full_text":   text,
		"result_type": resultType,
		"status_id":   t.StatusID,
	}

	l := &tasklog.TaskLog{
		ID:        ulid.Make().String(),
		ProjectID: t.ProjectID,
		TaskID:    t.ID,
		Level:     level,
		Category:  int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_RESULT),
		Message:   preview,
		Metadata:  meta,
		CreatedAt: now,
	}

	if err := s.taskLogRepo.Create(ctx, l); err != nil {
		slog.Error("failed to create result log", "task_id", t.ID, "error", err)
		return
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_LOG,
		l.ID,
		"",
		map[string]string{"task_id": t.ID, "project_id": t.ProjectID},
	)
}

func (s *Server) ClaimTask(ctx context.Context, req *connect.Request[taskguildv1.ClaimTaskRequest]) (*connect.Response[taskguildv1.ClaimTaskResponse], error) {
	if req.Msg.GetTaskId() == "" || req.Msg.GetAgentManagerId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_id and agent_manager_id are required", nil).ConnectError()
	}

	// Pre-read the task to check worktree occupancy before claiming.
	taskForCheck, err := s.taskRepo.Get(ctx, req.Msg.GetTaskId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var t *task.Task

	// Worktree concurrency control: if the task has a named worktree,
	// hold a per-project+worktree mutex to atomically check occupancy and claim.
	if worktreeName := taskForCheck.Metadata["worktree"]; worktreeName != "" {
		muKey := taskForCheck.ProjectID + "\x00" + worktreeName
		muVal, _ := s.worktreeClaimMu.LoadOrStore(muKey, &sync.Mutex{})
		mu := muVal.(*sync.Mutex)
		mu.Lock()
		if occupied, occupantID, occupantTitle := s.isWorktreeOccupied(ctx, taskForCheck.ProjectID, worktreeName, taskForCheck.ID); occupied {
			// Update task metadata with pending reason for UI display.
			if taskForCheck.Metadata == nil {
				taskForCheck.Metadata = make(map[string]string)
			}

			task.ClearPendingReason(taskForCheck.Metadata)
			taskForCheck.Metadata[task.MetaPendingReason] = task.PendingReasonWorktreeOccupied
			taskForCheck.Metadata[task.MetaPendingBlockerTaskID] = occupantID
			taskForCheck.Metadata[task.MetaPendingBlockerTaskTitle] = occupantTitle
			taskForCheck.UpdatedAt = time.Now()
			_ = s.taskRepo.Update(ctx, taskForCheck)
			mu.Unlock()
			slog.Info("worktree occupied, rejecting claim",
				"task_id", taskForCheck.ID,
				"worktree", worktreeName,
				"occupant_task_id", occupantID,
			)

			return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
				Success: false,
			}), nil
		}

		t, err = s.taskRepo.Claim(ctx, req.Msg.GetTaskId(), req.Msg.GetAgentManagerId())
		mu.Unlock()
	} else {
		t, err = s.taskRepo.Claim(ctx, req.Msg.GetTaskId(), req.Msg.GetAgentManagerId())
	}

	if err != nil {
		if cerr.IsCode(err, cerr.FailedPrecondition) {
			return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
				Success: false,
			}), nil
		}

		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Validate project name: if the agent declared a project, verify it matches.
	if agentProject, ok := s.registry.GetProjectName(req.Msg.GetAgentManagerId()); ok && agentProject != "" {
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

	var (
		instructions  string
		agentConfigID string
		agentName     string
		skillNames    []string
	)

	// Resolve the current status to find execution configuration.
	var currentStatus *workflow.Status

	for i, st := range wf.Statuses {
		if st.Name == t.StatusID {
			currentStatus = &wf.Statuses[i]
			break
		}
	}

	// Priority 1: Skill-based execution (skill_ids on status).
	if currentStatus != nil && len(currentStatus.SkillIDs) > 0 {
		for _, sid := range currentStatus.SkillIDs {
			if s.skillRepo != nil {
				if sk, err := s.skillRepo.Get(ctx, sid); err == nil {
					skillNames = append(skillNames, sk.Name)
				} else {
					slog.Warn("failed to resolve skill for status", "skill_id", sid, "status", t.StatusID, "error", err)
				}
			}
		}
	}

	// Priority 2: Agent-based execution (agent_id on status) — fallback.
	if len(skillNames) == 0 {
		var currentAgentID string
		if currentStatus != nil && currentStatus.AgentID != "" {
			currentAgentID = currentStatus.AgentID
		}

		if currentAgentID != "" {
			ag, err := s.agentRepo.Get(ctx, currentAgentID)
			if err == nil {
				agentConfigID = ag.ID
				agentName = ag.Name
			}
		}

		// Priority 3: Legacy AgentConfig list.
		if agentName == "" {
			for _, cfg := range wf.AgentConfigs {
				if cfg.WorkflowStatusID == t.StatusID {
					instructions = cfg.Instructions
					agentConfigID = cfg.ID

					break
				}
			}
		}
	}

	// Prepend workflow custom prompt to agent instructions.
	if len(skillNames) > 0 {
		// Skill-based: CustomPrompt goes into instructions directly.
		instructions = wf.CustomPrompt
	} else if agentName != "" {
		// Named agent: pass CustomPrompt separately (not merged into instructions).
		instructions = wf.CustomPrompt
	} else if wf.CustomPrompt != "" && instructions != "" {
		instructions = wf.CustomPrompt + "\n\n" + instructions
	} else if wf.CustomPrompt != "" {
		instructions = wf.CustomPrompt
	}

	// Build enriched metadata with task info and available transitions.
	enrichedMetadata := make(map[string]string)
	maps.Copy(enrichedMetadata, t.Metadata)
	enrichedMetadata["_task_title"] = t.Title
	enrichedMetadata["_task_description"] = t.Description
	enrichedMetadata["_project_id"] = t.ProjectID

	enrichedMetadata["_workflow_id"] = t.WorkflowID
	if t.UseWorktree {
		enrichedMetadata["_use_worktree"] = "true"
	}
	// Resolve permission mode from workflow status, falling back to workflow default.
	for _, st := range wf.Statuses {
		if st.Name == t.StatusID && st.PermissionMode != "" {
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

	// Inject skill-based execution metadata.
	if len(skillNames) > 0 {
		enrichedMetadata["_skill_names"] = strings.Join(skillNames, ",")
	}

	if currentStatus != nil {
		if currentStatus.Model != "" {
			enrichedMetadata["_model"] = currentStatus.Model
		}

		if len(currentStatus.Tools) > 0 {
			if b, err := json.Marshal(currentStatus.Tools); err == nil {
				enrichedMetadata["_tools"] = string(b)
			}
		}

		if len(currentStatus.DisallowedTools) > 0 {
			if b, err := json.Marshal(currentStatus.DisallowedTools); err == nil {
				enrichedMetadata["_disallowed_tools"] = string(b)
			}
		}
	}
	// Resolve effort: task override wins over WorkflowStatus.
	if effort := resolveEffort(t, currentStatus); effort != "" {
		enrichedMetadata["_effort"] = effort
	}

	// Resolve current status name and available transitions from workflow.
	for _, st := range wf.Statuses {
		if st.Name == t.StatusID {
			enrichedMetadata["_current_status_name"] = st.Name
			if st.InheritSessionFrom != "" {
				enrichedMetadata["_inherit_session_from"] = st.InheritSessionFrom
			}

			type transitionEntry struct {
				Name string `json:"name"`
			}

			var transitions []transitionEntry
			for _, targetName := range st.TransitionsTo {
				transitions = append(transitions, transitionEntry{
					Name: targetName,
				})
			}

			if len(transitions) > 0 {
				if b, err := json.Marshal(transitions); err == nil {
					enrichedMetadata["_available_transitions"] = string(b)
				}
			}

			break
		}
	}

	// Inject all workflow statuses so agents can create tasks with any status
	// and so the agent runner can look up inherit_session_from for transitions.
	{
		type statusInfo struct {
			Name               string `json:"name"`
			InheritSessionFrom string `json:"inherit_session_from,omitempty"`
		}

		var allStatuses []statusInfo
		for _, st := range wf.Statuses {
			allStatuses = append(allStatuses, statusInfo{
				Name:               st.Name,
				InheritSessionFrom: st.InheritSessionFrom,
			})
		}

		if b, err := json.Marshal(allStatuses); err == nil {
			enrichedMetadata["_workflow_statuses"] = string(b)
		}
	}

	// Resolve hooks for the current status and inject into metadata.
	for _, st := range wf.Statuses {
		if st.Name == t.StatusID && len(st.Hooks) > 0 {
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

	// Inject harness flags for the current status.
	if currentStatus != nil {
		// Skill harness: default enabled unless explicitly disabled.
		skillHarnessEnabled := !currentStatus.SkillHarnessExplicitlyDisabled
		if currentStatus.SkillHarnessExplicitlyDisabled {
			skillHarnessEnabled = currentStatus.EnableSkillHarness
		}

		if skillHarnessEnabled {
			enrichedMetadata["_enable_skill_harness"] = "true"
		} else {
			enrichedMetadata["_enable_skill_harness"] = "false"
		}
	}

	// Fetch RESULT logs for this task to provide history to the agent.
	if logs, _, err := s.taskLogRepo.List(ctx, t.ID, nil, 0, 0); err == nil {
		type resultEntry struct {
			ResultType string `json:"result_type"`
			Text       string `json:"text"`
			CreatedAt  string `json:"created_at"`
		}

		var history []resultEntry

		for _, l := range logs {
			if l.Category != int32(taskguildv1.TaskLogCategory_TASK_LOG_CATEGORY_RESULT) {
				continue
			}

			history = append(history, resultEntry{
				ResultType: l.Metadata["result_type"],
				Text:       l.Metadata["full_text"],
				CreatedAt:  l.CreatedAt.Format(time.RFC3339),
			})
		}

		if len(history) > 0 {
			if b, err := json.Marshal(history); err == nil {
				enrichedMetadata["_result_history"] = string(b)
			}
		}
	}

	// Publish agent assigned event.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_ASSIGNED,
		t.ID,
		"",
		map[string]string{
			"agent_manager_id": req.Msg.GetAgentManagerId(),
			"agent_config_id":  agentConfigID,
			"project_id":       t.ProjectID,
			"workflow_id":      t.WorkflowID,
		},
	)

	slog.Info("agent claimed task",
		"task_id", t.ID,
		"agent_manager_id", req.Msg.GetAgentManagerId(),
		"agent_config_id", agentConfigID,
	)

	return connect.NewResponse(&taskguildv1.ClaimTaskResponse{
		Success:       true,
		Instructions:  instructions,
		AgentConfigId: agentConfigID,
		Metadata:      enrichedMetadata,
	}), nil
}

// isWorktreeOccupied checks whether any other ASSIGNED task in the same project
// is using the given worktree name. Returns the occupant's ID and title.
func (s *Server) isWorktreeOccupied(ctx context.Context, projectID, worktreeName, excludeTaskID string) (bool, string, string) {
	tasks, _, err := s.taskRepo.List(ctx, projectID, "", "", 0, 0)
	if err != nil {
		return false, "", ""
	}

	for _, t := range tasks {
		if t.ID == excludeTaskID {
			continue
		}

		if t.Metadata["worktree"] != worktreeName {
			continue
		}

		if t.AssignmentStatus == task.AssignmentStatusAssigned {
			return true, t.ID, t.Title
		}
	}

	return false, "", ""
}

// rebroadcastWorktreeWaiters sends TaskAvailableCommand for any PENDING tasks
// that share the same worktree as the just-completed task, so they can be
// claimed now that the worktree is free.
func (s *Server) rebroadcastWorktreeWaiters(ctx context.Context, projectID, worktreeName, completedTaskID string) {
	tasks, _, err := s.taskRepo.List(ctx, projectID, "", "", 0, 0)
	if err != nil {
		return
	}

	var projectName string
	if p, pErr := s.projectRepo.Get(ctx, projectID); pErr == nil {
		projectName = p.Name
	}

	for _, t := range tasks {
		if t.ID == completedTaskID || t.AssignmentStatus != task.AssignmentStatusPending {
			continue
		}

		if t.Metadata["worktree"] != worktreeName {
			continue
		}
		// Clear the worktree_occupied pending reason since worktree is now free.
		if t.Metadata[task.MetaPendingReason] == task.PendingReasonWorktreeOccupied {
			task.ClearPendingReason(t.Metadata)
			t.UpdatedAt = time.Now()
			_ = s.taskRepo.Update(ctx, t)
		}

		wf, wfErr := s.workflowRepo.Get(ctx, t.WorkflowID)
		if wfErr != nil {
			continue
		}

		agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
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
		slog.Info("rebroadcast worktree waiter",
			"task_id", t.ID,
			"worktree", worktreeName,
			"freed_by", completedTaskID,
		)
	}
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
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}

	task.ClearPendingReason(t.Metadata)

	wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		return err
	}

	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)

	skillIDs := wf.FindSkillIDsForStatus(t.StatusID)
	if agentConfigID == "" && len(skillIDs) == 0 {
		return fmt.Errorf("no executor (skill or agent) configured for status %s", t.StatusID)
	}

	var projectName string
	if p, pErr := s.projectRepo.Get(ctx, t.ProjectID); pErr == nil {
		projectName = p.Name
	}

	// Set pending reason based on current state.
	if !s.registry.HasConnectedAgentForProject(projectName) {
		t.Metadata[task.MetaPendingReason] = task.PendingReasonWaitingAgent
	} else if worktreeName := t.Metadata["worktree"]; worktreeName != "" {
		if occupied, occupantID, occupantTitle := s.isWorktreeOccupied(ctx, t.ProjectID, worktreeName, t.ID); occupied {
			t.Metadata[task.MetaPendingReason] = task.PendingReasonWorktreeOccupied
			t.Metadata[task.MetaPendingBlockerTaskID] = occupantID
			t.Metadata[task.MetaPendingBlockerTaskTitle] = occupantTitle
		}
	}

	if err := s.taskRepo.Update(ctx, t); err != nil {
		return err
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

// resolveEffort returns the effort string used when dispatching the task.
// Task-level Effort (when non-empty) overrides the WorkflowStatus-level Effort.
// An empty return value means no explicit effort should be set (runner defaults apply).
func resolveEffort(t *task.Task, currentStatus *workflow.Status) string {
	effort := ""
	if currentStatus != nil {
		effort = currentStatus.Effort
	}

	if t != nil && t.Effort != "" {
		effort = t.Effort
	}

	return effort
}
