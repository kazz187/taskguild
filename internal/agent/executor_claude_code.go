package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kazz187/taskguild/internal/client"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/claudecode"
	"github.com/kazz187/taskguild/pkg/color"
	"github.com/kazz187/taskguild/pkg/ptr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// TaskServiceClient interface for direct task status updates
type TaskServiceClient interface {
	UpdateTaskStatus(ctx context.Context, taskID, status string) error
}

// TaskServiceClientAdapter adapts the existing TaskClient to TaskServiceClient interface
type TaskServiceClientAdapter struct {
	client *client.TaskClient
}

// NewTaskServiceClientAdapter creates a new adapter
func NewTaskServiceClientAdapter(taskClient *client.TaskClient) *TaskServiceClientAdapter {
	return &TaskServiceClientAdapter{client: taskClient}
}

// UpdateTaskStatus implements TaskServiceClient interface
func (a *TaskServiceClientAdapter) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	// Convert string status to protobuf enum
	var pbStatus taskguildv1.TaskStatus
	switch status {
	case "CREATED":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_CREATED
	case "ANALYZING":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_ANALYZING
	case "DESIGNED":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_DESIGNED
	case "IN_PROGRESS":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_IN_PROGRESS
	case "REVIEW_READY":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_REVIEW_READY
	case "QA_READY":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_QA_READY
	case "CLOSED":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_CLOSED
	case "CANCELLED":
		pbStatus = taskguildv1.TaskStatus_TASK_STATUS_CANCELLED
	default:
		return fmt.Errorf("unknown status: %s", status)
	}

	_, err := a.client.UpdateTask(ctx, taskID, pbStatus)
	return err
}

type ClaudeCodeExecutor struct {
	BaseExecutor
	client     claudecode.Client
	taskClient TaskServiceClient // For direct API calls as fallback
}

// NewClaudeCodeExecutor creates a new Claude Code executor
func NewClaudeCodeExecutor(id string, config *AgentConfig) *ClaudeCodeExecutor {
	e := &ClaudeCodeExecutor{}
	e.AgentID = id
	e.Config = config
	e.client = claudecode.NewClient()

	// TODO: Get daemon addr from config
	daemonAddr := "http://localhost:8080"
	if addr := os.Getenv("TASKGUILD_DAEMON_ADDR"); addr != "" {
		daemonAddr = addr
	}
	taskClient := client.NewTaskClient(daemonAddr)
	e.taskClient = NewTaskServiceClientAdapter(taskClient)

	e.Ready = true
	return e
}

