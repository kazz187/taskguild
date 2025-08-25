package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/claudecode"
	"github.com/kazz187/taskguild/pkg/color"
)

// ClaudeCodeExecutor implements AgentExecutor for Claude Code agents
type ClaudeCodeExecutor struct {
	BaseExecutor
	client claudecode.Client
}

// NewClaudeCodeExecutor creates a new Claude Code executor
func NewClaudeCodeExecutor() *ClaudeCodeExecutor {
	return &ClaudeCodeExecutor{}
}

// Initialize sets up the Claude Code client
func (e *ClaudeCodeExecutor) Initialize(ctx context.Context, agentID string, config AgentConfig, worktreePath string) error {
	e.AgentID = agentID
	e.Config = config
	e.WorktreePath = worktreePath
	e.client = claudecode.NewClient()
	e.Ready = true
	return nil
}

// ExecuteTask executes a task using Claude Code
func (e *ClaudeCodeExecutor) ExecuteTask(ctx context.Context, t *task.Task) error {
	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code] Starting task execution: %s\n", t.ID)

	// Create prompt based on task and instructions
	prompt := fmt.Sprintf("You are an AI agent named: %s\n\nTask ID: %s\nTask Title: %s\nTask Type: %s\nTask Status: %s\n\nInstructions:\n%s\n\nPlease analyze and execute this task.",
		e.Config.Name, t.ID, t.Title, t.Type, t.Status, e.Config.Instructions)

	color.ColoredPrintf(e.AgentID, "[Claude Code] Created prompt (%d chars)\n", len(prompt))

	// Create options with model, working directory and permission mode
	opts := &claudecode.ClaudeCodeOptions{
		Model:          stringPtr("claude-sonnet-4-20250514"),
		PermissionMode: permissionModePtr(claudecode.PermissionModeAcceptEdits),
		McpServers: map[string]claudecode.McpServerConfig{
			"taskguild": {
				Type:    "stdio",
				Command: "./bin/mcp-taskguild",
				Args:    []string{},
			},
		},
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = stringPtr(e.WorktreePath)
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
		default:
			color.ColoredPrintf(e.AgentID, "[Claude Code] Unknown message type: %T\n", msg)
		}
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code] Finished processing %d messages\n", messageCount)

	// After task execution, let Claude decide the next status
	if err := e.selectTaskStatus(ctx, t); err != nil {
		color.ColoredPrintf(e.AgentID, "[Claude Code] Error selecting task status: %v\n", err)
		return err
	}

	return nil
}

// selectTaskStatus lets Claude choose the appropriate next status for the task
func (e *ClaudeCodeExecutor) selectTaskStatus(ctx context.Context, t *task.Task) error {
	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	// Get status options from config
	statusOptions := e.Config.StatusOptions
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

Please use the mcp-taskguild tools to update the task status. Use the update_task_status function with the task ID and your chosen status.`,
		t.ID, t.Title, t.Type, t.Status, formatStatusOptions(availableStatuses))

	// Create options for continued conversation with MCP
	opts := &claudecode.ClaudeCodeOptions{
		Model:                stringPtr("claude-sonnet-4-20250514"),
		ContinueConversation: true, // This enables --continue functionality
		PermissionMode:       permissionModePtr(claudecode.PermissionModeAcceptEdits),
		McpServers: map[string]claudecode.McpServerConfig{
			"taskguild": {
				Type:    "stdio",
				Command: "./bin/mcp-taskguild",
				Args:    []string{},
			},
		},
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = stringPtr(e.WorktreePath)
	}

	color.ColoredPrintln(e.AgentID, "[Claude Code] Sending status selection query...")

	// Send query to Claude for status selection
	messages, err := e.client.Query(ctx, prompt, opts)
	if err != nil {
		return fmt.Errorf("failed to query Claude Code for status selection: %w", err)
	}

	// Process response messages
	messageCount := 0
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
					color.ColoredPrintf(e.AgentID, "[Claude Code Status] Tool Use: %s\n", c.Name)
				}
			}
		case claudecode.ResultMessage:
			if m.IsError {
				color.ColoredPrintf(e.AgentID, "[Claude Code Status] Status selection error\n")
				return fmt.Errorf("Claude Code status selection error")
			}
			color.ColoredPrintln(e.AgentID, "[Claude Code Status] Status selection completed")
		default:
			color.ColoredPrintf(e.AgentID, "[Claude Code Status] Unknown message type: %T\n", msg)
		}
	}

	color.ColoredPrintf(e.AgentID, "[Claude Code Status] Finished status selection, processed %d messages\n", messageCount)
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
		Model:          stringPtr("claude-sonnet-4-20250514"),
		PermissionMode: permissionModePtr(claudecode.PermissionModeAcceptEdits),
	}

	// Set working directory if we have a worktree
	if e.WorktreePath != "" {
		opts.Cwd = stringPtr(e.WorktreePath)
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
