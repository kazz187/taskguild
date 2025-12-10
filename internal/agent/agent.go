package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/color"
	"github.com/kazz187/taskguild/pkg/worktree"
)

type Status string

const (
	StatusIdle    Status = "IDLE"
	StatusBusy    Status = "BUSY"
	StatusWaiting Status = "WAITING"
	StatusError   Status = "ERROR"
	StatusStopped Status = "STOPPED"
)

type Action string

const (
	ActionFileWrite    Action = "file_write"
	ActionFileDelete   Action = "file_delete"
	ActionGitCommit    Action = "git_commit"
	ActionGitPush      Action = "git_push"
	ActionStatusChange Action = "status_change"
	ActionTaskCreate   Action = "task_create"
)

type ScalingConfig struct {
	Min  int  `yaml:"min"`
	Max  int  `yaml:"max"`
	Auto bool `yaml:"auto"`
}

type Agent struct {
	ID           string         `yaml:"id"`
	Name         string         `yaml:"name"`
	Type         string         `yaml:"type"`
	Process      string         `yaml:"process"` // The process this agent handles
	Description  string         `yaml:"description,omitempty"`
	Version      string         `yaml:"version,omitempty"`
	Instructions string         `yaml:"instructions,omitempty"`
	Scaling      *ScalingConfig `yaml:"scaling,omitempty"`
	Status       Status         `yaml:"status"`
	TaskID       string         `yaml:"task_id,omitempty"`
	ProcessName  string         `yaml:"process_name,omitempty"` // Currently executing process
	WorktreePath string         `yaml:"worktree_path,omitempty"`
	CreatedAt    time.Time      `yaml:"created_at"`
	UpdatedAt    time.Time      `yaml:"updated_at"`

	// Runtime fields
	ctx       context.Context
	cancel    context.CancelFunc
	mutex     sync.RWMutex
	waitGroup *conc.WaitGroup
	executor  Executor
	config    *AgentConfig

	// Process watcher for real-time status updates
	processWatchChan <-chan task.ProcessChangeEvent
	processWatchDone chan struct{}

	// Permission handling channels
	permissionRequestChan  chan PermissionRequest
	permissionResponseChan chan PermissionResponse

	// Dependencies - injected during initialization
	taskService     task.Service
	eventBus        *event.EventBus
	worktreeManager *worktree.Manager
}

func NewAgent(id string, config *AgentConfig, factory ExecutorFactory) (*Agent, error) {
	now := time.Now()

	// Create executor using factory
	executor, err := factory.CreateExecutor(config.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	return &Agent{
		ID:                     id,
		Name:                   config.Name,
		Type:                   config.Type,
		Process:                config.Process,
		Instructions:           config.Instructions,
		Scaling:                config.Scaling,
		Status:                 StatusIdle,
		CreatedAt:              now,
		UpdatedAt:              now,
		waitGroup:              conc.NewWaitGroup(),
		executor:               executor,
		config:                 config,
		processWatchDone:       make(chan struct{}),
		permissionRequestChan:  make(chan PermissionRequest, 10),
		permissionResponseChan: make(chan PermissionResponse, 10),
	}, nil
}

func (a *Agent) UpdateStatus(status Status) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	oldStatus := a.Status
	a.Status = status
	a.UpdatedAt = time.Now()
	color.ColoredPrintf(a.ID, "status updated: %s -> %s\n", oldStatus, status)
}

func (a *Agent) GetStatus() Status {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status
}

func (a *Agent) IsAvailable() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status == StatusIdle
}

// GetPermissionRequestChan returns the channel for receiving permission requests
// This can be used by UI/CLI to handle permission requests from the executor
func (a *Agent) GetPermissionRequestChan() <-chan PermissionRequest {
	return a.permissionRequestChan
}

// SendPermissionResponse sends a permission response to the executor
func (a *Agent) SendPermissionResponse(response PermissionResponse) {
	select {
	case a.permissionResponseChan <- response:
	default:
		color.ColoredPrintf(a.ID, "warning: permission response channel full, response dropped\n")
	}
}

