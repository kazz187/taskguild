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

	// Determine the agent config ID for this status.
	agentConfigID := wf.FindAgentIDForStatus(t.StatusID)
	if agentConfigID == "" {
		return // no agent config for this status (terminal or manual)
	}

	// Set assignment status to PENDING.
	t.AssignmentStatus = task.AssignmentStatusPending
	t.UpdatedAt = time.Now()
	if err := o.taskRepo.Update(ctx, t); err != nil {
		slog.Error("orchestrator: failed to update task assignment status", "task_id", t.ID, "error", err)
		return
	}

	// Resolve project name for filtered broadcast.
	var projectName string
	if p, err := o.projectRepo.Get(ctx, t.ProjectID); err == nil {
		projectName = p.Name
	} else {
		slog.Error("orchestrator: failed to get project", "project_id", t.ProjectID, "error", err)
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
	if err := o.taskRepo.Update(ctx, t); err != nil {
		slog.Error("orchestrator: failed to update task for comment-triggered launch", "task_id", t.ID, "error", err)
		return
	}

	var projectName string
	if p, err := o.projectRepo.Get(ctx, t.ProjectID); err == nil {
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
	o.registry.BroadcastCommandToProject(projectName, cmd)

	slog.Info("orchestrator: comment-triggered agent launch",
		"task_id", t.ID,
		"agent_config_id", agentConfigID,
		"project_name", projectName,
	)
}

