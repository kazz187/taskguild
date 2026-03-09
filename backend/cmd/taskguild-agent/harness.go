package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/pkg/clog"
)

const (
	// harnessTimeout is the maximum time allowed for the AGENT.md harness to run.
	harnessTimeout = 3 * time.Minute
	// harnessMaxTurns is the maximum number of turns for the harness agent.
	harnessMaxTurns = 10

	// agentMDFilename is the name of the AGENT.md file.
	agentMDFilename = "AGENT.md"
)

// agentMDHarnessPrompt is the system prompt for the AGENT.md harness agent.
const agentMDHarnessPrompt = `You are a retrospective reviewer. Your job is to review the work just completed on a task and update the project's AGENT.md file with lessons learned.

Rules:
1. Read the existing AGENT.md file (create it if it doesn't exist).
2. Analyze the task summary and identify any failures, mistakes, or inefficiencies encountered.
3. For each failure, determine a concise preventive guideline that would help avoid the same mistake in future tasks within this project.
4. Update AGENT.md by appending or merging new lessons into an existing "## Lessons Learned" section.
5. Keep entries concise (one line per lesson). Do not duplicate existing entries.
6. If no failures or issues were encountered, do not modify the file.
7. Do NOT remove any existing content from AGENT.md.
8. Write in English.`

// maybeRunAgentMDHarness checks the metadata flag and launches the AGENT.md
// harness in a background goroutine if enabled.
func maybeRunAgentMDHarness(
	ctx context.Context,
	metadata map[string]string,
	taskID string,
	taskSummary string,
	workDir string,
	tl *taskLogger,
) {
	if metadata["_enable_agent_md_harness"] != "true" {
		return
	}
	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]
	go runAgentMDHarness(ctx, taskID, taskTitle, taskDescription, taskSummary, workDir, tl)
}

// runAgentMDHarness runs the AGENT.md review harness in a background goroutine.
// It reviews the task summary, identifies failures, and updates AGENT.md
// with lessons learned to prevent the same failures in future tasks.
func runAgentMDHarness(
	ctx context.Context,
	taskID string,
	taskTitle string,
	taskDescription string,
	taskSummary string,
	workDir string,
	tl *taskLogger,
) {
	logger := clog.LoggerFromContext(ctx)
	logger.Info("starting AGENT.md harness in background", "task_id", taskID)

	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"AGENT.md harness started in background", nil)
	}

	// Build the user prompt with task context.
	userPrompt := buildHarnessUserPrompt(taskID, taskTitle, taskDescription, taskSummary, workDir)

	harnessCtx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	maxTurns := harnessMaxTurns
	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   agentMDHarnessPrompt,
		Cwd:            workDir,
		PermissionMode: claudeagent.PermissionModeBypassPermissions,
		MaxTurns:       &maxTurns,
	}

	result, err := claudeagent.RunQuerySync(harnessCtx, userPrompt, opts)
	if err != nil {
		logger.Error("AGENT.md harness failed", "task_id", taskID, "error", err)
		if tl != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
				fmt.Sprintf("AGENT.md harness failed: %v", err), nil)
		}
		return
	}

	if result.Result != nil && result.Result.IsError {
		logger.Error("AGENT.md harness returned error", "task_id", taskID, "result", result.Result.Result)
		if tl != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
				fmt.Sprintf("AGENT.md harness error: %s", result.Result.Result), nil)
		}
		return
	}

	logger.Info("AGENT.md harness completed successfully", "task_id", taskID)
	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"AGENT.md harness completed", nil)
	}
}

// buildHarnessUserPrompt constructs the prompt for the harness agent.
func buildHarnessUserPrompt(taskID, title, description, summary, workDir string) string {
	agentMDPath := filepath.Join(workDir, agentMDFilename)

	prompt := fmt.Sprintf(`## Completed Task

**Task ID:** %s
**Title:** %s

### Description
%s

### Task Summary / Output
%s

## Instructions

1. Check if %s exists at: %s
2. If it exists, read it. If not, create it with a header "# AGENT.md" followed by a "## Lessons Learned" section.
3. Review the task summary above for any failures, mistakes, regressions, or inefficiencies.
4. If issues are found, add concise one-line prevention guidelines to the "## Lessons Learned" section.
5. Do not duplicate existing lessons. Merge similar ones.
6. If no issues were found, leave the file unchanged.
`,
		taskID, title, description, summary, agentMDFilename, agentMDPath)

	// Check if AGENT.md already exists and mention it.
	if _, err := os.Stat(agentMDPath); err == nil {
		prompt += fmt.Sprintf("\nNote: %s already exists. Read it first before making changes.\n", agentMDFilename)
	} else {
		prompt += fmt.Sprintf("\nNote: %s does not exist yet. Create it if lessons are found.\n", agentMDFilename)
	}

	return prompt
}
