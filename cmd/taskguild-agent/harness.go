package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sourcegraph/conc"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	"github.com/kazz187/taskguild/pkg/clog"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

const (
	harnessTimeout  = 3 * time.Minute
	harnessMaxTurns = 10
)

type harnessTracker struct {
	mu      sync.Mutex
	running map[string]*harnessRun
}

type harnessRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var globalHarnessTracker = &harnessTracker{running: make(map[string]*harnessRun)}

const harnessReplaceTimeout = 30 * time.Second

func resolveClaudeCwd(workDir string, metadata map[string]string, sessionID string) string {
	if sessionID == "" {
		return workDir
	}
	if metadata["_use_worktree"] != "true" {
		return workDir
	}
	wt := metadata["worktree"]
	if wt == "" {
		return workDir
	}
	wtDir := filepath.Join(workDir, ".claude", "worktrees", wt)
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		return wtDir
	}
	return workDir
}

func (ht *harnessTracker) launchOrReplace(key string, fn func(ctx context.Context)) {
	ht.mu.Lock()

	if prev, ok := ht.running[key]; ok {
		prev.cancel()
		ht.mu.Unlock()
		select {
		case <-prev.done:
		case <-time.After(harnessReplaceTimeout):
		}
		ht.mu.Lock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ht.running[key] = &harnessRun{cancel: cancel, done: done}
	ht.mu.Unlock()

	var wg conc.WaitGroup
	wg.Go(func() {
		defer func() {
			close(done)
			ht.mu.Lock()
			if cur, ok := ht.running[key]; ok && cur.done == done {
				delete(ht.running, key)
			}
			ht.mu.Unlock()
		}()
		fn(ctx)
	})
}

// ---------------------------------------------------------------------------
// Skill-based harness
// ---------------------------------------------------------------------------

const skillHarnessPrompt = `You are a failure pattern analyst. Your job is to review the work just completed on a task and append non-obvious failure patterns to a single shared file so the same mistakes are not repeated on future tasks.

## What to Record

Record ONLY concrete, specific, recurrence-worthy failure patterns:
- Bash commands that failed (non-zero exit) and the correct command
- Commands that were tried, corrected, and retried (trial-and-error traces)
- Code edits that caused build/test failures and required fixing
- Files that were missed in a cross-cutting change (propagation path gaps)

## What NOT to Record

- Architectural knowledge (already documented in skill files / docs)
- Abstract lessons ("always write tests", "be careful with X")
- Anything already recorded in HARNESS.md
- Task-specific details that won't recur on a different task
- The fact that a task completed successfully

## Where to Record

There is exactly ONE target file. Its absolute path is provided in the user message under "## Harness File".

Append entries to the ` + "`" + `## 失敗パターン（自動追記）` + "`" + ` section at the end of that file. If the file or section does not exist yet, create it (the file may also be empty).

## Format

Each entry is a single line starting with "- ":
- ` + "`" + `npx buf generate` + "`" + ` → 失敗。正解は ` + "`" + `cd proto && make generate` + "`" + `

Group related entries under a short ` + "`" + `### <topic>` + "`" + ` subheading inside the 失敗パターン section if helpful, but keep individual lines terse.

## Rules

1. Read the harness file FIRST to see what is already recorded.
2. If the task completed cleanly with no failures, do nothing.
3. If a pattern is already recorded (even paraphrased), do nothing.
4. If the 失敗パターン section already has 30+ lines, do NOT append. Instead output: HARNESS_REVIEW_NEEDED
5. Do NOT modify anything above the ` + "`" + `## 失敗パターン（自動追記）` + "`" + ` heading.
6. Write in the language of the existing entries (Japanese by default).`

func harnessMDPath(workDir string) string {
	return filepath.Join(workDir, ".taskguild", "HARNESS.md")
}

func maybeRunSkillHarness(
	ctx context.Context,
	metadata map[string]string,
	taskID string,
	taskSummary string,
	workDir string,
	tl *taskLogger,
	client taskguildv1connect.AgentManagerServiceClient,
	qr QueryRunner,
	sessionID string,
) {
	if metadata["_enable_skill_harness"] != "true" {
		return
	}

	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"Skill harness started in background", nil)
	}

	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]
	claudeCwd := resolveClaudeCwd(workDir, metadata, sessionID)

	globalHarnessTracker.launchOrReplace("skill-harness", func(harnessCtx context.Context) {
		harnessTL := newTaskLogger(context.Background(), client, taskID)
		runSkillHarness(harnessCtx, taskID, taskTitle, taskDescription, taskSummary, workDir, claudeCwd, metadata, harnessTL, qr, sessionID)
	})
}

