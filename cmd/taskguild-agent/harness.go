package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	"github.com/kazz187/taskguild/pkg/clog"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/sourcegraph/conc"
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

const skillHarnessPrompt = `You are a failure pattern analyst. Your job is to review the work just completed on a task and record specific failure patterns to Skill files so the same mistakes are not repeated.

## What to Record

Record ONLY concrete, specific failure patterns:
- Bash commands that failed (non-zero exit) and what the correct command was
- Commands that were tried, corrected, and retried (trial-and-error traces)
- Code edits that caused build/test failures and required fixing
- Files that were missed in a cross-cutting change (propagation path gaps)

## What NOT to Record

- Architectural knowledge (humans write this in the Skill body)
- Abstract lessons ("always write tests", "be careful with X")
- Anything already recorded in the Skill
- Task-specific details that won't recur

## Where to Record

Append exactly ONE line to the "## 失敗パターン（自動追記）" section at the end of the appropriate Skill file:

| Failure type | Target Skill |
|---|---|
| Build/run command failures | project-rules |
| Go coding violations (logger, error types, style) | go-guards |
| Frontend violations (imports, components, pnpm) | frontend-guards |
| Missing changes in a propagation chain | codebase-map |
| Role-specific judgment errors | The role skill (architect, software-engineer, senior-engineer) |

## Format

Each entry is a single line starting with "- ":
- ` + "`" + `npx buf generate` + "`" + ` → 失敗。正解は ` + "`" + `cd proto && make generate` + "`" + `

## Rules

1. Read the relevant Skill file(s) FIRST.
2. If the task completed cleanly with no failures, do nothing.
3. If a pattern is already recorded, do nothing.
4. If the "失敗パターン" section has 20+ lines, do NOT append. Instead output: HARNESS_REVIEW_NEEDED: <skill-name>
5. Append to BOTH the worktree copy AND the main copy of each modified Skill file (if paths differ).
6. Do NOT modify anything above the "## 失敗パターン（自動追記）" heading.
7. Write in the language of the existing entries (Japanese or English).`

var knownSkillNames = []string{
	"project-rules", "codebase-map", "go-guards", "frontend-guards",
	"architect", "software-engineer", "senior-engineer",
}

func resolveSkillPaths(workDir string, metadata map[string]string, skillName string) (worktreePath, mainPath string) {
	skillRelPath := filepath.Join(".claude", "skills", skillName, "SKILL.md")
	mainPath = filepath.Join(workDir, skillRelPath)

	if metadata["_use_worktree"] == "true" {
		if wt := metadata["worktree"]; wt != "" {
			wtDir := filepath.Join(workDir, ".claude", "worktrees", wt)
			worktreePath = filepath.Join(wtDir, skillRelPath)
			return worktreePath, mainPath
		}
	}
	return mainPath, mainPath
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

	// Capture before-content for all known skill files.
	type skillSnapshot struct {
		name                       string
		worktreePath, mainPath     string
		beforeWT, beforeMain       string
	}
	var snapshots []skillSnapshot
	for _, name := range knownSkillNames {
		wtPath, mainPath := resolveSkillPaths(workDir, metadata, name)
		snap := skillSnapshot{
			name:         name,
			worktreePath: wtPath,
			mainPath:     mainPath,
			beforeWT:     readFileOrEmpty(wtPath),
		}
		if wtPath != mainPath {
			snap.beforeMain = readFileOrEmpty(mainPath)
		}
		snapshots = append(snapshots, snap)
	}

	userPrompt := buildSkillHarnessUserPrompt(taskID, taskTitle, taskDescription, taskSummary, workDir, metadata)

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
			fmt.Sprintf("Skill harness error: %s", result.Result.Result), nil)
		return
	}

	// Compute diffs for all modified skill files.
	var allDiffs []string
	for _, snap := range snapshots {
		afterWT := readFileOrEmpty(snap.worktreePath)
		diff := computeUnifiedDiff(snap.name+"/SKILL.md", snap.beforeWT, afterWT)
		if diff != "" {
			allDiffs = append(allDiffs, diff)
		}
		if snap.worktreePath != snap.mainPath {
			afterMain := readFileOrEmpty(snap.mainPath)
			mainDiff := computeUnifiedDiff(snap.name+"/SKILL.md (main)", snap.beforeMain, afterMain)
			if mainDiff != "" {
				allDiffs = append(allDiffs, mainDiff)
			}
		}
	}

	if len(allDiffs) == 0 {
		logger.Info("skill harness completed, no changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			"Skill harness completed: No changes", nil)
	} else {
		combined := strings.Join(allDiffs, "\n\n")
		logger.Info("skill harness completed with changes", "task_id", taskID)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Skill harness completed\n\n%s", combined), nil)
	}
}

func buildSkillHarnessUserPrompt(taskID, title, description, summary, workDir string, metadata map[string]string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Completed Task\n\n**Task ID:** %s\n**Title:** %s\n\n### Description\n%s\n\n### Task Summary / Output\n%s\n", taskID, title, description, summary))

	sb.WriteString("\n## Skill File Paths\n\nWhen appending failure patterns, write to BOTH paths for each skill:\n\n")
	for _, name := range knownSkillNames {
		wtPath, mainPath := resolveSkillPaths(workDir, metadata, name)
		if wtPath == mainPath {
			sb.WriteString(fmt.Sprintf("- %s: `%s`\n", name, mainPath))
		} else {
			sb.WriteString(fmt.Sprintf("- %s:\n  - worktree: `%s`\n  - main: `%s`\n", name, wtPath, mainPath))
		}
	}

	sb.WriteString("\n## Instructions\n\n")
	sb.WriteString("1. Read the relevant Skill file(s) based on the task summary.\n")
	sb.WriteString("2. Identify concrete failure patterns (commands that failed, edits that broke builds, etc.).\n")
	sb.WriteString("3. Append ONE line per pattern to the appropriate Skill's \"## 失敗パターン（自動追記）\" section.\n")
	sb.WriteString("4. If the task completed cleanly with no failures, leave all files unchanged.\n")

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
