package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// resultHistoryEntry represents a single entry in the task result history
// passed via _result_history metadata.
type resultHistoryEntry struct {
	ResultType string `json:"result_type"`
	Text       string `json:"text"`
	CreatedAt  string `json:"created_at"`
}

// buildUserPrompt constructs the user prompt from enriched metadata.
// Keeps only the task content and current status — all boilerplate instructions
// live in the system prompt (buildWorkflowContext).
func buildUserPrompt(metadata map[string]string, workDir string) string {
	title := metadata["_task_title"]
	description := metadata["_task_description"]

	// If no task info in metadata, fall back to prompt or generic message.
	if title == "" && description == "" {
		if p := metadata["prompt"]; p != "" {
			return p
		}

		return "Please complete the assigned task."
	}

	var sb strings.Builder

	// Inject skill invocations at the top of the prompt.
	// /$skill_name triggers Claude CLI to load SKILL.md content and frontmatter.
	if skillNames := metadata["_skill_names"]; skillNames != "" {
		for name := range strings.SplitSeq(skillNames, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				fmt.Fprintf(&sb, "/%s\n", name)
			}
		}

		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "# Task: %s\n", title)

	if currentStatusName := metadata["_current_status_name"]; currentStatusName != "" {
		fmt.Fprintf(&sb, "Current status: %s\n", currentStatusName)
	}

	if description != "" {
		fmt.Fprintf(&sb, "\n%s\n", description)
	}

	// Append result history from previous statuses so the agent has full context.
	if historyJSON := metadata["_result_history"]; historyJSON != "" {
		var history []resultHistoryEntry
		if json.Unmarshal([]byte(historyJSON), &history) == nil && len(history) > 0 {
			sb.WriteString("\n## Previous Results\n")

			for _, h := range history {
				fmt.Fprintf(&sb, "### %s (%s)\n%s\n\n", h.ResultType, h.CreatedAt, h.Text)
			}
		}
	}

	return sb.String()
}

var imageRefPattern = regexp.MustCompile(`\[Image#(\d+)\]`)

// buildUserPromptWithImages builds the user prompt, replacing [Image#N] references
// with actual image content blocks for the Claude API.
// Returns string if no images, or []map[string]any if images are present.
func buildUserPromptWithImages(ctx context.Context, metadata map[string]string, workDir string, taskClient taskguildv1connect.TaskServiceClient, taskID string) (any, error) {
	textPrompt := buildUserPrompt(metadata, workDir)

	// Find all [Image#N] references in the description.
	matches := imageRefPattern.FindAllStringIndex(textPrompt, -1)
	if len(matches) == 0 {
		return textPrompt, nil
	}

	// Fetch all images for this task.
	listResp, err := taskClient.ListTaskImages(ctx, connect.NewRequest(&v1.ListTaskImagesRequest{
		TaskId: taskID,
	}))
	if err != nil {
		return nil, fmt.Errorf("list task images: %w", err)
	}

	// Build a map of image ID -> image proto for quick lookup.
	imageMap := make(map[string]*v1.TaskImage)
	for _, img := range listResp.Msg.GetImages() {
		imageMap[img.GetId()] = img
	}

	// Split the text by [Image#N] references and interleave with image blocks.
	var blocks []map[string]any

	lastEnd := 0

	for _, match := range imageRefPattern.FindAllStringSubmatchIndex(textPrompt, -1) {
		// match[0:1] = full match start/end, match[2:3] = capture group (number)
		fullStart, fullEnd := match[0], match[1]
		imageID := textPrompt[match[2]:match[3]]

		// Add text before this image reference.
		if fullStart > lastEnd {
			textBefore := textPrompt[lastEnd:fullStart]
			if strings.TrimSpace(textBefore) != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": textBefore,
				})
			}
		}

		// Check if this image exists and fetch it.
		if _, exists := imageMap[imageID]; exists {
			imgResp, err := taskClient.GetTaskImage(ctx, connect.NewRequest(&v1.GetTaskImageRequest{
				TaskId:  taskID,
				ImageId: imageID,
			}))
			if err == nil {
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": imgResp.Msg.GetImage().GetMediaType(),
						"data":       base64.StdEncoding.EncodeToString(imgResp.Msg.GetData()),
					},
				})
			} else {
				// Keep the reference as text if fetch fails.
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": textPrompt[fullStart:fullEnd],
				})
			}
		} else {
			// Image not found — keep the reference as text.
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": textPrompt[fullStart:fullEnd],
			})
		}

		lastEnd = fullEnd
	}

	// Add remaining text after the last image reference.
	if lastEnd < len(textPrompt) {
		remaining := textPrompt[lastEnd:]
		if strings.TrimSpace(remaining) != "" {
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": remaining,
			})
		}
	}

	// If no image blocks were actually added, fall back to plain text.
	if len(blocks) == 0 {
		return textPrompt, nil
	}

	return blocks, nil
}

