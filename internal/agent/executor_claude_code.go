package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"

	"github.com/kazz187/taskguild/pkg/color"
)

// ClaudeCodeExecutor implements the Executor interface for Claude Code
// using persistent streaming connection for interactive communication
type ClaudeCodeExecutor struct {
	BaseExecutor

	// Claude SDK client for persistent connection
	client    *claudeagent.ClaudeSDKClient
	clientMu  sync.RWMutex
	connected bool

	// Current task being executed (for permission context)
	currentTaskID string
	taskMu        sync.RWMutex
}

// NewClaudeCodeExecutor creates a new Claude Code executor
func NewClaudeCodeExecutor() *ClaudeCodeExecutor {
	return &ClaudeCodeExecutor{}
}

// Initialize initializes the executor with configuration
func (e *ClaudeCodeExecutor) Initialize(ctx context.Context, config ExecutorConfig) error {
	// Initialize base executor
	if err := e.BaseExecutor.Initialize(ctx, config); err != nil {
		return fmt.Errorf("failed to initialize base executor: %w", err)
	}

	return nil
}

// Connect establishes a persistent connection to Claude Code
func (e *ClaudeCodeExecutor) Connect(ctx context.Context) error {
	e.clientMu.Lock()
	defer e.clientMu.Unlock()

	if e.connected {
		return nil // Already connected
	}

	// Build Claude Code options with permission callback
	opts := e.buildClaudeOptions()

	// Create SDK client
	e.client = claudeagent.NewClaudeSDKClient(opts)

	// Connect in streaming mode
	if err := e.client.Connect(ctx); err != nil {
		e.client = nil
		return fmt.Errorf("failed to connect to Claude Code: %w", err)
	}

	e.connected = true
	color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Connected to Claude Code CLI (streaming mode)")

	return nil
}

// Disconnect closes the persistent connection
func (e *ClaudeCodeExecutor) Disconnect() error {
	e.clientMu.Lock()
	defer e.clientMu.Unlock()

	if !e.connected || e.client == nil {
		return nil
	}

	if err := e.client.Close(); err != nil {
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Error closing connection: %v\n", err)
	}

	e.client = nil
	e.connected = false
	color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Disconnected from Claude Code CLI")

	return nil
}

// IsConnected returns true if the executor has an active connection
func (e *ClaudeCodeExecutor) IsConnected() bool {
	e.clientMu.RLock()
	defer e.clientMu.RUnlock()
	return e.connected
}

// Execute executes a work item using Claude Code
func (e *ClaudeCodeExecutor) Execute(ctx context.Context, work *WorkItem) (*ExecutionResult, error) {
	color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Starting work execution: %s\n", work.ID)

	// Set current task for permission context
	e.setCurrentTask(work.Task.ID)
	defer e.clearCurrentTask()

	// Ensure we have a connection
	if !e.IsConnected() {
		if err := e.Connect(ctx); err != nil {
			return &ExecutionResult{
				Success: false,
				Error:   fmt.Errorf("failed to connect: %w", err),
			}, nil
		}
	}

	// Generate prompt based on work item
	prompt := e.generatePrompt(work)

	color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Sending query to Claude Code CLI...")

	e.clientMu.RLock()
	client := e.client
	e.clientMu.RUnlock()

	if client == nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("client not connected"),
		}, nil
	}

	// Send query
	if err := client.SendQuery(ctx, prompt); err != nil {
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Query failed: %v\n", err)
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to send query: %w", err),
		}, nil
	}

	// Receive and process response
	messages, err := client.ReceiveResponse(ctx)
	if err != nil {
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Error receiving response: %v\n", err)
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to receive response: %w", err),
		}, nil
	}

	// Process response messages
	result := e.processMessages(messages)

	color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Work execution completed")
	return result, nil
}

// CanExecute checks if this executor can handle the work item
func (e *ClaudeCodeExecutor) CanExecute(work *WorkItem) bool {
	// Claude Code can execute any task
	return work != nil && work.Task != nil
}

