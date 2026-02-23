package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

func init() {
	// The SDK closes stdin after this timeout, breaking the control protocol
	// (permission responses can no longer be sent to Claude CLI).
	// Set to 30 days so user input waits are effectively unlimited.
	if os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT") == "" {
		os.Setenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT", "2592000000") // 30 days in ms
	}
}

const (
	maxConsecutiveErrors = 5
	initialBackoff       = 5 * time.Second
	maxBackoff           = 5 * time.Minute
)

func runTask(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskClient taskguildv1connect.TaskServiceClient,
	agentManagerID string,
	taskID string,
	instructions string,
	metadata map[string]string,
	workDir string,
) {
	reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "starting task")

	sessionID := metadata["_session_id"]
	prompt := buildUserPrompt(metadata)
	hasTransitions := metadata["_available_transitions"] != ""

	consecutiveErrors := 0
	backoff := initialBackoff

	for turn := 0; ; turn++ {
		opts := buildClaudeOptions(instructions, workDir, metadata, sessionID, client, ctx, taskID, agentManagerID)

		log.Printf("[task:%s] === Claude SDK Input (turn %d) ===", taskID, turn)
		if turn == 0 {
			log.Printf("[task:%s] SystemPrompt:\n%s", taskID, instructions)
			log.Printf("[task:%s] Metadata: %v", taskID, metadata)
			log.Printf("[task:%s] WorkDir: %s", taskID, workDir)
		}
		log.Printf("[task:%s] UserPrompt:\n%s", taskID, prompt)
		if sessionID != "" {
			log.Printf("[task:%s] Resume: %s", taskID, sessionID)
		}
		log.Printf("[task:%s] === End Claude SDK Input (turn %d) ===", taskID, turn)

		result, err := claudeagent.RunQuerySync(ctx, prompt, opts)

		log.Printf("[task:%s] === Claude SDK Output (turn %d) ===", taskID, turn)
		if err != nil {
			log.Printf("[task:%s] Error: %v", taskID, err)
		} else if result.Result != nil {
			log.Printf("[task:%s] IsError: %v", taskID, result.Result.IsError)
			log.Printf("[task:%s] SessionID: %s", taskID, result.Result.SessionID)
			log.Printf("[task:%s] Result: %s", taskID, result.Result.Result)
		} else {
			log.Printf("[task:%s] Result is nil", taskID)
		}
		log.Printf("[task:%s] === End Claude SDK Output (turn %d) ===", taskID, turn)

		// Save session ID for resume.
		if result.Result != nil && result.Result.SessionID != "" {
			sessionID = result.Result.SessionID
			saveSessionID(ctx, taskClient, taskID, sessionID)
		}

		// Handle errors with backoff retry.
		isError := false
		var errMsg string

		if err != nil {
			isError = true
			errMsg = err.Error()
		} else if result.Result != nil && result.Result.IsError {
			isError = true
			errMsg = result.Result.Result
			if errMsg == "" {
				errMsg = "Claude returned an error"
			}
		}

		if isError {
			consecutiveErrors++
			log.Printf("[task:%s] error (%d/%d): %s", taskID, consecutiveErrors, maxConsecutiveErrors, errMsg)

			if consecutiveErrors >= maxConsecutiveErrors {
				log.Printf("[task:%s] max consecutive errors reached, giving up", taskID)
				reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_FAILED, "", errMsg)
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_ERROR, errMsg)
				return
			}

			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_ERROR,
				fmt.Sprintf("error, retrying in %s: %s", backoff, errMsg))

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			// Exponential backoff, capped.
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Success — reset error tracking.
		consecutiveErrors = 0
		backoff = initialBackoff

		summary := ""
		if result.Result != nil {
			summary = result.Result.Result
		}

		// Check completion: NEXT_STATUS present means task is done.
		if parseNextStatus(summary) != "" {
			log.Printf("[task:%s] completed with NEXT_STATUS (turn %d)", taskID, turn)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			handleStatusTransition(ctx, taskClient, taskID, summary, metadata)
			return
		}

		// No transitions available (terminal status) means task is done.
		if !hasTransitions {
			log.Printf("[task:%s] completed at terminal status (turn %d)", taskID, turn)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			return
		}

		// Claude hasn't completed — wait for user input.
		log.Printf("[task:%s] waiting for user input (turn %d)", taskID, turn)
		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "waiting for user input")

		userResponse, err := waitForUserResponse(ctx, client, taskID, agentManagerID, summary)
		if err != nil {
			log.Printf("[task:%s] user response error: %v, completing task", taskID, err)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (no user response)")
			return
		}

		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "continuing task")
		prompt = userResponse
	}
}

