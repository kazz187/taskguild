package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/conc"

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
	Role         string         `yaml:"role"`
	Type         string         `yaml:"type"`
	MemoryPath   string         `yaml:"memory"`
	Triggers     []EventTrigger `yaml:"triggers"`
	Scaling      *ScalingConfig `yaml:"scaling,omitempty"`
	Status       Status         `yaml:"status"`
	TaskID       string         `yaml:"task_id,omitempty"`
	WorktreePath string         `yaml:"worktree_path,omitempty"`
	CreatedAt    time.Time      `yaml:"created_at"`
	UpdatedAt    time.Time      `yaml:"updated_at"`

	// Runtime fields
	ctx          context.Context
	cancel       context.CancelFunc
	mutex        sync.RWMutex
	waitGroup    *conc.WaitGroup
	claudeClient claudecode.Client
}

func NewAgent(role, agentType string) *Agent {
	now := time.Now()
	// Generate a temporary ID using timestamp for backward compatibility
	// This function is deprecated in favor of NewAgentWithID
	id := fmt.Sprintf("%s-%d", role, now.UnixNano())
	return &Agent{
		ID:        id,
		Role:      role,
		Type:      agentType,
		Status:    StatusIdle,
		CreatedAt: now,
		UpdatedAt: now,
		waitGroup: conc.NewWaitGroup(),
	}
}

func NewAgentWithID(id, role, agentType string) *Agent {
	now := time.Now()
	return &Agent{
		ID:        id,
		Role:      role,
		Type:      agentType,
		Status:    StatusIdle,
		CreatedAt: now,
		UpdatedAt: now,
		waitGroup: conc.NewWaitGroup(),
	}
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

	// Initialize Claude Code client
	a.claudeClient = claudecode.NewClient()

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
	a.claudeClient = nil
	a.Status = StatusStopped
	a.UpdatedAt = time.Now()
	a.mutex.Unlock()

	// Wait for all goroutines to finish (outside of mutex lock to avoid deadlock)
	a.waitGroup.Wait()

	return nil
}

func (a *Agent) run() {
	t := time.NewTimer(0)
	defer t.Stop()

	for {
		// Get context safely
		a.mutex.RLock()
		ctx := a.ctx
		client := a.claudeClient
		taskID := a.TaskID
		a.mutex.RUnlock()

		if ctx == nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		// Agent main loop
		if client != nil && taskID != "" && a.GetStatus() == StatusBusy {
			// Execute the task
			a.executeTask(ctx, client)
		} else {
			// Wait for task assignment
			t.Reset(1 * time.Second)
		}
	}
}

func (a *Agent) executeTask(ctx context.Context, client claudecode.Client) {
	// Read memory file
	memoryContent := ""
	if a.MemoryPath != "" {
		// TODO: Read memory file content
	}

	// Create initial prompt based on task and memory
	prompt := fmt.Sprintf("You are an AI agent with role: %s\n\nTask ID: %s\n\nInstructions:\n%s\n\nPlease analyze the task and execute it.",
		a.Role, a.TaskID, memoryContent)

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