// Cleanup releases resources
func (e *ClaudeCodeExecutor) Cleanup() error {
	return e.Disconnect()
}

// setCurrentTask sets the current task ID for permission context
func (e *ClaudeCodeExecutor) setCurrentTask(taskID string) {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	e.currentTaskID = taskID
}

// clearCurrentTask clears the current task ID
func (e *ClaudeCodeExecutor) clearCurrentTask() {
	e.taskMu.Lock()
	defer e.taskMu.Unlock()
	e.currentTaskID = ""
}

// getCurrentTask returns the current task ID
func (e *ClaudeCodeExecutor) getCurrentTask() string {
	e.taskMu.RLock()
	defer e.taskMu.RUnlock()
	return e.currentTaskID
}

// generatePrompt creates a prompt based on the work item
func (e *ClaudeCodeExecutor) generatePrompt(work *WorkItem) string {
	task := work.Task

	// Get status options for the prompt
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

	// Build base prompt
	prompt := fmt.Sprintf(`You are an AI agent named: %s

Task ID: %s
Task Title: %s
Task Type: %s
Task Status: %s

Instructions:
%s

Please analyze and execute this task.`,
		e.Config.Name, task.ID, task.Title, task.Type, task.Status, e.Config.Instructions)

	// Add trigger event information if present
	if work.TriggerEvent != nil {
		prompt += fmt.Sprintf(`

This task was triggered by an event:
Event Type: %s
Event Data: %+v

Please consider this event context when executing the task.`, work.TriggerEvent.Type, work.TriggerEvent.Data)
	}

	// Add MCP tools information
	prompt += fmt.Sprintf(`

IMPORTANT: You have access to the taskguild MCP server with the following tools:
- taskguild_update_task: Update task status
- taskguild_get_task: Get task information
- taskguild_list_tasks: List all tasks

After you finish implementing this task, you MUST evaluate your work and update the task status using the taskguild_update_task MCP tool with:
- id: "%s"
- status: one of the available options%s

Please proceed with implementing the task and remember to update the status at the end.`,
		task.ID, availableStatusesText)

	color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Created prompt (%d chars)\n", len(prompt))
	return prompt
}

// buildClaudeOptions builds Claude Code options with permission callback
func (e *ClaudeCodeExecutor) buildClaudeOptions() *claudeagent.ClaudeAgentOptions {
	// Get absolute path to MCP server binary
	mcpServerPath := getMCPServerPath()

	opts := &claudeagent.ClaudeAgentOptions{
		Model: "claude-sonnet-4-20250514",
		McpServers: map[string]claudeagent.McpServerConfig{
			"taskguild": {
				Type:    claudeagent.McpServerTypeStdio,
				Command: mcpServerPath,
				Args:    []string{},
			},
		},
	}

	// Set up permission callback if permission channels are configured
	if e.Config.PermissionRequestChan != nil && e.Config.PermissionResponseChan != nil {
		opts.PermissionMode = claudeagent.PermissionModeDefault
		opts.CanUseTool = e.createPermissionCallback()
	} else {
		// Fall back to auto-accept mode if no permission channels
		opts.PermissionMode = claudeagent.PermissionModeAcceptEdits
	}

	// Set working directory if we have a worktree
	if e.Config.WorktreePath != "" {
		opts.Cwd = e.Config.WorktreePath
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Using working directory: %s\n", e.Config.WorktreePath)
	}

	return opts
}

