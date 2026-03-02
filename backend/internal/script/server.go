package script

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ScriptServiceHandler = (*Server)(nil)

// ExecutionRequester is an interface for triggering script execution on agent-managers.
type ExecutionRequester interface {
	// RequestScriptExecution sends an execute command to a connected agent-manager
	// and returns a request_id for tracking the result.
	RequestScriptExecution(projectID string, script *Script) (string, error)
}

type Server struct {
	repo   Repository
	execReq ExecutionRequester
	broker  *ScriptExecutionBroker
}

func NewServer(repo Repository, execReq ExecutionRequester, broker *ScriptExecutionBroker) *Server {
	return &Server{repo: repo, execReq: execReq, broker: broker}
}

func (s *Server) CreateScript(ctx context.Context, req *connect.Request[taskguildv1.CreateScriptRequest]) (*connect.Response[taskguildv1.CreateScriptResponse], error) {
	now := time.Now()
	filename := req.Msg.Filename
	if filename == "" {
		filename = req.Msg.Name + ".sh"
	}
	sc := &Script{
		ID:          ulid.Make().String(),
		ProjectID:   req.Msg.ProjectId,
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
		Filename:    filename,
		Content:     req.Msg.Content,
		IsSynced:    false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, sc); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.CreateScriptResponse{
		Script: toProto(sc),
	}), nil
}

func (s *Server) GetScript(ctx context.Context, req *connect.Request[taskguildv1.GetScriptRequest]) (*connect.Response[taskguildv1.GetScriptResponse], error) {
	sc, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetScriptResponse{
		Script: toProto(sc),
	}), nil
}

func (s *Server) ListScripts(ctx context.Context, req *connect.Request[taskguildv1.ListScriptsRequest]) (*connect.Response[taskguildv1.ListScriptsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	scripts, total, err := s.repo.List(ctx, req.Msg.ProjectId, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = toProto(sc)
	}
	return connect.NewResponse(&taskguildv1.ListScriptsResponse{
		Scripts: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateScript(ctx context.Context, req *connect.Request[taskguildv1.UpdateScriptRequest]) (*connect.Response[taskguildv1.UpdateScriptResponse], error) {
	sc, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Name != "" {
		sc.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		sc.Description = req.Msg.Description
	}
	if req.Msg.Filename != "" {
		sc.Filename = req.Msg.Filename
	}
	if req.Msg.Content != "" {
		sc.Content = req.Msg.Content
	}
	sc.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, sc); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.UpdateScriptResponse{
		Script: toProto(sc),
	}), nil
}

func (s *Server) DeleteScript(ctx context.Context, req *connect.Request[taskguildv1.DeleteScriptRequest]) (*connect.Response[taskguildv1.DeleteScriptResponse], error) {
	if _, err := s.repo.Get(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.DeleteScriptResponse{}), nil
}

// SyncScriptsFromDir scans a directory for .claude/scripts/* files and syncs them.
func (s *Server) SyncScriptsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncScriptsFromDirRequest]) (*connect.Response[taskguildv1.SyncScriptsFromDirResponse], error) {
	dir := req.Msg.Directory
	if dir == "" {
		dir = "."
	}
	scriptsDir := filepath.Join(dir, ".claude", "scripts")

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return connect.NewResponse(&taskguildv1.SyncScriptsFromDirResponse{}), nil
		}
		return nil, fmt.Errorf("failed to read scripts directory: %w", err)
	}

	var (
		synced  []*taskguildv1.ScriptDefinition
		created int32
		updated int32
	)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		filePath := filepath.Join(scriptsDir, filename)

		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// Derive name from filename (strip extension).
		name := strings.TrimSuffix(filename, filepath.Ext(filename))
		if name == "" {
			continue
		}

		// Try to find existing script with same name in this project.
		existing, err := s.repo.FindByName(ctx, req.Msg.ProjectId, name)
		if err == nil && existing != nil {
			// Update existing script.
			existing.Filename = filename
			existing.Content = string(content)
			existing.IsSynced = true
			existing.UpdatedAt = time.Now()
			if err := s.repo.Update(ctx, existing); err != nil {
				continue
			}
			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new script.
			now := time.Now()
			sc := &Script{
				ID:        ulid.Make().String(),
				ProjectID: req.Msg.ProjectId,
				Name:      name,
				Filename:  filename,
				Content:   string(content),
				IsSynced:  true,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if err := s.repo.Create(ctx, sc); err != nil {
				continue
			}
			synced = append(synced, toProto(sc))
			created++
		}
	}

	return connect.NewResponse(&taskguildv1.SyncScriptsFromDirResponse{
		Scripts: synced,
		Created: created,
		Updated: updated,
	}), nil
}

// ExecuteScript triggers execution of a script on a connected agent-manager.
func (s *Server) ExecuteScript(ctx context.Context, req *connect.Request[taskguildv1.ExecuteScriptRequest]) (*connect.Response[taskguildv1.ExecuteScriptResponse], error) {
	if s.broker.IsDraining() {
		return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("server is shutting down; cannot accept new script executions"))
	}

	sc, err := s.repo.Get(ctx, req.Msg.ScriptId)
	if err != nil {
		return nil, err
	}

	requestID, err := s.execReq.RequestScriptExecution(sc.ProjectID, sc)
	if err != nil {
		return nil, fmt.Errorf("failed to request script execution: %w", err)
	}

	// Register execution in the broker so subscribers can stream output.
	s.broker.RegisterExecution(requestID)

	return connect.NewResponse(&taskguildv1.ExecuteScriptResponse{
		RequestId: requestID,
	}), nil
}

// StreamScriptExecution streams real-time output from a script execution.
func (s *Server) StreamScriptExecution(ctx context.Context, req *connect.Request[taskguildv1.StreamScriptExecutionRequest], stream *connect.ServerStream[taskguildv1.ScriptExecutionEvent]) error {
	ch, unsubscribe := s.broker.Subscribe(req.Msg.RequestId)
	if ch == nil {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("unknown execution request_id: %s", req.Msg.RequestId))
	}
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-ch:
			if !ok {
				// Channel closed — execution completed and all events sent.
				return nil
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

func toProto(s *Script) *taskguildv1.ScriptDefinition {
	return &taskguildv1.ScriptDefinition{
		Id:          s.ID,
		ProjectId:   s.ProjectID,
		Name:        s.Name,
		Description: s.Description,
		Filename:    s.Filename,
		Content:     s.Content,
		IsSynced:    s.IsSynced,
		CreatedAt:   timestamppb.New(s.CreatedAt),
		UpdatedAt:   timestamppb.New(s.UpdatedAt),
	}
}
