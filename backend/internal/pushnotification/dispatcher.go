package pushnotification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/task"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

type Dispatcher struct {
	eventBus        *eventbus.Bus
	interactionRepo interaction.Repository
	taskRepo        task.Repository
	sender          *Sender
}

func NewDispatcher(eventBus *eventbus.Bus, interactionRepo interaction.Repository, taskRepo task.Repository, sender *Sender) *Dispatcher {
	return &Dispatcher{
		eventBus:        eventBus,
		interactionRepo: interactionRepo,
		taskRepo:        taskRepo,
		sender:          sender,
	}
}

func (d *Dispatcher) Start(ctx context.Context) {
	subID, ch := d.eventBus.Subscribe(256)
	defer d.eventBus.Unsubscribe(subID)

	slog.Info("push notification dispatcher started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("push notification dispatcher stopped")
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Type == taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED {
				d.handleInteractionCreated(ctx, event)
			}
		}
	}
}

func (d *Dispatcher) handleInteractionCreated(ctx context.Context, event *taskguildv1.Event) {
	inter, err := d.interactionRepo.Get(ctx, event.ResourceId)
	if err != nil {
		slog.Error("push dispatcher: failed to get interaction", "id", event.ResourceId, "error", err)
		return
	}

	// Only send push for permission requests and questions.
	if inter.Type != interaction.TypePermissionRequest && inter.Type != interaction.TypeQuestion {
		return
	}

	// Build notification payload.
	title := "TaskGuild"
	switch inter.Type {
	case interaction.TypePermissionRequest:
		title = "Permission Request"
	case interaction.TypeQuestion:
		title = "Question from Agent"
	}

	var url string
	if inter.TaskID != "" {
		t, err := d.taskRepo.Get(ctx, inter.TaskID)
		if err == nil {
			url = fmt.Sprintf("/projects/%s/tasks/%s", t.ProjectID, t.ID)
		}
	}

	d.sender.SendToAll(ctx, &NotificationPayload{
		Title: title,
		Body:  inter.Title,
		URL:   url,
		Tag:   inter.ID,
	})
}