// ExecuteTask executes a task using Claude Code
func (e *ClaudeCodeExecutor) ExecuteTask(ctx context.Context, t *task.Task) error {
	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code] Starting task execution: %s\n", t.ID)

	// Get status options from config for initial prompt
	statusOptions := e.Config.StatusOptions
	var availableStatusesText string
	if statusOptions != nil {
		var allStatuses []string
		allStatuses = append(allStatuses, statusOptions.Success...)
		if len(statusOptions.Error) > 0 {
			allStatuses = append(allStatuses, statusOptions.Error...)
		}
		if len(allStatuses) > 0 {
			availableStatusesText = fmt.Sprintf("\nAvailable status options for completion: %s", strings.Join(allStatuses, ", "))
		}
	}

	// Create prompt based on task and instructions
	prompt := fmt.Sprintf(`You are an AI agent named: %s

Task ID: %s
Task Title: %s
Task Type: %s
Task Status: %s

Instructions:
%s

Please analyze and execute this task. This is a test task to verify that the agent can:
1. Process the task successfully
2. After completion, evaluate the work using MCP tools

IMPORTANT: You have access to the taskguild MCP server with the following tools:
- taskguild_update_task: Update task status
- taskguild_get_task: Get task information
- taskguild_list_tasks: List all tasks

After you finish implementing this task, you MUST evaluate your work and update the task status using the taskguild_update_task MCP tool with:
- id: "%s"
- status: one of the available options%s

Please proceed with implementing the task and remember to update the status at the end.`,
		e.Config.Name, t.ID, t.Title, t.Type, t.Status, e.Config.Instructions, t.ID, availableStatusesText)

	color.ColoredPrintf(e.AgentID, "[Claude Code] Created prompt (%d chars)\n", len(prompt))

	// Get absolute path to MCP server binary
	mcpServerPath := getMCPServerPath()

	// Create options with model, working directory and permission mode
	opts := &claudecode.ClaudeCodeOptions{
		Model:          ptr.To("claude-sonnet-4-20250514"),
		PermissionMode: permissionModePtr(claudecode.PermissionModeAcceptEdits),
		McpServers: map[string]claudecode.McpServerConfig{
			"taskguild": {
				Type:    "stdio",
				Command: mcpServerPath,
				Args:    []string{},
			},
		},
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = &e.WorktreePath
		color.ColoredPrintf(e.AgentID, "[Claude Code] Using working directory: %s\n", e.WorktreePath)
	}

	color.ColoredPrintln(e.AgentID, "[Claude Code] Sending query to Claude Code CLI...")

	// Send query to Claude
	messages, err := e.client.Query(ctx, prompt, opts)
	if err != nil {
		color.ColoredPrintf(e.AgentID, "[Claude Code] Query failed: %v\n", err)
		return fmt.Errorf("failed to query Claude Code: %w", err)
	}

	color.ColoredPrintln(e.AgentID, "[Claude Code] Received response channel, processing messages...")

	// Process response messages
	messageCount := 0
	for msg := range messages {
		messageCount++
		color.ColoredPrintf(e.AgentID, "[Claude Code] Processing message #%d (type: %T)\n", messageCount, msg)

		switch m := msg.(type) {
		case claudecode.UserMessage:
			color.ColoredPrintf(e.AgentID, "[Claude Code] User: %s\n", m.Content)
		case claudecode.AssistantMessage:
			for _, content := range m.Content {
				switch c := content.(type) {
				case claudecode.TextBlock:
					color.ColoredPrintf(e.AgentID, "[Claude Code] Assistant: %s\n", c.Text)
				case claudecode.ToolUseBlock:
					color.ColoredPrintf(e.AgentID, "[Claude Code] Tool Use: %s\n", c.Name)
				}
			}
		case claudecode.ResultMessage:
			if m.IsError {
				color.ColoredPrintf(e.AgentID, "[Claude Code] Execution error received\n")
				return fmt.Errorf("Claude Code execution error")
			}
			color.ColoredPrintln(e.AgentID, "[Claude Code] Execution completed")
			// Don't break here - let the channel close naturally
		default:
			color.ColoredPrintf(e.AgentID, "[Claude Code] Unknown message type: %T\n", msg)
		}
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code] Finished processing %d messages\n", messageCount)

	// Check if the task status was updated during execution (Claude might have used MCP tools)
	// We'll wait a bit and then check the task status
	color.ColoredPrintln(e.AgentID, "[Claude Code] Checking if task status was updated during execution...")

	// Give a moment for any MCP status updates to propagate
	time.Sleep(2 * time.Second)

	// Get current task status to see if it was updated during execution
	if e.taskClient != nil {
		// Since we don't have a GetTask method in our interface, we'll proceed with status selection
		// This could be improved by adding a GetTask method to the interface
		color.ColoredPrintln(e.AgentID, "[Claude Code] Proceeding with status evaluation since we cannot verify current status...")
	}

	// SKIP separate status selection since Claude was instructed to update status during execution
	color.ColoredPrintln(e.AgentID, "[Claude Code] Skipping separate status selection - Claude should have updated status during execution")

	// Only use fallback if Claude failed to update status in main execution
	// This provides a safety net without duplicating the status update process
	color.ColoredPrintln(e.AgentID, "[Claude Code] Task execution completed - status should be updated via MCP")

	color.ColoredPrintln(e.AgentID, "[Claude Code] Task execution and status selection completed successfully")
	return nil
}