// createPermissionCallback creates a callback function for tool permission requests
func (e *ClaudeCodeExecutor) createPermissionCallback() claudeagent.CanUseToolFunc {
	return func(toolName string, input map[string]interface{}, permCtx claudeagent.ToolPermissionContext) (claudeagent.PermissionResult, error) {
		taskID := e.getCurrentTask()

		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Permission requested for tool: %s\n", toolName)

		// Create permission request
		req := PermissionRequest{
			ID:        fmt.Sprintf("%s-%s-%d", taskID, toolName, time.Now().UnixNano()),
			ToolName:  toolName,
			Input:     input,
			TaskID:    taskID,
			AgentID:   e.Config.AgentID,
			Timestamp: time.Now().Unix(),
		}

		// Send request to permission channel
		select {
		case e.Config.PermissionRequestChan <- req:
			// Request sent successfully
		case <-permCtx.Signal.Done():
			return claudeagent.PermissionResultDeny{
				Behavior: "deny",
				Message:  "Permission request cancelled",
			}, nil
		}

		// Wait for response
		select {
		case resp := <-e.Config.PermissionResponseChan:
			if resp.Allowed {
				color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Permission GRANTED for %s\n", toolName)
				result := claudeagent.PermissionResultAllow{
					Behavior: "allow",
				}
				if resp.UpdatedInput != nil {
					result.UpdatedInput = resp.UpdatedInput
				}
				return result, nil
			}

			color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Permission DENIED for %s: %s\n", toolName, resp.Message)
			return claudeagent.PermissionResultDeny{
				Behavior: "deny",
				Message:  resp.Message,
			}, nil

		case <-permCtx.Signal.Done():
			return claudeagent.PermissionResultDeny{
				Behavior: "deny",
				Message:  "Permission request cancelled",
			}, nil
		}
	}
}

// processMessages processes Claude Code response messages
func (e *ClaudeCodeExecutor) processMessages(messages []claudeagent.Message) *ExecutionResult {
	color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Processing response messages...")

	statusUpdated := false

	for i, msg := range messages {
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Processing message #%d (type: %T)\n", i+1, msg)

		switch m := msg.(type) {
		case *claudeagent.UserMessage:
			if content, ok := m.Content.(string); ok {
				color.ColoredPrintf(e.Config.AgentID, "[Claude Code] User: %s\n", content)
			}
		case *claudeagent.AssistantMessage:
			for _, content := range m.Content {
				switch c := content.(type) {
				case claudeagent.TextBlock:
					color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Assistant: %s\n", c.Text)
				case claudeagent.ToolUseBlock:
					color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Tool Use: %s\n", c.Name)
					if strings.Contains(c.Name, "taskguild_update_task") {
						statusUpdated = true
						color.ColoredPrintln(e.Config.AgentID, "[Claude Code] ✅ Task status updated via MCP")
					}
				}
			}
		case *claudeagent.ResultMessage:
			if m.IsError {
				color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Execution error received\n")
				return &ExecutionResult{
					Success: false,
					Message: "Claude Code execution error",
					Error:   fmt.Errorf("claude code execution error"),
				}
			}
			color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Execution completed")
			if m.TotalCostUSD != nil {
				color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Total cost: $%.4f\n", *m.TotalCostUSD)
			}
		default:
			color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Unknown message type: %T\n", msg)
		}
	}

	color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Finished processing %d messages\n", len(messages))

	// If status wasn't updated via MCP, try fallback
	if !statusUpdated {
		color.ColoredPrintln(e.Config.AgentID, "[Claude Code] Status not updated via MCP, using fallback")
		if err := e.updateStatusFallback(); err != nil {
			color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Fallback status update failed: %v\n", err)
			return &ExecutionResult{
				Success: false,
				Message: "Failed to update task status",
				Error:   err,
			}
		}
	}

	return &ExecutionResult{
		Success: true,
		Message: "Claude Code execution completed successfully",
	}
}

// updateStatusFallback updates task status as fallback when MCP didn't work
func (e *ClaudeCodeExecutor) updateStatusFallback() error {
	// Use the first success status if available
	if e.Config.StatusOptions != nil && len(e.Config.StatusOptions.Success) > 0 {
		defaultStatus := e.Config.StatusOptions.Success[0]
		color.ColoredPrintf(e.Config.AgentID, "[Claude Code] Fallback status update to: %s\n", defaultStatus)

		// Try to update via base executor's task service
		return e.updateTaskStatus(context.Background(), e.getCurrentTask(), defaultStatus)
	}

	return nil
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
