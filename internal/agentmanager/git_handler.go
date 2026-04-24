package agentmanager

import (
	"context"
	"log/slog"
	"strconv"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Git pull main RPCs ---

func (s *Server) RequestGitPullMain(ctx context.Context, req *connect.Request[taskguildv1.RequestGitPullMainRequest]) (*connect.Response[taskguildv1.RequestGitPullMainResponse], error) {
	if req.Msg.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send GitPullMainCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_GitPullMain{
			GitPullMain: &taskguildv1.GitPullMainCommand{
				RequestId: requestID,
			},
		},
	})

	slog.Info("git pull main requested",
		"project_id", req.Msg.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestGitPullMainResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportGitPullMainResult(ctx context.Context, req *connect.Request[taskguildv1.ReportGitPullMainResultRequest]) (*connect.Response[taskguildv1.ReportGitPullMainResultResponse], error) {
	projectName := req.Msg.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_GIT_PULL_MAIN_RESULT,
		req.Msg.GetRequestId(),
		"",
		map[string]string{
			"project_id":    proj.ID,
			"request_id":    req.Msg.GetRequestId(),
			"success":       strconv.FormatBool(req.Msg.GetSuccess()),
			"output":        req.Msg.GetOutput(),
			"error_message": req.Msg.GetErrorMessage(),
		},
	)

	slog.Info("git pull main result reported",
		"project_id", proj.ID,
		"success", req.Msg.GetSuccess(),
		"request_id", req.Msg.GetRequestId(),
	)

	return connect.NewResponse(&taskguildv1.ReportGitPullMainResultResponse{}), nil
}
