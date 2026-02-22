package orchestrator

import (
	"context"
	"log/slog"

	"github.com/kazz187/taskguild/backend/internal/agentmanager"
	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

type Orchestrator struct {
	eventBus     *eventbus.Bus
	taskRepo     task.Repository
	workflowRepo workflow.Repository
	registry     *agentmanager.Registry
}

func New(eventBus *eventbus.Bus, taskRepo task.Repository, workflowRepo workflow.Repository, registry *agentmanager.Registry) *Orchestrator {
	return &Orchestrator{
		eventBus:     eventBus,
		taskRepo:     taskRepo,
		workflowRepo: workflowRepo,
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

	agentCfg := findAgentConfig(wf, t.StatusID)
	if agentCfg == nil {
		return // no agent config for this status (terminal or manual)
	}

	agentManagerID, ok := o.registry.FindAvailable()
	if !ok {
		slog.Warn("orchestrator: no available agent-manager", "task_id", t.ID, "status_id", t.StatusID)
		return
	}

	// Update task with assigned agent.
	t.AssignedAgentID = agentManagerID
	if err := o.taskRepo.Update(ctx, t); err != nil {
		slog.Error("orchestrator: failed to update task", "task_id", t.ID, "error", err)
		return
	}

	// Send assign command to the agent-manager.
	cmd := &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_AssignTask{
			AssignTask: &taskguildv1.AssignTaskCommand{
				TaskId:       t.ID,
				AgentConfigId: agentCfg.ID,
				Instructions: agentCfg.Instructions,
				Metadata:     t.Metadata,
			},
		},
	}
	if !o.registry.SendCommand(agentManagerID, cmd) {
		slog.Error("orchestrator: failed to send command to agent-manager", "agent_manager_id", agentManagerID, "task_id", t.ID)
		return
	}

	// Publish agent assigned event.
	o.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_ASSIGNED,
		t.ID,
		"",
		map[string]string{
			"agent_manager_id": agentManagerID,
			"agent_config_id":  agentCfg.ID,
			"project_id":       t.ProjectID,
			"workflow_id":      t.WorkflowID,
		},
	)

	slog.Info("orchestrator: agent assigned",
		"task_id", t.ID,
		"agent_manager_id", agentManagerID,
		"agent_config_id", agentCfg.ID,
	)
}

func findAgentConfig(wf *workflow.Workflow, statusID string) *workflow.AgentConfig {
	for i := range wf.AgentConfigs {
		if wf.AgentConfigs[i].WorkflowStatusID == statusID {
			return &wf.AgentConfigs[i]
		}
	}
	return nil
}
