package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/pkg/clog"
)

// errInvalidTransition is returned when the agent outputs a NEXT_STATUS
// that does not match any of the available transitions.
var errInvalidTransition = errors.New("invalid status transition")

// createTaskDirective holds parsed CREATE_TASK directive fields.
type createTaskDirective struct {
	Title       string
	Description string
	StatusID    string // could be status name, resolved from _workflow_statuses
	UseWorktree *bool
	Worktree    string
}

// parseCreateTasks extracts all CREATE_TASK_START...CREATE_TASK_END blocks from the result text.
// Key-value headers are parsed until the first empty line; everything after is the description.
func parseCreateTasks(resultText string) []createTaskDirective {
	var directives []createTaskDirective
	remaining := resultText
	for {
		startIdx := strings.Index(remaining, "CREATE_TASK_START")
		if startIdx == -1 {
			break
		}
		afterStart := remaining[startIdx+len("CREATE_TASK_START"):]
		endIdx := strings.Index(afterStart, "CREATE_TASK_END")
		if endIdx == -1 {
			break
		}
		body := afterStart[:endIdx]
		remaining = afterStart[endIdx+len("CREATE_TASK_END"):]

		// Trim leading/trailing newlines from the body.
		body = strings.TrimLeft(body, "\r\n")
		body = strings.TrimRight(body, "\r\n \t")

		var d createTaskDirective
		lines := strings.Split(body, "\n")
		descStart := 0
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				// Empty line marks end of headers; rest is description.
				descStart = i + 1
				break
			}
			if colonIdx := strings.Index(trimmed, ":"); colonIdx > 0 {
				key := strings.TrimSpace(trimmed[:colonIdx])
				value := strings.TrimSpace(trimmed[colonIdx+1:])
				switch strings.ToLower(key) {
				case "title":
					d.Title = value
				case "status":
					d.StatusID = value
				case "use_worktree":
					b := strings.ToLower(value) == "true"
					d.UseWorktree = &b
				case "worktree":
					d.Worktree = value
				}
			}
			descStart = i + 1
		}
		if descStart < len(lines) {
			d.Description = strings.TrimSpace(strings.Join(lines[descStart:], "\n"))
		}

		if d.Title != "" {
			directives = append(directives, d)
		}
	}
	return directives
}

// stripCreateTasks removes all CREATE_TASK_START...CREATE_TASK_END blocks from the result text.
func stripCreateTasks(resultText string) string {
	for {
		startIdx := strings.Index(resultText, "CREATE_TASK_START")
		if startIdx == -1 {
			break
		}
		endIdx := strings.Index(resultText[startIdx:], "CREATE_TASK_END")
		if endIdx == -1 {
			break
		}
		// Find the start of the line containing the start marker.
		lineStart := strings.LastIndex(resultText[:startIdx], "\n")
		if lineStart == -1 {
			lineStart = 0
		}
		fullEndIdx := startIdx + endIdx + len("CREATE_TASK_END")
		// Skip trailing newline if present.
		if fullEndIdx < len(resultText) && resultText[fullEndIdx] == '\n' {
			fullEndIdx++
		}
		resultText = resultText[:lineStart] + resultText[fullEndIdx:]
	}
	return strings.TrimSpace(resultText)
}

// createTaskFromDirective creates a new task via TaskService.CreateTask based on the directive.
func createTaskFromDirective(
	ctx context.Context,
	taskClient taskguildv1connect.TaskServiceClient,
	sourceTaskID string,
	metadata map[string]string,
	directive createTaskDirective,
) {
	logger := clog.LoggerFromContext(ctx)

	projectID := metadata["_project_id"]
	workflowID := metadata["_workflow_id"]

	if projectID == "" || workflowID == "" {
		logger.Error("cannot create task: missing _project_id or _workflow_id")
		return
	}

	// Resolve status name if needed.
	statusID := directive.StatusID
	if statusID != "" {
		statusID = resolveStatusID(statusID, metadata["_workflow_statuses"])
	}

	// Determine use_worktree: inherit from current task if not specified.
	useWorktree := false
	if directive.UseWorktree != nil {
		useWorktree = *directive.UseWorktree
	} else if metadata["_use_worktree"] == "true" {
		useWorktree = true
	}

	taskMeta := map[string]string{
		"source_task_id": sourceTaskID,
	}
	if directive.Worktree != "" {
		taskMeta["worktree"] = directive.Worktree
	}

	// Pass the parent's per-status session ID so the subtask can resume it.
	targetStatus := statusID
	if targetStatus == "" {
		targetStatus = metadata["_current_status_name"]
	}
	if targetStatus != "" {
		if parentSessionID := metadata["session_id_"+targetStatus]; parentSessionID != "" {
			taskMeta["session_id_"+targetStatus] = parentSessionID
		}
	}

	req := &v1.CreateTaskRequest{
		ProjectId:   projectID,
		WorkflowId:  workflowID,
		Title:       directive.Title,
		Description: directive.Description,
		UseWorktree: useWorktree,
		Metadata:    taskMeta,
	}
	if statusID != "" {
		req.StatusId = &statusID
	}

	resp, err := taskClient.CreateTask(ctx, connect.NewRequest(req))
	if err != nil {
		logger.Error("failed to create task", "title", directive.Title, "error", err)
		return
	}

	newTask := resp.Msg.GetTask()
	if newTask != nil {
		logger.Info("created child task", "child_task_id", newTask.GetId(), "title", directive.Title)
	} else {
		logger.Info("created child task (no task in response)", "title", directive.Title)
	}
}

