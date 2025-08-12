package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ClaudeCodeAgent represents a Claude Code agent integration
type ClaudeCodeAgent struct {
	Role           string
	TaskID         string
	WorkingDir     string
	Config         *AgentConfigWithPrompt
	CompletionChan chan *TaskCompletionReport
}

// AgentConfigWithPrompt extends the existing AgentConfig with prompt configuration
type AgentConfigWithPrompt struct {
	Role        string        `yaml:"role"`
	Type        string        `yaml:"type"`
	Description string        `yaml:"description"`
	Version     string        `yaml:"version"`
	Prompt      PromptConfig  `yaml:"prompt"`
	Triggers    []Trigger     `yaml:"triggers"`
	Scaling     ScalingConfig `yaml:"scaling"`
}

// PromptConfig represents the prompt configuration for an agent
type PromptConfig struct {
	SystemContext      string          `yaml:"system_context"`
	Instructions       string          `yaml:"instructions"`
	CompletionCriteria map[string]bool `yaml:"completion_criteria"`
	NextStatusRules    NextStatusRules `yaml:"next_status_rules"`
	NextActions        []string        `yaml:"next_actions"`
	RequiredAgents     []string        `yaml:"required_agents"`
}

// NextStatusRules defines the next status based on completion result
type NextStatusRules struct {
	Success string `yaml:"success"`
	Failure string `yaml:"failure"`
}

// Trigger represents an event trigger for an agent
type Trigger struct {
	Event     string `yaml:"event"`
	Condition string `yaml:"condition"`
}

// Removed duplicate type definitions - they exist in agent.go

// ExecuteTask executes a task using Claude Code and returns completion report
func (a *ClaudeCodeAgent) ExecuteTask(ctx context.Context, task *TaskContext) (*TaskCompletionReport, error) {
	// This is a conceptual implementation - actual Claude Code integration would be implemented here

	// Simulate task execution
	startTime := time.Now()

	// Build the prompt for Claude Code based on task context and agent role
	prompt := a.buildPrompt(task)

	// Execute Claude Code (this would be actual SDK call)
	result, err := a.executeClaudeCode(ctx, prompt, task)
	if err != nil {
		return &TaskCompletionReport{
			TaskID:           task.TaskID,
			AgentRole:        a.Role,
			CompletionStatus: "failed",
			WorkSummary:      fmt.Sprintf("Failed to execute task: %v", err),
			Duration:         time.Since(startTime),
		}, err
	}

	// Parse Claude Code result and determine next status
	return a.parseResult(result, task, time.Since(startTime))
}

// TaskContext represents the context passed to Claude Code
type TaskContext struct {
	TaskID        string
	Title         string
	Description   string
	CurrentStatus string
	WorkingDir    string
	PreviousWork  []WorkHistoryEntry
	NextActions   []string
	RequiredFiles []string
	Dependencies  []string
}

// WorkHistoryEntry represents previous work done on the task
type WorkHistoryEntry struct {
	AgentRole   string    `json:"agent_role"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Status      string    `json:"status"`
	WorkSummary string    `json:"work_summary"`
	Artifacts   []string  `json:"artifacts"`
	NextActions []string  `json:"next_actions"`
}

// ClaudeCodeResult represents the result from Claude Code execution
type ClaudeCodeResult struct {
	Success        bool           `json:"success"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	WorkSummary    string         `json:"work_summary"`
	FilesModified  []string       `json:"files_modified"`
	FilesCreated   []string       `json:"files_created"`
	NextTaskStatus string         `json:"next_task_status"`
	NextActions    []string       `json:"next_actions"`
	RequiredAgents []string       `json:"required_agents"`
	Metadata       map[string]any `json:"metadata"`
}

