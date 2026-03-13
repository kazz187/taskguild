package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	"github.com/kazz187/taskguild/pkg/clog"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/sourcegraph/conc"
)

const (
	// harnessTimeout is the maximum time allowed for the agent MD harness to run.
	harnessTimeout = 3 * time.Minute
	// harnessMaxTurns is the maximum number of turns for the harness agent.
	harnessMaxTurns = 10
)

// agentMDHarnessPrompt is the system prompt for the agent MD harness agent.
const agentMDHarnessPrompt = `You are a retrospective reviewer. Your job is to review the work just completed on a task and update the agent's definition file (.claude/agents/<name>.md) with lessons learned.

The agent definition file uses YAML frontmatter (between --- delimiters) followed by the system prompt body. You MUST preserve the YAML frontmatter exactly as-is. Only modify the prompt body below the closing ---.

Rules:
1. Read the existing agent definition file.
2. Analyze the task summary and identify any failures, mistakes, or inefficiencies encountered.
3. For each failure, determine a concise preventive guideline.
4. Append or merge new lessons into a "## Lessons Learned" section at the end of the prompt body.
5. Keep entries concise (one line per lesson). Do not duplicate existing entries.
6. If no failures or issues were encountered, do not modify the file.
7. Do NOT modify the YAML frontmatter between the --- delimiters.
8. Write in English.`

// maybeRunAgentMDHarness checks the metadata flag and launches the agent MD
// harness in a background goroutine if enabled.
// It creates a dedicated taskLogger for the harness goroutine so that it is
// independent of the caller's taskLogger lifecycle.
func maybeRunAgentMDHarness(
	ctx context.Context,
	metadata map[string]string,
	taskID string,
	taskSummary string,
	workDir string,
	tl *taskLogger,
	client taskguildv1connect.AgentManagerServiceClient,
) {
	if metadata["_enable_agent_md_harness"] != "true" {
		return
	}

	agentName := metadata["_agent_name"]
	if agentName == "" {
		return
	}

	// Log the start using the caller's taskLogger (still alive at this point).
	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness started in background (agent: %s)", agentName), nil)
	}

	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]

	// Create a dedicated taskLogger for the harness goroutine.
	// This uses context.Background() so it is not tied to the parent context
	// and will remain valid for the full lifetime of the harness execution.
	harnessTL := newTaskLogger(context.Background(), client, taskID)

	var harnessWg conc.WaitGroup
	harnessWg.Go(func() {
		runAgentMDHarness(ctx, taskID, taskTitle, taskDescription, taskSummary, workDir, agentName, harnessTL)
	})
}

// runAgentMDHarness runs the agent MD review harness in a background goroutine.
// It reviews the task summary, identifies failures, and updates the agent's
// .claude/agents/<name>.md file with lessons learned.
// The provided taskLogger is owned by this goroutine and will be closed on exit.
func runAgentMDHarness(
	ctx context.Context,
	taskID string,
	taskTitle string,
	taskDescription string,
	taskSummary string,
	workDir string,
	agentName string,
	tl *taskLogger,
) {
	defer tl.Close()

	logger := clog.LoggerFromContext(ctx)

	agentMDFilename := agentName + ".md"
	agentMDPath := filepath.Join(workDir, ".claude", "agents", agentMDFilename)

	logger.Info("starting agent MD harness in background",
		"task_id", taskID,
		"agent_name", agentName,
		"agent_md_path", agentMDPath,
	)

	// Skip if the agent definition file doesn't exist (nothing to update).
	if _, err := os.Stat(agentMDPath); os.IsNotExist(err) {
		logger.Info("agent MD file does not exist, skipping harness",
			"task_id", taskID, "path", agentMDPath)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness skipped: %s does not exist", agentMDPath), nil)
		return
	}

	// Capture content before the harness runs.
	beforeContent := readFileOrEmpty(agentMDPath)

	// Build the user prompt with task context.
	userPrompt := buildHarnessUserPrompt(taskID, taskTitle, taskDescription, taskSummary, agentMDPath)

	harnessCtx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	maxTurns := harnessMaxTurns
	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   agentMDHarnessPrompt,
		Cwd:            workDir,
		PermissionMode: claudeagent.PermissionModeBypassPermissions,
		MaxTurns:       &maxTurns,
	}

	result, err := runQuerySyncWithLog(harnessCtx, userPrompt, opts, workDir, taskID, "harness")
	if err != nil {
		logger.Error("agent MD harness failed", "task_id", taskID, "error", err)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
			fmt.Sprintf("Agent MD harness failed: %v", err), nil)
		return
	}

	if result.Result != nil && result.Result.IsError {
		logger.Error("agent MD harness returned error", "task_id", taskID, "result", result.Result.Result)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
			fmt.Sprintf("Agent MD harness error: %s", result.Result.Result), nil)
		return
	}

	// Capture content after the harness runs and compute diff.
	afterContent := readFileOrEmpty(agentMDPath)
	diff := computeUnifiedDiff(agentMDFilename, beforeContent, afterContent)

	if diff == "" {
		logger.Info("agent MD harness completed, no changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness completed: No changes to %s", agentMDFilename), nil)
	} else {
		logger.Info("agent MD harness completed with changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness completed\n\n%s", diff), nil)
	}
}

// buildHarnessUserPrompt constructs the prompt for the harness agent.
func buildHarnessUserPrompt(taskID, title, description, summary, agentMDPath string) string {
	return fmt.Sprintf(`## Completed Task

**Task ID:** %s
**Title:** %s

### Description
%s

### Task Summary / Output
%s

## Instructions

1. Read the agent definition file at: %s
2. Review the task summary above for any failures, mistakes, regressions, or inefficiencies.
3. If issues are found, append concise one-line prevention guidelines to a "## Lessons Learned" section at the end of the prompt body (after the YAML frontmatter).
4. Do not duplicate existing lessons. Merge similar ones.
5. Do NOT modify the YAML frontmatter (the content between --- delimiters).
6. If no issues were found, leave the file unchanged.
`,
		taskID, title, description, summary, agentMDPath)
}

// readFileOrEmpty reads a file and returns its content as a string.
// If the file does not exist or cannot be read, it returns an empty string.
func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// computeUnifiedDiff computes a unified diff between two strings.
// Returns an empty string if there are no differences.
func computeUnifiedDiff(filename, before, after string) string {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: "a/" + filename,
		ToFile:   "b/" + filename,
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("(failed to compute diff: %v)", err)
	}
	return strings.TrimRight(diff, "\n")
}
