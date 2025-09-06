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
	ctx             context.Context
	cancel          context.CancelFunc
	mutex           sync.RWMutex
	waitGroup       *conc.WaitGroup
	executor        Executor
	taskService     task.Service
	eventBus        *event.EventBus
	eventChan       chan interface{}
	config          *AgentConfig
	worktreeManager *worktree.Manager
}

func NewAgent(id string, config *AgentConfig, taskService task.Service, eventBus *event.EventBus, worktreeManager *worktree.Manager) (*Agent, error) {
	now := time.Now()

	// Create executor based on agent type
	executor, err := NewExecutor(id, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}
	return &Agent{
		ID:              id,
		Name:            config.Name,
		Type:            config.Type,
		Instructions:    config.Instructions,
		Triggers:        config.Triggers,
		Scaling:         config.Scaling,
		Status:          StatusIdle,
		CreatedAt:       now,
		UpdatedAt:       now,
		waitGroup:       conc.NewWaitGroup(),
		executor:        executor,
		taskService:     taskService,
		eventBus:        eventBus,
		eventChan:       make(chan interface{}, 100),
		config:          config,
		worktreeManager: worktreeManager,
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
		// Check context
		a.mutex.RLock()
		ctx := a.ctx
		a.mutex.RUnlock()

		if ctx == nil {
			color.ColoredPrintln(a.ID, "context is nil, stopping")
			return
		}

		select {
		case <-ctx.Done():
			color.ColoredPrintln(a.ID, "context cancelled, stopping")
			return
		default:
		}

		// Try to fetch a task
		availableTasks := a.fetchAvailableTask()
		if availableTasks != nil {
			a.mutex.Lock()
			a.TaskID = availableTasks.ID
			a.Status = StatusBusy
			a.UpdatedAt = time.Now()

			// Set and create worktree path from the task
			if availableTasks.Worktree != "" {
				a.WorktreePath = availableTasks.Worktree
				color.ColoredPrintf(a.ID, "assigned worktree path: %s\n", a.WorktreePath)

				// Create the worktree if we have a worktree manager
				if a.worktreeManager != nil {
					worktreePath, err := a.worktreeManager.CreateWorktree(availableTasks.ID, availableTasks.Branch)
					if err != nil {
						color.ColoredPrintf(a.ID, "error creating worktree: %v\n", err)
						a.UpdatedAt = time.Now()
						a.mutex.Unlock()
						a.UpdateStatus(StatusError)
						// Clear task assignment
						a.mutex.Lock()
						a.TaskID = ""
						a.WorktreePath = ""
						a.mutex.Unlock()
						continue
					}
					a.WorktreePath = worktreePath
					color.ColoredPrintf(a.ID, "created worktree at: %s\n", worktreePath)
				}
			} else {
				color.ColoredPrintf(a.ID, "warning: task %s has no worktree path set\n", availableTasks.ID)
			}
			a.mutex.Unlock()

			// Execute the task
			color.ColoredPrintf(a.ID, "starting task execution: %s\n", availableTasks.ID)
			if err := a.executor.ExecuteTask(ctx, availableTasks); err != nil {
				color.ColoredPrintf(a.ID, "error executing task %s: %v\n", availableTasks.ID, err)
				a.UpdateStatus(StatusError)
			} else {
				color.ColoredPrintf(a.ID, "completed task %s\n", availableTasks.ID)
				a.UpdateStatus(StatusIdle)
			}

			// Note: Task status update is now handled by Claude via mcp-taskguild
			// Just clear local task assignment
			a.mutex.Lock()
			a.TaskID = ""
			a.WorktreePath = ""
			a.mutex.Unlock()
			color.ColoredPrintf(a.ID, "cleared local task assignment (status handled by Claude)\n")
			continue
		}

		// No task available, wait for events
		select {
		case <-ctx.Done():
			color.ColoredPrintln(a.ID, "context cancelled during wait")
			return
		case eventData := <-a.eventChan:
			// Check if this event matches our triggers
			for _, trigger := range a.Triggers {
				if a.matchesEventTrigger(trigger, eventData) {
					a.UpdateStatus(StatusBusy)

					// Handle the event
					if err := a.executor.HandleEvent(ctx, trigger.Event, eventData); err != nil {
						color.ColoredPrintf(a.ID, "error handling event %s: %v\n", trigger.Event, err)
						a.UpdateStatus(StatusError)
					} else {
						color.ColoredPrintf(a.ID, "handled event %s\n", trigger.Event)
						a.UpdateStatus(StatusIdle)
					}
					break
				}
			}
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
