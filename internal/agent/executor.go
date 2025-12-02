package agent

import (
	"context"
	"fmt"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/worktree"
)

// WorkItem represents a unified work item (always a task, but may be triggered by events)
type WorkItem struct {
	ID           string
	Task         *task.Task
	TriggerEvent *EventData // Optional: event that triggered this work
	Context      map[string]interface{}
}

// EventData wraps event information that triggered work
type EventData struct {
	Type string
	Data interface{}
}

// ExecutionResult represents the result of work execution
type ExecutionResult struct {
	Success    bool
	NextStatus string
	Message    string
	Artifacts  []string
	Error      error
}

// ExecutorConfig holds configuration for executor initialization
type ExecutorConfig struct {
	AgentID       string
	Name          string
	Instructions  string
	WorktreePath  string
	StatusOptions *StatusOptions

	// Dependency injection
	TaskService     task.Service
	EventBus        *event.EventBus
	WorktreeManager *worktree.Manager
}

// Executor defines the interface for executing agent-specific logic
type Executor interface {
	// Initialize the executor with configuration
	Initialize(ctx context.Context, config ExecutorConfig) error

	// Execute a work item (task or event)
	Execute(ctx context.Context, work *WorkItem) (*ExecutionResult, error)

	// Check if the executor can handle this work item
	CanExecute(work *WorkItem) bool

	// Cleanup resources
	Cleanup() error
}

// ExecutorFactory creates executors
type ExecutorFactory interface {
	CreateExecutor(executorType string) (Executor, error)
}

// DefaultExecutorFactory implements ExecutorFactory
type DefaultExecutorFactory struct {
	taskService     task.Service
	eventBus        *event.EventBus
	worktreeManager *worktree.Manager
}

// NewDefaultExecutorFactory creates a new default executor factory
func NewDefaultExecutorFactory(taskService task.Service, eventBus *event.EventBus, worktreeManager *worktree.Manager) *DefaultExecutorFactory {
	return &DefaultExecutorFactory{
		taskService:     taskService,
		eventBus:        eventBus,
		worktreeManager: worktreeManager,
	}
}

// CreateExecutor creates the appropriate executor based on type
func (f *DefaultExecutorFactory) CreateExecutor(executorType string) (Executor, error) {
	switch executorType {
	case "claude-code":
		return NewClaudeCodeExecutor(), nil
	default:
		return nil, fmt.Errorf("unsupported executor type: %s", executorType)
	}
}

// BaseExecutor provides common functionality for all executors
type BaseExecutor struct {
	Config ExecutorConfig

	// Common services
	taskService     task.Service
	eventBus        *event.EventBus
	worktreeManager *worktree.Manager
}

// Initialize sets up the base executor
func (e *BaseExecutor) Initialize(ctx context.Context, config ExecutorConfig) error {
	e.Config = config
	e.taskService = config.TaskService
	e.eventBus = config.EventBus
	e.worktreeManager = config.WorktreeManager

	// Initialize worktree if needed
	if e.Config.WorktreePath != "" && e.worktreeManager != nil {
		// Worktree initialization logic can be added here if needed
	}
	return nil
}

// updateTaskStatus is a helper method for updating task status
func (e *BaseExecutor) updateTaskStatus(ctx context.Context, taskID, status string) error {
	if e.taskService != nil {
		req := &task.UpdateTaskRequest{
			ID:     taskID,
			Status: task.Status(status),
		}
		_, err := e.taskService.UpdateTask(req)
		return err
	}
	return nil
}
