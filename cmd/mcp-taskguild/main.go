package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	logger := slog.Default()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := NewConfig()
	if err != nil {
		logger.ErrorContext(ctx, "failed to create config", "error", err)
		os.Exit(1)
	}

	client, err := NewTaskGuildClient(cfg)
	if err != nil {
		logger.ErrorContext(ctx, "failed to create TaskGuild client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcp-taskguild",
			Title:   "TaskGuild MCP Server",
			Version: "v1.0.0",
		},
		&mcp.ServerOptions{
			Instructions: "MCP server for TaskGuild task management. Use this to create, list, update, and manage tasks in TaskGuild. Workflow: 1) Use taskguild_list_tasks to browse existing tasks, 2) Use taskguild_create_task to create new tasks, 3) Use taskguild_update_task to modify task status or content, 4) Use taskguild_get_task for detailed task information.",
		},
	)

	// Register TaskGuild task management tools
	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "taskguild_list_tasks",
			Title:       "TaskGuild: List Tasks",
			Description: "List all tasks in TaskGuild with optional filtering by status, type, or assigned agents.",
			InputSchema: ListTasksInputSchema,
		},
		client.ListTasksHandler,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "taskguild_get_task",
			Title:       "TaskGuild: Get Task Details",
			Description: "Get detailed information about a specific task including status, assigned agents, and metadata.",
			InputSchema: GetTaskInputSchema,
		},
		client.GetTaskHandler,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "taskguild_create_task",
			Title:       "TaskGuild: Create Task",
			Description: "Create a new task in TaskGuild with title, description, type, and optional metadata.",
			InputSchema: CreateTaskInputSchema,
		},
		client.CreateTaskHandler,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "taskguild_update_task",
			Title:       "TaskGuild: Update Task",
			Description: "Update an existing task's status, title, description, or other properties.",
			InputSchema: UpdateTaskInputSchema,
		},
		client.UpdateTaskHandler,
	)

	mcp.AddTool(
		server,
		&mcp.Tool{
			Name:        "taskguild_close_task",
			Title:       "TaskGuild: Close Task",
			Description: "Close a task by transitioning it to CLOSED status with optional completion message.",
			InputSchema: CloseTaskInputSchema,
		},
		client.CloseTaskHandler,
	)

	if err := server.Run(ctx, mcp.NewStdioTransport()); err != nil {
		logger.ErrorContext(ctx, "failed to run server", "error", err)
		os.Exit(1)
	}
}
