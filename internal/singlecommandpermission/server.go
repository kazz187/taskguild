package singlecommandpermission

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/pkg/cerr"
)

// validateWildcardPattern checks that a wildcard pattern is valid.
// It converts the wildcard to a regex and attempts to compile it.
func validateWildcardPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("pattern must not be empty")
	}
	regex := wildcardToRegex(pattern)
	if _, err := regexp.Compile(regex); err != nil {
		return fmt.Errorf("invalid wildcard pattern: %s", err)
	}
	return nil
}

// wildcardToRegex converts a wildcard pattern to a Go regular expression.
func wildcardToRegex(pattern string) string {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return "^" + strings.Join(parts, ".*") + "$"
}

var _ taskguildv1connect.SingleCommandPermissionServiceHandler = (*Server)(nil)

// ChangeNotifier is called after permission creates/updates/deletes to push
// updates to connected Agent Managers so they can refresh their caches.
type ChangeNotifier interface {
	NotifySingleCommandPermissionChange(projectID string)
}

// Server implements the SingleCommandPermissionService RPC handlers.
type Server struct {
	repo     Repository
	notifier ChangeNotifier
}

// NewServer creates a new single-command permission service server.
func NewServer(repo Repository, notifier ChangeNotifier) *Server {
	return &Server{repo: repo, notifier: notifier}
}

func (s *Server) notifyChange(projectID string) {
	if s.notifier != nil {
		s.notifier.NotifySingleCommandPermissionChange(projectID)
	}
}

// ListSingleCommandPermissions returns all rules for a project.
func (s *Server) ListSingleCommandPermissions(
	ctx context.Context,
	req *connect.Request[taskguildv1.ListSingleCommandPermissionsRequest],
) (*connect.Response[taskguildv1.ListSingleCommandPermissionsResponse], error) {
	perms, err := s.repo.List(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, err
	}

	var pbPerms []*taskguildv1.SingleCommandPermission
	for _, p := range perms {
		pbPerms = append(pbPerms, toProto(p))
	}

	return connect.NewResponse(&taskguildv1.ListSingleCommandPermissionsResponse{
		Permissions: pbPerms,
	}), nil
}

// CreateSingleCommandPermission adds a new wildcard permission rule.
// If a rule with the same pattern+type already exists in the project, any extra
// duplicates are removed. This makes the operation idempotent and cleans up
// legacy duplicates.
func (s *Server) CreateSingleCommandPermission(
	ctx context.Context,
	req *connect.Request[taskguildv1.CreateSingleCommandPermissionRequest],
) (*connect.Response[taskguildv1.CreateSingleCommandPermissionResponse], error) {
	// Validate the wildcard pattern.
	if err := validateWildcardPattern(req.Msg.Pattern); err != nil {
		return nil, cerr.NewError(cerr.InvalidArgument, err.Error(), err)
	}

	// Validate type.
	if req.Msg.Type != TypeCommand && req.Msg.Type != TypeRedirect {
		return nil, cerr.NewError(cerr.InvalidArgument, fmt.Sprintf("type must be %q or %q", TypeCommand, TypeRedirect), nil)
	}

	// Check for existing duplicates (pattern + type within the same project).
	existing, err := s.repo.FindByPatternAndType(ctx, req.Msg.ProjectId, req.Msg.Pattern, req.Msg.Type)
	if err != nil {
		return nil, err
	}

	var p *SingleCommandPermission

	if len(existing) > 0 {
		// Keep the oldest entry.
		p = existing[0]

		// Remove extra duplicates (index 1+).
		for _, dup := range existing[1:] {
			_ = s.repo.Delete(ctx, dup.ID)
		}
	} else {
		// No duplicate — create a new entry.
		p = &SingleCommandPermission{
			ID:        ulid.Make().String(),
			ProjectID: req.Msg.ProjectId,
			Pattern:   req.Msg.Pattern,
			Type:      req.Msg.Type,
			CreatedAt: time.Now(),
		}
		if err := s.repo.Create(ctx, p); err != nil {
			return nil, err
		}
	}

	s.notifyChange(p.ProjectID)

	return connect.NewResponse(&taskguildv1.CreateSingleCommandPermissionResponse{
		Permission: toProto(p),
	}), nil
}

// UpdateSingleCommandPermission modifies an existing permission rule.
func (s *Server) UpdateSingleCommandPermission(
	ctx context.Context,
	req *connect.Request[taskguildv1.UpdateSingleCommandPermissionRequest],
) (*connect.Response[taskguildv1.UpdateSingleCommandPermissionResponse], error) {
	// Validate the wildcard pattern.
	if err := validateWildcardPattern(req.Msg.Pattern); err != nil {
		return nil, cerr.NewError(cerr.InvalidArgument, err.Error(), err)
	}

	// Validate type.
	if req.Msg.Type != TypeCommand && req.Msg.Type != TypeRedirect {
		return nil, cerr.NewError(cerr.InvalidArgument, fmt.Sprintf("type must be %q or %q", TypeCommand, TypeRedirect), nil)
	}

	existing, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	existing.Pattern = req.Msg.Pattern
	existing.Type = req.Msg.Type

	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}

	s.notifyChange(existing.ProjectID)

	return connect.NewResponse(&taskguildv1.UpdateSingleCommandPermissionResponse{
		Permission: toProto(existing),
	}), nil
}

// DeleteSingleCommandPermission removes a permission rule.
func (s *Server) DeleteSingleCommandPermission(
	ctx context.Context,
	req *connect.Request[taskguildv1.DeleteSingleCommandPermissionRequest],
) (*connect.Response[taskguildv1.DeleteSingleCommandPermissionResponse], error) {
	// Get the permission first so we know the project ID for notification.
	existing, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}

	s.notifyChange(existing.ProjectID)

	return connect.NewResponse(&taskguildv1.DeleteSingleCommandPermissionResponse{}), nil
}

func toProto(p *SingleCommandPermission) *taskguildv1.SingleCommandPermission {
	return &taskguildv1.SingleCommandPermission{
		Id:        p.ID,
		ProjectId: p.ProjectID,
		Pattern:   p.Pattern,
		Type:      p.Type,
		CreatedAt: timestamppb.New(p.CreatedAt),
	}
}