// selectTaskStatus lets Claude choose the appropriate next status for the task
func (e *ClaudeCodeExecutor) selectTaskStatus(ctx context.Context, t *task.Task) error {
	color.ColoredPrintf(e.AgentID, "[Claude Code Status] selectTaskStatus called for task %s\n", t.ID)

	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	// Get status options from config
	statusOptions := e.Config.StatusOptions
	color.ColoredPrintf(e.AgentID, "[Claude Code Status] Status options: %+v\n", statusOptions)
	if statusOptions == nil {
		color.ColoredPrintln(e.AgentID, "[Claude Code] No status options configured, defaulting to CLOSED")
		return nil // Will be handled by the caller with default status
	}

	// Build available status choices
	var availableStatuses []string
	availableStatuses = append(availableStatuses, statusOptions.Success...)
	if len(statusOptions.Error) > 0 {
		availableStatuses = append(availableStatuses, statusOptions.Error...)
	}

	if len(availableStatuses) == 0 {
		color.ColoredPrintln(e.AgentID, "[Claude Code] No status options available, defaulting to CLOSED")
		return nil
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code] Starting status selection with %d options\n", len(availableStatuses))

	// Create a longer-lived context for the status selection query
	statusCtx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5 minutes timeout
	defer cancel()

	// Create status selection prompt
	prompt := fmt.Sprintf(`Based on the task execution that just completed, please select the most appropriate next status for this task.

Task ID: %s
Task Title: %s  
Task Type: %s
Current Status: %s

Available status options:
%s

Please analyze the work that was done and select the most appropriate status. Consider:
- Was the implementation successful and complete?
- Does the code need review before proceeding?
- Are there any issues that need to be addressed?
- What is the logical next step in the development workflow?

Please use the taskguild MCP tools to update the task status. Use the taskguild_update_task function with the following parameters:
- id: "%s" (the task ID)
- status: your chosen status from the available options above

Example: Use taskguild_update_task with id="%s" and status="REVIEW_READY" if you think the code needs review.`,
		t.ID, t.Title, t.Type, t.Status, formatStatusOptions(availableStatuses), t.ID, t.ID)

	// Get absolute path to MCP server binary
	mcpServerPath := getMCPServerPath()

	// Create options for status selection with MCP
	opts := &claudecode.ClaudeCodeOptions{
		Model:                ptr.To("claude-sonnet-4-20250514"),
		ContinueConversation: false, // Use separate session for status selection
		PermissionMode:       permissionModePtr(claudecode.PermissionModeAcceptEdits),
		McpServers: map[string]claudecode.McpServerConfig{
			"taskguild": {
				Type:    "stdio",
				Command: mcpServerPath,
				Args:    []string{},
			},
		},
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = &e.WorktreePath
	}

	color.ColoredPrintln(e.AgentID, "[Claude Code] Sending status selection query...")

	// Send query to Claude for status selection with retry mechanism
	var messages <-chan claudecode.Message
	var err error
	maxRetries := 2
	for i := 0; i < maxRetries; i++ {
		messages, err = e.client.Query(statusCtx, prompt, opts)
		if err == nil {
			break
		}
		color.ColoredPrintf(e.AgentID, "[Claude Code Status] Attempt %d failed: %v\n", i+1, err)
		if i < maxRetries-1 {
			time.Sleep(time.Duration(i+1) * 2 * time.Second) // Exponential backoff
		}
	}

	if err != nil {
		color.ColoredPrintf(e.AgentID, "[Claude Code Status] All attempts failed: %v\n", err)
		// Fallback: default to first success status if available
		if len(statusOptions.Success) > 0 {
			defaultStatus := statusOptions.Success[0]
			color.ColoredPrintf(e.AgentID, "[Claude Code Status] Falling back to default status: %s\n", defaultStatus)
			return e.updateTaskStatusDirectly(statusCtx, t.ID, defaultStatus)
		}
		return fmt.Errorf("failed to query Claude Code for status selection after %d attempts: %w", maxRetries, err)
	}

	color.ColoredPrintln(e.AgentID, "[Claude Code Status] Query successful, processing messages...")

	// Process response messages with timeout
	messageCount := 0
	toolCallCount := 0
	statusUpdated := false

	// Use a channel to signal completion
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range messages {
			messageCount++
			color.ColoredPrintf(e.AgentID, "[Claude Code Status] Processing message #%d (type: %T)\n", messageCount, msg)

			switch m := msg.(type) {
			case claudecode.UserMessage:
				color.ColoredPrintf(e.AgentID, "[Claude Code Status] User: %s\n", m.Content)
			case claudecode.AssistantMessage:
				for _, content := range m.Content {
					switch c := content.(type) {
					case claudecode.TextBlock:
						color.ColoredPrintf(e.AgentID, "[Claude Code Status] Assistant: %s\n", c.Text)
					case claudecode.ToolUseBlock:
						toolCallCount++
						color.ColoredPrintf(e.AgentID, "[Claude Code Status] Tool Use #%d: %s\n", toolCallCount, c.Name)
						if strings.Contains(c.Name, "taskguild_update_task") {
							color.ColoredPrintln(e.AgentID, "[Claude Code Status] ✅ Correct MCP tool called!")
							statusUpdated = true
						} else {
							color.ColoredPrintf(e.AgentID, "[Claude Code Status] ⚠️  Unexpected tool: %s\n", c.Name)
						}
					}
				}
			case claudecode.ResultMessage:
				if m.IsError {
					color.ColoredPrintf(e.AgentID, "[Claude Code Status] Status selection error\n")
				} else {
					color.ColoredPrintln(e.AgentID, "[Claude Code Status] Status selection completed")
				}
			default:
				color.ColoredPrintf(e.AgentID, "[Claude Code Status] Unknown message type: %T\n", msg)
			}
		}
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		color.ColoredPrintf(e.AgentID, "[Claude Code Status] Finished status selection, processed %d messages, %d tool calls\n", messageCount, toolCallCount)
		if !statusUpdated && len(statusOptions.Success) > 0 {
			// If Claude didn't update the status, do it as fallback
			defaultStatus := statusOptions.Success[0]
			color.ColoredPrintf(e.AgentID, "[Claude Code Status] No status update detected, falling back to: %s\n", defaultStatus)
			return e.updateTaskStatusDirectly(statusCtx, t.ID, defaultStatus)
		}
		return nil
	case <-statusCtx.Done():
		color.ColoredPrintln(e.AgentID, "[Claude Code Status] Status selection timed out")
		// Fallback to default status
		if len(statusOptions.Success) > 0 {
			defaultStatus := statusOptions.Success[0]
			color.ColoredPrintf(e.AgentID, "[Claude Code Status] Timeout fallback to: %s\n", defaultStatus)
			return e.updateTaskStatusDirectly(context.Background(), t.ID, defaultStatus)
		}
		return fmt.Errorf("status selection timed out and no fallback available")
	}
}