// InitializeWithDependencies injects dependencies and initializes the executor
func (a *Agent) InitializeWithDependencies(taskService task.Service, eventBus *event.EventBus, worktreeManager *worktree.Manager) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.taskService = taskService
	a.eventBus = eventBus
	a.worktreeManager = worktreeManager

	// Initialize executor with configuration
	executorConfig := ExecutorConfig{
		AgentID:                a.ID,
		Name:                   a.Name,
		Process:                a.Process,
		Instructions:           a.Instructions,
		WorktreePath:           a.WorktreePath,
		PermissionRequestChan:  a.permissionRequestChan,
		PermissionResponseChan: a.permissionResponseChan,
		TaskService:            taskService,
		EventBus:               eventBus,
		WorktreeManager:        worktreeManager,
	}

	return a.executor.Initialize(context.Background(), executorConfig)
}

func (a *Agent) Start(ctx context.Context) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.ctx != nil {
		return fmt.Errorf("[%s] already running", a.ID)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)

	// Establish persistent connection to the executor (e.g., Claude Code)
	if err := a.executor.Connect(a.ctx); err != nil {
		a.cancel()
		a.ctx = nil
		a.cancel = nil
		return fmt.Errorf("failed to connect executor: %w", err)
	}

	a.Status = StatusIdle
	a.UpdatedAt = time.Now()
	color.ColoredPrintf(a.ID, "started with persistent connection (process: %s)\n", a.Process)

	// Start agent goroutine using conc.WaitGroup for proper goroutine management
	a.waitGroup.Go(a.run)

	return nil
}

func (a *Agent) Stop() error {
	// Cancel the context first
	a.mutex.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
		a.ctx = nil
	}
	a.Status = StatusStopped
	a.UpdatedAt = time.Now()

	// Signal process watcher to stop
	select {
	case a.processWatchDone <- struct{}{}:
	default:
	}
	a.mutex.Unlock()

	// Wait for all goroutines to finish (outside of mutex lock to avoid deadlock)
	a.waitGroup.Wait()

	// Disconnect from the executor (close persistent connection)
	if err := a.executor.Disconnect(); err != nil {
		color.ColoredPrintf(a.ID, "error disconnecting executor: %v\n", err)
	}

	// Close permission channels
	close(a.permissionRequestChan)
	close(a.permissionResponseChan)

	color.ColoredPrintf(a.ID, "stopped and disconnected\n")
	return nil
}

func (a *Agent) run() {
	for {
		select {
		case <-a.ctx.Done():
			color.ColoredPrintln(a.ID, "context cancelled, stopping")
			return

		default:
			// Get next work item (process to execute)
			work := a.getNextWorkItem()
			if work == nil {
				// No work available, brief pause
				time.Sleep(1 * time.Second)
				continue
			}

			// Check if executor can handle this work
			if !a.executor.CanExecute(work) {
				continue
			}

			// Update status to busy
			a.UpdateStatus(StatusBusy)

			// Set task assignment
			a.setTaskAssignment(work.Task, work.ProcessName)

			// Start watching for process status changes
			watchCtx, watchCancel := context.WithCancel(a.ctx)
			abortChan := a.startProcessWatcher(watchCtx, work.Task.ID, work.ProcessName)

			// Execute the work
			color.ColoredPrintf(a.ID, "starting process execution: %s (task: %s)\n", work.ProcessName, work.Task.ID)

			// Execute in a goroutine that can be aborted
			resultChan := make(chan struct {
				result *ExecutionResult
				err    error
			}, 1)

			go func() {
				result, err := a.executor.Execute(a.ctx, work)
				resultChan <- struct {
					result *ExecutionResult
					err    error
				}{result, err}
			}()

			// Wait for either completion or abort
			select {
			case r := <-resultChan:
				// Normal completion
				watchCancel()
				a.handleExecutionResult(work, r.result, r.err)
			case <-abortChan:
				// Process was reset, abort execution
				watchCancel()
				color.ColoredPrintf(a.ID, "process %s was reset to pending, aborting execution\n", work.ProcessName)
			case <-a.ctx.Done():
				// Agent stopped
				watchCancel()
				color.ColoredPrintln(a.ID, "agent stopped during execution")
			}

			// Clear task assignment
			a.clearTaskAssignment()

			// Update status back to idle
			a.UpdateStatus(StatusIdle)
		}
	}
}

