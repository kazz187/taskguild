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
const agentMDHarnessPrompt = `You are an efficiency analyst. Your job is to review the work just completed on a task and update the agent's definition file (.claude/agents/<name>.md) with knowledge that will make future tasks faster and cheaper.

The agent definition file uses YAML frontmatter (between --- delimiters) followed by the system prompt body. You MUST preserve the YAML frontmatter exactly as-is. Only modify the prompt body below the closing ---.

Your focus is on TWO types of insights:

**Codebase & Process knowledge** (HIGHER priority):
- What parts of the codebase did the agent explore unnecessarily? What should it have known to go directly to the right place?
- What architectural assumptions did the agent make that turned out to be wrong?
- What existing patterns, utilities, or conventions did the agent miss that would have saved time?
- What commands or approaches did the agent try that were wrong for this system?
- What knowledge would have reduced the number of turns or context consumed?

**Technical Guards** (LOWER priority):
- What specific technical mistakes caused bugs, regressions, or test failures?
- What API contracts or behavioral quirks need to be remembered?

Rules:
1. Read the existing agent definition file.
2. Analyze the task summary using the questions above.
3. Prioritize generalizable process insights over task-specific technical fixes.
4. Append new lessons to the "## Lessons Learned" section at the end of the prompt body, organized under subsections:
   - "### Codebase & Process" for navigational/architectural/workflow knowledge
   - "### Technical Guards" for specific technical pitfalls
5. If subsections do not exist yet, create them and move any existing uncategorized lessons under "### Technical Guards".
6. Keep entries concise (one or two lines per lesson). Do not duplicate existing entries.
7. A lesson is worth adding ONLY if it would save time on a DIFFERENT future task -- not just prevent the exact same bug.
8. If the task was completed efficiently with no wasted exploration or mistakes, do not modify the file.
9. Do NOT modify the YAML frontmatter between the --- delimiters.
10. Write in English.`

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
	qr QueryRunner,
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
		runAgentMDHarness(ctx, taskID, taskTitle, taskDescription, taskSummary, workDir, agentName, harnessTL, qr)
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
	qr QueryRunner,
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

	result, err := qr.RunQuerySync(harnessCtx, userPrompt, opts, workDir, taskID, "harness")
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

## Analysis

Before updating the agent definition file, answer these questions internally:

1. **Wasted exploration**: Did the agent spend turns reading files or exploring directories that turned out to be irrelevant? What prior knowledge of the codebase structure would have let it go directly to the right files?
2. **Wrong assumptions**: Did the agent assume something about the architecture, build system, or conventions that turned out to be incorrect? What should the agent know about how this system works?
3. **Missed existing patterns**: Did the agent write something from scratch when an existing utility, helper, or pattern was already available? Where should it look first in the future?
4. **Inefficient commands**: Did the agent run commands that failed or were unnecessary because it misunderstood the development workflow?
5. **Technical errors**: Did the agent introduce bugs, fail tests, or hit runtime errors that required backtracking?

## Instructions

1. Read the agent definition file at: %s
2. Based on your analysis above, add lessons that would help the agent work MORE EFFICIENTLY on future tasks.
3. Organize lessons under "### Codebase & Process" (for navigational/workflow knowledge) or "### Technical Guards" (for specific technical pitfalls) within the "## Lessons Learned" section.
4. Prefer process-level insights over task-specific fixes. Good examples: "always check existing test files in pkg/X/testdata/ before writing new test fixtures", "the workflow status definitions are in internal/project/seeder.go, not in the proto files". Bad examples: "use buffered channel for X".
5. Do not duplicate existing lessons. Merge similar ones.
6. Do NOT modify the YAML frontmatter (the content between --- delimiters).
7. If the task was completed efficiently with no issues, leave the file unchanged.
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
