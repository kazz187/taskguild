package agentmanager

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Git pull main RPCs ---

func (s *Server) RequestGitPullMain(ctx context.Context, req *taskguildv1.RequestGitPullMainRequest) (*taskguildv1.RequestGitPullMainResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
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
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return &taskguildv1.RequestGitPullMainResponse{
		RequestId: requestID,
	}, nil
}

func (s *Server) ReportGitPullMainResult(ctx context.Context, req *taskguildv1.ReportGitPullMainResultRequest) (*taskguildv1.ReportGitPullMainResultResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, err
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_GIT_PULL_MAIN_RESULT,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID:    proj.ID,
			eventbus.MetaRequestID:    req.GetRequestId(),
			eventbus.MetaSuccess:      strconv.FormatBool(req.GetSuccess()),
			eventbus.MetaOutput:       req.GetOutput(),
			eventbus.MetaErrorMessage: req.GetErrorMessage(),
		},
	)

	slog.Info("git pull main result reported",
		"project_id", proj.ID,
		"success", req.GetSuccess(),
		"request_id", req.GetRequestId(),
	)

	return &taskguildv1.ReportGitPullMainResultResponse{}, nil
}
