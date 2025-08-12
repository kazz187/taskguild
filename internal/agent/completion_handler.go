package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
)

// CompletionHandler handles agent task completion and determines next actions
type CompletionHandler struct {
	taskService     task.Service
	eventBus        *event.EventBus
	transitionRules *TransitionRules
}

// TaskCompletionReport represents an agent's completion report
type TaskCompletionReport struct {
	TaskID           string `json:"task_id"`
	AgentRole        string `json:"agent_role"`
	CompletionStatus string `json:"completion_status"` // "success", "failed", "blocked", "needs_review"
	WorkSummary      string `json:"work_summary"`

	// Next action decisions
	NextTaskStatus string       `json:"next_task_status"`
	NextActions    []NextAction `json:"next_actions"`
	RequiredAgents []string     `json:"required_agents"`

	// Status transition logic
	StatusTransition *StatusTransition `json:"status_transition,omitempty"`

	// Work artifacts
	Artifacts     []Artifact `json:"artifacts"`
	ModifiedFiles []string   `json:"modified_files"`

	// Metadata
	Duration time.Duration  `json:"duration"`
	Metadata map[string]any `json:"metadata"`
}

type NextAction struct {
	Description string `json:"description"`
	Priority    string `json:"priority"` // "high", "medium", "low"
	AssignedTo  string `json:"assigned_to,omitempty"`
}

type StatusTransition struct {
	From       string         `json:"from"`
	To         string         `json:"to"`
	Reason     string         `json:"reason"`
	Conditions map[string]any `json:"conditions,omitempty"`
}

type Artifact struct {
	Path        string `json:"path"`
	Type        string `json:"type"` // "file", "document", "test", "config"
	Description string `json:"description"`
}

// TransitionRules defines rules for status transitions based on agent roles
type TransitionRules struct {
	Rules map[string]RoleTransitionRule `yaml:"rules"`
}

type RoleTransitionRule struct {
	SuccessTransitions []TransitionRule `yaml:"success_transitions"`
	FailureTransitions []TransitionRule `yaml:"failure_transitions"`
}

type TransitionRule struct {
	From       string         `yaml:"from"`
	To         string         `yaml:"to"`
	Conditions map[string]any `yaml:"conditions"`
}

// NewCompletionHandler creates a new completion handler
func NewCompletionHandler(taskService task.Service, eventBus *event.EventBus, transitionRules *TransitionRules) *CompletionHandler {
	return &CompletionHandler{
		taskService:     taskService,
		eventBus:        eventBus,
		transitionRules: transitionRules,
	}
}

