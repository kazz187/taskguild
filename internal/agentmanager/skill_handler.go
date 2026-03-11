package agentmanager

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Skill sync RPCs ---

func (s *Server) SyncSkills(ctx context.Context, req *connect.Request[taskguildv1.SyncSkillsRequest]) (*connect.Response[taskguildv1.SyncSkillsResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	skills, _, err := s.skillRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.SkillDefinition, len(skills))
	for i, sk := range skills {
		protos[i] = skillToProto(sk)
	}

	slog.Info("syncing skills to agent", "project_name", projectName, "count", len(protos))

	return connect.NewResponse(&taskguildv1.SyncSkillsResponse{
		Skills: protos,
	}), nil
}

func skillToProto(s *skill.Skill) *taskguildv1.SkillDefinition {
	return &taskguildv1.SkillDefinition{
		Id:                     s.ID,
		ProjectId:              s.ProjectID,
		Name:                   s.Name,
		Description:            s.Description,
		Content:                s.Content,
		DisableModelInvocation: s.DisableModelInvocation,
		UserInvocable:          s.UserInvocable,
		AllowedTools:           s.AllowedTools,
		Model:                  s.Model,
		Context:                s.Context,
		Agent:                  s.Agent,
		ArgumentHint:           s.ArgumentHint,
		IsSynced:               s.IsSynced,
		CreatedAt:              timestamppb.New(s.CreatedAt),
		UpdatedAt:              timestamppb.New(s.UpdatedAt),
	}
}
