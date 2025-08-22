package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/claudecode"
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
	ctx         context.Context
	cancel      context.CancelFunc
	mutex       sync.RWMutex
	waitGroup   *conc.WaitGroup
	executor    AgentExecutor
	taskService task.Service
	eventBus    *event.EventBus
	eventChan   chan interface{}
	config      AgentConfig
}

func NewAgent(name, agentType string, taskService task.Service, eventBus *event.EventBus, config AgentConfig) (*Agent, error) {
	now := time.Now()
	// Generate a temporary ID using timestamp for backward compatibility
	// This function is deprecated in favor of NewAgentWithID
	id := fmt.Sprintf("%s-%d", name, now.UnixNano())
	return NewAgentWithID(id, name, agentType, taskService, eventBus, config)
}

func NewAgentWithID(id, name, agentType string, taskService task.Service, eventBus *event.EventBus, config AgentConfig) (*Agent, error) {
	now := time.Now()

	// Create executor based on agent type
	executor, err := NewExecutor(agentType)
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	return &Agent{
		ID:           id,
		Name:         name,
		Type:         agentType,
		Instructions: config.Instructions,
		Triggers:     config.Triggers,
		Scaling:      config.Scaling,
		Status:       StatusIdle,
		CreatedAt:    now,
		UpdatedAt:    now,
		waitGroup:    conc.NewWaitGroup(),
		executor:     executor,
		taskService:  taskService,
		eventBus:     eventBus,
		eventChan:    make(chan interface{}, 100),
		config:       config,
	}, nil
}

func (a *Agent) UpdateStatus(status Status) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.Status = status
	a.UpdatedAt = time.Now()
}

func (a *Agent) GetStatus() Status {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status
}

func (a *Agent) AssignTask(taskID, worktreePath string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.TaskID = taskID
	a.WorktreePath = worktreePath
	a.UpdatedAt = time.Now()
}

func (a *Agent) ClearTask() {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	a.TaskID = ""
	a.WorktreePath = ""
	a.UpdatedAt = time.Now()
}

func (a *Agent) IsAvailable() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.Status == StatusIdle
}

func (a *Agent) IsAssigned() bool {
	a.mutex.RLock()
	defer a.mutex.RUnlock()
	return a.TaskID != ""
}

func (a *Agent) MatchesTrigger(eventName string, context map[string]interface{}) bool {
	for _, trigger := range a.Triggers {
		if trigger.Event == eventName {
			if trigger.Condition != "" {
				// Simple condition evaluation for common patterns
				return a.evaluateCondition(trigger.Condition, context)
			}
			return true // No condition means always match for this event
		}
	}
	return false
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
		return fmt.Errorf("agent %s is already running", a.ID)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.Status = StatusIdle
	a.UpdatedAt = time.Now()

	// Initialize executor
	if err := a.executor.Initialize(ctx, a.config, a.WorktreePath); err != nil {
		return fmt.Errorf("failed to initialize executor: %w", err)
	}

	// Subscribe to events
	if a.eventBus != nil {
		// Subscribe to all event types this agent is interested in
		for _, trigger := range a.Triggers {
			eventType := event.EventType(trigger.Event)
			handlerName := fmt.Sprintf("%s-%s", a.ID, trigger.Event)

			// Subscribe using the event bus
			err := a.eventBus.SubscribeAsync(eventType, handlerName, func(msg *message.Message) error {
				// Parse the event message
				var eventMsg event.EventMessage
				if err := json.Unmarshal(msg.Payload, &eventMsg); err != nil {
					return fmt.Errorf("failed to unmarshal event: %w", err)
				}

				// Forward to agent's event channel
				select {
				case a.eventChan <- eventMsg.Data:
				case <-time.After(1 * time.Second):
					// Timeout to avoid blocking
				}
				return nil
			})

			if err != nil {
				return fmt.Errorf("failed to subscribe to event %s: %w", trigger.Event, err)
			}
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
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}

		// Try to fetch a task
		if a.taskService != nil {
			availableTasks := a.fetchAvailableTask()
			if availableTasks != nil {
				a.mutex.Lock()
				a.TaskID = availableTasks.ID
				a.Status = StatusBusy
				a.UpdatedAt = time.Now()
				a.mutex.Unlock()

				// Execute the task
				if err := a.executor.ExecuteTask(ctx, availableTasks); err != nil {
					fmt.Printf("Agent %s: error executing task %s: %v\n", a.ID, availableTasks.ID, err)
					a.UpdateStatus(StatusError)
				} else {
					fmt.Printf("Agent %s: completed task %s\n", a.ID, availableTasks.ID)
					a.UpdateStatus(StatusIdle)
				}

				// Clear task assignment
				a.mutex.Lock()
				a.TaskID = ""
				a.mutex.Unlock()
				continue
			}
		}

		// No task available, wait for events
		select {
		case <-ctx.Done():
			return
		case eventData := <-a.eventChan:
			// Check if this event matches our triggers
			for _, trigger := range a.Triggers {
				if a.matchesEventTrigger(trigger, eventData) {
					a.UpdateStatus(StatusBusy)

					// Handle the event
					if err := a.executor.HandleEvent(ctx, trigger.Event, eventData); err != nil {
						fmt.Printf("Agent %s: error handling event %s: %v\n", a.ID, trigger.Event, err)
						a.UpdateStatus(StatusError)
					} else {
						fmt.Printf("Agent %s: handled event %s\n", a.ID, trigger.Event)
						a.UpdateStatus(StatusIdle)
					}
					break
				}
			}
		case <-time.After(5 * time.Second):
			// Timeout, loop back to check for tasks
		}
	}
}