// buildClaudeOptions constructs ClaudeAgentOptions for each turn.
func buildClaudeOptions(
	instructions string,
	workDir string,
	metadata map[string]string,
	sessionID string,
	client taskguildv1connect.AgentManagerServiceClient,
	ctx context.Context,
	taskID string,
	agentManagerID string,
) *claudeagent.ClaudeAgentOptions {
	// Permission mode from agent config (default if empty)
	permMode := claudeagent.PermissionModeDefault
	if pm := metadata["_permission_mode"]; pm != "" {
		permMode = claudeagent.PermissionMode(pm)
	}

	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   instructions,
		Cwd:            workDir,
		PermissionMode: permMode,
		CanUseTool: func(toolName string, input map[string]any, toolCtx claudeagent.ToolPermissionContext) (claudeagent.PermissionResult, error) {
			return handlePermissionRequest(ctx, client, taskID, agentManagerID, toolName, input)
		},
	}

	if metadata["_use_worktree"] == "true" {
		empty := ""
		opts.Worktree = &empty
	}

	if sessionID != "" {
		opts.Resume = sessionID
	}

	return opts
}

// waitForUserResponse creates a QUESTION interaction and polls until the user responds.
func waitForUserResponse(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentManagerID string,
	claudeOutput string,
) (string, error) {
	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentManagerID,
		Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
		Title:       "Agent needs your input",
		Description: claudeOutput,
	}))
	if err != nil {
		return "", fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for user response (interaction: %s)", taskID, interactionID)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			pollResp, err := client.GetInteractionResponse(ctx, connect.NewRequest(&v1.GetInteractionResponseRequest{
				InteractionId: interactionID,
			}))
			if err != nil {
				log.Printf("[task:%s] poll error for interaction %s: %v", taskID, interactionID, err)
				continue
			}

			interaction := pollResp.Msg.GetInteraction()
			if interaction.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_RESPONDED {
				response := interaction.GetResponse()
				log.Printf("[task:%s] user responded to interaction %s", taskID, interactionID)
				return response, nil
			}

			if interaction.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
				return "", fmt.Errorf("interaction expired")
			}
		}
	}
}

