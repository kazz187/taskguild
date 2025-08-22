package agent

import (
	"context"
	"fmt"

	"github.com/kazz187/taskguild/internal/task"
)

// AgentExecutor defines the interface for executing agent-specific logic
type AgentExecutor interface {
	// Initialize the executor with agent configuration
	Initialize(ctx context.Context, config AgentConfig, worktreePath string) error

	// ExecuteTask Execute a task
	ExecuteTask(ctx context.Context, task *task.Task) error

	// HandleEvent Handle an event
	HandleEvent(ctx context.Context, eventType string, data interface{}) error

	// Cleanup resources
	Cleanup() error

	// IsReady Check if the executor is ready
	IsReady() bool
}

// NewExecutor ExecutorFactory creates the appropriate executor based on agent type
func NewExecutor(agentType string) (AgentExecutor, error) {
	switch agentType {
	case "claude-code":
		return NewClaudeCodeExecutor(), nil
	default:
		return nil, fmt.Errorf("unsupported agent type: %s", agentType)
	}
}

// BaseExecutor provides common functionality for all executors
type BaseExecutor struct {
	Config       AgentConfig
	WorktreePath string
	Ready        bool
}

func (e *BaseExecutor) IsReady() bool {
	return e.Ready
}