// resolveStatusID attempts to resolve a status name using the workflow_statuses JSON.
// It performs a case-insensitive match on Name and returns the matched Name.
func resolveStatusID(statusIDOrName string, workflowStatusesJSON string) string {
	if workflowStatusesJSON == "" {
		return statusIDOrName
	}
	type statusEntry struct {
		Name string `json:"name"`
	}
	var statuses []statusEntry
	if err := json.Unmarshal([]byte(workflowStatusesJSON), &statuses); err != nil {
		return statusIDOrName
	}
	// Try matching by name (case-insensitive).
	for _, s := range statuses {
		if strings.EqualFold(s.Name, statusIDOrName) {
			return s.Name
		}
	}
	return statusIDOrName
}

// parseNextStatus extracts a "NEXT_STATUS: <id>" directive from the result text.
func parseNextStatus(resultText string) string {
	lines := strings.Split(resultText, "\n")
	// Scan from the end to find the last NEXT_STATUS directive.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "NEXT_STATUS:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "NEXT_STATUS:"))
		}
	}
	return ""
}

// stripNextStatus removes all "NEXT_STATUS: ..." lines from the result text
// so that the control directive does not appear in the stored result_summary.
func stripNextStatus(resultText string) string {
	lines := strings.Split(resultText, "\n")
	var filtered []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "NEXT_STATUS:") {
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

// parseTaskDescription extracts a task description update from the result text.
// The description is enclosed between TASK_DESCRIPTION_START and TASK_DESCRIPTION_END markers.
// Returns the extracted description (trimmed) or empty string if no markers found.
func parseTaskDescription(resultText string) string {
	const startMarker = "TASK_DESCRIPTION_START"
	const endMarker = "TASK_DESCRIPTION_END"

	startIdx := strings.Index(resultText, startMarker)
	if startIdx == -1 {
		return ""
	}
	contentStart := startIdx + len(startMarker)

	endIdx := strings.Index(resultText[contentStart:], endMarker)
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(resultText[contentStart : contentStart+endIdx])
}

// stripTaskDescription removes the TASK_DESCRIPTION block from the result text
// so it doesn't clutter the reported summary.
func stripTaskDescription(resultText string) string {
	const startMarker = "TASK_DESCRIPTION_START"
	const endMarker = "TASK_DESCRIPTION_END"

	startIdx := strings.Index(resultText, startMarker)
	if startIdx == -1 {
		return resultText
	}

	endIdx := strings.Index(resultText[startIdx:], endMarker)
	if endIdx == -1 {
		return resultText
	}

	// Find the start of the line containing the start marker.
	lineStart := strings.LastIndex(resultText[:startIdx], "\n")
	if lineStart == -1 {
		lineStart = 0
	}

	fullEndIdx := startIdx + endIdx + len(endMarker)
	// Skip trailing newline if present.
	if fullEndIdx < len(resultText) && resultText[fullEndIdx] == '\n' {
		fullEndIdx++
	}

	return strings.TrimSpace(resultText[:lineStart] + resultText[fullEndIdx:])
}

// transitionEntry represents one available status transition.
type transitionEntry struct {
	Name string `json:"name"`
}

// parseAvailableTransitions parses the _available_transitions JSON from metadata.
// Self-transitions (target == current status) are filtered out.
func parseAvailableTransitions(metadata map[string]string) ([]transitionEntry, error) {
	transitionsJSON := metadata["_available_transitions"]
	if transitionsJSON == "" {
		return nil, fmt.Errorf("no available transitions in metadata")
	}
	var raw []transitionEntry
	if err := json.Unmarshal([]byte(transitionsJSON), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse available transitions: %w", err)
	}
	// Filter out self-transitions.
	currentStatus := metadata["_current_status_name"]
	transitions := make([]transitionEntry, 0, len(raw))
	for _, t := range raw {
		if !strings.EqualFold(t.Name, currentStatus) {
			transitions = append(transitions, t)
		}
	}
	if len(transitions) == 0 {
		return nil, fmt.Errorf("available transitions list is empty (after filtering self-transitions)")
	}
	return transitions, nil
}

// validateAndResolveTransition checks whether nextStatusID matches one of the
// available transitions in metadata. It performs a case-insensitive name match.
// Returns the resolved status name on success, or errInvalidTransition if no match is found.
func validateAndResolveTransition(nextStatusID string, metadata map[string]string) (string, error) {
	transitions, err := parseAvailableTransitions(metadata)
	if err != nil {
		return "", err
	}

	// Case-insensitive name match.
	for _, t := range transitions {
		if strings.EqualFold(t.Name, nextStatusID) {
			return t.Name, nil
		}
	}

	return "", fmt.Errorf("%w: %q (available: %s)", errInvalidTransition, nextStatusID, formatTransitionList(transitions))
}

// formatTransitionList formats a list of transitions as "Name, ..." for error messages.
func formatTransitionList(transitions []transitionEntry) string {
	parts := make([]string, len(transitions))
	for i, t := range transitions {
		parts[i] = t.Name
	}
	return strings.Join(parts, ", ")
}

// buildTransitionRetryPrompt constructs a corrective prompt to send to the agent
// when it outputs an invalid NEXT_STATUS value.
func buildTransitionRetryPrompt(failedStatusID string, metadata map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("The status transition to %q failed because it is not a valid transition from the current status.\n\n", failedStatusID))

	transitions, err := parseAvailableTransitions(metadata)
	if err == nil && len(transitions) > 0 {
		sb.WriteString("Valid transitions are:\n")
		for _, t := range transitions {
			sb.WriteString(fmt.Sprintf("- %s\n", t.Name))
		}
	}

	sb.WriteString("\nPlease output the correct status on the LAST LINE of your response in the format:\n")
	sb.WriteString("NEXT_STATUS: <status>\n\n")
	sb.WriteString("Use ONLY one of the statuses listed above.")
	return sb.String()
}

