package chatnotifier

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
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
// for task status changes. It blocks until ctx is cancelled.
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
			switch event.Type {
			case taskguildv1.EventType_EVENT_TYPE_TASK_STATUS_CHANGED:
				n.handleTaskStatusChanged(ctx, event)
			}
		}
	}
}

func (n *Notifier) handleTaskStatusChanged(ctx context.Context, event *taskguildv1.Event) {
	taskID := event.ResourceId
	newStatusID := event.Metadata["new_status_id"]

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
			if s.ID == newStatusID {
				statusName = s.Name
				break
			}
		}
	}

	// Create a notification interaction.
	now := time.Now()
	inter := &interaction.Interaction{
		ID:          ulid.Make().String(),
		TaskID:      taskID,
		Type:        interaction.TypeNotification,
		Status:      interaction.StatusResponded,
		Title:       fmt.Sprintf("Task status changed to %s", statusName),
		CreatedAt:   now,
		RespondedAt: &now,
	}

	if err := n.interactionRepo.Create(ctx, inter); err != nil {
		slog.Error("chat notifier: failed to create notification interaction", "task_id", taskID, "error", err)
		return
	}

	// Publish event with the interaction proto embedded in the payload so that
	// stream subscribers can use it directly without re-reading from the repo.
	pb := interaction.ToProto(inter)
	payload := ""
	if data, err := protojson.Marshal(pb); err == nil {
		payload = string(data)
	}
	n.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		payload,
		map[string]string{"task_id": inter.TaskID, "project_id": t.ProjectID},
	)

	slog.Info("chat notifier: status change notification created",
		"task_id", taskID,
		"task_title", t.Title,
		"new_status", statusName,
	)
}