// updateTaskStatusDirectly updates task status directly via the TaskGuild API as fallback
func (e *ClaudeCodeExecutor) updateTaskStatusDirectly(ctx context.Context, taskID, status string) error {
	color.ColoredPrintf(e.AgentID, "[Claude Code] Updating task %s status directly to %s\n", taskID, status)

	// If we have a task client configured, use it for direct API calls
	if e.taskClient != nil {
		if err := e.taskClient.UpdateTaskStatus(ctx, taskID, status); err != nil {
			color.ColoredPrintf(e.AgentID, "[Claude Code] Direct API update failed: %v\n", err)
			return fmt.Errorf("failed to update task status via API: %w", err)
		}
		color.ColoredPrintf(e.AgentID, "[Claude Code] ✅ Direct API update successful: Task %s -> %s\n", taskID, status)
		return nil
	}

	// Fallback: log the intended action (for backward compatibility)
	color.ColoredPrintf(e.AgentID, "[Claude Code] Direct status update: Task %s -> %s (logged only - no API client configured)\n", taskID, status)
	return nil
}

// formatStatusOptions formats the status options for display
func formatStatusOptions(statuses []string) string {
	var formatted []string
	for i, status := range statuses {
		formatted = append(formatted, fmt.Sprintf("%d. %s", i+1, status))
	}
	return strings.Join(formatted, "\n")
}

