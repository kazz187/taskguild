package task

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.TaskServiceHandler = (*Server)(nil)

type Server struct {
	repo         Repository
	workflowRepo workflow.Repository
	eventBus     *eventbus.Bus
}

func NewServer(repo Repository, workflowRepo workflow.Repository, eventBus *eventbus.Bus) *Server {
	return &Server{
		repo:         repo,
		workflowRepo: workflowRepo,
		eventBus:     eventBus,
	}
}

func (s *Server) CreateTask(ctx context.Context, req *connect.Request[taskguildv1.CreateTaskRequest]) (*connect.Response[taskguildv1.CreateTaskResponse], error) {
	// Fetch workflow to find initial status.
	wf, err := s.workflowRepo.Get(ctx, req.Msg.WorkflowId)
	if err != nil {
		return nil, err
	}

	var initialStatusID string
	for _, st := range wf.Statuses {
		if st.IsInitial {
			initialStatusID = st.ID
			break
		}
	}
	if initialStatusID == "" {
		return nil, cerr.NewError(cerr.FailedPrecondition, "workflow has no initial status", nil).ConnectError()
	}

	now := time.Now()
	t := &Task{
		ID:               ulid.Make().String(),
		ProjectID:        req.Msg.ProjectId,
		WorkflowID:       req.Msg.WorkflowId,
		Title:            req.Msg.Title,
		Description:      req.Msg.Description,
		StatusID:         initialStatusID,
		AssignmentStatus: AssignmentStatusUnassigned,
		Metadata:         req.Msg.Metadata,
		UseWorktree:      req.Msg.UseWorktree,
		PermissionMode:   req.Msg.PermissionMode,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_CREATED,
		t.ID,
		"",
		map[string]string{"project_id": t.ProjectID, "workflow_id": t.WorkflowID},
	)

	return connect.NewResponse(&taskguildv1.CreateTaskResponse{
		Task: toProto(t),
	}), nil
}

func (s *Server) GetTask(ctx context.Context, req *connect.Request[taskguildv1.GetTaskRequest]) (*connect.Response[taskguildv1.GetTaskResponse], error) {
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetTaskResponse{
		Task: toProto(t),
	}), nil
}

func (s *Server) ListTasks(ctx context.Context, req *connect.Request[taskguildv1.ListTasksRequest]) (*connect.Response[taskguildv1.ListTasksResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	tasks, total, err := s.repo.List(ctx, req.Msg.ProjectId, req.Msg.WorkflowId, req.Msg.StatusId, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.Task, len(tasks))
	for i, t := range tasks {
		protos[i] = toProto(t)
	}
	return connect.NewResponse(&taskguildv1.ListTasksResponse{
		Tasks: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateTask(ctx context.Context, req *connect.Request[taskguildv1.UpdateTaskRequest]) (*connect.Response[taskguildv1.UpdateTaskResponse], error) {
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Title != "" {
		t.Title = req.Msg.Title
	}
	if req.Msg.Description != "" {
		t.Description = req.Msg.Description
	}
	if req.Msg.Metadata != nil {
		if t.Metadata == nil {
			t.Metadata = make(map[string]string)
		}
		for k, v := range req.Msg.Metadata {
			t.Metadata[k] = v
		}
	}
	if req.Msg.UseWorktree != nil {
		t.UseWorktree = *req.Msg.UseWorktree
	}
	if req.Msg.PermissionMode != nil {
		t.PermissionMode = *req.Msg.PermissionMode
	}
	t.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_UPDATED,
		t.ID,
		"",
		map[string]string{"project_id": t.ProjectID, "workflow_id": t.WorkflowID},
	)

	return connect.NewResponse(&taskguildv1.UpdateTaskResponse{
		Task: toProto(t),
	}), nil
}

func (s *Server) DeleteTask(ctx context.Context, req *connect.Request[taskguildv1.DeleteTaskRequest]) (*connect.Response[taskguildv1.DeleteTaskResponse], error) {
	// Get task before delete for event metadata.
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_DELETED,
		req.Msg.Id,
		"",
		map[string]string{"project_id": t.ProjectID, "workflow_id": t.WorkflowID},
	)

	return connect.NewResponse(&taskguildv1.DeleteTaskResponse{}), nil
}

func (s *Server) UpdateTaskStatus(ctx context.Context, req *connect.Request[taskguildv1.UpdateTaskStatusRequest]) (*connect.Response[taskguildv1.UpdateTaskStatusResponse], error) {
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	// Validate transition.
	wf, err := s.workflowRepo.Get(ctx, t.WorkflowID)
	if err != nil {
		return nil, err
	}

	var currentStatus *workflow.Status
	for i := range wf.Statuses {
		if wf.Statuses[i].ID == t.StatusID {
			currentStatus = &wf.Statuses[i]
			break
		}
	}
	if currentStatus == nil {
		return nil, cerr.NewError(cerr.Internal, "current status not found in workflow", nil).ConnectError()
	}

	allowed := false
	for _, to := range currentStatus.TransitionsTo {
		if to == req.Msg.StatusId {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, cerr.NewError(
			cerr.FailedPrecondition,
			fmt.Sprintf("transition from %q to %q is not allowed", currentStatus.Name, req.Msg.StatusId),
			nil,
		).ConnectError()
	}

	t.StatusID = req.Msg.StatusId
	t.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_TASK_STATUS_CHANGED,
		t.ID,
		"",
		map[string]string{
			"project_id":    t.ProjectID,
			"workflow_id":   t.WorkflowID,
			"new_status_id": req.Msg.StatusId,
		},
	)

	return connect.NewResponse(&taskguildv1.UpdateTaskStatusResponse{
		Task: toProto(t),
	}), nil
}

func toProto(t *Task) *taskguildv1.Task {
	return &taskguildv1.Task{
		Id:               t.ID,
		ProjectId:        t.ProjectID,
		WorkflowId:       t.WorkflowID,
		Title:            t.Title,
		Description:      t.Description,
		StatusId:         t.StatusID,
		AssignedAgentId:  t.AssignedAgentID,
		AssignmentStatus: assignmentStatusToProto(t.AssignmentStatus),
		Metadata:         t.Metadata,
		UseWorktree:      t.UseWorktree,
		PermissionMode:   t.PermissionMode,
		CreatedAt:        timestamppb.New(t.CreatedAt),
		UpdatedAt:        timestamppb.New(t.UpdatedAt),
	}
}

func assignmentStatusToProto(s AssignmentStatus) taskguildv1.TaskAssignmentStatus {
	switch s {
	case AssignmentStatusUnassigned:
		return taskguildv1.TaskAssignmentStatus_TASK_ASSIGNMENT_STATUS_UNASSIGNED
	case AssignmentStatusPending:
		return taskguildv1.TaskAssignmentStatus_TASK_ASSIGNMENT_STATUS_PENDING
	case AssignmentStatusAssigned:
		return taskguildv1.TaskAssignmentStatus_TASK_ASSIGNMENT_STATUS_ASSIGNED
	default:
		return taskguildv1.TaskAssignmentStatus_TASK_ASSIGNMENT_STATUS_UNSPECIFIED
	}
}
