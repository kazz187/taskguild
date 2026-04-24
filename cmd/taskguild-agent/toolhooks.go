package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"connectrpc.com/connect"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// buildToolUseHooks creates PreToolUse, PostToolUse and PostToolUseFail hook matchers
// that log tool invocations (with input and output) to the task timeline.
// It also tracks plan file writes and saves the plan content as a RESULT log
// when ExitPlanMode is called.
// The PreToolUse hook intercepts ExitPlanMode to require user approval before
// the plan is accepted and the agent exits plan mode.
func buildToolUseHooks(
	tl *taskLogger,
	taskID string,
	onModeChange func(newMode string),
	client taskguildv1connect.AgentManagerServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	agentManagerID string,
	waiter *interactionWaiter,
) map[claudeagent.HookEvent][]*claudeagent.HookMatcher {
	// Track the most recently written plan file path across hook invocations.
	var planFilePath string

	return map[claudeagent.HookEvent][]*claudeagent.HookMatcher{
		claudeagent.HookEventPreToolUse: {
			{
				Matcher: "",
				Hooks: []claudeagent.HookCallback{
					func(input claudeagent.HookInput, toolUseID string, hookCtx claudeagent.HookContext) (claudeagent.HookOutput, error) {
						if input.ToolName != "ExitPlanMode" {
							return claudeagent.HookOutput{}, nil
						}

						return handleExitPlanModeApproval(hookCtx.Signal, input, client, interClient, taskID, agentManagerID, waiter, planFilePath)
					},
				},
			},
		},
		claudeagent.HookEventPostToolUse: {
			{
				Matcher: "",
				Hooks: []claudeagent.HookCallback{
					func(input claudeagent.HookInput, toolUseID string, ctx claudeagent.HookContext) (claudeagent.HookOutput, error) {
						// Track permission mode changes from CLI.
						if input.PermissionMode != "" && onModeChange != nil {
							onModeChange(input.PermissionMode)
						}

						logToolUse(tl, taskID, input, false)

						// Track plan file writes.
						if input.ToolName == "Write" || input.ToolName == "Edit" {
							if fp, ok := input.ToolInput["file_path"].(string); ok {
								if strings.Contains(fp, ".claude/plans/") {
									planFilePath = fp
								}
							}
						}

						// Save plan result when ExitPlanMode is called.
						if input.ToolName == "ExitPlanMode" && tl != nil {
							var planContent string

							if input.ToolResponse != nil {
								if s, ok := input.ToolResponse.(string); ok {
									// Try to parse as JSON and extract the "plan" field.
									var obj map[string]any
									if json.Unmarshal([]byte(s), &obj) == nil {
										if plan, ok := obj["plan"].(string); ok {
											planContent = plan
										} else {
											planContent = s
										}
									} else {
										planContent = s
									}
								} else if m, ok := input.ToolResponse.(map[string]any); ok {
									if plan, ok := m["plan"].(string); ok {
										planContent = plan
									}
								}
							}

							if planContent == "" && planFilePath != "" {
								// Fallback: read from plan file if no tool response.
								if content, err := os.ReadFile(planFilePath); err == nil {
									planContent = string(content)
								}
							}

							if planContent != "" {
								savePlanResult(context.Background(), taskID, planContent, tl)
							}
						}

						return claudeagent.HookOutput{}, nil
					},
				},
			},
		},
		claudeagent.HookEventPostToolUseFail: {
			{
				Matcher: "",
				Hooks: []claudeagent.HookCallback{
					func(input claudeagent.HookInput, toolUseID string, ctx claudeagent.HookContext) (claudeagent.HookOutput, error) {
						// Track permission mode changes from CLI.
						if input.PermissionMode != "" && onModeChange != nil {
							onModeChange(input.PermissionMode)
						}

						logToolUse(tl, taskID, input, true)

						return claudeagent.HookOutput{}, nil
					},
				},
			},
		},
	}
}

