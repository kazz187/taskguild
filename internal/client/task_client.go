package client

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// TaskClient provides client operations for tasks
type TaskClient struct {
	client taskguildv1connect.TaskServiceClient
}

// NewTaskClient creates a new task client
func NewTaskClient(baseURL string) *TaskClient {
	client := taskguildv1connect.NewTaskServiceClient(
		http.DefaultClient,
		baseURL,
	)

	return &TaskClient{
		client: client,
	}
}

// CreateTask creates a new task
func (c *TaskClient) CreateTask(ctx context.Context, title, description, taskType string) (*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.CreateTaskRequest{
		Title:       title,
		Description: description,
		Type:        taskType,
	})

	resp, err := c.client.CreateTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return resp.Msg.Task, nil
}

// ListTasks lists all tasks
func (c *TaskClient) ListTasks(ctx context.Context) ([]*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.ListTasksRequest{})

	resp, err := c.client.ListTasks(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	return resp.Msg.Tasks, nil
}

// GetTask gets a specific task
func (c *TaskClient) GetTask(ctx context.Context, taskID string) (*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.GetTaskRequest{
		Id: taskID,
	})

	resp, err := c.client.GetTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}

	return resp.Msg.Task, nil
}

// UpdateTask updates a task's description and metadata
func (c *TaskClient) UpdateTask(ctx context.Context, taskID string, description string, metadata map[string]string) (*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.UpdateTaskRequest{
		Id:          taskID,
		Description: description,
		Metadata:    metadata,
	})

	resp, err := c.client.UpdateTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return resp.Msg.Task, nil
}

// TryAcquireProcess attempts to atomically acquire a process for an agent
func (c *TaskClient) TryAcquireProcess(ctx context.Context, taskID, processName, agentID string) (*taskguildv1.Task, bool, error) {
	req := connect.NewRequest(&taskguildv1.TryAcquireProcessRequest{
		TaskId:      taskID,
		ProcessName: processName,
		AgentId:     agentID,
	})

	resp, err := c.client.TryAcquireProcess(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to try acquire process: %w", err)
	}

	return resp.Msg.Task, resp.Msg.Success, nil
}

// CompleteProcess marks a process as completed
func (c *TaskClient) CompleteProcess(ctx context.Context, taskID, processName, agentID string) (*taskguildv1.Task, bool, error) {
	req := connect.NewRequest(&taskguildv1.CompleteProcessRequest{
		TaskId:      taskID,
		ProcessName: processName,
		AgentId:     agentID,
	})

	resp, err := c.client.CompleteProcess(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("failed to complete process: %w", err)
	}

	return resp.Msg.Task, resp.Msg.Success, nil
}

// RejectProcess marks a process as rejected with cascade reset
func (c *TaskClient) RejectProcess(ctx context.Context, taskID, processName, agentID, reason string) (*taskguildv1.Task, bool, []string, error) {
	req := connect.NewRequest(&taskguildv1.RejectProcessRequest{
		TaskId:      taskID,
		ProcessName: processName,
		AgentId:     agentID,
		Reason:      reason,
	})

	resp, err := c.client.RejectProcess(ctx, req)
	if err != nil {
		return nil, false, nil, fmt.Errorf("failed to reject process: %w", err)
	}

	return resp.Msg.Task, resp.Msg.Success, resp.Msg.ResetProcesses, nil
}

// CloseTask closes a task
func (c *TaskClient) CloseTask(ctx context.Context, taskID string) (*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.CloseTaskRequest{
		Id: taskID,
	})

	resp, err := c.client.CloseTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to close task: %w", err)
	}

	return resp.Msg.Task, nil
}
