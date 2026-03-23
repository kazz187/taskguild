package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// buildToolUseHooks creates PostToolUse and PostToolUseFail hook matchers
// that log tool invocations (with input and output) to the task timeline.
// When a taskClient is provided, it also tracks plan file writes and saves
// the plan content to task metadata when ExitPlanMode is called.
func buildToolUseHooks(tl *taskLogger, taskID string, taskClient taskguildv1connect.TaskServiceClient) map[claudeagent.HookEvent][]*claudeagent.HookMatcher {
	// Track the most recently written plan file path across hook invocations.
	var planFilePath string

	return map[claudeagent.HookEvent][]*claudeagent.HookMatcher{
		claudeagent.HookEventPostToolUse: {
			{
				Matcher: "",
				Hooks: []claudeagent.HookCallback{
					func(input claudeagent.HookInput, toolUseID string, ctx claudeagent.HookContext) (claudeagent.HookOutput, error) {
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
						if input.ToolName == "ExitPlanMode" && taskClient != nil {
							var planContent string
							if input.ToolResponse != nil {
								if s, ok := input.ToolResponse.(string); ok {
									// Try to parse as JSON and extract the "plan" field.
									var obj map[string]interface{}
									if json.Unmarshal([]byte(s), &obj) == nil {
										if plan, ok := obj["plan"].(string); ok {
											planContent = plan
										} else {
											planContent = s
										}
									} else {
										planContent = s
									}
								} else if m, ok := input.ToolResponse.(map[string]interface{}); ok {
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
								savePlanResult(context.Background(), taskClient, taskID, planContent, tl)
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
	if input.ToolResponse != nil {
		if outputJSON, err := json.Marshal(input.ToolResponse); err == nil {
			metadata["tool_output"] = truncateText(string(outputJSON), maxToolOutputSize)
		} else {
			// Fallback: convert to string.
			metadata["tool_output"] = truncateText(fmt.Sprintf("%v", input.ToolResponse), maxToolOutputSize)
		}
	}

	// Capture error if present.
	if input.Error != "" {
		metadata["error"] = input.Error
		if !isFail {
			level = v1.TaskLogLevel_TASK_LOG_LEVEL_WARN
		}
	}

	slog.Info("tool_use", "task_id", taskID, "summary", summary, "failed", isFail)

	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TOOL_USE, level, summary, metadata)
}

// formatToolSummary creates a human-readable one-line summary for a tool invocation.
func formatToolSummary(toolName string, toolInput map[string]any) string {
	switch toolName {
	case "Read":
		if fp, ok := toolInput["file_path"].(string); ok {
			return fmt.Sprintf("Read: %s", fp)
		}
	case "Write":
		if fp, ok := toolInput["file_path"].(string); ok {
			return fmt.Sprintf("Write: %s", fp)
		}
	case "Edit":
		if fp, ok := toolInput["file_path"].(string); ok {
			return fmt.Sprintf("Edit: %s", fp)
		}
	case "Bash":
		if cmd, ok := toolInput["command"].(string); ok {
			// Truncate long commands.
			if len(cmd) > 80 {
				cmd = cmd[:77] + "..."
			}
			return fmt.Sprintf("Bash: %s", cmd)
		}
	case "Glob":
		if pattern, ok := toolInput["pattern"].(string); ok {
			return fmt.Sprintf("Glob: %s", pattern)
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
			return fmt.Sprintf("WebFetch: %s", url)
		}
	case "Agent":
		if desc, ok := toolInput["description"].(string); ok {
			return fmt.Sprintf("Agent: %s", desc)
		}
	case "TodoWrite":
		return "TodoWrite"
	case "NotebookEdit":
		if nbPath, ok := toolInput["notebook_path"].(string); ok {
			return fmt.Sprintf("NotebookEdit: %s", nbPath)
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

// truncateLines returns the first few lines of a multi-line string.
func truncateLines(s string, maxLines int) string {
	lines := strings.SplitN(s, "\n", maxLines+1)
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n..."
}
