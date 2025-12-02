package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

type EventTrigger struct {
	Event     string `yaml:"event"`
	Condition string `yaml:"condition"`
}

type ScalingConfig struct {
	Min  int  `yaml:"min"`
	Max  int  `yaml:"max"`
	Auto bool `yaml:"auto"`
}

type Agent struct {
	ID           string         `yaml:"id"`
	Name         string         `yaml:"name"`
	Type         string         `yaml:"type"`
	Description  string         `yaml:"description,omitempty"`
	Version      string         `yaml:"version,omitempty"`
	Instructions string         `yaml:"instructions,omitempty"`
	Triggers     []EventTrigger `yaml:"triggers"`
	Scaling      *ScalingConfig `yaml:"scaling,omitempty"`
	Status       Status         `yaml:"status"`
	TaskID       string         `yaml:"task_id,omitempty"`
	WorktreePath string         `yaml:"worktree_path,omitempty"`
	CreatedAt    time.Time      `yaml:"created_at"`
	UpdatedAt    time.Time      `yaml:"updated_at"`

	// Runtime fields
	ctx       context.Context
	cancel    context.CancelFunc
	mutex     sync.RWMutex
	waitGroup *conc.WaitGroup
	executor  Executor
	eventChan chan interface{}
	config    *AgentConfig

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
		ID:           id,
		Name:         config.Name,
		Type:         config.Type,
		Instructions: config.Instructions,
		Triggers:     config.Triggers,
		Scaling:      config.Scaling,
		Status:       StatusIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
		waitGroup:    conc.NewWaitGroup(),
		executor:     executor,
		eventChan:    make(chan interface{}, 100),
		config:       config,
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

// InitializeWithDependencies injects dependencies and initializes the executor
func (a *Agent) InitializeWithDependencies(taskService task.Service, eventBus *event.EventBus, worktreeManager *worktree.Manager) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.taskService = taskService
	a.eventBus = eventBus
	a.worktreeManager = worktreeManager

	// Initialize executor with configuration
	executorConfig := ExecutorConfig{
		AgentID:         a.ID,
		Name:            a.Name,
		Instructions:    a.Instructions,
		WorktreePath:    a.WorktreePath,
		StatusOptions:   a.config.StatusOptions,
		TaskService:     taskService,
		EventBus:        eventBus,
		WorktreeManager: worktreeManager,
	}

	return a.executor.Initialize(context.Background(), executorConfig)
}

// evaluateCondition provides simple condition evaluation
func (a *Agent) evaluateCondition(condition string, context map[string]interface{}) bool {
	// Handle OR conditions (||)
	if strings.Contains(condition, "||") {
		parts := strings.Split(condition, "||")
		for _, part := range parts {
			if a.evaluateSimpleCondition(strings.TrimSpace(part), context) {
				return true
			}
		}
		return false
	}

	// Handle AND conditions (&&)
	if strings.Contains(condition, "&&") {
		parts := strings.Split(condition, "&&")
		for _, part := range parts {
			if !a.evaluateSimpleCondition(strings.TrimSpace(part), context) {
				return false
			}
		}
		return true
	}

	// Single condition
	return a.evaluateSimpleCondition(condition, context)
}

// evaluateSimpleCondition evaluates a single condition like: task.type == "feature"
func (a *Agent) evaluateSimpleCondition(condition string, context map[string]interface{}) bool {
	// Parse condition: variable == "value" or variable == value
	if strings.Contains(condition, "==") {
		parts := strings.SplitN(condition, "==", 2)
		if len(parts) != 2 {
			return false
		}

		variable := strings.TrimSpace(parts[0])
		expectedValue := strings.TrimSpace(parts[1])

		// Remove quotes from expected value if present
		if (strings.HasPrefix(expectedValue, `"`) && strings.HasSuffix(expectedValue, `"`)) ||
			(strings.HasPrefix(expectedValue, `'`) && strings.HasSuffix(expectedValue, `'`)) {
			expectedValue = expectedValue[1 : len(expectedValue)-1]
		}

		// Get actual value from context
		contextKey := strings.Replace(variable, "task.", "task_", 1)
		contextKey = strings.Replace(contextKey, ".", "_", -1)

		actualValue, exists := context[contextKey]
		if !exists {
			return false
		}

		// Convert to string for comparison
		actualStr := fmt.Sprintf("%v", actualValue)
		return actualStr == expectedValue
	}

	return false
}

