package pushnotification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	"github.com/kazz187/taskguild/internal/task"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

type Dispatcher struct {
	eventBus        *eventbus.Bus
	interactionRepo interaction.Repository
	taskRepo        task.Repository
	sender          *Sender
	baseEnv         *config.BaseEnv
}

func NewDispatcher(eventBus *eventbus.Bus, interactionRepo interaction.Repository, taskRepo task.Repository, sender *Sender, baseEnv *config.BaseEnv) *Dispatcher {
	return &Dispatcher{
		eventBus:        eventBus,
		interactionRepo: interactionRepo,
		taskRepo:        taskRepo,
		sender:          sender,
		baseEnv:         baseEnv,
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
			if event.GetType() == taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED {
				d.handleInteractionCreated(ctx, event)
			}
		}
	}
}

func (d *Dispatcher) handleInteractionCreated(ctx context.Context, event *taskguildv1.Event) {
	// Quick-filter using event payload to avoid DB fetch for
	// interaction types that don't need push notifications.
	if pb := interaction.UnmarshalInteractionPayload(event.GetPayload()); pb != nil {
		if pb.GetType() != taskguildv1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST &&
			pb.GetType() != taskguildv1.InteractionType_INTERACTION_TYPE_QUESTION {
			return
		}
	}

	// Fetch full interaction from DB because ResponseToken (not
	// included in the proto) is required for push notification actions.
	inter, err := d.interactionRepo.Get(ctx, event.GetResourceId())
	if err != nil {
		slog.Error("push dispatcher: failed to get interaction", "id", event.GetResourceId(), "error", err)
		return
	}

	// Only send push for permission requests and questions.
	if inter.Type != interaction.TypePermissionRequest && inter.Type != interaction.TypeQuestion {
		return
	}

	// Build notification payload.
	title := "TaskGuild"
	var payloadType string
	switch inter.Type {
	case interaction.TypePermissionRequest:
		title = "Permission Request"
		payloadType = "permission_request"
	case interaction.TypeQuestion:
		title = "Question from Agent"
		payloadType = "question"
	}

	var url string
	if inter.TaskID != "" {
		t, err := d.taskRepo.Get(ctx, inter.TaskID)
		if err == nil {
			url = fmt.Sprintf("/projects/%s/tasks/%s", t.ProjectID, t.ID)
		}
	}

	// Build notification actions based on interaction type.
	actions := d.buildActions(inter)

	d.sender.SendToAll(ctx, &NotificationPayload{
		Title:         title,
		Body:          inter.Title,
		URL:           url,
		Tag:           inter.ID,
		InteractionID: inter.ID,
		ResponseToken: inter.ResponseToken,
		APIBaseURL:    d.baseEnv.GetPublicURL(),
		Type:          payloadType,
		Actions:       actions,
	})
}

// buildActions constructs notification action buttons based on the interaction type.
func (d *Dispatcher) buildActions(inter *interaction.Interaction) []NotificationAction {
	switch inter.Type {
	case interaction.TypePermissionRequest:
		// Permission requests: Allow / Deny.
		// "Always Allow Command" requires pattern metadata that push notifications
		// cannot carry, so we only offer the simple allow/deny actions here.
		return []NotificationAction{
			{Action: "allow", Title: "Allow"},
			{Action: "deny", Title: "Deny"},
		}

	case interaction.TypeQuestion:
		var actions []NotificationAction
		// Use the first 2 options as action buttons (Web Push limit).
		// The action name is set to the option's value so the SW can
		// pass it directly to RespondToInteractionByToken.
		for i, opt := range inter.Options {
			if i >= 2 {
				break
			}
			actions = append(actions, NotificationAction{
				Action: opt.Value,
				Title:  opt.Label,
			})
		}
		// If the question supports free-text input (has no options or
		// explicitly includes "Other"), add a text input action for
		// Chrome Android inline reply support.
		if len(inter.Options) == 0 {
			actions = append(actions, NotificationAction{
				Action: "reply",
				Title:  "Reply",
				Type:   "text",
			})
		}
		return actions

	default:
		return nil
	}
}
