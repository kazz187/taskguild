package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/kazz187/taskguild/internal/agentmanager"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/project"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

type Orchestrator struct {
	eventBus     *eventbus.Bus
	taskRepo     task.Repository
	workflowRepo workflow.Repository
	projectRepo  project.Repository
	registry     *agentmanager.Registry
}

func New(eventBus *eventbus.Bus, taskRepo task.Repository, workflowRepo workflow.Repository, projectRepo project.Repository, registry *agentmanager.Registry) *Orchestrator {
	return &Orchestrator{
		eventBus:     eventBus,
		taskRepo:     taskRepo,
		workflowRepo: workflowRepo,
		projectRepo:  projectRepo,
		registry:     registry,
	}
}

// Start subscribes to the event bus and processes task lifecycle events.
// It blocks until ctx is cancelled.
func (o *Orchestrator) Start(ctx context.Context) {
	subID, ch := o.eventBus.Subscribe(256)
	defer o.eventBus.Unsubscribe(subID)

	slog.Info("orchestrator started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("orchestrator stopped")
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			switch event.Type {
			case taskguildv1.EventType_EVENT_TYPE_TASK_CREATED,
				taskguildv1.EventType_EVENT_TYPE_TASK_STATUS_CHANGED:
				o.handleTaskEvent(ctx, event)
			case taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED:
				o.handleInteractionCreated(ctx, event)
			}
		}
	}
}

func (o *Orchestrator) handleTaskEvent(ctx context.Context, event *taskguildv1.Event) {
	t, err := o.taskRepo.Get(ctx, event.ResourceId)
	if err != nil {
		slog.Error("orchestrator: failed to get task", "task_id", event.ResourceId, "error", err)
		return
	}

	wf, err := o.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		slog.Error("orchestrator: failed to get workflow", "workflow_id", t.WorkflowID, "error", err)
		return
	}

	// Determine the executor for this status. A status can be driven by
	// skill_ids (new skill-based flow), an agent_id, or a legacy
	// AgentConfig — resolved via ClaimTask's 3-tier fallback. Only skip if
	// the status has none of these configured (e.g. Draft / Closed).
	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
	skillIDs := wf.FindSkillIDsForStatus(t.StatusID)
	if agentConfigID == "" && len(skillIDs) == 0 {
		return // no executor configured for this status (initial/terminal)
	}

	// If the task is already assigned to an agent (e.g. still running hooks
	// after a status transition triggered by that agent), skip reassignment.
	// The running agent will unassign the task when it finishes, and the
	// subsequent status change will re-trigger this handler.
	if t.AssignmentStatus == task.AssignmentStatusAssigned {
		slog.Info("orchestrator: task already assigned to agent, skipping",
			"task_id", t.ID, "agent_id", t.AssignedAgentID)
		return
	}

	// Set assignment status to PENDING.
	t.AssignmentStatus = task.AssignmentStatusPending
	t.UpdatedAt = time.Now()
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}
	task.ClearPendingReason(t.Metadata)

	// Resolve project name for filtered broadcast.
	var projectName string
	if p, err := o.projectRepo.Get(ctx, t.ProjectID); err == nil {
		projectName = p.Name
	} else {
		slog.Error("orchestrator: failed to get project", "project_id", t.ProjectID, "error", err)
	}

	// Determine pending reason.
	o.setPendingReason(ctx, t, projectName)

	if err := o.taskRepo.Update(ctx, t); err != nil {
		slog.Error("orchestrator: failed to update task assignment status", "task_id", t.ID, "error", err)
		return
	}

	// Broadcast TaskAvailableCommand to matching agent-managers.
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
	o.registry.BroadcastCommandToProject(projectName, cmd)

	slog.Info("orchestrator: task available broadcast",
		"task_id", t.ID,
		"agent_config_id", agentConfigID,
		"project_name", projectName,
	)
}

// handleInteractionCreated launches an agent when a user comment is added to
// an unassigned task, so the task is immediately acted on without requiring a
// manual status change or resume.
func (o *Orchestrator) handleInteractionCreated(ctx context.Context, event *taskguildv1.Event) {
	// The event's ResourceId is the interaction ID; task_id is in metadata.
	taskID := event.Metadata["task_id"]
	if taskID == "" {
		return
	}

	// Only react to user-sent comments, not agent-created interactions
	// (permission requests, questions, etc.).
	interProto := interaction.UnmarshalInteractionPayload(event.Payload)
	if interProto == nil || interProto.Type != taskguildv1.InteractionType_INTERACTION_TYPE_USER_MESSAGE {
		return
	}

	t, err := o.taskRepo.Get(ctx, taskID)
	if err != nil {
		slog.Error("orchestrator: failed to get task for interaction", "task_id", taskID, "error", err)
		return
	}

	// Only launch if the task is idle (unassigned). If it's already PENDING
	// or ASSIGNED, the running/incoming agent will see the comment.
	if t.AssignmentStatus != task.AssignmentStatusUnassigned {
		return
	}

	// Clear stop/retry metadata for a fresh start (same as ResumeTask).
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}
	delete(t.Metadata, "_stopped_by_user")
	delete(t.Metadata, "_retry_count")
	delete(t.Metadata, "result_error")

	wf, err := o.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		slog.Error("orchestrator: failed to get workflow for interaction", "workflow_id", t.WorkflowID, "error", err)
		return
	}

	// agentConfigID may be empty if no agent is configured for this status.
	// ClaimTask handles this gracefully by falling back to a plain agent.
	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)

	t.AssignmentStatus = task.AssignmentStatusPending
	t.UpdatedAt = time.Now()
	task.ClearPendingReason(t.Metadata)

	var projectName string
	if p, err := o.projectRepo.Get(ctx, t.ProjectID); err == nil {
		projectName = p.Name
	}

	o.setPendingReason(ctx, t, projectName)

	if err := o.taskRepo.Update(ctx, t); err != nil {
		slog.Error("orchestrator: failed to update task for comment-triggered launch", "task_id", t.ID, "error", err)
		return
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
	o.registry.BroadcastCommandToProject(projectName, cmd)

	slog.Info("orchestrator: comment-triggered agent launch",
		"task_id", t.ID,
		"agent_config_id", agentConfigID,
		"project_name", projectName,
	)
}

// setPendingReason computes why a task is pending and stores the reason in metadata.
func (o *Orchestrator) setPendingReason(ctx context.Context, t *task.Task, projectName string) {
	// Check if any agent is connected for this project.
	if !o.registry.HasConnectedAgentForProject(projectName) {
		t.Metadata[task.MetaPendingReason] = task.PendingReasonWaitingAgent
		return
	}

	// Check worktree occupancy.
	if worktreeName := t.Metadata["worktree"]; worktreeName != "" {
		tasks, _, err := o.taskRepo.List(ctx, t.ProjectID, "", "", 0, 0)
		if err != nil {
			return
		}
		for _, other := range tasks {
			if other.ID == t.ID {
				continue
			}
			if other.Metadata["worktree"] != worktreeName {
				continue
			}
			if other.AssignmentStatus == task.AssignmentStatusAssigned {
				t.Metadata[task.MetaPendingReason] = task.PendingReasonWorktreeOccupied
				t.Metadata[task.MetaPendingBlockerTaskID] = other.ID
				t.Metadata[task.MetaPendingBlockerTaskTitle] = other.Title
				return
			}
		}
	}
}
