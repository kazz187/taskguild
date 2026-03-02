package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// createTaskDirective holds parsed CREATE_TASK directive fields.
type createTaskDirective struct {
	Title          string
	Description    string
	StatusID       string // could be status name, resolved from _workflow_statuses
	UseWorktree    *bool
	Worktree       string
	PermissionMode string
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
				case "permission_mode":
					d.PermissionMode = value
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
	projectID := metadata["_project_id"]
	workflowID := metadata["_workflow_id"]

	if projectID == "" || workflowID == "" {
		log.Printf("[task:%s] cannot create task: missing _project_id or _workflow_id", sourceTaskID)
		return
	}

	// Resolve status name to ID if needed.
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

	req := &v1.CreateTaskRequest{
		ProjectId:      projectID,
		WorkflowId:     workflowID,
		Title:          directive.Title,
		Description:    directive.Description,
		UseWorktree:    useWorktree,
		PermissionMode: directive.PermissionMode,
		Metadata:       taskMeta,
	}
	if statusID != "" {
		req.StatusId = &statusID
	}

	resp, err := taskClient.CreateTask(ctx, connect.NewRequest(req))
	if err != nil {
		log.Printf("[task:%s] failed to create task %q: %v", sourceTaskID, directive.Title, err)
		return
	}

	newTask := resp.Msg.GetTask()
	if newTask != nil {
		log.Printf("[task:%s] created child task %s: %q", sourceTaskID, newTask.GetId(), directive.Title)
	} else {
		log.Printf("[task:%s] created child task (no task in response): %q", sourceTaskID, directive.Title)
	}
}

// resolveStatusID attempts to resolve a status name to its ID using the workflow_statuses JSON.
// If the value already looks like an ID (found directly), it is returned as-is.
func resolveStatusID(statusIDOrName string, workflowStatusesJSON string) string {
	if workflowStatusesJSON == "" {
		return statusIDOrName
	}
	type statusEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var statuses []statusEntry
	if err := json.Unmarshal([]byte(workflowStatusesJSON), &statuses); err != nil {
		return statusIDOrName
	}
	// Check if it matches an ID directly.
	for _, s := range statuses {
		if s.ID == statusIDOrName {
			return statusIDOrName
		}
	}
	// Try matching by name (case-insensitive).
	for _, s := range statuses {
		if strings.EqualFold(s.Name, statusIDOrName) {
			return s.ID
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
	transitionsJSON := metadata["_available_transitions"]
	if transitionsJSON == "" {
		return fmt.Errorf("no available transitions in metadata")
	}

	type transitionEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var transitions []transitionEntry
	if err := json.Unmarshal([]byte(transitionsJSON), &transitions); err != nil {
		return fmt.Errorf("failed to parse available transitions: %w", err)
	}
	if len(transitions) == 0 {
		return fmt.Errorf("available transitions list is empty")
	}

	if nextStatusID == "" {
		// Auto-transition if exactly one transition is available.
		if len(transitions) == 1 {
			nextStatusID = transitions[0].ID
			log.Printf("[task:%s] no NEXT_STATUS found, auto-transitioning to %s (%s)", taskID, nextStatusID, transitions[0].Name)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Auto-transitioning to %s (%s)", nextStatusID, transitions[0].Name), nil)
		} else {
			var ids []string
			for _, t := range transitions {
				ids = append(ids, t.ID)
			}
			return fmt.Errorf("no NEXT_STATUS found and %d transitions available (%s), cannot auto-transition", len(transitions), strings.Join(ids, ", "))
		}
	} else {
		// Validate the chosen status is in available transitions.
		valid := false
		for _, t := range transitions {
			if t.ID == nextStatusID {
				valid = true
				break
			}
		}
		if !valid {
			var ids []string
			for _, t := range transitions {
				ids = append(ids, t.ID)
			}
			return fmt.Errorf("NEXT_STATUS %q is not a valid transition (available: %s)", nextStatusID, strings.Join(ids, ", "))
		}
	}

	_, err := taskClient.UpdateTaskStatus(ctx, connect.NewRequest(&v1.UpdateTaskStatusRequest{
		Id:       taskID,
		StatusId: nextStatusID,
	}))
	if err != nil {
		return fmt.Errorf("UpdateTaskStatus RPC failed for %s: %w", nextStatusID, err)
	}

	log.Printf("[task:%s] status transitioned to %s", taskID, nextStatusID)
	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
		fmt.Sprintf("Status transitioned to %s", nextStatusID), nil)
	return nil
}

func saveSessionID(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, sessionID string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"session_id": sessionID},
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save session_id: %v", taskID, err)
	}
}

func saveWorktreeName(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, name string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"worktree": name},
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save worktree_name: %v", taskID, err)
	} else {
		log.Printf("[task:%s] worktree name: %s", taskID, name)
	}
}

func saveTaskDescription(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, description string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:          taskID,
		Description: description,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save task description: %v", taskID, err)
	} else {
		log.Printf("[task:%s] task description updated (%d chars)", taskID, len(description))
	}
}
