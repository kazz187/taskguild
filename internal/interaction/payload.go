package interaction

import (
	"log/slog"

	"google.golang.org/protobuf/encoding/protojson"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// MarshalInteractionPayload serializes an Interaction proto to a JSON string
// suitable for embedding in an Event's payload field.
func MarshalInteractionPayload(pb *taskguildv1.Interaction) string {
	b, err := protojson.Marshal(pb)
	if err != nil {
		slog.Warn("failed to marshal interaction payload", "id", pb.GetId(), "error", err)
		return ""
	}

	return string(b)
}

// UnmarshalInteractionPayload deserializes an Interaction proto from a JSON
// payload string. Returns nil if the payload is empty or cannot be parsed.
func UnmarshalInteractionPayload(payload string) *taskguildv1.Interaction {
	if payload == "" {
		return nil
	}

	pb := &taskguildv1.Interaction{}
	if err := protojson.Unmarshal([]byte(payload), pb); err != nil {
		slog.Warn("failed to unmarshal interaction payload", "error", err)
		return nil
	}

	return pb
}

// FromProto converts a protobuf Interaction back to the domain model.
func FromProto(pb *taskguildv1.Interaction) *Interaction {
	if pb == nil {
		return nil
	}

	inter := &Interaction{
		ID:          pb.GetId(),
		TaskID:      pb.GetTaskId(),
		AgentID:     pb.GetAgentId(),
		Type:        InteractionType(pb.GetType()),
		Status:      InteractionStatus(pb.GetStatus()),
		Title:       pb.GetTitle(),
		Description: pb.GetDescription(),
		Response:    pb.GetResponse(),
	}
	if pb.GetCreatedAt() != nil {
		inter.CreatedAt = pb.GetCreatedAt().AsTime()
	}

	if pb.GetRespondedAt() != nil {
		t := pb.GetRespondedAt().AsTime()
		inter.RespondedAt = &t
	}

	for _, opt := range pb.GetOptions() {
		inter.Options = append(inter.Options, Option{
			Label:       opt.GetLabel(),
			Value:       opt.GetValue(),
			Description: opt.GetDescription(),
		})
	}

	return inter
}
