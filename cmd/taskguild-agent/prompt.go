package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	sb.WriteString(fmt.Sprintf("# Task: %s\n", title))
	if currentStatusName := metadata["_current_status_name"]; currentStatusName != "" {
		sb.WriteString(fmt.Sprintf("Current status: %s\n", currentStatusName))
	}
	if description != "" {
		sb.WriteString(fmt.Sprintf("\n%s\n", description))
	}

	return sb.String()
}

// buildWorkflowContext builds the workflow context block for the system prompt
// (passed via --append-system-prompt). Contains all TaskGuild operational
// instructions so the user prompt stays focused on task content.
func buildWorkflowContext(metadata map[string]string) string {
	if metadata["_workflow_id"] == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## TaskGuild Workflow Context\n")
	sb.WriteString("You are an agent in a TaskGuild workflow.\n")

	if agentName := metadata["_agent_name"]; agentName != "" {
		sb.WriteString(fmt.Sprintf("@\"%s (agent)\"\n", agentName))
	}

	// Workflow statuses with current marker.
	if statusesJSON := metadata["_workflow_statuses"]; statusesJSON != "" {
		type statusEntry struct {
			Name string `json:"name"`
		}
		var statuses []statusEntry
		if err := json.Unmarshal([]byte(statusesJSON), &statuses); err == nil && len(statuses) > 0 {
			currentName := metadata["_current_status_name"]
			sb.WriteString("\n### Workflow\n")
			for _, s := range statuses {
				if s.Name == currentName {
					sb.WriteString(fmt.Sprintf("- **%s** (current)\n", s.Name))
				} else {
					sb.WriteString(fmt.Sprintf("- %s\n", s.Name))
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
		if err := json.Unmarshal([]byte(transitionsJSON), &raw); err == nil {
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
					sb.WriteString(fmt.Sprintf("- %s\n", t.Name))
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
		if err := json.Unmarshal([]byte(hooksJSON), &hooks); err == nil && len(hooks) > 0 {
			sb.WriteString("\n### Hooks\n")
			for _, h := range hooks {
				sb.WriteString(fmt.Sprintf("- %q (%s) — %s\n", h.Name, h.ActionType, h.Trigger))
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
		if err := json.Unmarshal([]byte(statusesJSON), &statuses); err == nil && len(statuses) > 0 {
			names := make([]string, len(statuses))
			for i, s := range statuses {
				names[i] = s.Name
			}
			sb.WriteString(fmt.Sprintf("Available statuses: %s\n", strings.Join(names, ", ")))
		}
	}

	// Git worktree.
	if metadata["_use_worktree"] == "true" {
		if wt := metadata["worktree"]; wt != "" {
			sb.WriteString("\n### Git Worktree\n")
			sb.WriteString(fmt.Sprintf("Branch: `worktree-%s` | Dir: `.claude/worktrees/%s/`\n", wt, wt))
			sb.WriteString("All file modifications and commits must occur within this worktree.\n")
			sb.WriteString("IMPORTANT: Do NOT use `cd` to navigate outside of this worktree directory.\n")
			sb.WriteString("Your CWD is already set to the worktree — use relative paths or paths within this directory.\n")
			sb.WriteString("Any `git add`, `git commit`, `git push` commands must be executed from within the worktree.\n")
		}
	}

	// Interactive session.
	sb.WriteString("\n### Interactive Session\n")
	sb.WriteString("If you need user input or clarification, clearly state what you need and wait for a response.\n")

	return sb.String()
}