// startProcessWatcher starts watching for process status changes
// Returns a channel that will receive a signal if the process is reset to pending
func (a *Agent) startProcessWatcher(ctx context.Context, taskID, processName string) <-chan struct{} {
	abortChan := make(chan struct{}, 1)

	if a.taskService == nil {
		return abortChan
	}

	watchChan := a.taskService.WatchProcess(taskID, processName)

	go func() {
		defer a.taskService.UnwatchProcess(taskID, processName, watchChan)

		for {
			select {
			case <-ctx.Done():
				return
			case <-a.processWatchDone:
				return
			case event, ok := <-watchChan:
				if !ok {
					return
				}
				// If process was reset to pending, signal abort
				if event.NewStatus == task.ProcessStatusPending {
					color.ColoredPrintf(a.ID, "process %s status changed to pending (changed by: %s)\n",
						processName, event.ChangedBy)
					select {
					case abortChan <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	return abortChan
}

// fetchAvailableProcess fetches an available process that this agent can work on
func (a *Agent) fetchAvailableProcess() (*task.Task, string) {
	if a.taskService == nil {
		color.ColoredPrintln(a.ID, "task service is nil")
		return nil, ""
	}

	if a.Process == "" {
		color.ColoredPrintln(a.ID, "no process configured for this agent")
		return nil, ""
	}

	// Get available processes for this agent's process type
	available, err := a.taskService.GetAvailableProcesses(a.Process)
	if err != nil {
		color.ColoredPrintf(a.ID, "error fetching available processes: %v\n", err)
		return nil, ""
	}

	// Try to acquire the first available process
	for _, proc := range available {
		acquireReq := &task.TryAcquireProcessRequest{
			TaskID:      proc.TaskID,
			ProcessName: proc.ProcessName,
			AgentID:     a.ID,
		}

		acquiredTask, err := a.taskService.TryAcquireProcess(acquireReq)
		if err != nil {
			// Process was already acquired by another agent
			// This is expected in concurrent scenarios, just continue to next
			continue
		}

		color.ColoredPrintf(a.ID, "successfully acquired process %s for task %s\n",
			proc.ProcessName, proc.TaskID)
		return acquiredTask, proc.ProcessName
	}

	return nil, ""
}

// getNextWorkItem gets the next available work item (process to execute)
func (a *Agent) getNextWorkItem() *WorkItem {
	// Try to get an available process
	t, processName := a.fetchAvailableProcess()
	if t != nil {
		return &WorkItem{
			ID:          fmt.Sprintf("%s-%s", t.ID, processName),
			Task:        t,
			ProcessName: processName,
		}
	}

	return nil
}

// setTaskAssignment sets the task assignment for this agent
func (a *Agent) setTaskAssignment(t *task.Task, processName string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.TaskID = t.ID
	a.ProcessName = processName
	a.UpdatedAt = time.Now()

	// Handle worktree creation if needed
	if t.Worktree != "" && a.worktreeManager != nil {
		worktreePath, err := a.worktreeManager.CreateWorktree(t.ID, t.Branch)
		if err != nil {
			color.ColoredPrintf(a.ID, "error creating worktree: %v\n", err)
			return
		}
		a.WorktreePath = worktreePath
		color.ColoredPrintf(a.ID, "created worktree at: %s\n", worktreePath)
	}
}

// clearTaskAssignment clears the task assignment
func (a *Agent) clearTaskAssignment() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.TaskID = ""
	a.ProcessName = ""
	a.WorktreePath = ""
	a.UpdatedAt = time.Now()
}

// handleExecutionResult handles the result of work execution
func (a *Agent) handleExecutionResult(work *WorkItem, result *ExecutionResult, err error) {
	if err != nil {
		color.ColoredPrintf(a.ID, "error executing process %s: %v\n", work.ProcessName, err)
		a.UpdateStatus(StatusError)
		return
	}

	if result != nil && !result.Success {
		color.ColoredPrintf(a.ID, "process execution failed %s: %s\n", work.ProcessName, result.Message)
		// If execution failed, the executor should have called RejectProcess
		a.UpdateStatus(StatusError)
		return
	}

	color.ColoredPrintf(a.ID, "completed process %s for task %s\n", work.ProcessName, work.Task.ID)
}

// generateWorkItemID generates a unique work item ID
func generateWorkItemID(taskID, processName string) string {
	return fmt.Sprintf("%s-%s-%d", taskID, processName, time.Now().Unix())
}
