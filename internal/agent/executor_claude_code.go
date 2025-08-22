package agent

import (
	"context"
	"fmt"

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

	// Create prompt based on task and instructions
	prompt := fmt.Sprintf("You are an AI agent named: %s\n\nTask ID: %s\nTask Title: %s\nTask Type: %s\nTask Status: %s\n\nInstructions:\n%s\n\nPlease analyze and execute this task.",
		e.Config.Name, t.ID, t.Title, t.Type, t.Status, e.Config.Instructions)

	// Create options with model and working directory
	opts := &claudecode.ClaudeCodeOptions{
		Model: stringPtr("claude-sonnet-4-20250514"),
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
				return fmt.Errorf("Claude Code execution error")
			}
			color.ColoredPrintln(e.AgentID, "[Claude Code] Execution completed")
		}
	}

	return nil
}

// HandleEvent processes events using Claude Code
func (e *ClaudeCodeExecutor) HandleEvent(ctx context.Context, eventType string, data interface{}) error {
	if !e.Ready {
		return fmt.Errorf("executor not initialized")
	}

	// Create prompt based on event
	prompt := fmt.Sprintf("You are an AI agent named: %s\n\nEvent Type: %s\nEvent Data: %+v\n\nInstructions:\n%s\n\nPlease respond to this event appropriately.",
		e.Config.Name, eventType, data, e.Config.Instructions)

	// Create options with model and working directory
	opts := &claudecode.ClaudeCodeOptions{
		Model: stringPtr("claude-sonnet-4-20250514"),
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
