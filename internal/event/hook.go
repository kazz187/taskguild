package event

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Hook represents an event hook configuration
type Hook struct {
	Name      string    `yaml:"name"`
	Event     EventType `yaml:"event"`
	Condition string    `yaml:"condition,omitempty"`
	Command   string    `yaml:"command"`
	Timeout   int       `yaml:"timeout,omitempty"` // in seconds
}

// HookExecutor executes hooks in response to events
type HookExecutor struct {
	hooks []Hook
}

// NewHookExecutor creates a new hook executor
func NewHookExecutor(hooks []Hook) *HookExecutor {
	return &HookExecutor{
		hooks: hooks,
	}
}

// Execute runs all hooks that match the given event
func (he *HookExecutor) Execute(ctx context.Context, eventMsg *EventMessage) error {
	for _, hook := range he.hooks {
		if hook.Event != eventMsg.Type {
			continue
		}

		// TODO: Implement condition evaluation
		if hook.Condition != "" {
			// For now, skip hooks with conditions
			continue
		}

		if err := he.executeHook(ctx, hook, eventMsg); err != nil {
			return fmt.Errorf("failed to execute hook %s: %w", hook.Name, err)
		}
	}
	return nil
}

// executeHook executes a single hook
func (he *HookExecutor) executeHook(ctx context.Context, hook Hook, eventMsg *EventMessage) error {
	// Prepare environment variables
	env := make([]string, 0)
	env = append(env, fmt.Sprintf("TASKGUILD_EVENT_TYPE=%s", eventMsg.Type))
	env = append(env, fmt.Sprintf("TASKGUILD_EVENT_ID=%s", eventMsg.ID))
	env = append(env, fmt.Sprintf("TASKGUILD_EVENT_SOURCE=%s", eventMsg.Source))
	env = append(env, fmt.Sprintf("TASKGUILD_EVENT_TIMESTAMP=%s", eventMsg.Timestamp.Format(time.RFC3339)))

	// Add event data as environment variable
	env = append(env, fmt.Sprintf("TASKGUILD_EVENT_DATA=%s", string(eventMsg.Data)))

	// Set timeout
	timeout := 30 * time.Second
	if hook.Timeout > 0 {
		timeout = time.Duration(hook.Timeout) * time.Second
	}

	// Create context with timeout
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute command
	cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Command)
	cmd.Env = append(os.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook command failed: %w, output: %s", err, string(output))
	}

	return nil
}

// LoadHooks loads hook configurations from a slice
func LoadHooks(configs []Hook) *HookExecutor {
	return NewHookExecutor(configs)
}

// RegisterHooks registers hooks with the event bus
func RegisterHooks(eventBus *EventBus, executor *HookExecutor) {
	// Register a handler for all event types
	allEventTypes := []EventType{
		TaskCreated, TaskStatusChanged, TaskClosed, TaskAssigned, TaskUnassigned,
		AgentStarted, AgentStopped, AgentStatusChanged, AgentAssigned, AgentUnassigned,
		ApprovalRequested, ApprovalGranted, ApprovalRejected,
		GitCommitted, GitPushed, GitMerged,
	}

	for _, eventType := range allEventTypes {
		eventBus.SubscribeAsync(eventType, fmt.Sprintf("hook-%s", eventType), func(msg *message.Message) error {
			var eventMsg EventMessage
			if err := json.Unmarshal(msg.Payload, &eventMsg); err != nil {
				return err
			}
			return executor.Execute(msg.Context(), &eventMsg)
		})
	}
}