// HandleTaskCompletion processes an agent's task completion report
func (h *CompletionHandler) HandleTaskCompletion(ctx context.Context, report *TaskCompletionReport) error {
	// 1. Validate the completion report
	if err := h.validateReport(report); err != nil {
		return fmt.Errorf("invalid completion report: %w", err)
	}

	// 2. Get current task
	currentTask, err := h.taskService.GetTask(report.TaskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// 3. Save work history
	if err := h.saveWorkHistory(ctx, report); err != nil {
		return fmt.Errorf("failed to save work history: %w", err)
	}

	// 4. Determine next status based on completion status and rules
	nextStatus, err := h.determineNextStatus(currentTask, report)
	if err != nil {
		return fmt.Errorf("failed to determine next status: %w", err)
	}

	// 5. Update task status if needed
	if nextStatus != currentTask.Status {
		if err := h.updateTaskStatus(ctx, currentTask, nextStatus, report); err != nil {
			return fmt.Errorf("failed to update task status: %w", err)
		}
	}

	// 6. Schedule next actions
	if err := h.scheduleNextActions(ctx, report); err != nil {
		return fmt.Errorf("failed to schedule next actions: %w", err)
	}

	// 7. Publish completion event
	if err := h.publishCompletionEvent(ctx, report); err != nil {
		return fmt.Errorf("failed to publish completion event: %w", err)
	}

	return nil
}

// determineNextStatus determines the next task status based on completion report and rules
func (h *CompletionHandler) determineNextStatus(currentTask *task.Task, report *TaskCompletionReport) (string, error) {
	// If agent explicitly suggests a status, validate it
	if report.NextTaskStatus != "" {
		if err := h.validateStatusTransition(currentTask.Status, report.NextTaskStatus); err != nil {
			return "", fmt.Errorf("invalid status transition: %w", err)
		}
		return report.NextTaskStatus, nil
	}

	// Use rule-based determination
	rules, exists := h.transitionRules.Rules[report.AgentRole]
	if !exists {
		return currentTask.Status, nil // No rules defined, keep current status
	}

	var applicableRules []TransitionRule
	if report.CompletionStatus == "success" {
		applicableRules = rules.SuccessTransitions
	} else {
		applicableRules = rules.FailureTransitions
	}

	// Find matching rule
	for _, rule := range applicableRules {
		if rule.From == currentTask.Status {
			if h.evaluateConditions(rule.Conditions, report) {
				return rule.To, nil
			}
		}
	}

	return currentTask.Status, nil // No matching rule found
}

// evaluateConditions evaluates rule conditions against the completion report
func (h *CompletionHandler) evaluateConditions(conditions map[string]any, report *TaskCompletionReport) bool {
	for key, expectedValue := range conditions {
		actualValue, exists := report.Metadata[key]
		if !exists {
			return false
		}
		if actualValue != expectedValue {
			return false
		}
	}
	return true
}

// validateReport validates the completion report
func (h *CompletionHandler) validateReport(report *TaskCompletionReport) error {
	if report.TaskID == "" {
		return fmt.Errorf("task_id is required")
	}
	if report.AgentRole == "" {
		return fmt.Errorf("agent_role is required")
	}
	if report.CompletionStatus == "" {
		return fmt.Errorf("completion_status is required")
	}
	return nil
}

// validateStatusTransition validates if a status transition is allowed
func (h *CompletionHandler) validateStatusTransition(from, to string) error {
	// This should check against task definition rules
	// For now, implement basic validation
	allowedTransitions := map[string][]string{
		"CREATED":      {"ANALYZING", "DESIGNED", "CANCELLED"},
		"ANALYZING":    {"DESIGNED", "NEEDS_INFO", "CANCELLED"},
		"DESIGNED":     {"IN_PROGRESS", "CANCELLED"},
		"IN_PROGRESS":  {"REVIEW_READY", "BLOCKED", "CANCELLED"},
		"REVIEW_READY": {"IN_PROGRESS", "QA_READY", "CANCELLED"},
		"QA_READY":     {"IN_PROGRESS", "CLOSED", "CANCELLED"},
		"BLOCKED":      {"IN_PROGRESS", "CANCELLED"},
		"NEEDS_INFO":   {"ANALYZING", "CANCELLED"},
	}

	allowed, exists := allowedTransitions[from]
	if !exists {
		return fmt.Errorf("no transitions allowed from status %s", from)
	}

	for _, allowedTo := range allowed {
		if allowedTo == to {
			return nil
		}
	}

	return fmt.Errorf("transition from %s to %s is not allowed", from, to)
}

// saveWorkHistory saves the work history to the task context
func (h *CompletionHandler) saveWorkHistory(ctx context.Context, report *TaskCompletionReport) error {
	// This should save to .taskguild/worktrees/TASK-XXX/task-context.yaml
	// Implementation depends on your file system structure
	return nil
}

// updateTaskStatus updates the task status and publishes status change event
func (h *CompletionHandler) updateTaskStatus(ctx context.Context, currentTask *task.Task, newStatus string, report *TaskCompletionReport) error {
	// Update task
	status := task.Status(newStatus)
	updateReq := &task.UpdateTaskRequest{
		ID:     currentTask.ID,
		Status: status,
	}

	_, err := h.taskService.UpdateTask(updateReq)
	if err != nil {
		return err
	}

	// Event will be published by the task service automatically
	return nil
}

// scheduleNextActions schedules the next actions based on the completion report
func (h *CompletionHandler) scheduleNextActions(ctx context.Context, report *TaskCompletionReport) error {
	// This could trigger specific agents based on NextActions and RequiredAgents
	// For now, just log the actions
	return nil
}

// publishCompletionEvent publishes the task completion event
func (h *CompletionHandler) publishCompletionEvent(ctx context.Context, report *TaskCompletionReport) error {
	// Convert types for event data
	var eventArtifacts []event.Artifact
	for _, artifact := range report.Artifacts {
		eventArtifacts = append(eventArtifacts, event.Artifact{
			Path:        artifact.Path,
			Type:        artifact.Type,
			Description: artifact.Description,
		})
	}

	var eventNextActions []event.NextAction
	for _, action := range report.NextActions {
		eventNextActions = append(eventNextActions, event.NextAction{
			Description: action.Description,
			Priority:    action.Priority,
			AssignedTo:  action.AssignedTo,
		})
	}

	eventData := event.TaskCompletedData{
		TaskID:      report.TaskID,
		AgentRole:   report.AgentRole,
		CompletedAt: time.Now(),
		WorkSummary: report.WorkSummary,
		Artifacts:   eventArtifacts,
		NextActions: eventNextActions,
	}

	return h.eventBus.Publish(ctx, "task.completed", eventData)
}
