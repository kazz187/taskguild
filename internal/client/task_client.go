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

// UpdateTask updates a task
func (c *TaskClient) UpdateTask(ctx context.Context, taskID string, status taskguildv1.TaskStatus) (*taskguildv1.Task, error) {
	req := connect.NewRequest(&taskguildv1.UpdateTaskRequest{
		Id:     taskID,
		Status: status,
	})

	resp, err := c.client.UpdateTask(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return resp.Msg.Task, nil
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