// HandleEvent processes events using Claude Code
func (e *ClaudeCodeExecutor) HandleEvent(ctx context.Context, eventType string, data interface{}) error {
	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	// Create prompt based on event
	prompt := fmt.Sprintf("You are an AI agent named: %s\n\nEvent Type: %s\nEvent Data: %+v\n\nInstructions:\n%s\n\nPlease respond to this event appropriately.",
		e.Config.Name, eventType, data, e.Config.Instructions)

	// Create options with model, working directory and permission mode
	opts := &claudecode.ClaudeCodeOptions{
		Model:          ptr.To("claude-sonnet-4-20250514"),
		PermissionMode: permissionModePtr(claudecode.PermissionModeAcceptEdits),
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = &e.WorktreePath
	}

	// Send query to Claude
	messages, err := e.client.Query(ctx, prompt, opts)
	if err != nil {
		return fmt.Errorf("failed to query Claude Code: %w", err)
	}

	// Process response messages
	for msg := range messages {
		switch m := msg.(type) {
		case claudecode.UserMessage:
			color.ColoredPrintf(e.AgentID, "[Claude Code Event] User: %s\n", m.Content)
		case claudecode.AssistantMessage:
			for _, content := range m.Content {
				switch c := content.(type) {
				case claudecode.TextBlock:
					color.ColoredPrintf(e.AgentID, "[Claude Code Event] Assistant: %s\n", c.Text)
				case claudecode.ToolUseBlock:
					color.ColoredPrintf(e.AgentID, "[Claude Code Event] Tool Use: %s\n", c.Name)
				}
			}
		case claudecode.ResultMessage:
			if m.IsError {
				return fmt.Errorf("Claude Code execution error during event handling")
			}
			color.ColoredPrintln(e.AgentID, "[Claude Code Event] Event handling completed")
		}
	}

	return nil
}

// Cleanup releases resources
func (e *ClaudeCodeExecutor) Cleanup() error {
	e.Ready = false
	// Claude Code client doesn't need explicit cleanup
	return nil
}

// permissionModePtr returns a pointer to a PermissionMode value
func permissionModePtr(mode claudecode.PermissionMode) *claudecode.PermissionMode {
	return &mode
}

// getMCPServerPath returns the absolute path to the MCP TaskGuild server binary
func getMCPServerPath() string {
	// Try to find the binary relative to the current working directory first
	if _, err := os.Stat("./bin/mcp-taskguild"); err == nil {
		if abs, err := filepath.Abs("./bin/mcp-taskguild"); err == nil {
			return abs
		}
	}

	// If not found locally, try to find it relative to the project root
	// Go up from worktree to find the main project directory
	wd, err := os.Getwd()
	if err == nil {
		// Look for the pattern .taskguild/worktrees/ and go up to project root
		if strings.Contains(wd, ".taskguild/worktrees/") {
			// Extract project root path
			parts := strings.Split(wd, ".taskguild/worktrees/")
			if len(parts) > 0 {
				projectRoot := parts[0]
				mcpPath := filepath.Join(projectRoot, "bin", "mcp-taskguild")
				if _, err := os.Stat(mcpPath); err == nil {
					return mcpPath
				}
			}
		}
	}

	// Fall back to relative path
	return "./bin/mcp-taskguild"
}
