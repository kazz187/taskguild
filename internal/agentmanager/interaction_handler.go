package agentmanager

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/interaction"
	taskguildv1 "github.com/kazz187/taskguild/gen/proto/taskguild/v1"
)

func (s *Server) CreateInteraction(ctx context.Context, req *connect.Request[taskguildv1.CreateInteractionRequest]) (*connect.Response[taskguildv1.CreateInteractionResponse], error) {
	now := time.Now()
	inter := &interaction.Interaction{
		ID:          ulid.Make().String(),
		TaskID:      req.Msg.TaskId,
		AgentID:     req.Msg.AgentId,
		Type:        interaction.InteractionType(req.Msg.Type),
		Status:      interaction.StatusPending,
		Title:       req.Msg.Title,
		Description: req.Msg.Description,
		Metadata:    req.Msg.Metadata,
		CreatedAt:   now,
	}
	for _, opt := range req.Msg.Options {
		inter.Options = append(inter.Options, interaction.Option{
			Label:       opt.Label,
			Value:       opt.Value,
			Description: opt.Description,
		})
	}

	// Generate a single-use response token for push notification actions.
	// This allows the Service Worker to respond to interactions without
	// exposing the main API key.
	interType := interaction.InteractionType(req.Msg.Type)
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
		map[string]string{"task_id": inter.TaskID, "agent_id": inter.AgentID},
	)

	return connect.NewResponse(&taskguildv1.CreateInteractionResponse{
		Interaction: interProto,
	}), nil
}

func (s *Server) GetInteractionResponse(ctx context.Context, req *connect.Request[taskguildv1.GetInteractionResponseRequest]) (*connect.Response[taskguildv1.GetInteractionResponseResponse], error) {
	inter, err := s.interactionRepo.Get(ctx, req.Msg.InteractionId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetInteractionResponseResponse{
		Interaction: interaction.ToProto(inter),
	}), nil
}
