package agentmanager

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	scp "github.com/kazz187/taskguild/internal/singlecommandpermission"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Single Command Permissions ---

// ListSingleCommandPermissions returns all wildcard-based single-command permission
// rules for a project (used by agents to populate their permission cache).
func (s *Server) ListSingleCommandPermissions(ctx context.Context, req *connect.Request[taskguildv1.ListSingleCommandPermissionsAgentRequest]) (*connect.Response[taskguildv1.ListSingleCommandPermissionsAgentResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	// Resolve project name to ID.
	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project: %w", err)
	}

	perms, err := s.scpRepo.List(ctx, proj.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list single command permissions: %w", err)
	}

	var pbPerms []*taskguildv1.SingleCommandPermission
	for _, p := range perms {
		pbPerms = append(pbPerms, &taskguildv1.SingleCommandPermission{
			Id:        p.ID,
			ProjectId: p.ProjectID,
			Pattern:   p.Pattern,
			Type:      p.Type,
			Label:     p.Label,
			CreatedAt: timestamppb.New(p.CreatedAt),
		})
	}

	return connect.NewResponse(&taskguildv1.ListSingleCommandPermissionsAgentResponse{
		Permissions: pbPerms,
	}), nil
}

// AddSingleCommandPermission adds a new wildcard permission rule from an agent.
// If a rule with the same pattern+type already exists in the project, the
// existing rule is updated (label overwrite) and any extra duplicates are
// removed. This makes the operation idempotent and cleans up legacy duplicates.
func (s *Server) AddSingleCommandPermission(ctx context.Context, req *connect.Request[taskguildv1.AddSingleCommandPermissionRequest]) (*connect.Response[taskguildv1.AddSingleCommandPermissionResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	// Resolve project name to ID.
	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve project: %w", err)
	}

	// Check for existing duplicates (pattern + type within the same project).
	existing, err := s.scpRepo.FindByPatternAndType(ctx, proj.ID, req.Msg.Pattern, req.Msg.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing single command permissions: %w", err)
	}

	var p *scp.SingleCommandPermission

	if len(existing) > 0 {
		// Keep the oldest entry and update its label.
		p = existing[0]
		p.Label = req.Msg.Label
		if err := s.scpRepo.Update(ctx, p); err != nil {
			return nil, fmt.Errorf("failed to update single command permission: %w", err)
		}

		// Remove extra duplicates (index 1+).
		for _, dup := range existing[1:] {
			_ = s.scpRepo.Delete(ctx, dup.ID)
		}
	} else {
		// No duplicate — create a new entry.
		p = &scp.SingleCommandPermission{
			ID:        ulid.Make().String(),
			ProjectID: proj.ID,
			Pattern:   req.Msg.Pattern,
			Type:      req.Msg.Type,
			Label:     req.Msg.Label,
			CreatedAt: time.Now(),
		}
		if err := s.scpRepo.Create(ctx, p); err != nil {
			return nil, fmt.Errorf("failed to create single command permission: %w", err)
		}
	}

	return connect.NewResponse(&taskguildv1.AddSingleCommandPermissionResponse{
		Permission: &taskguildv1.SingleCommandPermission{
			Id:        p.ID,
			ProjectId: p.ProjectID,
			Pattern:   p.Pattern,
			Type:      p.Type,
			Label:     p.Label,
			CreatedAt: timestamppb.New(p.CreatedAt),
		},
	}), nil
}
