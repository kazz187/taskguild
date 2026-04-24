package agentmanager

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/claudesettings"
	"github.com/kazz187/taskguild/internal/permission"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) SyncAgents(ctx context.Context, req *connect.Request[taskguildv1.SyncAgentsRequest]) (*connect.Response[taskguildv1.SyncAgentsResponse], error) {
	projectName := req.Msg.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	agents, _, err := s.agentRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.AgentDefinition, len(agents))
	for i, ag := range agents {
		protos[i] = agentToProto(ag)
	}

	return connect.NewResponse(&taskguildv1.SyncAgentsResponse{
		Agents: protos,
	}), nil
}

func (s *Server) SyncPermissions(ctx context.Context, req *connect.Request[taskguildv1.SyncPermissionsRequest]) (*connect.Response[taskguildv1.SyncPermissionsResponse], error) {
	projectName := req.Msg.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Get stored permissions for the project.
	stored, err := s.permissionRepo.Get(ctx, proj.ID)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Merge local permissions with stored (union strategy).
	merged := permission.Merge(stored, req.Msg.GetLocalAllow(), req.Msg.GetLocalAsk(), req.Msg.GetLocalDeny())

	// Save merged result.
	if err := s.permissionRepo.Upsert(ctx, merged); err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	return connect.NewResponse(&taskguildv1.SyncPermissionsResponse{
		Permissions: &taskguildv1.PermissionSet{
			ProjectId: proj.ID,
			Allow:     merged.Allow,
			Ask:       merged.Ask,
			Deny:      merged.Deny,
			UpdatedAt: timestamppb.New(merged.UpdatedAt),
		},
	}), nil
}

func agentToProto(a *agent.Agent) *taskguildv1.AgentDefinition {
	return &taskguildv1.AgentDefinition{
		Id:              a.ID,
		ProjectId:       a.ProjectID,
		Name:            a.Name,
		Description:     a.Description,
		Prompt:          a.Prompt,
		Tools:           a.Tools,
		DisallowedTools: a.DisallowedTools,
		Model:           a.Model,
		PermissionMode:  a.PermissionMode,
		Skills:          a.Skills,
		Memory:          a.Memory,
		IsSynced:        a.IsSynced,
		CreatedAt:       timestamppb.New(a.CreatedAt),
		UpdatedAt:       timestamppb.New(a.UpdatedAt),
	}
}

func (s *Server) ReportAgentStatus(ctx context.Context, req *connect.Request[taskguildv1.ReportAgentStatusRequest]) (*connect.Response[taskguildv1.ReportAgentStatusResponse], error) {
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_STATUS_CHANGED,
		req.Msg.GetTaskId(),
		"",
		map[string]string{
			"agent_manager_id": req.Msg.GetAgentManagerId(),
			"agent_status":     req.Msg.GetStatus().String(),
			"message":          req.Msg.GetMessage(),
		},
	)

	return connect.NewResponse(&taskguildv1.ReportAgentStatusResponse{}), nil
}

func (s *Server) SyncClaudeSettings(ctx context.Context, req *connect.Request[taskguildv1.SyncClaudeSettingsAgentRequest]) (*connect.Response[taskguildv1.SyncClaudeSettingsAgentResponse], error) {
	projectName := req.Msg.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	stored, err := s.claudeSettingsRepo.Get(ctx, proj.ID)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Merge: local value fills in nil fields.
	merged := mergeClaudeSettings(stored, req.Msg.LocalLanguage, attributionFromProto(req.Msg.GetLocalAttribution()))

	if err := s.claudeSettingsRepo.Upsert(ctx, merged); err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	return connect.NewResponse(&taskguildv1.SyncClaudeSettingsAgentResponse{
		Settings: &taskguildv1.ClaudeSettings{
			ProjectId:   proj.ID,
			Language:    merged.Language, // both are *string
			Attribution: attributionToProto(merged.Attribution),
			UpdatedAt:   timestamppb.New(merged.UpdatedAt),
		},
	}), nil
}

func attributionFromProto(a *taskguildv1.Attribution) *claudesettings.Attribution {
	if a == nil {
		return nil
	}

	return &claudesettings.Attribution{
		Commit: a.Commit,
		Pr:     a.Pr,
	}
}

func attributionToProto(a *claudesettings.Attribution) *taskguildv1.Attribution {
	if a == nil {
		return nil
	}

	return &taskguildv1.Attribution{
		Commit: a.Commit,
		Pr:     a.Pr,
	}
}

// mergeClaudeSettings merges local settings with stored settings.
// Stored values take precedence; local values fill in nil fields.
func mergeClaudeSettings(stored *claudesettings.ClaudeSettings, localLanguage *string, localAttribution *claudesettings.Attribution) *claudesettings.ClaudeSettings {
	result := &claudesettings.ClaudeSettings{
		ProjectID:   stored.ProjectID,
		Language:    stored.Language,
		Attribution: stored.Attribution,
		UpdatedAt:   time.Now(),
	}
	if result.Language == nil && localLanguage != nil {
		result.Language = localLanguage
	}

	if localAttribution != nil {
		if result.Attribution == nil {
			result.Attribution = &claudesettings.Attribution{}
		}

		if result.Attribution.Commit == nil && localAttribution.Commit != nil {
			result.Attribution.Commit = localAttribution.Commit
		}

		if result.Attribution.Pr == nil && localAttribution.Pr != nil {
			result.Attribution.Pr = localAttribution.Pr
		}
	}

	return result
}
