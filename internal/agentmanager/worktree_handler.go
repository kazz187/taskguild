package agentmanager

import (
	"context"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Worktree management RPCs ---

func (s *Server) RequestWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.RequestWorktreeListRequest]) (*connect.Response[taskguildv1.RequestWorktreeListResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
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
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestWorktreeListResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.ReportWorktreeListRequest]) (*connect.Response[taskguildv1.ReportWorktreeListResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the worktree list for this project.
	s.worktreeMu.Lock()
	s.worktreeCache[proj.ID] = req.Msg.Worktrees
	s.worktreeMu.Unlock()

	// Publish event so frontend can pick up the update.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_WORKTREE_LIST,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":  proj.ID,
			"request_id":  req.Msg.RequestId,
		},
	)

	slog.Info("worktree list reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.RequestId,
		"count", len(req.Msg.Worktrees),
	)

	return connect.NewResponse(&taskguildv1.ReportWorktreeListResponse{}), nil
}

func (s *Server) GetWorktreeList(ctx context.Context, req *connect.Request[taskguildv1.GetWorktreeListRequest]) (*connect.Response[taskguildv1.GetWorktreeListResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.worktreeMu.RLock()
	worktrees := s.worktreeCache[req.Msg.ProjectId]
	s.worktreeMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetWorktreeListResponse{
		Worktrees: worktrees,
	}), nil
}

func (s *Server) RequestWorktreeDelete(ctx context.Context, req *connect.Request[taskguildv1.RequestWorktreeDeleteRequest]) (*connect.Response[taskguildv1.RequestWorktreeDeleteResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}
	if req.Msg.WorktreeName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "worktree_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	requestID := ulid.Make().String()

	// Send DeleteWorktreeCommand to connected agent-managers for this project.
	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_DeleteWorktree{
			DeleteWorktree: &taskguildv1.DeleteWorktreeCommand{
				RequestId:    requestID,
				WorktreeName: req.Msg.WorktreeName,
				Force:        req.Msg.Force,
			},
		},
	})

	slog.Info("worktree delete requested",
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"worktree_name", req.Msg.WorktreeName,
		"force", req.Msg.Force,
		"request_id", requestID,
	)

	return connect.NewResponse(&taskguildv1.RequestWorktreeDeleteResponse{
		RequestId: requestID,
	}), nil
}

func (s *Server) ReportWorktreeDeleteResult(ctx context.Context, req *connect.Request[taskguildv1.ReportWorktreeDeleteResultRequest]) (*connect.Response[taskguildv1.ReportWorktreeDeleteResultResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// If deletion was successful, remove the worktree from the cache.
	if req.Msg.Success {
		s.worktreeMu.Lock()
		if cached, ok := s.worktreeCache[proj.ID]; ok {
			filtered := make([]*taskguildv1.WorktreeInfo, 0, len(cached))
			for _, wt := range cached {
				if wt.Name != req.Msg.WorktreeName {
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
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id":    proj.ID,
			"request_id":    req.Msg.RequestId,
			"worktree_name": req.Msg.WorktreeName,
			"success":       fmt.Sprintf("%v", req.Msg.Success),
			"error_message": req.Msg.ErrorMessage,
		},
	)

	slog.Info("worktree delete result reported",
		"project_id", proj.ID,
		"worktree_name", req.Msg.WorktreeName,
		"success", req.Msg.Success,
		"error_message", req.Msg.ErrorMessage,
	)

	return connect.NewResponse(&taskguildv1.ReportWorktreeDeleteResultResponse{}), nil
}