// logToolUse sends a TOOL_USE task log with the tool name, input parameters, and output/error.
func logToolUse(tl *taskLogger, taskID string, input claudeagent.HookInput, isFail bool) {
	toolName := input.ToolName
	summary := formatToolSummary(toolName, input.ToolInput)

	level := v1.TaskLogLevel_TASK_LOG_LEVEL_INFO
	if isFail {
		level = v1.TaskLogLevel_TASK_LOG_LEVEL_ERROR
	}

	metadata := map[string]string{
		"tool_name": toolName,
	}

	// Serialize tool input.
	if input.ToolInput != nil {
		if inputJSON, err := json.Marshal(input.ToolInput); err == nil {
			metadata["tool_input"] = truncateText(string(inputJSON), maxToolOutputSize)
		}
	}

	// Serialize tool output/response.
	// Note: input.ToolResponse is often already a JSON string (from Claude's raw
	// tool result). Re-marshaling such a string would double-encode it —
	// wrapping the content in quotes and escaping inner characters (e.g. `<`
	// becomes `\u003c`, newlines become literal `\n`). Detect that case and
	// store the string as-is; only marshal non-string payloads.
	if input.ToolResponse != nil {
		switch v := input.ToolResponse.(type) {
		case string:
			metadata["tool_output"] = truncateText(v, maxToolOutputSize)
		default:
			if outputJSON, err := json.Marshal(v); err == nil {
				metadata["tool_output"] = truncateText(string(outputJSON), maxToolOutputSize)
			} else {
				// Fallback: convert to string.
				metadata["tool_output"] = truncateText(fmt.Sprintf("%v", v), maxToolOutputSize)
			}
		}
	}

	// Capture error if present.
	if input.Error != "" {
		metadata["error"] = input.Error

		if !isFail {
			level = v1.TaskLogLevel_TASK_LOG_LEVEL_WARN
		}
	}

	// Add permission mode if available.
	if input.PermissionMode != "" {
		metadata["claude_mode"] = input.PermissionMode
	}

	slog.Info("tool_use", "task_id", taskID, "summary", summary, "failed", isFail, "claude_mode", input.PermissionMode)

	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TOOL_USE, level, summary, metadata)
}

// formatToolSummary creates a human-readable one-line summary for a tool invocation.
func formatToolSummary(toolName string, toolInput map[string]any) string {
	switch toolName {
	case "Read":
		if fp, ok := toolInput["file_path"].(string); ok {
			return "Read: " + fp
		}
	case "Write":
		if fp, ok := toolInput["file_path"].(string); ok {
			return "Write: " + fp
		}
	case "Edit":
		if fp, ok := toolInput["file_path"].(string); ok {
			return "Edit: " + fp
		}
	case "Bash":
		if cmd, ok := toolInput["command"].(string); ok {
			// Truncate long commands.
			if len(cmd) > 80 {
				cmd = cmd[:77] + "..."
			}

			return "Bash: " + cmd
		}
	case "Glob":
		if pattern, ok := toolInput["pattern"].(string); ok {
			return "Glob: " + pattern
		}
	case "Grep":
		if pattern, ok := toolInput["pattern"].(string); ok {
			path := ""
			if p, ok := toolInput["path"].(string); ok {
				path = " in " + p
			}

			return fmt.Sprintf("Grep: %q%s", pattern, path)
		}
	case "WebSearch":
		if query, ok := toolInput["query"].(string); ok {
			return fmt.Sprintf("WebSearch: %q", query)
		}
	case "WebFetch":
		if url, ok := toolInput["url"].(string); ok {
			return "WebFetch: " + url
		}
	case "Agent":
		if desc, ok := toolInput["description"].(string); ok {
			return "Agent: " + desc
		}
	case "TodoWrite":
		return "TodoWrite"
	case "NotebookEdit":
		if nbPath, ok := toolInput["notebook_path"].(string); ok {
			return "NotebookEdit: " + nbPath
		}
	case "Skill":
		if skill, ok := toolInput["skill"].(string); ok {
			return "Skill /" + skill
		}
	case "AskUserQuestion":
		return "AskUserQuestion"
	}

	// Generic fallback: just the tool name.
	return toolName
}

