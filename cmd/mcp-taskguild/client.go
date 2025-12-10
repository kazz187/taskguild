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

	connectReq := connect.NewRequest(&taskguildv1.UpdateTaskRequest{
		Id:          req.ID,
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
	processes := make(map[string]interface{})
	for name, state := range task.Processes {
		processes[name] = map[string]interface{}{
			"status":     processStatusToString(state.Status),
			"assignedTo": state.AssignedTo,
		}
	}

	return map[string]interface{}{
		"id":          task.Id,
		"title":       task.Title,
		"description": task.Description,
		"status":      taskStatusToString(task.Status),
		"type":        task.Type,
		"createdAt":   task.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		"updatedAt":   task.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		"metadata":    task.Metadata,
		"processes":   processes,
	}
}

func processStatusToString(status taskguildv1.ProcessStatus) string {
	switch status {
	case taskguildv1.ProcessStatus_PROCESS_STATUS_PENDING:
		return "PENDING"
	case taskguildv1.ProcessStatus_PROCESS_STATUS_IN_PROGRESS:
		return "IN_PROGRESS"
	case taskguildv1.ProcessStatus_PROCESS_STATUS_COMPLETED:
		return "COMPLETED"
	case taskguildv1.ProcessStatus_PROCESS_STATUS_REJECTED:
		return "REJECTED"
	default:
		return "UNSPECIFIED"
	}
}

func (c *TaskGuildClient) CompleteProcessHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[CompleteProcessInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments

	connectReq := connect.NewRequest(&taskguildv1.CompleteProcessRequest{
		TaskId:      req.TaskID,
		ProcessName: req.ProcessName,
		AgentId:     req.AgentID,
	})

	resp, err := c.client.CompleteProcess(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error completing process: %v", err),
				},
			},
		}, nil
	}

	if !resp.Msg.Success {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Failed to complete process: %s", resp.Msg.ErrorMessage),
				},
			},
		}, nil
	}

	result := map[string]interface{}{
		"success":      true,
		"task_id":      req.TaskID,
		"process_name": req.ProcessName,
		"task":         taskToMap(resp.Msg.Task),
		"message":      fmt.Sprintf("Process %s completed successfully", req.ProcessName),
	}

	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func (c *TaskGuildClient) RejectProcessHandler(ctx context.Context, _ *mcp.ServerSession, params *mcp.CallToolParamsFor[RejectProcessInput]) (*mcp.CallToolResultFor[any], error) {
	req := params.Arguments

	connectReq := connect.NewRequest(&taskguildv1.RejectProcessRequest{
		TaskId:      req.TaskID,
		ProcessName: req.ProcessName,
		AgentId:     req.AgentID,
		Reason:      req.Reason,
	})

	resp, err := c.client.RejectProcess(ctx, connectReq)
	if err != nil {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Error rejecting process: %v", err),
				},
			},
		}, nil
	}

	if !resp.Msg.Success {
		return &mcp.CallToolResultFor[any]{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: fmt.Sprintf("Failed to reject process: %s", resp.Msg.ErrorMessage),
				},
			},
		}, nil
	}

	result := map[string]interface{}{
		"success":         true,
		"task_id":         req.TaskID,
		"process_name":    req.ProcessName,
		"reason":          req.Reason,
		"task":            taskToMap(resp.Msg.Task),
		"reset_processes": resp.Msg.ResetProcesses,
		"message":         fmt.Sprintf("Process %s rejected: %s. Dependencies have been reset to pending.", req.ProcessName, req.Reason),
	}

	jsonData, _ := json.MarshalIndent(result, "", "  ")
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(jsonData),
			},
		},
	}, nil
}

func taskStatusToString(status taskguildv1.TaskStatus) string {
	switch status {
	case taskguildv1.TaskStatus_TASK_STATUS_PENDING:
		return "PENDING"
	case taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS:
		return "IN_PROGRESS"
	case taskguildv1.TaskStatus_TASK_STATUS_COMPLETED:
		return "COMPLETED"
	case taskguildv1.TaskStatus_TASK_STATUS_REJECTED:
		return "REJECTED"
	case taskguildv1.TaskStatus_TASK_STATUS_CLOSED:
		return "CLOSED"
	default:
		return "UNSPECIFIED"
	}
}
