package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/kazz187/taskguild/backend/internal/agentmanager"
	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/project"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
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
	// Prefer agent_id on the status; fall back to legacy AgentConfig list.
	agentConfigID := findAgentIDForStatus(wf, t.StatusID)
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

// findAgentIDForStatus returns the agent ID for the given status.
// It first checks if the status has a direct agent_id set, then falls back
// to the legacy AgentConfig list on the workflow.
func findAgentIDForStatus(wf *workflow.Workflow, statusID string) string {
	// Check status-level agent_id first (new approach).
	for _, s := range wf.Statuses {
		if s.ID == statusID && s.AgentID != "" {
			return s.AgentID
		}
	}
	// Fall back to legacy AgentConfig list.
	for i := range wf.AgentConfigs {
		if wf.AgentConfigs[i].WorkflowStatusID == statusID {
			return wf.AgentConfigs[i].ID
		}
	}
	return ""
}