func (a *Agent) Start(ctx context.Context) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.ctx != nil {
		return fmt.Errorf("[%s] already running", a.ID)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.UpdateStatus(StatusIdle)

	// Subscribe to all event types this agent is interested in
	for _, trigger := range a.Triggers {
		eventType := event.EventType(trigger.Event)
		handlerName := fmt.Sprintf("%s-%s", a.ID, trigger.Event)

		// Subscribe using the event bus
		err := a.eventBus.SubscribeAsync(eventType, handlerName, func(msg *event.EventMessage) error {
			// Forward to agent's event channel
			select {
			case a.eventChan <- msg.Data:
			case <-a.ctx.Done():
				return a.ctx.Err()
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to subscribe to event %s: %w", trigger.Event, err)
		}
	}

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
	a.mutex.Unlock()

	// Wait for all goroutines to finish (outside of mutex lock to avoid deadlock)
	a.waitGroup.Wait()

	return nil
}

func (a *Agent) run() {
	for {
		select {
		case <-a.ctx.Done():
			color.ColoredPrintln(a.ID, "context cancelled, stopping")
			return

		default:
			// Get next work item (task or event)
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
			a.setTaskAssignment(work.Task)

			// Execute the work
			color.ColoredPrintf(a.ID, "starting work execution: %s\n", work.ID)
			result, err := a.executor.Execute(a.ctx, work)

			// Handle execution result
			a.handleExecutionResult(work, result, err)

			// Clear task assignment
			a.clearTaskAssignment()

			// Update status back to idle
			a.UpdateStatus(StatusIdle)
		}
	}
}

// fetchAvailableTask fetches an available task that matches this agent's capabilities
func (a *Agent) fetchAvailableTask() *task.Task {
	if a.taskService == nil {
		color.ColoredPrintln(a.ID, "task service is nil")
		return nil
	}

	// Get all tasks
	tasks, err := a.taskService.ListTasks()
	if err != nil {
		color.ColoredPrintf(a.ID, "error fetching tasks: %v\n", err)
		return nil
	}

	// Find a task that needs work and matches our capabilities
	for _, t := range tasks {
		// Skip if task is already assigned to another agent or completed
		if t.Status == "CLOSED" || t.Status == "CANCELLED" {
			continue
		}

		// Skip if task is already assigned to a different agent
		if t.AssignedTo != "" && t.AssignedTo != a.ID {
			continue
		}

		// Check if this task matches our triggers
		for _, trigger := range a.Triggers {
			var shouldAcquire bool
			var expectedStatus task.Status
			var newStatus task.Status

			// For tasks in specific status, check if we have a trigger for status change
			if trigger.Event == "task.status_changed" {
				// Create context data that matches the event trigger condition
				contextData := map[string]interface{}{
					"task_id":    t.ID,
					"task_type":  t.Type,
					"to_status":  t.Status,
					"new_status": t.Status,
				}

				if a.evaluateCondition(trigger.Condition, contextData) {
					shouldAcquire = true
					expectedStatus = task.Status(t.Status)
					newStatus = "IN_PROGRESS"
				}
			}

			// For task.created events, check if task is recently created
			if trigger.Event == "task.created" {
				contextData := map[string]interface{}{
					"task_id":   t.ID,
					"task_type": t.Type,
				}

				if trigger.Condition == "" || a.evaluateCondition(trigger.Condition, contextData) {
					shouldAcquire = true
					expectedStatus = task.Status(t.Status)
					newStatus = "IN_PROGRESS"
				}
			}

			if shouldAcquire {
				// Try to atomically acquire the task using compare-and-swap
				acquireReq := &task.TryAcquireTaskRequest{
					ID:             t.ID,
					ExpectedStatus: expectedStatus,
					NewStatus:      newStatus,
					AgentID:        a.ID,
				}

				acquiredTask, err := a.taskService.TryAcquireTask(acquireReq)
				if err != nil {
					// Task was already acquired by another agent or status changed
					// This is expected in concurrent scenarios, just continue to next task
					continue
				}

				color.ColoredPrintf(a.ID, "successfully acquired task %s (status: %s -> %s)\n",
					acquiredTask.ID, expectedStatus, newStatus)
				return acquiredTask
			}
		}
	}

	return nil
}

// getNextWorkItem gets the next available work item (task or event-triggered task)
func (a *Agent) getNextWorkItem() *WorkItem {
	// First, try to get an available task
	if task := a.fetchAvailableTask(); task != nil {
		return &WorkItem{
			ID:   task.ID,
			Task: task,
		}
	}

	// Then check for events (non-blocking)
	select {
	case eventData := <-a.eventChan:
		// Check if this event matches our triggers and get appropriate task
		for _, trigger := range a.Triggers {
			if a.matchesEventTrigger(trigger, eventData) {
				// For events, we might need to fetch the related task
				// or create a work item based on the event
				task := a.getTaskFromEvent(eventData)
				if task != nil {
					return &WorkItem{
						ID:           generateWorkItemID(task.ID, trigger.Event),
						Task:         task,
						TriggerEvent: &EventData{Type: trigger.Event, Data: eventData},
					}
				}
				break
			}
		}
	default:
		// No events available
	}

	return nil
}

// setTaskAssignment sets the task assignment for this agent
func (a *Agent) setTaskAssignment(task *task.Task) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.TaskID = task.ID
	a.UpdatedAt = time.Now()

	// Handle worktree creation if needed
	if task.Worktree != "" && a.worktreeManager != nil {
		worktreePath, err := a.worktreeManager.CreateWorktree(task.ID, task.Branch)
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
	a.WorktreePath = ""
	a.UpdatedAt = time.Now()
}

// handleExecutionResult handles the result of work execution
func (a *Agent) handleExecutionResult(work *WorkItem, result *ExecutionResult, err error) {
	if err != nil {
		color.ColoredPrintf(a.ID, "error executing work %s: %v\n", work.ID, err)
		a.UpdateStatus(StatusError)
		return
	}

	if result != nil && !result.Success {
		color.ColoredPrintf(a.ID, "work execution failed %s: %s\n", work.ID, result.Message)
		a.UpdateStatus(StatusError)
		return
	}

	color.ColoredPrintf(a.ID, "completed work %s\n", work.ID)
}

// getTaskFromEvent extracts or fetches a task based on event data
func (a *Agent) getTaskFromEvent(eventData interface{}) *task.Task {
	// Extract task ID from event data
	var taskID string

	switch data := eventData.(type) {
	case *event.TaskCreatedData:
		taskID = data.TaskID
	case *event.TaskStatusChangedData:
		taskID = data.TaskID
	default:
		// Try to parse as JSON for unknown event types
		if jsonData, ok := eventData.(json.RawMessage); ok {
			var statusData event.TaskStatusChangedData
			if err := json.Unmarshal(jsonData, &statusData); err == nil {
				taskID = statusData.TaskID
			}
		}
	}

	if taskID == "" {
		return nil
	}

	// Fetch the task
	if a.taskService != nil {
		task, err := a.taskService.GetTask(taskID)
		if err != nil {
			color.ColoredPrintf(a.ID, "failed to get task %s from event: %v\n", taskID, err)
			return nil
		}
		return task
	}

	return nil
}

// generateWorkItemID generates a unique work item ID
func generateWorkItemID(taskID, eventType string) string {
	return fmt.Sprintf("%s-%s-%d", taskID, eventType, time.Now().Unix())
}

// matchesEventTrigger checks if an event matches a trigger
func (a *Agent) matchesEventTrigger(trigger EventTrigger, eventData interface{}) bool {
	// Create context data from event
	contextData := map[string]interface{}{
		"event_data": eventData,
	}

	// Add specific fields based on event type
	switch data := eventData.(type) {
	case *event.TaskCreatedData:
		contextData["task_id"] = data.TaskID
		contextData["task_type"] = data.Type
		contextData["task_title"] = data.Title
	case *event.TaskStatusChangedData:
		contextData["task_id"] = data.TaskID
		contextData["old_status"] = data.OldStatus
		contextData["new_status"] = data.NewStatus
		contextData["from_status"] = data.OldStatus
		contextData["to_status"] = data.NewStatus
	default:
		// Try to parse as JSON for unknown event types
		if jsonData, ok := eventData.(json.RawMessage); ok {
			// Try to unmarshal as TaskStatusChangedData
			var statusData event.TaskStatusChangedData
			if err := json.Unmarshal(jsonData, &statusData); err == nil {
				contextData["task_id"] = statusData.TaskID
				contextData["old_status"] = statusData.OldStatus
				contextData["new_status"] = statusData.NewStatus
				contextData["from_status"] = statusData.OldStatus
				contextData["to_status"] = statusData.NewStatus
			}
		}
	}

	return a.evaluateCondition(trigger.Condition, contextData)
}
