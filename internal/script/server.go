package script

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ScriptServiceHandler = (*Server)(nil)

// ExecutionRequester is an interface for triggering script execution on agent-managers.
type ExecutionRequester interface {
	// RequestScriptExecution sends an execute command to a connected agent-manager
	// using the provided requestID for tracking the result.
	RequestScriptExecution(requestID string, projectID string, script *Script) error
	// RequestScriptStop sends a stop command to connected agent-managers
	// for the given project to cancel a running script execution.
	RequestScriptStop(projectID string, requestID string) error
}

// ChangeNotifier is called after script CRUD operations to notify connected
// agents that they should re-sync their local script files.
// changedScriptIDs lists scripts that were created or updated and should be
// force-overwritten on the agent side even if a local file already exists.
type ChangeNotifier interface {
	NotifyScriptChange(projectID string, changedScriptIDs []string)
}

// WorkDirResolver resolves the absolute working directory for a project
// by looking up the connected agent's work_dir.
type WorkDirResolver interface {
	ResolveWorkDir(projectID string) (string, error)
}

type Server struct {
	repo     Repository
	execReq  ExecutionRequester
	broker   *ScriptExecutionBroker
	resolver WorkDirResolver
	notifier ChangeNotifier
}

func NewServer(repo Repository, execReq ExecutionRequester, broker *ScriptExecutionBroker, resolver WorkDirResolver, notifier ChangeNotifier) *Server {
	return &Server{repo: repo, execReq: execReq, broker: broker, resolver: resolver, notifier: notifier}
}

func (s *Server) notifyChange(projectID string, changedScriptIDs []string) {
	if s.notifier != nil {
		s.notifier.NotifyScriptChange(projectID, changedScriptIDs)
	}
}

func (s *Server) CreateScript(ctx context.Context, req *taskguildv1.CreateScriptRequest) (*taskguildv1.CreateScriptResponse, error) {
	now := time.Now()

	filename := req.GetFilename()
	if filename == "" {
		filename = req.GetName() + ".sh"
	}

	sc := &Script{
		ID:          ulid.Make().String(),
		ProjectID:   req.GetProjectId(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		Filename:    filename,
		Content:     req.GetContent(),
		IsSynced:    false,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := s.repo.Create(ctx, sc)
	if err != nil {
		return nil, err
	}

	s.notifyChange(sc.ProjectID, []string{sc.ID})

	return &taskguildv1.CreateScriptResponse{
		Script: toProto(sc),
	}, nil
}

func (s *Server) GetScript(ctx context.Context, req *taskguildv1.GetScriptRequest) (*taskguildv1.GetScriptResponse, error) {
	sc, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetScriptResponse{
		Script: toProto(sc),
	}, nil
}

func (s *Server) ListScripts(ctx context.Context, req *taskguildv1.ListScriptsRequest) (*taskguildv1.ListScriptsResponse, error) {
	limit, offset := int32(50), int32(0)

	if req.GetPagination() != nil {
		if req.GetPagination().GetLimit() > 0 {
			limit = req.GetPagination().GetLimit()
		}

		offset = req.GetPagination().GetOffset()
	}

	scripts, total, err := s.repo.List(ctx, req.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = toProto(sc)
	}

	return &taskguildv1.ListScriptsResponse{
		Scripts: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}, nil
}

func (s *Server) UpdateScript(ctx context.Context, req *taskguildv1.UpdateScriptRequest) (*taskguildv1.UpdateScriptResponse, error) {
	sc, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if req.GetName() != "" {
		sc.Name = req.GetName()
	}

	if req.GetDescription() != "" {
		sc.Description = req.GetDescription()
	}

	if req.GetFilename() != "" {
		sc.Filename = req.GetFilename()
	}

	if req.GetContent() != "" {
		sc.Content = req.GetContent()
	}

	sc.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, sc); err != nil {
		return nil, err
	}

	s.notifyChange(sc.ProjectID, []string{sc.ID})

	return &taskguildv1.UpdateScriptResponse{
		Script: toProto(sc),
	}, nil
}

func (s *Server) DeleteScript(ctx context.Context, req *taskguildv1.DeleteScriptRequest) (*taskguildv1.DeleteScriptResponse, error) {
	sc, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if err := s.repo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}

	s.notifyChange(sc.ProjectID, nil)

	return &taskguildv1.DeleteScriptResponse{}, nil
}

