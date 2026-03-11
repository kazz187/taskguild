package agentmanager

import (
	"context"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/permission"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func (s *Server) SyncAgents(ctx context.Context, req *connect.Request[taskguildv1.SyncAgentsRequest]) (*connect.Response[taskguildv1.SyncAgentsResponse], error) {
	projectName := req.Msg.ProjectName
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
	projectName := req.Msg.ProjectName
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
	merged := permission.Merge(stored, req.Msg.LocalAllow, req.Msg.LocalAsk, req.Msg.LocalDeny)

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
		req.Msg.TaskId,
		"",
		map[string]string{
			"agent_manager_id": req.Msg.AgentManagerId,
			"agent_status":     req.Msg.Status.String(),
			"message":          req.Msg.Message,
		},
	)
	return connect.NewResponse(&taskguildv1.ReportAgentStatusResponse{}), nil
}