// handleStatusTransition validates and executes a task status transition.
// nextStatusID is the pre-parsed target status ID (from parseNextStatus).
// When nextStatusID is empty, auto-transition is attempted if exactly one
// transition is available.  Errors are returned so the caller can log them
// to the task logger for user visibility.
func handleStatusTransition(
	ctx context.Context,
	taskClient taskguildv1connect.TaskServiceClient,
	taskID string,
	nextStatusID string,
	metadata map[string]string,
	tl *taskLogger,
) error {
	logger := clog.LoggerFromContext(ctx)

	transitions, err := parseAvailableTransitions(metadata)
	if err != nil {
		return err
	}

	if nextStatusID == "" {
		// Auto-transition if exactly one transition is available.
		if len(transitions) == 1 {
			nextStatusID = transitions[0].Name
			logger.Info("no NEXT_STATUS found, auto-transitioning", "next_status", nextStatusID)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Auto-transitioning to %s", nextStatusID), nil)
		} else {
			return fmt.Errorf("no NEXT_STATUS found and %d transitions available (%s), cannot auto-transition", len(transitions), formatTransitionList(transitions))
		}
	} else {
		// Validate and resolve the chosen status (supports name fallback).
		resolvedName, err := validateAndResolveTransition(nextStatusID, metadata)
		if err != nil {
			return err
		}
		nextStatusID = resolvedName
	}

	// Per-status session IDs (session_id_{StatusName}) are never cleared.
	// They survive all transitions for subtask inheritance and session resume.

	_, err = taskClient.UpdateTaskStatus(ctx, connect.NewRequest(&v1.UpdateTaskStatusRequest{
		Id:       taskID,
		StatusId: nextStatusID,
	}))
	if err != nil {
		return fmt.Errorf("UpdateTaskStatus RPC failed for %s: %w", nextStatusID, err)
	}

	logger.Info("status transitioned", "next_status", nextStatusID)
	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
		fmt.Sprintf("Status transitioned to %s", nextStatusID), nil)
	return nil
}

// saveSessionID persists the session ID under a per-status key (session_id_{StatusName}).
// The global "session_id" key is no longer used. Per-status keys survive all
// transitions and are used for session resume and subtask inheritance.
func saveSessionID(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, sessionID string, metadata map[string]string) {
	if sessionID == "" {
		return // nothing to save; per-status keys are never cleared
	}
	statusName := metadata["_current_status_name"]
	if statusName == "" {
		return
	}
	logger := clog.LoggerFromContext(ctx)
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"session_id_" + statusName: sessionID},
	}))
	if err != nil {
		logger.Error("failed to save session_id", "error", err)
	}
}

func saveWorktreeName(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, name string) {
	logger := clog.LoggerFromContext(ctx)
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"worktree": name},
	}))
	if err != nil {
		logger.Error("failed to save worktree_name", "error", err)
	} else {
		logger.Info("worktree name saved", "worktree_name", name)
	}
}

func saveClaudeMode(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, mode string) {
	logger := clog.LoggerFromContext(ctx)
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"claude_mode": mode},
	}))
	if err != nil {
		logger.Error("failed to save claude_mode", "error", err)
	}
}

func saveTaskDescription(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, description string) {
	logger := clog.LoggerFromContext(ctx)
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:          taskID,
		Description: description,
	}))
	if err != nil {
		logger.Error("failed to save task description", "error", err)
	} else {
		logger.Info("task description updated", "description_length", len(description))
	}
}

func savePlanResult(ctx context.Context, taskID, content string, tl *taskLogger) {
	logger := clog.LoggerFromContext(ctx)
	// Plan results are stored only as append-only RESULT logs (no metadata overwrite).
	if tl != nil {
		preview := content
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_RESULT, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			preview,
			map[string]string{
				"full_text":   content,
				"result_type": "plan",
			})
		logger.Info("plan_result saved as log", "content_length", len(content))
	}
}
