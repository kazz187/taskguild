package agent

import (
	"context"
	"fmt"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/worktree"
)

// WorkItem represents a unified work item (task process to execute)
type WorkItem struct {
	ID          string
	Task        *task.Task
	ProcessName string // The process being executed (e.g., "implement", "review", "qa")
	Context     map[string]interface{}
}

// ExecutionResult represents the result of work execution
type ExecutionResult struct {
	Success    bool
	NextStatus string
	Message    string
	Artifacts  []string
	Error      error
}

// PermissionRequest represents a tool permission request from Claude
type PermissionRequest struct {
	ID        string                 // Unique request ID
	ToolName  string                 // Tool being requested (e.g., "Bash", "Write", "Edit")
	Input     map[string]interface{} // Tool input parameters
	TaskID    string                 // Associated task ID
	AgentID   string                 // Agent making the request
	Timestamp int64                  // Request timestamp
}

// PermissionResponse represents the user's response to a permission request
type PermissionResponse struct {
	RequestID    string                 // Matching request ID
	Allowed      bool                   // Whether the action is allowed
	Message      string                 // Optional message (e.g., reason for denial)
	UpdatedInput map[string]interface{} // Optional: modified input parameters
}

// ExecutorConfig holds configuration for executor initialization
type ExecutorConfig struct {
	AgentID      string
	Name         string
	Process      string // The process type this executor handles
	Instructions string
	WorktreePath string

	// Permission handling
	PermissionRequestChan  chan<- PermissionRequest  // Channel to send permission requests
	PermissionResponseChan <-chan PermissionResponse // Channel to receive permission responses

	// Dependency injection
	TaskService     task.Service
	EventBus        *event.EventBus
	WorktreeManager *worktree.Manager
}

// Executor defines the interface for executing agent-specific logic
type Executor interface {
	// Initialize the executor with configuration
	Initialize(ctx context.Context, config ExecutorConfig) error

	// Connect establishes a persistent connection (for streaming executors)
	Connect(ctx context.Context) error

	// Disconnect closes the persistent connection
	Disconnect() error

	// IsConnected returns true if the executor has an active connection
	IsConnected() bool

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

// Connect is a no-op for base executor (override in streaming executors)
func (e *BaseExecutor) Connect(ctx context.Context) error {
	return nil
}

// Disconnect is a no-op for base executor (override in streaming executors)
func (e *BaseExecutor) Disconnect() error {
	return nil
}

// IsConnected returns false for base executor (override in streaming executors)
func (e *BaseExecutor) IsConnected() bool {
	return false
}

// completeProcess marks the current process as completed
func (e *BaseExecutor) completeProcess(taskID, processName, agentID string) error {
	if e.taskService != nil {
		return e.taskService.CompleteProcess(taskID, processName, agentID)
	}
	return nil
}

// rejectProcess marks the current process as rejected
func (e *BaseExecutor) rejectProcess(taskID, processName, agentID, reason string) error {
	if e.taskService != nil {
		return e.taskService.RejectProcess(taskID, processName, agentID, reason)
	}
	return nil
}