// truncateText truncates a string to maxLen characters, appending "..." if truncated.
func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen-3] + "..."
}

// handleExitPlanModeApproval intercepts ExitPlanMode via a PreToolUse hook.
// It reads the plan content from the tool input or the plan file, creates an
// interaction asking the user to approve or provide feedback, and blocks the
// tool if the user does not approve.
func handleExitPlanModeApproval(
	ctx context.Context,
	input claudeagent.HookInput,
	client taskguildv1connect.AgentManagerServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	taskID string,
	agentManagerID string,
	waiter *interactionWaiter,
	planFilePath string,
) (claudeagent.HookOutput, error) {
	logger := slog.Default().With("task_id", taskID)

	// Extract plan content from ExitPlanMode input or the tracked plan file.
	planContent := extractPlanContent(input.ToolInput, planFilePath)
	if planContent == "" {
		// No plan content available — allow ExitPlanMode without approval.
		logger.Warn("ExitPlanMode called but no plan content found, allowing without approval")
		return claudeagent.HookOutput{}, nil
	}

	// Create an interaction for user approval.
	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentManagerID,
		Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
		Title:       "Plan review",
		Description: planContent,
		Options: []*v1.InteractionOption{
			{Label: "Approve", Value: "approve", Description: "Approve the plan and proceed"},
			{Label: "Reject", Value: "reject", Description: "Reject the plan with feedback"},
		},
	}))
	if err != nil {
		logger.Error("failed to create plan approval interaction", "error", err)
		// On failure, allow ExitPlanMode to proceed rather than blocking indefinitely.
		return claudeagent.HookOutput{}, nil
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	logger.Info("waiting for plan approval", "interaction_id", interactionID)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return claudeagent.HookOutput{
			Decision: "block",
			Reason:   "context canceled while waiting for plan approval",
		}, nil
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			logger.Info("plan approval expired, blocking ExitPlanMode")

			return claudeagent.HookOutput{
				Decision: "block",
				Reason:   "Plan approval expired. Please revise the plan and try again.",
			}, nil
		}

		responseStr := inter.GetResponse()
		logger.Info("plan approval response", "response", responseStr)

		if responseStr == "approve" {
			return claudeagent.HookOutput{}, nil
		}

		// Any other response is treated as rejection with feedback.
		feedback := responseStr
		if feedback == "reject" {
			feedback = "The user rejected the plan. Please ask the user for feedback and revise the plan."
		}

		return claudeagent.HookOutput{
			Decision: "block",
			Reason:   "Plan not approved. User feedback: " + feedback,
		}, nil

	case msg := <-waiter.UserMessages():
		// User sent a free-form message — treat as feedback and block.
		logger.Info("user sent message during plan approval", "message_id", msg.GetId())

		if _, expErr := interClient.ExpireInteraction(ctx, connect.NewRequest(&v1.ExpireInteractionRequest{
			Id: interactionID,
		})); expErr != nil {
			logger.Error("failed to expire plan approval interaction", "error", expErr)
		}

		return claudeagent.HookOutput{
			Decision: "block",
			Reason:   "Plan not approved. User feedback: " + msg.GetTitle(),
		}, nil
	}
}

// extractPlanContent reads the plan content from ExitPlanMode tool input or
// falls back to reading the tracked plan file.
func extractPlanContent(toolInput map[string]any, planFilePath string) string {
	// ExitPlanMode's plan field contains the plan content directly.
	if plan, ok := toolInput["plan"].(string); ok && plan != "" {
		return plan
	}

	// Check planFilePath from tool input.
	if fp, ok := toolInput["planFilePath"].(string); ok && fp != "" {
		if content, err := os.ReadFile(fp); err == nil && len(content) > 0 {
			return string(content)
		}
	}

	// Fallback: read from the tracked plan file written by a prior Write tool call.
	if planFilePath != "" {
		if content, err := os.ReadFile(planFilePath); err == nil && len(content) > 0 {
			return string(content)
		}
	}

	return ""
}
