package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

type TaskGuildClient struct {
	client taskguildv1connect.TaskServiceClient
}

func NewTaskGuildClient(cfg *Config) (*TaskGuildClient, error) {
	client := taskguildv1connect.NewTaskServiceClient(
		http.DefaultClient,
		cfg.TaskGuildAddr,
		connect.WithGRPC(),
	)

	return &TaskGuildClient{
		client: client,
	}, nil
}

func (c *TaskGuildClient) Close() error {
	// No connection to close with Connect
	return nil
}

func (c *TaskGuildClient) ListTasksHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[ListTasksInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments
	connectReq := connect.NewRequest(&taskguildv1.ListTasksRequest{
		StatusFilter: req.StatusFilter,
		TypeFilter:   req.TypeFilter,
		Limit:        int32(req.Limit),
		Offset:       int32(req.Offset),
	})

	resp, err := c.client.ListTasks(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error listing tasks: %v", err),
				},
			},
		}, nil
	}

	result := make([]map[string]interface{}, len(resp.Msg.Tasks))
	for i, task := range resp.Msg.Tasks {
		result[i] = taskToMap(task)
	}

	resultData := map[string]interface{}{
		"tasks": result,
		"total": resp.Msg.Total,
	}

	jsonData, _ := json.MarshalIndent(resultData, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func (c *TaskGuildClient) GetTaskHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[GetTaskInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments
	connectReq := connect.NewRequest(&taskguildv1.GetTaskRequest{
		Id: req.ID,
	})

	resp, err := c.client.GetTask(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error getting task: %v", err),
				},
			},
		}, nil
	}

	result := taskToMap(resp.Msg.Task)
	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func (c *TaskGuildClient) CreateTaskHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[CreateTaskInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments
	connectReq := connect.NewRequest(&taskguildv1.CreateTaskRequest{
		Title:       req.Title,
		Description: req.Description,
		Type:        req.Type,
		Metadata:    req.Metadata,
	})

	resp, err := c.client.CreateTask(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error creating task: %v", err),
				},
			},
		}, nil
	}

	result := taskToMap(resp.Msg.Task)
	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func (c *TaskGuildClient) UpdateTaskHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[UpdateTaskInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments
	status := taskguildv1.TaskStatus_TASK_STATUS_UNSPECIFIED
	if req.Status != "" {
		if s, ok := taskStatusMap[req.Status]; ok {
			status = s
		}
	}

	connectReq := connect.NewRequest(&taskguildv1.UpdateTaskRequest{
		Id:          req.ID,
		Status:      status,
		Description: req.Description,
		Metadata:    req.Metadata,
	})

	resp, err := c.client.UpdateTask(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error updating task: %v", err),
				},
			},
		}, nil
	}

	result := taskToMap(resp.Msg.Task)
	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func (c *TaskGuildClient) CloseTaskHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[CloseTaskInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments
	connectReq := connect.NewRequest(&taskguildv1.CloseTaskRequest{
		Id:     req.ID,
		Reason: req.Reason,
	})

	resp, err := c.client.CloseTask(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error closing task: %v", err),
				},
			},
		}, nil
	}

	result := taskToMap(resp.Msg.Task)
	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func taskToMap(task *taskguildv1.Task) map[string]interface{} {
	return map[string]interface{}{
		"id":          task.Id,
		"title":       task.Title,
		"description": task.Description,
		"status":      taskStatusToString(task.Status),
		"type":        task.Type,
		"assignedTo":  task.AssignedTo,
		"createdAt":   task.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		"updatedAt":   task.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		"metadata":    task.Metadata,
	}
}

var taskStatusMap = map[string]taskguildv1.TaskStatus{
	"CREATED":      taskguildv1.TaskStatus_TASK_STATUS_CREATED,
	"ANALYZING":    taskguildv1.TaskStatus_TASK_STATUS_ANALYZING,
	"DESIGNED":     taskguildv1.TaskStatus_TASK_STATUS_DESIGNED,
	"IN_PROGRESS":  taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS,
	"REVIEW_READY": taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY,
	"QA_READY":     taskguildv1.TaskStatus_TASK_STATUS_QA_READY,
	"CLOSED":       taskguildv1.TaskStatus_TASK_STATUS_CLOSED,
	"CANCELLED":    taskguildv1.TaskStatus_TASK_STATUS_CANCELLED,
}

func taskStatusToString(status taskguildv1.TaskStatus) string {
	switch status {
	case taskguildv1.TaskStatus_TASK_STATUS_CREATED:
		return "CREATED"
	case taskguildv1.TaskStatus_TASK_STATUS_ANALYZING:
		return "ANALYZING"
	case taskguildv1.TaskStatus_TASK_STATUS_DESIGNED:
		return "DESIGNED"
	case taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "IN_PROGRESS"
	case taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY:
		return "REVIEW_READY"
	case taskguildv1.TaskStatus_TASK_STATUS_QA_READY:
		return "QA_READY"
	case taskguildv1.TaskStatus_TASK_STATUS_CLOSED:
		return "CLOSED"
	case taskguildv1.TaskStatus_TASK_STATUS_CANCELLED:
		return "CANCELLED"
	default:
		return "UNSPECIFIED"
	}
}
