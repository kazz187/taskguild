package permission

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.PermissionServiceHandler = (*Server)(nil)

// ChangeNotifier is called after permission updates from the UI
// to push a SyncPermissionsCommand to connected Agent Managers.
type ChangeNotifier interface {
	NotifyPermissionChange(projectID string)
}

// Server implements the PermissionService RPC handlers.
type Server struct {
	repo     Repository
	notifier ChangeNotifier
}

// NewServer creates a new permission service server.
func NewServer(repo Repository, notifier ChangeNotifier) *Server {
	return &Server{repo: repo, notifier: notifier}
}

func (s *Server) notifyChange(projectID string) {
	if s.notifier != nil {
		s.notifier.NotifyPermissionChange(projectID)
	}
}

// GetPermissions returns the permission set for a project.
func (s *Server) GetPermissions(ctx context.Context, req *connect.Request[taskguildv1.GetPermissionsRequest]) (*connect.Response[taskguildv1.GetPermissionsResponse], error) {
	ps, err := s.repo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetPermissionsResponse{
		Permissions: toProto(ps),
	}), nil
}

// UpdatePermissions replaces the full permission set for a project.
func (s *Server) UpdatePermissions(ctx context.Context, req *connect.Request[taskguildv1.UpdatePermissionsRequest]) (*connect.Response[taskguildv1.UpdatePermissionsResponse], error) {
	ps := &PermissionSet{
		ProjectID: req.Msg.ProjectId,
		Allow:     dedup(req.Msg.Allow),
		Ask:       dedup(req.Msg.Ask),
		Deny:      dedup(req.Msg.Deny),
		UpdatedAt: time.Now(),
	}
	if err := s.repo.Upsert(ctx, ps); err != nil {
		return nil, err
	}
	s.notifyChange(ps.ProjectID)
	return connect.NewResponse(&taskguildv1.UpdatePermissionsResponse{
		Permissions: toProto(ps),
	}), nil
}

// Merge performs a union merge of local permissions into stored permissions.
// Each category (allow, ask, deny) is independently merged with deduplication.
func Merge(stored *PermissionSet, localAllow, localAsk, localDeny []string) *PermissionSet {
	return &PermissionSet{
		ProjectID: stored.ProjectID,
		Allow:     unionDedup(stored.Allow, localAllow),
		Ask:       unionDedup(stored.Ask, localAsk),
		Deny:      unionDedup(stored.Deny, localDeny),
		UpdatedAt: time.Now(),
	}
}

// unionDedup merges two string slices, removing duplicates while preserving order.
func unionDedup(a, b []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// dedup removes duplicate strings while preserving order.
func dedup(items []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range items {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

func toProto(ps *PermissionSet) *taskguildv1.PermissionSet {
	return &taskguildv1.PermissionSet{
		ProjectId: ps.ProjectID,
		Allow:     ps.Allow,
		Ask:       ps.Ask,
		Deny:      ps.Deny,
		UpdatedAt: timestamppb.New(ps.UpdatedAt),
	}
}