// SyncScriptsFromDir scans a directory for .taskguild/scripts/* files and syncs them.
func (s *Server) SyncScriptsFromDir(ctx context.Context, req *taskguildv1.SyncScriptsFromDirRequest) (*taskguildv1.SyncScriptsFromDirResponse, error) {
	dir := req.GetDirectory()
	if (dir == "" || dir == ".") && s.resolver != nil {
		resolved, err := s.resolver.ResolveWorkDir(req.GetProjectId())
		if err != nil {
			return nil, connect.Errorf(connect.CodeFailedPrecondition, "failed to resolve work directory: %v", err).WithCause(err)
		}

		dir = resolved
	}

	if dir == "" {
		dir = "."
	}

	scriptsDir := filepath.Join(dir, ".taskguild", "scripts")

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &taskguildv1.SyncScriptsFromDirResponse{}, nil
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

		// Skip editor swap files, backups, and other temporary files.
		if ShouldSkipScriptFile(filename) {
			continue
		}

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
		existing, err := s.repo.FindByName(ctx, req.GetProjectId(), name)
		if err == nil && existing != nil {
			// Update existing script.
			existing.Filename = filename
			existing.Content = string(content)
			existing.IsSynced = true

			existing.UpdatedAt = time.Now()

			err := s.repo.Update(ctx, existing)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new script.
			now := time.Now()

			sc := &Script{
				ID:        ulid.Make().String(),
				ProjectID: req.GetProjectId(),
				Name:      name,
				Filename:  filename,
				Content:   string(content),
				IsSynced:  true,
				CreatedAt: now,
				UpdatedAt: now,
			}

			err := s.repo.Create(ctx, sc)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(sc))
			created++
		}
	}

	return &taskguildv1.SyncScriptsFromDirResponse{
		Scripts: synced,
		Created: created,
		Updated: updated,
	}, nil
}

// ExecuteScript triggers execution of a script on a connected agent-manager.
func (s *Server) ExecuteScript(ctx context.Context, req *taskguildv1.ExecuteScriptRequest) (*taskguildv1.ExecuteScriptResponse, error) {
	if s.broker.IsDraining() {
		return nil, connect.NewError(connect.CodeUnavailable, "server is shutting down; cannot accept new script executions")
	}

	sc, err := s.repo.Get(ctx, req.GetScriptId())
	if err != nil {
		return nil, err
	}

	// Generate requestID and register with the broker BEFORE sending the
	// command to the agent. This prevents a race where the agent starts
	// sending output (via ReportScriptOutputChunk) before the broker knows
	// about the execution, which would silently drop all log entries.
	requestID := ulid.Make().String()
	s.broker.RegisterExecution(requestID, sc.ID, sc.ProjectID)

	if err := s.execReq.RequestScriptExecution(requestID, sc.ProjectID, sc); err != nil {
		// Clean up the broker registration on failure.
		s.broker.RemoveExecution(requestID)
		return nil, fmt.Errorf("failed to request script execution: %w", err)
	}

	return &taskguildv1.ExecuteScriptResponse{
		RequestId: requestID,
	}, nil
}

// StopScriptExecution stops a running script execution by sending a stop command to the agent.
func (s *Server) StopScriptExecution(ctx context.Context, req *taskguildv1.StopScriptExecutionRequest) (*taskguildv1.StopScriptExecutionResponse, error) {
	requestID := req.GetRequestId()
	if requestID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "request_id is required")
	}

	projectID := s.broker.GetProjectID(requestID)
	if projectID == "" {
		return nil, connect.Errorf(connect.CodeNotFound, "unknown execution request_id: %s", requestID)
	}

	err := s.execReq.RequestScriptStop(projectID, requestID)
	if err != nil {
		return nil, fmt.Errorf("failed to request script stop: %w", err)
	}

	return &taskguildv1.StopScriptExecutionResponse{}, nil
}

// ListActiveExecutions returns currently running and recently completed executions.
func (s *Server) ListActiveExecutions(ctx context.Context, req *taskguildv1.ListActiveExecutionsRequest) (*taskguildv1.ListActiveExecutionsResponse, error) {
	executions := s.broker.ListExecutions(req.GetProjectId())

	return &taskguildv1.ListActiveExecutionsResponse{
		Executions: executions,
	}, nil
}

// StreamScriptExecution streams real-time output from a script execution.
func (s *Server) StreamScriptExecution(ctx context.Context, req *taskguildv1.StreamScriptExecutionRequest, stream taskguildv1connect.ScriptServiceStreamScriptExecutionServerStream) error {
	slog.Info("frontend subscribed to script execution stream", "request_id", req.GetRequestId())

	ch, unsubscribe := s.broker.Subscribe(req.GetRequestId())
	if ch == nil {
		slog.Warn("script execution stream: unknown request_id", "request_id", req.GetRequestId())
		return connect.Errorf(connect.CodeNotFound, "unknown execution request_id: %s", req.GetRequestId())
	}
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			slog.Info("script execution stream ended: client disconnected", "request_id", req.GetRequestId())
			return nil
		case event, ok := <-ch:
			if !ok {
				// Channel closed — execution completed and all events sent.
				slog.Info("[STREAM-TRACE] server->frontend: stream ended (channel closed)", "request_id", req.GetRequestId())
				return nil
			}

			switch e := event.GetEvent().(type) {
			case *taskguildv1.ScriptExecutionEvent_Output:
				slog.Info("[STREAM-TRACE] server->frontend: sending output event", "request_id", req.GetRequestId(), "entry_count", len(e.Output.GetEntries()))
			case *taskguildv1.ScriptExecutionEvent_Complete:
				slog.Info("[STREAM-TRACE] server->frontend: sending complete event", "request_id", req.GetRequestId(), "success", e.Complete.GetSuccess(), "exit_code", e.Complete.GetExitCode())
			}

			err := stream.Send(event)
			if err != nil {
				slog.Warn("[STREAM-TRACE] server->frontend: send error", "request_id", req.GetRequestId(), "error", err)
				return err
			}

			slog.Info("[STREAM-TRACE] server->frontend: event sent successfully", "request_id", req.GetRequestId())
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
