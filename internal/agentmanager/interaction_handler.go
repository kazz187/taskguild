package agentmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/interaction"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) CreateInteraction(ctx context.Context, req *taskguildv1.CreateInteractionRequest) (*taskguildv1.CreateInteractionResponse, error) {
	// Look up the task to get ProjectID for storage path construction.
	t, err := s.taskRepo.Get(ctx, req.GetTaskId())
	if err != nil {
		return nil, err
	}

	now := time.Now()

	inter := &interaction.Interaction{
		ID:          ulid.Make().String(),
		ProjectID:   t.ProjectID,
		TaskID:      req.GetTaskId(),
		AgentID:     req.GetAgentId(),
		Type:        interaction.InteractionType(req.GetType()),
		Status:      interaction.StatusPending,
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		Metadata:    req.GetMetadata(),
		CreatedAt:   now,
	}
	for _, opt := range req.GetOptions() {
		inter.Options = append(inter.Options, interaction.Option{
			Label:       opt.GetLabel(),
			Value:       opt.GetValue(),
			Description: opt.GetDescription(),
		})
	}

	// Generate a single-use response token for push notification actions.
	// This allows the Service Worker to respond to interactions without
	// exposing the main API key.
	interType := interaction.InteractionType(req.GetType())
	if interType == interaction.TypePermissionRequest || interType == interaction.TypeQuestion {
		tokenBytes := make([]byte, 32)
		if _, err := rand.Read(tokenBytes); err == nil {
			inter.ResponseToken = hex.EncodeToString(tokenBytes)
		}
	}

	if err := s.interactionRepo.Create(ctx, inter); err != nil {
		return nil, err
	}

	interProto := interaction.ToProto(inter)
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_INTERACTION_CREATED,
		inter.ID,
		interaction.MarshalInteractionPayload(interProto),
		map[string]string{eventbus.MetaTaskID: inter.TaskID, eventbus.MetaAgentID: inter.AgentID},
	)

	return &taskguildv1.CreateInteractionResponse{
		Interaction: interProto,
	}, nil
}

func (s *Server) GetInteractionResponse(ctx context.Context, req *taskguildv1.GetInteractionResponseRequest) (*taskguildv1.GetInteractionResponseResponse, error) {
	inter, err := s.interactionRepo.Get(ctx, req.GetInteractionId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetInteractionResponseResponse{
		Interaction: interaction.ToProto(inter),
	}, nil
}