// buildWorkflowContext builds the workflow context block for the system prompt
// (passed via --append-system-prompt). Contains all TaskGuild operational
// instructions so the user prompt stays focused on task content.
func buildWorkflowContext(metadata map[string]string, workDir string) string {
	if metadata["_workflow_id"] == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## TaskGuild Workflow Context\n")
	sb.WriteString("You are an agent in a TaskGuild workflow.\n")

	// Agent Identity section: only shown for agent-based execution (fallback).
	// In skill-based mode, role definition is loaded via /$skill_name invocations.
	if metadata["_skill_names"] == "" {
		if agentName := metadata["_agent_name"]; agentName != "" {
			sb.WriteString("\n### Agent Identity\n")
			fmt.Fprintf(&sb, "You are executing this task as the **%s** agent.\n", agentName)
			fmt.Fprintf(&sb, "Your agent definition file (`.claude/agents/%s.md`) has been loaded as your system prompt.\n", agentName)
			sb.WriteString("You MUST follow all instructions, role definitions, and constraints defined in that agent definition.\n")
		}
	}

	// Workflow statuses with current marker.
	if statusesJSON := metadata["_workflow_statuses"]; statusesJSON != "" {
		type statusEntry struct {
			Name string `json:"name"`
		}

		var statuses []statusEntry

		err := json.Unmarshal([]byte(statusesJSON), &statuses)
		if err == nil && len(statuses) > 0 {
			currentName := metadata["_current_status_name"]

			sb.WriteString("\n### Workflow\n")

			for _, s := range statuses {
				if s.Name == currentName {
					fmt.Fprintf(&sb, "- **%s** (current)\n", s.Name)
				} else {
					fmt.Fprintf(&sb, "- %s\n", s.Name)
				}
			}
		}
	}

	// Available transitions (self-transitions filtered out).
	if transitionsJSON := metadata["_available_transitions"]; transitionsJSON != "" {
		type transitionEntry struct {
			Name string `json:"name"`
		}

		var raw []transitionEntry

		currentName := metadata["_current_status_name"]

		err := json.Unmarshal([]byte(transitionsJSON), &raw)
		if err == nil {
			var transitions []transitionEntry

			for _, t := range raw {
				if !strings.EqualFold(t.Name, currentName) {
					transitions = append(transitions, t)
				}
			}

			if len(transitions) > 0 {
				sb.WriteString("\n### Status Transition\n")
				sb.WriteString("When your work is complete, output on the last line:\n")
				sb.WriteString("`NEXT_STATUS: <status>`\n")
				sb.WriteString("Available next statuses:\n")

				for _, t := range transitions {
					fmt.Fprintf(&sb, "- %s\n", t.Name)
				}
			}
		}
	}

	// Hooks.
	if hooksJSON := metadata["_hooks"]; hooksJSON != "" {
		type hookEntry struct {
			Name       string `json:"name"`
			ActionType string `json:"action_type"`
			Trigger    string `json:"trigger"`
		}

		var hooks []hookEntry

		err := json.Unmarshal([]byte(hooksJSON), &hooks)
		if err == nil && len(hooks) > 0 {
			sb.WriteString("\n### Hooks\n")

			for _, h := range hooks {
				fmt.Fprintf(&sb, "- %q (%s) — %s\n", h.Name, h.ActionType, h.Trigger)
			}
		}
	}

	// Task description update.
	sb.WriteString("\n### Updating Task Description\n")
	sb.WriteString("Include this block in your output to update the task description:\n")
	sb.WriteString("```\nTASK_DESCRIPTION_START\n<description>\nTASK_DESCRIPTION_END\n```\n")

	// Task creation.
	sb.WriteString("\n### Creating New Tasks\n")
	sb.WriteString("```\nCREATE_TASK_START\ntitle: <required>\nstatus: <optional>\nuse_worktree: <optional, true/false>\nworktree: <optional, existing worktree name>\n\n<description>\nCREATE_TASK_END\n```\n")

	// List available statuses for new tasks.
	if statusesJSON := metadata["_workflow_statuses"]; statusesJSON != "" {
		type statusEntry struct {
			Name string `json:"name"`
		}

		var statuses []statusEntry

		err := json.Unmarshal([]byte(statusesJSON), &statuses)
		if err == nil && len(statuses) > 0 {
			names := make([]string, len(statuses))
			for i, s := range statuses {
				names[i] = s.Name
			}

			fmt.Fprintf(&sb, "Available statuses: %s\n", strings.Join(names, ", "))
		}
	}

	// Git worktree.
	if metadata["_use_worktree"] == "true" {
		if wt := metadata["worktree"]; wt != "" {
			wtDir := fmt.Sprintf(".claude/worktrees/%s/", wt)
			if workDir != "" {
				wtDir = filepath.Join(workDir, ".claude", "worktrees", wt) + "/"
			}

			sb.WriteString("\n### Git Worktree\n")
			fmt.Fprintf(&sb, "Branch: `worktree-%s` | Dir: `%s`\n", wt, wtDir)
			sb.WriteString("All file modifications and commits must occur within this worktree.\n")
			sb.WriteString("IMPORTANT: Do NOT use `cd` to navigate outside of this worktree directory.\n")
			sb.WriteString("Your CWD is already set to the worktree — use relative paths or paths within this directory.\n")
			sb.WriteString("Any `git add`, `git commit`, `git push` commands must be executed from within the worktree.\n")
		}
	}

	// Harness knowledge file — accumulated failure patterns from past tasks.
	if workDir != "" {
		harnessPath := filepath.Join(workDir, ".taskguild", "HARNESS.md")

		sb.WriteString("\n### Past Failure Patterns\n")
		fmt.Fprintf(&sb, "Before starting work, consult `%s` (absolute path) for failure patterns recorded by previous tasks. ", harnessPath)
		sb.WriteString("It lives at the repository root under `.taskguild/`, NOT inside any worktree, so always use the absolute path even when your CWD is a worktree. ")
		sb.WriteString("The file may not exist yet — that is fine, just skip it.\n")
	}

	// Interactive session.
	sb.WriteString("\n### Interactive Session\n")
	sb.WriteString("If you need user input or clarification, clearly state what you need and wait for a response.\n")

	return sb.String()
}
