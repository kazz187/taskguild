package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildUserPrompt constructs the user prompt from enriched metadata.
func buildUserPrompt(metadata map[string]string, workDir string) string {
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
				sb.WriteString(fmt.Sprintf("- %s\n", t.Name))
			}
			sb.WriteString("\nAfter completing the task, output your chosen next status on the last line:\nNEXT_STATUS: <status>\n")
		}
	}

	sb.WriteString("\n## Updating Task Description\n")
	sb.WriteString("You can update the task description at any time by including the following block in your output:\n")
	sb.WriteString("TASK_DESCRIPTION_START\n")
	sb.WriteString("Your updated task description here.\n")
	sb.WriteString("Multiline content is supported.\n")
	sb.WriteString("TASK_DESCRIPTION_END\n")
	sb.WriteString("Use this to summarize planning discussions, refine requirements, or document decisions made during the session.\n")
	sb.WriteString("The description will be saved and visible in the task UI immediately.\n")

	// Add task creation instructions.
	sb.WriteString("\n## Creating New Tasks\n")
	sb.WriteString("You can create new tasks by including one or more CREATE_TASK blocks in your output:\n")
	sb.WriteString("```\n")
	sb.WriteString("CREATE_TASK_START\n")
	sb.WriteString("title: Task title (required)\n")
	sb.WriteString("status: Status name (optional)\n")
	sb.WriteString("use_worktree: true or false (optional, inherits current task setting)\n")
	sb.WriteString("worktree: existing-worktree-name (optional)\n")
	sb.WriteString("\n")
	sb.WriteString("Task description here.\n")
	sb.WriteString("Multiple lines supported.\n")
	sb.WriteString("CREATE_TASK_END\n")
	sb.WriteString("```\n")
	sb.WriteString("Headers are key-value pairs parsed until the first empty line. Everything after the empty line is the description.\n")

	// List available workflow statuses if present.
	if statusesJSON := metadata["_workflow_statuses"]; statusesJSON != "" {
		type statusEntry struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var statuses []statusEntry
		if err := json.Unmarshal([]byte(statusesJSON), &statuses); err == nil && len(statuses) > 0 {
			sb.WriteString("\nAvailable statuses for new tasks:\n")
			for _, s := range statuses {
				sb.WriteString(fmt.Sprintf("- %s\n", s.Name))
			}
		}
	}

	// List available worktrees.
	if worktrees := listLocalWorktrees(workDir); len(worktrees) > 0 {
		sb.WriteString("\nExisting worktrees:\n")
		for _, wt := range worktrees {
			sb.WriteString(fmt.Sprintf("- %s\n", wt))
		}
	}

	// Add worktree instructions when the task uses a git worktree.
	if metadata["_use_worktree"] == "true" {
		if wt := metadata["worktree"]; wt != "" {
			sb.WriteString("\n## Git Worktree\n")
			sb.WriteString("This task uses a git worktree for file isolation.\n")
			sb.WriteString(fmt.Sprintf("- Worktree branch: `worktree-%s`\n", wt))
			sb.WriteString(fmt.Sprintf("- Worktree directory: `.claude/worktrees/%s/`\n", wt))
			sb.WriteString("\nBefore starting work, verify you are on the correct branch:\n")
			sb.WriteString("```\ngit branch --show-current\n```\n")
			sb.WriteString(fmt.Sprintf("\nIf you are NOT on branch `worktree-%s`, navigate to the worktree:\n", wt))
			sb.WriteString(fmt.Sprintf("```\ncd $(git rev-parse --show-toplevel)/.claude/worktrees/%s\n```\n", wt))
			sb.WriteString("\nAll file modifications and commits must occur within this worktree.\n")
		}
	}

	sb.WriteString("\n## Interactive Session\n")
	sb.WriteString("You are in an interactive session. ")
	sb.WriteString("If you need user input, approval, or clarification, ")
	sb.WriteString("clearly state what you need. You will receive a response and can continue.\n")
	sb.WriteString("When the task is fully complete, output NEXT_STATUS on the last line.\n")

	return sb.String()
}

// buildWorkflowContext builds a concise workflow context block for the system prompt.
// Returns "" if no workflow context is available.
func buildWorkflowContext(metadata map[string]string) string {
	if metadata["_workflow_id"] == "" {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## TaskGuild Workflow Context\n")
	sb.WriteString("You are an agent in a TaskGuild workflow. Complete your work for the current status, then transition the task forward.\n")

	// List all workflow statuses, marking the current one.
	if statusesJSON := metadata["_workflow_statuses"]; statusesJSON != "" {
		type statusEntry struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var statuses []statusEntry
		if err := json.Unmarshal([]byte(statusesJSON), &statuses); err == nil && len(statuses) > 0 {
			currentID := metadata["_current_status_id"]
			sb.WriteString("\n### Workflow Statuses\n")
			for _, s := range statuses {
				sb.WriteString(fmt.Sprintf("- %s", s.Name))
				if s.ID == currentID {
					sb.WriteString("  <-- current")
				}
				sb.WriteString("\n")
			}
		}
	}

	// List available transitions.
	if transitionsJSON := metadata["_available_transitions"]; transitionsJSON != "" {
		type transitionEntry struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var transitions []transitionEntry
		if err := json.Unmarshal([]byte(transitionsJSON), &transitions); err == nil && len(transitions) > 0 {
			sb.WriteString("\n### Available Transitions\n")
			for _, t := range transitions {
				sb.WriteString(fmt.Sprintf("- %s\n", t.Name))
			}
		}
	}

	// List hooks only if configured.
	if hooksJSON := metadata["_hooks"]; hooksJSON != "" {
		type hookEntry struct {
			Name       string `json:"name"`
			ActionType string `json:"action_type"`
			Trigger    string `json:"trigger"`
		}
		var hooks []hookEntry
		if err := json.Unmarshal([]byte(hooksJSON), &hooks); err == nil && len(hooks) > 0 {
			sb.WriteString("\n### Hooks\n")
			sb.WriteString("Hooks run automatically for this status:\n")
			for _, h := range hooks {
				sb.WriteString(fmt.Sprintf("- %q (%s) — %s\n", h.Name, h.ActionType, h.Trigger))
			}
		}
	}

	return sb.String()
}