func runSkillHarnessAndWait(
	ctx context.Context,
	metadata map[string]string,
	taskID string,
	taskSummary string,
	workDir string,
	tl *taskLogger,
	client taskguildv1connect.AgentManagerServiceClient,
	qr QueryRunner,
	sessionID string,
) {
	if metadata["_enable_skill_harness"] != "true" {
		return
	}

	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"Skill harness started", nil)
	}

	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]

	globalHarnessTracker.mu.Lock()
	if prev, ok := globalHarnessTracker.running["skill-harness"]; ok {
		prev.cancel()
		globalHarnessTracker.mu.Unlock()
		select {
		case <-prev.done:
		case <-time.After(harnessReplaceTimeout):
		}
	} else {
		globalHarnessTracker.mu.Unlock()
	}

	claudeCwd := resolveClaudeCwd(workDir, metadata, sessionID)
	harnessTL := newTaskLogger(context.Background(), client, taskID)
	runSkillHarness(ctx, taskID, taskTitle, taskDescription, taskSummary, workDir, claudeCwd, metadata, harnessTL, qr, sessionID)
}

func runSkillHarness(
	ctx context.Context,
	taskID string,
	taskTitle string,
	taskDescription string,
	taskSummary string,
	workDir string,
	claudeCwd string,
	metadata map[string]string,
	tl *taskLogger,
	qr QueryRunner,
	sessionID string,
) {
	defer tl.Close()

	logger := clog.LoggerFromContext(ctx)

	logger.Info("starting skill harness", "task_id", taskID)

	harnessPath := harnessMDPath(workDir)
	beforeContent := readFileOrEmpty(harnessPath)

	userPrompt := buildSkillHarnessUserPrompt(taskID, taskTitle, taskDescription, taskSummary, harnessPath)

	harnessCtx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()

	maxTurns := harnessMaxTurns
	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   skillHarnessPrompt,
		Cwd:            claudeCwd,
		PermissionMode: claudeagent.PermissionModeBypassPermissions,
		MaxTurns:       &maxTurns,
	}
	if sessionID != "" {
		opts.Resume = sessionID
		opts.ForkSession = true
	}

	result, err := qr.RunQuerySync(harnessCtx, userPrompt, opts, workDir, taskID, "harness")
	if err != nil {
		logger.Error("skill harness failed", "task_id", taskID, "error", err)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
			fmt.Sprintf("Skill harness failed: %v", err), nil)
		return
	}

	if result.Result != nil && result.Result.IsError {
		logger.Error("skill harness returned error", "task_id", taskID, "result", result.Result.Result)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
			"Skill harness error: "+result.Result.Result, nil)
		return
	}

	afterContent := readFileOrEmpty(harnessPath)
	diff := computeUnifiedDiff(".taskguild/HARNESS.md", beforeContent, afterContent)

	if diff == "" {
		logger.Info("skill harness completed, no changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"Skill harness completed: No changes", nil)
	} else {
		logger.Info("skill harness completed with changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"Skill harness completed\n\n"+diff, nil)
	}
}

func buildSkillHarnessUserPrompt(taskID, title, description, summary, harnessPath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Completed Task\n\n**Task ID:** %s\n**Title:** %s\n\n### Description\n%s\n\n### Task Summary / Output\n%s\n", taskID, title, description, summary)

	fmt.Fprintf(&sb, "\n## Harness File\n\nAppend any worth-recording failure patterns to this single file (absolute path):\n\n`%s`\n", harnessPath)
	sb.WriteString("\nThe file lives at the repository root under `.taskguild/`, NOT inside any worktree. Always use the absolute path above when reading or writing.\n")

	sb.WriteString("\n## Instructions\n\n")
	sb.WriteString("1. Read the harness file (it may not exist yet — that is fine).\n")
	sb.WriteString("2. Identify concrete failure patterns from the completed task.\n")
	sb.WriteString("3. Append ONE line per pattern to the `## 失敗パターン（自動追記）` section, creating the file/section if needed.\n")
	sb.WriteString("4. If the task completed cleanly with no failures, leave the file unchanged.\n")

	return sb.String()
}

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

func readFileOrEmpty(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

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
