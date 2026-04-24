package chatnotifier

import (
	"context"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// Notifier subscribes to task lifecycle events and creates notification
// interactions so that status changes appear in Chat and Global Chat.
type Notifier struct {
	eventBus        *eventbus.Bus
	interactionRepo interaction.Repository
	taskRepo        task.Repository
	workflowRepo    workflow.Repository
}

func New(eventBus *eventbus.Bus, interactionRepo interaction.Repository, taskRepo task.Repository, workflowRepo workflow.Repository) *Notifier {
	return &Notifier{
		eventBus:        eventBus,
		interactionRepo: interactionRepo,
		taskRepo:        taskRepo,
		workflowRepo:    workflowRepo,
	}
}

// Start subscribes to the event bus and creates notification interactions
// for task status changes. It blocks until ctx is canceled.
func (n *Notifier) Start(ctx context.Context) {
	subID, ch := n.eventBus.Subscribe(256)
	defer n.eventBus.Unsubscribe(subID)

	slog.Info("chat notifier started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("chat notifier stopped")
			return
		case event, ok := <-ch:
			if !ok {
				return
			}

			switch event.GetType() {
			case taskguildv1.EventType_EVENT_TYPE_TASK_STATUS_CHANGED:
				n.handleTaskStatusChanged(ctx, event)
			}
		}
	}
}

func (n *Notifier) handleTaskStatusChanged(ctx context.Context, event *taskguildv1.Event) {
	taskID := event.GetResourceId()
	newStatusID := event.GetMetadata()["new_status_id"]

	t, err := n.taskRepo.Get(ctx, taskID)
	if err != nil {
		slog.Error("chat notifier: failed to get task", "task_id", taskID, "error", err)
		return
	}

	// Resolve the new status name from the workflow.
	statusName := newStatusID

	wf, err := n.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		slog.Warn("chat notifier: failed to get workflow, using status ID", "workflow_id", t.WorkflowID, "error", err)
	} else {
		for _, s := range wf.Statuses {
			if s.Name == newStatusID {
				statusName = s.Name
				break
			}
		}
	}

	// Create a notification interaction.
	now := time.Now()
	inter := &interaction.Interaction{
		ID:          ulid.Make().String(),
		ProjectID:   t.ProjectID,
		TaskID:      taskID,
		Type:        interaction.TypeNotification,
		Status:      interaction.StatusResponded,
		Title:       "Task status changed to " + statusName,
		CreatedAt:   now,
		RespondedAt: &now,
	}

	if err := n.interactionRepo.Create(ctx, inter); err != nil {
		slog.Error("chat notifier: failed to create notification interaction", "task_id", taskID, "error", err)
		return
	}

	// Publish event so that Chat and Global Chat receive the notification in real time.
	interProto := interaction.ToProto(inter)
	n.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		interaction.MarshalInteractionPayload(interProto),
		map[string]string{"task_id": inter.TaskID, "project_id": t.ProjectID},
	)

	slog.Info("chat notifier: status change notification created",
		"task_id", taskID,
		"task_title", t.Title,
		"new_status", statusName,
	)
}
