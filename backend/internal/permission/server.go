package permission

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// SyncPermissionsFromDir reads .claude/settings.json from the given directory
// and merges its permission rules into the stored set using union strategy.
func (s *Server) SyncPermissionsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncPermissionsFromDirRequest]) (*connect.Response[taskguildv1.SyncPermissionsFromDirResponse], error) {
	dir := req.Msg.Directory
	if dir == "" {
		dir = "."
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	localAllow, localAsk, localDeny, err := readSettingsPermissions(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings.json: %w", err)
	}

	// If no local permissions found, return existing stored permissions as-is.
	if len(localAllow) == 0 && len(localAsk) == 0 && len(localDeny) == 0 {
		stored, err := s.repo.Get(ctx, req.Msg.ProjectId)
		if err != nil {
			return nil, err
		}
		return connect.NewResponse(&taskguildv1.SyncPermissionsFromDirResponse{
			Permissions: toProto(stored),
		}), nil
	}

	// Get stored permissions and merge with local.
	stored, err := s.repo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, err
	}

	merged := Merge(stored, localAllow, localAsk, localDeny)
	if err := s.repo.Upsert(ctx, merged); err != nil {
		return nil, err
	}

	s.notifyChange(merged.ProjectID)

	return connect.NewResponse(&taskguildv1.SyncPermissionsFromDirResponse{
		Permissions: toProto(merged),
	}), nil
}

// readSettingsPermissions reads and parses permission rules from a .claude/settings.json file.
// Returns empty slices if the file does not exist or has no permissions section.
func readSettingsPermissions(path string) (allow, ask, deny []string, err error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil, nil, nil
		}
		return nil, nil, nil, readErr
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}

	permsRaw, ok := raw["permissions"]
	if !ok {
		return nil, nil, nil, nil
	}
	permsMap, ok := permsRaw.(map[string]interface{})
	if !ok {
		return nil, nil, nil, nil
	}

	allow = toStringSlice(permsMap["allow"])
	ask = toStringSlice(permsMap["ask"])
	deny = toStringSlice(permsMap["deny"])
	return allow, ask, deny, nil
}

// toStringSlice converts an interface{} (expected to be []interface{} of strings) to []string.
func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
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
