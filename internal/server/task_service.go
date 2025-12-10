package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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

	protoTask := h.taskToProto(createdTask)

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
		protoTask := h.taskToProto(t)
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

	protoTask := h.taskToProto(taskObj)

	return connect.NewResponse(&taskguildv1.GetTaskResponse{
		Task: protoTask,
	}), nil
}

// UpdateTask updates a task metadata
func (h *TaskServiceHandler) UpdateTask(
	ctx context.Context,
	req *connect.Request[taskguildv1.UpdateTaskRequest],
) (*connect.Response[taskguildv1.UpdateTaskResponse], error) {
	updateReq := &task.UpdateTaskRequest{
		ID:          req.Msg.Id,
		Description: req.Msg.Description,
		Metadata:    req.Msg.Metadata,
	}

	updatedTask, err := h.service.UpdateTask(updateReq)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update task: %w", err))
	}

	protoTask := h.taskToProto(updatedTask)

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

	protoTask := h.taskToProto(closedTask)

	return connect.NewResponse(&taskguildv1.CloseTaskResponse{
		Task: protoTask,
	}), nil
}

// TryAcquireProcess handles atomic process acquisition
func (h *TaskServiceHandler) TryAcquireProcess(ctx context.Context, req *connect.Request[taskguildv1.TryAcquireProcessRequest]) (*connect.Response[taskguildv1.TryAcquireProcessResponse], error) {
	acquireReq := &task.TryAcquireProcessRequest{
		TaskID:      req.Msg.TaskId,
		ProcessName: req.Msg.ProcessName,
		AgentID:     req.Msg.AgentId,
	}

	taskResult, err := h.service.TryAcquireProcess(acquireReq)
	if err != nil {
		return connect.NewResponse(&taskguildv1.TryAcquireProcessResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: err.Error(),
		}), nil
	}

	protoTask := h.taskToProto(taskResult)

	return connect.NewResponse(&taskguildv1.TryAcquireProcessResponse{
		Task:         protoTask,
		Success:      true,
		ErrorMessage: "",
	}), nil
}

// CompleteProcess marks a process as completed
func (h *TaskServiceHandler) CompleteProcess(ctx context.Context, req *connect.Request[taskguildv1.CompleteProcessRequest]) (*connect.Response[taskguildv1.CompleteProcessResponse], error) {
	err := h.service.CompleteProcess(req.Msg.TaskId, req.Msg.ProcessName, req.Msg.AgentId)
	if err != nil {
		return connect.NewResponse(&taskguildv1.CompleteProcessResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: err.Error(),
		}), nil
	}

	// Get updated task
	taskResult, err := h.service.GetTask(req.Msg.TaskId)
	if err != nil {
		return connect.NewResponse(&taskguildv1.CompleteProcessResponse{
			Task:         nil,
			Success:      false,
			ErrorMessage: fmt.Sprintf("process completed but failed to get task: %v", err),
		}), nil
	}

	protoTask := h.taskToProto(taskResult)

	return connect.NewResponse(&taskguildv1.CompleteProcessResponse{
		Task:         protoTask,
		Success:      true,
		ErrorMessage: "",
	}), nil
}

// RejectProcess marks a process as rejected and cascades reset to dependencies
func (h *TaskServiceHandler) RejectProcess(ctx context.Context, req *connect.Request[taskguildv1.RejectProcessRequest]) (*connect.Response[taskguildv1.RejectProcessResponse], error) {
	err := h.service.RejectProcess(req.Msg.TaskId, req.Msg.ProcessName, req.Msg.AgentId, req.Msg.Reason)
	if err != nil {
		return connect.NewResponse(&taskguildv1.RejectProcessResponse{
			Task:           nil,
			Success:        false,
			ErrorMessage:   err.Error(),
			ResetProcesses: nil,
		}), nil
	}

	// Get updated task
	taskResult, err := h.service.GetTask(req.Msg.TaskId)
	if err != nil {
		return connect.NewResponse(&taskguildv1.RejectProcessResponse{
			Task:           nil,
			Success:        false,
			ErrorMessage:   fmt.Sprintf("process rejected but failed to get task: %v", err),
			ResetProcesses: nil,
		}), nil
	}

	protoTask := h.taskToProto(taskResult)

	// Find reset processes (those that are now pending but were in progress/completed before)
	resetProcesses := make([]string, 0)
	for name, state := range taskResult.Processes {
		if state.Status == task.ProcessStatusPending && name != req.Msg.ProcessName {
			resetProcesses = append(resetProcesses, name)
		}
	}

	return connect.NewResponse(&taskguildv1.RejectProcessResponse{
		Task:           protoTask,
		Success:        true,
		ErrorMessage:   "",
		ResetProcesses: resetProcesses,
	}), nil
}

// Helper methods for conversion

func (h *TaskServiceHandler) taskToProto(t *task.Task) *taskguildv1.Task {
	// Convert overall status from process states
	status := h.overallStatusToProto(t.GetOverallStatus())

	// Build process status string for metadata
	var processStatusParts []string
	for name, state := range t.Processes {
		processStatusParts = append(processStatusParts, fmt.Sprintf("%s:%s", name, state.Status))
	}

	// Merge process info into metadata
	metadata := make(map[string]string)
	if t.Metadata != nil {
		for k, v := range t.Metadata {
			metadata[k] = v
		}
	}
	metadata["processes"] = strings.Join(processStatusParts, ",")

	// Convert process states to proto
	protoProcesses := make(map[string]*taskguildv1.ProcessState)
	for name, state := range t.Processes {
		protoProcesses[name] = h.processStateToProto(state)
	}

	return &taskguildv1.Task{
		Id:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      status,
		Type:        t.Type,
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
		Metadata:    metadata,
		Processes:   protoProcesses,
	}
}

func (h *TaskServiceHandler) processStateToProto(state *task.ProcessState) *taskguildv1.ProcessState {
	protoStatus := taskguildv1.ProcessStatus_PROCESS_STATUS_UNSPECIFIED
	switch state.Status {
	case task.ProcessStatusPending:
		protoStatus = taskguildv1.ProcessStatus_PROCESS_STATUS_PENDING
	case task.ProcessStatusInProgress:
		protoStatus = taskguildv1.ProcessStatus_PROCESS_STATUS_IN_PROGRESS
	case task.ProcessStatusCompleted:
		protoStatus = taskguildv1.ProcessStatus_PROCESS_STATUS_COMPLETED
	case task.ProcessStatusRejected:
		protoStatus = taskguildv1.ProcessStatus_PROCESS_STATUS_REJECTED
	}

	return &taskguildv1.ProcessState{
		Status:     protoStatus,
		AssignedTo: state.AssignedTo,
	}
}

func (h *TaskServiceHandler) overallStatusToProto(status string) taskguildv1.TaskStatus {
	switch status {
	case "CLOSED":
		return taskguildv1.TaskStatus_TASK_STATUS_CLOSED
	case "IN_PROGRESS":
		return taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	case "PENDING":
		return taskguildv1.TaskStatus_TASK_STATUS_PENDING
	case "REJECTED":
		return taskguildv1.TaskStatus_TASK_STATUS_REJECTED
	case "COMPLETED":
		return taskguildv1.TaskStatus_TASK_STATUS_COMPLETED
	default:
		return taskguildv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	}
}
