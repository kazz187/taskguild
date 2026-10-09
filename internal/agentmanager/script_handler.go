package agentmanager

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/script"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Script sync & execution RPCs ---

func (s *Server) SyncScripts(ctx context.Context, req *taskguildv1.SyncScriptsRequest) (*taskguildv1.SyncScriptsResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, err
	}

	scripts, _, err := s.scriptRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.ScriptDefinition, len(scripts))
	for i, sc := range scripts {
		protos[i] = scriptToProto(sc)
	}

	return &taskguildv1.SyncScriptsResponse{
		Scripts: protos,
	}, nil
}

func (s *Server) ReportScriptExecutionResult(ctx context.Context, req *taskguildv1.ReportScriptExecutionResultRequest) (*taskguildv1.ReportScriptExecutionResultResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, err
	}

	// Complete execution in the broker — this sends the completion event
	// to all streaming subscribers and closes their channels.
	slog.Info("[STREAM-TRACE] backend(agentmanager): received execution result from agent", "request_id", req.GetRequestId(), "success", req.GetSuccess(), "exit_code", req.GetExitCode(), "log_entry_count", len(req.GetLogEntries()))

	if s.scriptBroker != nil {
		s.scriptBroker.CompleteExecution(
			req.GetRequestId(),
			req.GetSuccess(),
			req.GetExitCode(),
			req.GetLogEntries(),
			req.GetErrorMessage(),
			req.GetStoppedByUser(),
		)
	}

	// Publish event so other consumers (e.g. notifications) can react.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SCRIPT_EXECUTION_RESULT,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID:    proj.ID,
			eventbus.MetaRequestID:    req.GetRequestId(),
			eventbus.MetaScriptID:     req.GetScriptId(),
			eventbus.MetaSuccess:      strconv.FormatBool(req.GetSuccess()),
			eventbus.MetaExitCode:     strconv.Itoa(int(req.GetExitCode())),
			eventbus.MetaErrorMessage: req.GetErrorMessage(),
		},
	)

	slog.Info("script execution result reported",
		"project_id", proj.ID,
		"script_id", req.GetScriptId(),
		"success", req.GetSuccess(),
		"exit_code", req.GetExitCode(),
		"request_id", req.GetRequestId(),
	)

	return &taskguildv1.ReportScriptExecutionResultResponse{}, nil
}

func (s *Server) ReportScriptOutputChunk(ctx context.Context, req *taskguildv1.ReportScriptOutputChunkRequest) (*taskguildv1.ReportScriptOutputChunkResponse, error) {
	if req.GetProjectName() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	slog.Info("[STREAM-TRACE] backend(agentmanager): received output chunk from agent", "request_id", req.GetRequestId(), "entry_count", len(req.GetEntries()))

	if s.scriptBroker != nil {
		s.scriptBroker.PushOutput(req.GetRequestId(), req.GetEntries())
	} else {
		slog.Warn("[STREAM-TRACE] backend(agentmanager): scriptBroker is nil, cannot push output", "request_id", req.GetRequestId())
	}

	return &taskguildv1.ReportScriptOutputChunkResponse{}, nil
}

// RequestScriptExecution sends an ExecuteScriptCommand to connected agent-managers
// for the project and returns a request_id.
func (s *Server) RequestScriptExecution(requestID string, projectID string, sc *script.Script) error {
	proj, err := s.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to look up project: %w", err)
	}

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_ExecuteScript{
			ExecuteScript: &taskguildv1.ExecuteScriptCommand{
				RequestId: requestID,
				ScriptId:  sc.ID,
				Filename:  sc.Filename,
				// Content is no longer sent; the agent reads from the local
				// .taskguild/scripts/{filename} file directly.
			},
		},
	})

	slog.Info("script execution requested",
		"project_id", projectID,
		"project_name", proj.Name,
		"script_id", sc.ID,
		"request_id", requestID,
	)

	return nil
}

// RequestScriptStop sends a StopScriptCommand to connected agent-managers
// for the project to cancel a running script execution.
func (s *Server) RequestScriptStop(projectID string, requestID string) error {
	proj, err := s.projectRepo.Get(context.Background(), projectID)
	if err != nil {
		return fmt.Errorf("failed to look up project: %w", err)
	}

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_StopScript{
			StopScript: &taskguildv1.StopScriptCommand{
				RequestId: requestID,
			},
		},
	})

	slog.Info("script stop requested",
		"project_id", projectID,
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return nil
}

func scriptToProto(s *script.Script) *taskguildv1.ScriptDefinition {
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