// fetchAvailableTask fetches an available task that matches this agent's capabilities
func (a *Agent) fetchAvailableTask() *task.Task {
	if a.taskService == nil {
		return nil
	}

	// Get all tasks
	tasks, err := a.taskService.ListTasks()
	if err != nil {
		fmt.Printf("Agent %s: error fetching tasks: %v\n", a.ID, err)
		return nil
	}

	// Find a task that needs work and matches our capabilities
	for _, t := range tasks {
		// Skip if task is already assigned to another agent or completed
		if t.Status == "CLOSED" || t.Status == "CANCELLED" {
			continue
		}

		// Check if this task matches our triggers
		for _, trigger := range a.Triggers {
			// For tasks in DESIGNED status, check if we have a trigger for status change to DESIGNED
			if trigger.Event == "task.status_changed" && t.Status == "DESIGNED" {
				// Create context data that matches the event trigger condition
				contextData := map[string]interface{}{
					"task_id":    t.ID,
					"task_type":  t.Type,
					"to_status":  t.Status,
					"new_status": t.Status,
				}

				if a.evaluateCondition(trigger.Condition, contextData) {
					return t
				}
			}

			// For task.created events, check if task is recently created
			if trigger.Event == "task.created" && (trigger.Condition == "" || a.evaluateCondition(trigger.Condition, map[string]interface{}{
				"task_id":   t.ID,
				"task_type": t.Type,
			})) {
				return t
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

func (a *Agent) executeTask(ctx context.Context, client claudecode.Client) {
	// Check if this is a claude-code type agent
	if a.Type != "claude-code" {
		fmt.Printf("Agent %s: type %s is not supported for task execution\n", a.ID, a.Type)
		a.UpdateStatus(StatusError)
		return
	}

	// Create initial prompt based on task and instructions
	prompt := fmt.Sprintf("You are an AI agent with role: %s\n\nTask ID: %s\n\nInstructions:\n%s\n\nPlease analyze the task and execute it.",
		a.Name, a.TaskID, a.Instructions)

	// Create options with model and working directory
	opts := &claudecode.ClaudeCodeOptions{
		Model: stringPtr("claude-sonnet-4-20250514"), // Use Claude Sonnet 4 which is balanced and suitable for coding
	}

	// Set working directory if we have a worktree
	if a.WorktreePath != "" {
		opts.Cwd = stringPtr(a.WorktreePath)
	}

	// Send query to Claude
	messages, err := client.Query(ctx, prompt, opts)
	if err != nil {
		fmt.Printf("Agent %s error: %v\n", a.ID, err)
		a.UpdateStatus(StatusError)
		return
	}

	// Process response messages
	for msg := range messages {
		switch m := msg.(type) {
		case claudecode.UserMessage:
			fmt.Printf("Agent %s user message: %s\n", a.ID, m.Content)
		case claudecode.AssistantMessage:
			for _, content := range m.Content {
				switch c := content.(type) {
				case claudecode.TextBlock:
					fmt.Printf("Agent %s response: %s\n", a.ID, c.Text)
				case claudecode.ToolUseBlock:
					// TODO: Handle tool use blocks for actions that require approval
					fmt.Printf("Agent %s tool use: %s\n", a.ID, c.Name)
				}
			}
		case claudecode.ResultMessage:
			if m.IsError {
				fmt.Printf("Agent %s execution error\n", a.ID)
				a.UpdateStatus(StatusError)
				return
			}
			fmt.Printf("Agent %s execution completed\n", a.ID)
		}
	}

	// Mark task as completed
	a.UpdateStatus(StatusIdle)
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