// buildPrompt builds the prompt for Claude Code based on task context and agent configuration
func (a *ClaudeCodeAgent) buildPrompt(task *TaskContext) string {
	var prompt strings.Builder

	// Add system context from configuration
	if a.Config != nil && a.Config.Prompt.SystemContext != "" {
		prompt.WriteString(a.Config.Prompt.SystemContext)
		prompt.WriteString("\n\n")
	} else {
		prompt.WriteString(fmt.Sprintf("You are a %s agent in a TaskGuild system.\n\n", a.Role))
	}

	// Add task information
	prompt.WriteString(fmt.Sprintf("Task: %s\n", task.Title))
	prompt.WriteString(fmt.Sprintf("Description: %s\n", task.Description))
	prompt.WriteString(fmt.Sprintf("Current Status: %s\n", task.CurrentStatus))
	prompt.WriteString(fmt.Sprintf("Working Directory: %s\n\n", task.WorkingDir))

	// Add previous work context
	if len(task.PreviousWork) > 0 {
		prompt.WriteString("Previous Work Done:\n")
		for _, work := range task.PreviousWork {
			prompt.WriteString(fmt.Sprintf("- %s: %s\n", work.AgentRole, work.WorkSummary))
			if len(work.Artifacts) > 0 {
				prompt.WriteString(fmt.Sprintf("  Artifacts: %s\n", strings.Join(work.Artifacts, ", ")))
			}
		}
		prompt.WriteString("\n")
	}

	// Add next actions if any
	if len(task.NextActions) > 0 {
		prompt.WriteString("Next Actions Suggested:\n")
		for _, action := range task.NextActions {
			prompt.WriteString(fmt.Sprintf("- %s\n", action))
		}
		prompt.WriteString("\n")
	}

	// Add role-specific instructions from configuration
	if a.Config != nil && a.Config.Prompt.Instructions != "" {
		prompt.WriteString(a.Config.Prompt.Instructions)
		prompt.WriteString("\n\n")
	}

	// Add completion format requirements
	prompt.WriteString(a.buildCompletionFormat())

	return prompt.String()
}

// buildCompletionFormat builds the completion format requirements based on agent configuration
func (a *ClaudeCodeAgent) buildCompletionFormat() string {
	var format strings.Builder

	format.WriteString("After completing your work, provide a summary in the following format:\n\n")
	format.WriteString("WORK_SUMMARY: [Brief description of what you accomplished]\n")
	format.WriteString("NEXT_TASK_STATUS: [Suggested next status for the task]\n")
	format.WriteString("NEXT_ACTIONS: [Comma-separated list of next actions]\n")
	format.WriteString("REQUIRED_AGENTS: [Comma-separated list of agent roles needed next]\n")
	format.WriteString("METADATA: [JSON object with any additional metadata]\n\n")

	// Add configuration-specific requirements
	if a.Config != nil {
		// Add completion criteria requirements
		if len(a.Config.Prompt.CompletionCriteria) > 0 {
			format.WriteString("Required completion criteria to set in METADATA:\n")
			for key, value := range a.Config.Prompt.CompletionCriteria {
				format.WriteString(fmt.Sprintf("- %s: %v\n", key, value))
			}
			format.WriteString("\n")
		}

		// Add default next status suggestions
		if a.Config.Prompt.NextStatusRules.Success != "" {
			format.WriteString(fmt.Sprintf("Default NEXT_TASK_STATUS for success: %s\n", a.Config.Prompt.NextStatusRules.Success))
		}
		if a.Config.Prompt.NextStatusRules.Failure != "" {
			format.WriteString(fmt.Sprintf("Default NEXT_TASK_STATUS for failure: %s\n", a.Config.Prompt.NextStatusRules.Failure))
		}

		// Add default next actions
		if len(a.Config.Prompt.NextActions) > 0 {
			format.WriteString(fmt.Sprintf("Default NEXT_ACTIONS: %s\n", strings.Join(a.Config.Prompt.NextActions, ", ")))
		}

		// Add default required agents
		if len(a.Config.Prompt.RequiredAgents) > 0 {
			format.WriteString(fmt.Sprintf("Default REQUIRED_AGENTS: %s\n", strings.Join(a.Config.Prompt.RequiredAgents, ", ")))
		}

		format.WriteString("\n")
	}

	// Add example
	format.WriteString("Example:\n")
	format.WriteString("WORK_SUMMARY: Implemented JWT authentication handler and unit tests\n")
	format.WriteString("NEXT_TASK_STATUS: REVIEW_READY\n")
	format.WriteString("NEXT_ACTIONS: Code review, Integration testing, Security review\n")
	format.WriteString("REQUIRED_AGENTS: reviewer, qa\n")
	format.WriteString("METADATA: {\"tests_passing\": true, \"code_formatted\": true, \"implementation_complete\": true}\n")

	return format.String()
}

