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

// Server implements the ClaudeSettingsService RPC handlers.
type Server struct {
	repo     Repository
	notifier ChangeNotifier
}

// NewServer creates a new Claude Code settings service server.
func NewServer(repo Repository, notifier ChangeNotifier) *Server {
	return &Server{repo: repo, notifier: notifier}
}

func (s *Server) notifyChange(projectID string) {
	if s.notifier != nil {
		s.notifier.NotifyClaudeSettingsChange(projectID)
	}
}

// GetClaudeSettings returns the settings for a project.
func (s *Server) GetClaudeSettings(ctx context.Context, req *connect.Request[taskguildv1.GetClaudeSettingsRequest]) (*connect.Response[taskguildv1.GetClaudeSettingsResponse], error) {
	cs, err := s.repo.Get(ctx, req.Msg.ProjectId)
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
		ProjectID: req.Msg.ProjectId,
		Language:  req.Msg.Language,
		UpdatedAt: time.Now(),
	}
	if err := s.repo.Upsert(ctx, cs); err != nil {
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
	dir := req.Msg.Directory
	if dir == "" {
		dir = "."
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")

	localLanguage, err := readSettingsLanguage(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read settings.json: %w", err)
	}

	stored, err := s.repo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, err
	}

	// Merge: local value takes precedence if non-empty.
	if localLanguage != "" {
		stored.Language = localLanguage
		stored.UpdatedAt = time.Now()
		if err := s.repo.Upsert(ctx, stored); err != nil {
			return nil, err
		}
		s.notifyChange(stored.ProjectID)
	}

	return connect.NewResponse(&taskguildv1.SyncClaudeSettingsFromDirResponse{
		Settings: toProto(stored),
	}), nil
}

// readSettingsLanguage reads the "language" field from a .claude/settings.json file.
// Returns empty string if the file does not exist or has no language field.
func readSettingsLanguage(path string) (string, error) {
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", nil
		}
		return "", readErr
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("invalid JSON in %s: %w", path, err)
	}

	lang, _ := raw["language"].(string)
	return lang, nil
}

func toProto(cs *ClaudeSettings) *taskguildv1.ClaudeSettings {
	return &taskguildv1.ClaudeSettings{
		ProjectId: cs.ProjectID,
		Language:  cs.Language,
		UpdatedAt: timestamppb.New(cs.UpdatedAt),
	}
}
