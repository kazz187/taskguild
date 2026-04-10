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
// Agent MD harness (deprecated, kept as fallback)
// ---------------------------------------------------------------------------

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

func maybeRunAgentMDHarness(
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
	if metadata["_enable_agent_md_harness"] != "true" {
		return
	}

	agentName := metadata["_agent_name"]
	if agentName == "" {
		return
	}

	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness started in background (agent: %s)", agentName), nil)
	}

	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]

	agentMDPath := filepath.Join(workDir, ".claude", "agents", agentName+".md")

	claudeCwd := resolveClaudeCwd(workDir, metadata, sessionID)

	globalHarnessTracker.launchOrReplace(agentMDPath, func(harnessCtx context.Context) {
		harnessTL := newTaskLogger(context.Background(), client, taskID)
		runAgentMDHarness(harnessCtx, taskID, taskTitle, taskDescription, taskSummary, workDir, claudeCwd, agentName, harnessTL, qr, sessionID)
	})
}

func runAgentMDHarnessAndWait(
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
	if metadata["_enable_agent_md_harness"] != "true" {
		return
	}

	agentName := metadata["_agent_name"]
	if agentName == "" {
		return
	}

	if tl != nil {
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness started (agent: %s)", agentName), nil)
	}

	taskTitle := metadata["_task_title"]
	taskDescription := metadata["_task_description"]

	agentMDPath := filepath.Join(workDir, ".claude", "agents", agentName+".md")

	globalHarnessTracker.mu.Lock()
	if prev, ok := globalHarnessTracker.running[agentMDPath]; ok {
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
	runAgentMDHarness(ctx, taskID, taskTitle, taskDescription, taskSummary, workDir, claudeCwd, agentName, harnessTL, qr, sessionID)
}

func runAgentMDHarness(
	ctx context.Context,
	taskID string,
	taskTitle string,
	taskDescription string,
	taskSummary string,
	workDir string,
	claudeCwd string,
	agentName string,
	tl *taskLogger,
	qr QueryRunner,
	sessionID string,
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

	if _, err := os.Stat(agentMDPath); os.IsNotExist(err) {
		logger.Info("agent MD file does not exist, skipping harness",
			"task_id", taskID, "path", agentMDPath)
		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Agent MD harness skipped: %s does not exist", agentMDPath), nil)
		return
	}

	beforeContent := readFileOrEmpty(agentMDPath)

	userPrompt := buildAgentMDHarnessUserPrompt(taskID, taskTitle, taskDescription, taskSummary, agentMDPath)

	harnessCtx, cancel := context.WithTimeout(ctx, harnessTimeout)
	defer cancel()

	maxTurns := harnessMaxTurns
	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   agentMDHarnessPrompt,
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

func buildAgentMDHarnessUserPrompt(taskID, title, description, summary, agentMDPath string) string {
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