// executeClaudeCode executes Claude Code with the given prompt (mock implementation)
func (a *ClaudeCodeAgent) executeClaudeCode(ctx context.Context, prompt string, task *TaskContext) (*ClaudeCodeResult, error) {
	// This is a mock implementation - in reality, this would call the actual Claude Code SDK

	// Use configuration-based responses if available
	if a.Config != nil {
		return &ClaudeCodeResult{
			Success:        true,
			WorkSummary:    fmt.Sprintf("Completed task using %s agent configuration", a.Role),
			NextTaskStatus: a.Config.Prompt.NextStatusRules.Success,
			NextActions:    a.Config.Prompt.NextActions,
			RequiredAgents: a.Config.Prompt.RequiredAgents,
			Metadata:       a.convertCompletionCriteriaToMetadata(),
		}, nil
	}

	// Fallback to generic response
	return &ClaudeCodeResult{
		Success:        true,
		WorkSummary:    fmt.Sprintf("Completed task using %s agent", a.Role),
		NextTaskStatus: "IN_PROGRESS",
		NextActions:    []string{"Continue with next phase"},
		RequiredAgents: []string{},
		Metadata:       map[string]any{},
	}, nil
}

// convertCompletionCriteriaToMetadata converts completion criteria to metadata format
func (a *ClaudeCodeAgent) convertCompletionCriteriaToMetadata() map[string]any {
	metadata := make(map[string]any)

	if a.Config != nil && a.Config.Prompt.CompletionCriteria != nil {
		for key, value := range a.Config.Prompt.CompletionCriteria {
			metadata[key] = value
		}
	}

	return metadata
}

// parseResult parses the Claude Code result and creates a completion report
func (a *ClaudeCodeAgent) parseResult(result *ClaudeCodeResult, task *TaskContext, duration time.Duration) (*TaskCompletionReport, error) {
	var artifacts []Artifact

	// Convert file paths to artifacts
	for _, file := range result.FilesCreated {
		artifacts = append(artifacts, Artifact{
			Path:        file,
			Type:        "file",
			Description: fmt.Sprintf("Created by %s agent", a.Role),
		})
	}

	for _, file := range result.FilesModified {
		artifacts = append(artifacts, Artifact{
			Path:        file,
			Type:        "file",
			Description: fmt.Sprintf("Modified by %s agent", a.Role),
		})
	}

	// Convert next actions
	var nextActions []NextAction
	for _, action := range result.NextActions {
		nextActions = append(nextActions, NextAction{
			Description: action,
			Priority:    "medium",
		})
	}

	// Determine completion status
	completionStatus := "success"
	if !result.Success {
		completionStatus = "failed"
	}

	return &TaskCompletionReport{
		TaskID:           task.TaskID,
		AgentRole:        a.Role,
		CompletionStatus: completionStatus,
		WorkSummary:      result.WorkSummary,
		NextTaskStatus:   result.NextTaskStatus,
		NextActions:      nextActions,
		RequiredAgents:   result.RequiredAgents,
		Artifacts:        artifacts,
		ModifiedFiles:    append(result.FilesCreated, result.FilesModified...),
		Duration:         duration,
		Metadata:         result.Metadata,
	}, nil
}
