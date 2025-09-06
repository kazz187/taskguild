package agent

import (
	"context"
	"fmt"

	"github.com/kazz187/taskguild/internal/task"
)

// Executor defines the interface for executing agent-specific logic
type Executor interface {
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
func NewExecutor(id string, config *AgentConfig) (Executor, error) {
	switch config.Type {
	case "claude-code":
		return NewClaudeCodeExecutor(id, config), nil
	default:
		return nil, fmt.Errorf("unsupported agent type: %s", config.Type)
	}
}

// BaseExecutor provides common functionality for all executors
type BaseExecutor struct {
	AgentID      string
	Config       *AgentConfig
	WorktreePath string
	Ready        bool
}

func (e *BaseExecutor) IsReady() bool {
	return e.Ready
}
