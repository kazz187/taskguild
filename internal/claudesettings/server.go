package claudesettings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ClaudeSettingsServiceHandler = (*Server)(nil)

// ChangeNotifier is called after settings updates from the UI
// to push a SyncClaudeSettingsCommand to connected Agent Managers.
type ChangeNotifier interface {
	NotifyClaudeSettingsChange(projectID string)
}

// WorkDirResolver resolves the absolute working directory for a project
// by looking up the connected agent's work_dir.
type WorkDirResolver interface {
	ResolveWorkDir(projectID string) (string, error)
}

// Server implements the ClaudeSettingsService RPC handlers.
type Server struct {
	repo     Repository
	notifier ChangeNotifier
	resolver WorkDirResolver
}

// NewServer creates a new Claude Code settings service server.
func NewServer(repo Repository, notifier ChangeNotifier, resolver WorkDirResolver) *Server {
	return &Server{repo: repo, notifier: notifier, resolver: resolver}
}

func (s *Server) notifyChange(projectID string) {
	if s.notifier != nil {
		s.notifier.NotifyClaudeSettingsChange(projectID)
	}
}

// GetClaudeSettings returns the settings for a project.
func (s *Server) GetClaudeSettings(ctx context.Context, req *connect.Request[taskguildv1.GetClaudeSettingsRequest]) (*connect.Response[taskguildv1.GetClaudeSettingsResponse], error) {
	cs, err := s.repo.Get(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.GetClaudeSettingsResponse{
		Settings: toProto(cs),
	}), nil
}

// UpdateClaudeSettings replaces the settings for a project.
func (s *Server) UpdateClaudeSettings(ctx context.Context, req *connect.Request[taskguildv1.UpdateClaudeSettingsRequest]) (*connect.Response[taskguildv1.UpdateClaudeSettingsResponse], error) {
	cs := &ClaudeSettings{
		ProjectID:   req.Msg.GetProjectId(),
		Language:    req.Msg.Language,
		Attribution: attributionFromProto(req.Msg.GetAttribution()),
		UpdatedAt:   time.Now(),
	}
	err := s.repo.Upsert(ctx, cs)
	if err != nil {
		return nil, err
	}

	s.notifyChange(cs.ProjectID)

	return connect.NewResponse(&taskguildv1.UpdateClaudeSettingsResponse{
		Settings: toProto(cs),
	}), nil
}

// SyncClaudeSettingsFromDir reads .claude/settings.json from the given directory
// and merges its settings into the stored set.
func (s *Server) SyncClaudeSettingsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncClaudeSettingsFromDirRequest]) (*connect.Response[taskguildv1.SyncClaudeSettingsFromDirResponse], error) {
	dir := req.Msg.GetDirectory()
	if (dir == "" || dir == ".") && s.resolver != nil {
		resolved, err := s.resolver.ResolveWorkDir(req.Msg.GetProjectId())
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("failed to resolve work directory: %w", err))
		}

		dir = resolved
	}

	if dir == "" {
		dir = "."
	}

	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	localLanguage, localAttribution, err := readSettingsFromFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings.json: %w", err)
	}

	stored, err := s.repo.Get(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, err
	}

	// Merge: local value takes precedence if non-nil and stored is nil.
	changed := false

	if localLanguage != nil && stored.Language == nil {
		stored.Language = localLanguage
		changed = true
	}

	if localAttribution != nil {
		if stored.Attribution == nil {
			stored.Attribution = &Attribution{}
		}

		if localAttribution.Commit != nil && stored.Attribution.Commit == nil {
			stored.Attribution.Commit = localAttribution.Commit
			changed = true
		}

		if localAttribution.Pr != nil && stored.Attribution.Pr == nil {
			stored.Attribution.Pr = localAttribution.Pr
			changed = true
		}
	}

	if changed {
		stored.UpdatedAt = time.Now()
		err := s.repo.Upsert(ctx, stored)
		if err != nil {
			return nil, err
		}

		s.notifyChange(stored.ProjectID)
	}

	return connect.NewResponse(&taskguildv1.SyncClaudeSettingsFromDirResponse{
		Settings: toProto(stored),
	}), nil
}

// readSettingsFromFile reads the "language" and "attribution" fields from a .claude/settings.json file.
// Returns nil values if the file does not exist or fields are absent.
func readSettingsFromFile(path string) (*string, *Attribution, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return nil, nil, nil
		}

		return nil, nil, readErr
	}

	var raw map[string]any
	err := json.Unmarshal(data, &raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}

	var lang *string

	if v, exists := raw["language"]; exists {
		if s, ok := v.(string); ok {
			lang = &s
		}
	}

	var attr *Attribution
	if attrRaw, ok := raw["attribution"].(map[string]any); ok {
		attr = &Attribution{}

		if v, exists := attrRaw["commit"]; exists {
			if s, ok := v.(string); ok {
				attr.Commit = &s
			}
		}

		if v, exists := attrRaw["pr"]; exists {
			if s, ok := v.(string); ok {
				attr.Pr = &s
			}
		}
	}

	return lang, attr, nil
}

func attributionFromProto(a *taskguildv1.Attribution) *Attribution {
	if a == nil {
		return nil
	}

	return &Attribution{
		Commit: a.Commit,
		Pr:     a.Pr,
	}
}

func attributionToProto(a *Attribution) *taskguildv1.Attribution {
	if a == nil {
		return nil
	}

	return &taskguildv1.Attribution{
		Commit: a.Commit,
		Pr:     a.Pr,
	}
}

func toProto(cs *ClaudeSettings) *taskguildv1.ClaudeSettings {
	return &taskguildv1.ClaudeSettings{
		ProjectId:   cs.ProjectID,
		Language:    cs.Language, // both are *string
		Attribution: attributionToProto(cs.Attribution),
		UpdatedAt:   timestamppb.New(cs.UpdatedAt),
	}
}