// buildUserPrompt constructs the user prompt from enriched metadata.
func buildUserPrompt(metadata map[string]string) string {
	title := metadata["_task_title"]
	description := metadata["_task_description"]
	currentStatusName := metadata["_current_status_name"]
	transitionsJSON := metadata["_available_transitions"]

	// If no task info in metadata, fall back to prompt or generic message.
	if title == "" && description == "" {
		if p := metadata["prompt"]; p != "" {
			return p
		}
		return "Please complete the assigned task."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", title))
	if description != "" {
		sb.WriteString(fmt.Sprintf("## Description\n%s\n\n", description))
	}

	// Add status transition instructions if transitions are available.
	if transitionsJSON != "" {
		type transitionEntry struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var transitions []transitionEntry
		if err := json.Unmarshal([]byte(transitionsJSON), &transitions); err == nil && len(transitions) > 0 {
			sb.WriteString("## Status Transition\n")
			if currentStatusName != "" {
				sb.WriteString(fmt.Sprintf("Current status: %s\n", currentStatusName))
			}
			sb.WriteString("Available next statuses:\n")
			for _, t := range transitions {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", t.ID, t.Name))
			}
			sb.WriteString("\nAfter completing the task, output your chosen next status on the last line:\nNEXT_STATUS: <status_id>\n")
		}
	}

	sb.WriteString("\n## Interactive Session\n")
	sb.WriteString("You are in an interactive session. ")
	sb.WriteString("If you need user input, approval, or clarification, ")
	sb.WriteString("clearly state what you need. You will receive a response and can continue.\n")
	sb.WriteString("When the task is fully complete, output NEXT_STATUS on the last line.\n")

	return sb.String()
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

// handleStatusTransition parses the agent result and transitions the task status.
func handleStatusTransition(
	ctx context.Context,
	taskClient taskguildv1connect.TaskServiceClient,
	taskID string,
	resultText string,
	metadata map[string]string,
) {
	transitionsJSON := metadata["_available_transitions"]
	if transitionsJSON == "" {
		return
	}

	type transitionEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var transitions []transitionEntry
	if err := json.Unmarshal([]byte(transitionsJSON), &transitions); err != nil || len(transitions) == 0 {
		return
	}

	nextStatusID := parseNextStatus(resultText)

	if nextStatusID == "" {
		// Auto-transition if exactly one transition is available.
		if len(transitions) == 1 {
			nextStatusID = transitions[0].ID
			log.Printf("[task:%s] no NEXT_STATUS found, auto-transitioning to %s (%s)", taskID, nextStatusID, transitions[0].Name)
		} else {
			log.Printf("[task:%s] WARNING: no NEXT_STATUS found and %d transitions available, skipping transition", taskID, len(transitions))
			return
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
			log.Printf("[task:%s] WARNING: NEXT_STATUS %q is not a valid transition, skipping", taskID, nextStatusID)
			return
		}
	}

	_, err := taskClient.UpdateTaskStatus(ctx, connect.NewRequest(&v1.UpdateTaskStatusRequest{
		Id:       taskID,
		StatusId: nextStatusID,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to transition status to %s: %v", taskID, nextStatusID, err)
		return
	}
	log.Printf("[task:%s] status transitioned to %s", taskID, nextStatusID)
}

func saveSessionID(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, sessionID string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"_session_id": sessionID},
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save session_id: %v", taskID, err)
	}
}

func handlePermissionRequest(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	toolName string,
	input map[string]any,
) (claudeagent.PermissionResult, error) {
	description := fmt.Sprintf("Tool: %s\nInput: %v", toolName, input)

	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentID,
		Type:        v1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST,
		Title:       fmt.Sprintf("Permission request: %s", toolName),
		Description: description,
		Options: []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
			{Label: "Deny", Value: "deny", Description: "Deny this tool use"},
		},
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for permission response (interaction: %s, tool: %s)", taskID, interactionID, toolName)

	// Poll for response
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
		case <-ticker.C:
			pollResp, err := client.GetInteractionResponse(ctx, connect.NewRequest(&v1.GetInteractionResponseRequest{
				InteractionId: interactionID,
			}))
			if err != nil {
				log.Printf("[task:%s] poll error for interaction %s: %v", taskID, interactionID, err)
				continue
			}

			interaction := pollResp.Msg.GetInteraction()
			if interaction.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_RESPONDED {
				response := interaction.GetResponse()
				if response == "allow" {
					log.Printf("[task:%s] permission granted for %s", taskID, toolName)
					return claudeagent.PermissionResultAllow{}, nil
				}
				log.Printf("[task:%s] permission denied for %s", taskID, toolName)
				return claudeagent.PermissionResultDeny{Message: "user denied permission"}, nil
			}

			if interaction.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
				return claudeagent.PermissionResultDeny{Message: "permission request expired"}, nil
			}
		}
	}
}

func reportTaskResult(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	status v1.TaskResultStatus,
	summary string,
	errMsg string,
) {
	_, err := client.ReportTaskResult(ctx, connect.NewRequest(&v1.ReportTaskResultRequest{
		TaskId:       taskID,
		Status:       status,
		Summary:      summary,
		ErrorMessage: errMsg,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to report task result: %v", taskID, err)
	}
}

func reportAgentStatus(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	agentManagerID string,
	taskID string,
	status v1.AgentStatus,
	message string,
) {
	_, err := client.ReportAgentStatus(ctx, connect.NewRequest(&v1.ReportAgentStatusRequest{
		AgentManagerId: agentManagerID,
		TaskId:         taskID,
		Status:         status,
		Message:        message,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to report agent status: %v", taskID, err)
	}
}
