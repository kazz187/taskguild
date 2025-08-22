package server

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/task"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// TaskServiceHandler implements the TaskService Connect handler
type TaskServiceHandler struct {
	service task.Service
}

// NewTaskServiceHandler creates a new TaskService handler
func NewTaskServiceHandler(service task.Service) *TaskServiceHandler {
	return &TaskServiceHandler{
		service: service,
	}
}

// PathAndHandler returns the Connect path and handler
func (h *TaskServiceHandler) PathAndHandler() (string, http.Handler) {
	return taskguildv1connect.NewTaskServiceHandler(h)
}

// CreateTask creates a new task
func (h *TaskServiceHandler) CreateTask(
	ctx context.Context,
	req *connect.Request[taskguildv1.CreateTaskRequest],
) (*connect.Response[taskguildv1.CreateTaskResponse], error) {
	taskReq := &task.CreateTaskRequest{
		Title:       req.Msg.Title,
		Description: req.Msg.Description,
		Type:        req.Msg.Type,
		Metadata:    req.Msg.Metadata,
	}

	createdTask, err := h.service.CreateTask(taskReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create task: %w", err))
	}

	protoTask, err := h.taskToProto(createdTask)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert task: %w", err))
	}

	return connect.NewResponse(&taskguildv1.CreateTaskResponse{
		Task: protoTask,
	}), nil
}

// ListTasks lists all tasks
func (h *TaskServiceHandler) ListTasks(
	ctx context.Context,
	req *connect.Request[taskguildv1.ListTasksRequest],
) (*connect.Response[taskguildv1.ListTasksResponse], error) {
	tasks, err := h.service.ListTasks()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list tasks: %w", err))
	}

	protoTasks := make([]*taskguildv1.Task, 0, len(tasks))
	for _, t := range tasks {
		protoTask, err := h.taskToProto(t)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert task: %w", err))
		}
		protoTasks = append(protoTasks, protoTask)
	}

	return connect.NewResponse(&taskguildv1.ListTasksResponse{
		Tasks: protoTasks,
		Total: int32(len(protoTasks)),
	}), nil
}

// GetTask gets a specific task
func (h *TaskServiceHandler) GetTask(
	ctx context.Context,
	req *connect.Request[taskguildv1.GetTaskRequest],
) (*connect.Response[taskguildv1.GetTaskResponse], error) {
	taskObj, err := h.service.GetTask(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("task not found: %w", err))
	}

	protoTask, err := h.taskToProto(taskObj)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert task: %w", err))
	}

	return connect.NewResponse(&taskguildv1.GetTaskResponse{
		Task: protoTask,
	}), nil
}

// UpdateTask updates a task
func (h *TaskServiceHandler) UpdateTask(
	ctx context.Context,
	req *connect.Request[taskguildv1.UpdateTaskRequest],
) (*connect.Response[taskguildv1.UpdateTaskResponse], error) {
	status, err := h.protoToTaskStatus(req.Msg.Status)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid status: %w", err))
	}

	updateReq := &task.UpdateTaskRequest{
		ID:          req.Msg.Id,
		Status:      status,
		Description: req.Msg.Description,
		Metadata:    req.Msg.Metadata,
	}

	updatedTask, err := h.service.UpdateTask(updateReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update task: %w", err))
	}

	protoTask, err := h.taskToProto(updatedTask)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert task: %w", err))
	}

	return connect.NewResponse(&taskguildv1.UpdateTaskResponse{
		Task: protoTask,
	}), nil
}

// CloseTask closes a task
func (h *TaskServiceHandler) CloseTask(
	ctx context.Context,
	req *connect.Request[taskguildv1.CloseTaskRequest],
) (*connect.Response[taskguildv1.CloseTaskResponse], error) {
	closedTask, err := h.service.CloseTask(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to close task: %w", err))
	}

	protoTask, err := h.taskToProto(closedTask)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert task: %w", err))
	}

	return connect.NewResponse(&taskguildv1.CloseTaskResponse{
		Task: protoTask,
	}), nil
}

