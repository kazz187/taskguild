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

// --- Worktree management RPCs ---

func (s *Server) RequestWorktreeList(ctx context.Context, req *taskguildv1.RequestWorktreeListRequest) (*taskguildv1.RequestWorktreeListResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send ListWorktreesCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_ListWorktrees{
			ListWorktrees: &taskguildv1.ListWorktreesCommand{
				RequestId: requestID,
			},
		},
	})

	slog.Info("worktree list requested",
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return &taskguildv1.RequestWorktreeListResponse{
		RequestId: requestID,
	}, nil
}

func (s *Server) ReportWorktreeList(ctx context.Context, req *taskguildv1.ReportWorktreeListRequest) (*taskguildv1.ReportWorktreeListResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the worktree list for this project.
	s.worktreeMu.Lock()
	s.worktreeCache[proj.ID] = req.GetWorktrees()
	s.worktreeMu.Unlock()

	// Publish event so frontend can pick up the update.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_WORKTREE_LIST,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID: proj.ID,
			eventbus.MetaRequestID: req.GetRequestId(),
		},
	)

	slog.Info("worktree list reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.GetRequestId(),
		"count", len(req.GetWorktrees()),
	)

	return &taskguildv1.ReportWorktreeListResponse{}, nil
}

func (s *Server) GetWorktreeList(ctx context.Context, req *taskguildv1.GetWorktreeListRequest) (*taskguildv1.GetWorktreeListResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.worktreeMu.RLock()
	worktrees := s.worktreeCache[req.GetProjectId()]
	s.worktreeMu.RUnlock()

	return &taskguildv1.GetWorktreeListResponse{
		Worktrees: worktrees,
	}, nil
}

func (s *Server) RequestWorktreeDelete(ctx context.Context, req *taskguildv1.RequestWorktreeDeleteRequest) (*taskguildv1.RequestWorktreeDeleteResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	if req.GetWorktreeName() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "worktree_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send DeleteWorktreeCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_DeleteWorktree{
			DeleteWorktree: &taskguildv1.DeleteWorktreeCommand{
				RequestId:    requestID,
				WorktreeName: req.GetWorktreeName(),
				Force:        req.GetForce(),
			},
		},
	})

	slog.Info("worktree delete requested",
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"worktree_name", req.GetWorktreeName(),
		"force", req.GetForce(),
		"request_id", requestID,
	)

	return &taskguildv1.RequestWorktreeDeleteResponse{
		RequestId: requestID,
	}, nil
}

func (s *Server) ReportWorktreeDeleteResult(ctx context.Context, req *taskguildv1.ReportWorktreeDeleteResultRequest) (*taskguildv1.ReportWorktreeDeleteResultResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// If deletion was successful, remove the worktree from the cache.
	if req.GetSuccess() {
		s.worktreeMu.Lock()
		if cached, ok := s.worktreeCache[proj.ID]; ok {
			filtered := make([]*taskguildv1.WorktreeInfo, 0, len(cached))
			for _, wt := range cached {
				if wt.GetName() != req.GetWorktreeName() {
					filtered = append(filtered, wt)
				}
			}

			s.worktreeCache[proj.ID] = filtered
		}
		s.worktreeMu.Unlock()
	}

	// Publish event so frontend can pick up the result.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_WORKTREE_DELETED,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID:    proj.ID,
			eventbus.MetaRequestID:    req.GetRequestId(),
			eventbus.MetaWorktreeName: req.GetWorktreeName(),
			eventbus.MetaSuccess:      strconv.FormatBool(req.GetSuccess()),
			eventbus.MetaErrorMessage: req.GetErrorMessage(),
		},
	)

	slog.Info("worktree delete result reported",
		"project_id", proj.ID,
		"worktree_name", req.GetWorktreeName(),
		"success", req.GetSuccess(),
		"error_message", req.GetErrorMessage(),
	)

	return &taskguildv1.ReportWorktreeDeleteResultResponse{}, nil
}