// TryAcquireTask handles atomic task acquisition requests
func (h *TaskServiceHandler) TryAcquireTask(ctx context.Context, req *connect.Request[taskguildv1.TryAcquireTaskRequest]) (*connect.Response[taskguildv1.TryAcquireTaskResponse], error) {
	// Convert proto request to internal type
	expectedStatus, err := h.protoToTaskStatus(req.Msg.ExpectedStatus)
	if err != nil {
		return connect.NewResponse(&taskguildv1.TryAcquireTaskResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: fmt.Sprintf("invalid expected status: %v", err),
		}), nil
	}

	newStatus, err := h.protoToTaskStatus(req.Msg.NewStatus)
	if err != nil {
		return connect.NewResponse(&taskguildv1.TryAcquireTaskResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: fmt.Sprintf("invalid new status: %v", err),
		}), nil
	}

	acquireReq := &task.TryAcquireTaskRequest{
		ID:             req.Msg.Id,
		ExpectedStatus: expectedStatus,
		NewStatus:      newStatus,
		AgentID:        req.Msg.AgentId,
	}

	taskResult, err := h.service.TryAcquireTask(acquireReq)
	if err != nil {
		// Return response with error message but don't fail the RPC
		return connect.NewResponse(&taskguildv1.TryAcquireTaskResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: err.Error(),
		}), nil
	}

	protoTask, err := h.taskToProto(taskResult)
	if err != nil {
		return connect.NewResponse(&taskguildv1.TryAcquireTaskResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: fmt.Sprintf("failed to convert task to proto: %v", err),
		}), nil
	}

	return connect.NewResponse(&taskguildv1.TryAcquireTaskResponse{
		Task:         protoTask,
		Success:      true,
		ErrorMessage: "",
	}), nil
}

// ReleaseTask handles task release requests
func (h *TaskServiceHandler) ReleaseTask(ctx context.Context, req *connect.Request[taskguildv1.ReleaseTaskRequest]) (*connect.Response[taskguildv1.ReleaseTaskResponse], error) {
	err := h.service.ReleaseTask(req.Msg.Id, req.Msg.AgentId)
	if err != nil {
		// Return response with error message but don't fail the RPC
		return connect.NewResponse(&taskguildv1.ReleaseTaskResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}), nil
	}

	return connect.NewResponse(&taskguildv1.ReleaseTaskResponse{
		Success:      true,
		ErrorMessage: "",
	}), nil
}

// Helper methods for conversion

func (h *TaskServiceHandler) taskToProto(t *task.Task) (*taskguildv1.Task, error) {
	status, err := h.taskStatusToProto(t.Status)
	if err != nil {
		return nil, err
	}

	return &taskguildv1.Task{
		Id:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      status,
		Type:        t.Type,
		AssignedTo:  t.AssignedTo,
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
		Metadata:    t.Metadata,
	}, nil
}

func (h *TaskServiceHandler) taskStatusToProto(status string) (taskguildv1.TaskStatus, error) {
	switch status {
	case string(task.StatusCreated):
		return taskguildv1.TaskStatus_TASK_STATUS_CREATED, nil
	case string(task.StatusAnalyzing):
		return taskguildv1.TaskStatus_TASK_STATUS_ANALYZING, nil
	case string(task.StatusDesigned):
		return taskguildv1.TaskStatus_TASK_STATUS_DESIGNED, nil
	case string(task.StatusInProgress):
		return taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS, nil
	case string(task.StatusReviewReady):
		return taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY, nil
	case string(task.StatusQAReady):
		return taskguildv1.TaskStatus_TASK_STATUS_QA_READY, nil
	case string(task.StatusClosed):
		return taskguildv1.TaskStatus_TASK_STATUS_CLOSED, nil
	case string(task.StatusCancelled):
		return taskguildv1.TaskStatus_TASK_STATUS_CANCELLED, nil
	default:
		return taskguildv1.TaskStatus_TASK_STATUS_UNSPECIFIED, fmt.Errorf("unknown task status: %s", status)
	}
}

func (h *TaskServiceHandler) protoToTaskStatus(status taskguildv1.TaskStatus) (task.Status, error) {
	switch status {
	case taskguildv1.TaskStatus_TASK_STATUS_CREATED:
		return task.StatusCreated, nil
	case taskguildv1.TaskStatus_TASK_STATUS_ANALYZING:
		return task.StatusAnalyzing, nil
	case taskguildv1.TaskStatus_TASK_STATUS_DESIGNED:
		return task.StatusDesigned, nil
	case taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return task.StatusInProgress, nil
	case taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY:
		return task.StatusReviewReady, nil
	case taskguildv1.TaskStatus_TASK_STATUS_QA_READY:
		return task.StatusQAReady, nil
	case taskguildv1.TaskStatus_TASK_STATUS_CLOSED:
		return task.StatusClosed, nil
	case taskguildv1.TaskStatus_TASK_STATUS_CANCELLED:
		return task.StatusCancelled, nil
	default:
		return "", fmt.Errorf("unknown task status: %v", status)
	}
}
