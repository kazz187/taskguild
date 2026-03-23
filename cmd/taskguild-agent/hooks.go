package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/pkg/clog"
)

// hookEntry represents a resolved hook from metadata.
type hookEntry struct {
	ID      string `json:"id"`
	SkillID string `json:"skill_id"`
	Trigger string `json:"trigger"`
	Order   int32  `json:"order"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// executeHooks parses _hooks from metadata, filters by trigger, and runs each
// hook sequentially via runQuerySyncWithLog. Failures are logged but do
// not block the main task.
// If taskClient is provided, hook results containing TASK_METADATA directives
// will be used to update the task's metadata.
func executeHooks(ctx context.Context, taskID string, trigger string, metadata map[string]string, workDir string, taskClient taskguildv1connect.TaskServiceClient, tl *taskLogger, qr QueryRunner) {
	logger := clog.LoggerFromContext(ctx)

	hooksJSON := metadata["_hooks"]
	if hooksJSON == "" {
		return
	}

	var hooks []hookEntry
	if err := json.Unmarshal([]byte(hooksJSON), &hooks); err != nil {
		logger.Error("failed to parse _hooks metadata", "error", err)
		return
	}

	// Filter by trigger and sort by order.
	var filtered []hookEntry
	for _, h := range hooks {
		if h.Trigger == trigger {
			filtered = append(filtered, h)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Order < filtered[j].Order
	})

	if len(filtered) == 0 {
		return
	}

	logger.Info("executing hooks", "count", len(filtered), "trigger", trigger)

	// Warn if worktree branch has no commits ahead of the default branch.
	// This catches cases where the agent accidentally committed on the wrong branch.
	if trigger == "after_task_execution" {
		warnIfWorktreeEmpty(ctx, workDir, logger, tl)
	}

	for _, h := range filtered {
		logger.Info("running hook", "name", h.Name, "hook_id", h.ID, "skill_id", h.SkillID)
		if tl != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_HOOK, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Executing hook: %s (%s)", h.Name, trigger), nil)
		}

		hookCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		maxTurns := 20
		opts := &claudeagent.ClaudeAgentOptions{
			SystemPrompt:   "You are executing a hook. Follow the instructions precisely.",
			Cwd:            workDir,
			PermissionMode: claudeagent.PermissionModeBypassPermissions,
			MaxTurns:       &maxTurns,
		}

		result, err := qr.RunQuerySync(hookCtx, h.Content, opts, workDir, taskID, fmt.Sprintf("hook_%s", h.Name))
		cancel()

		if err != nil {
			logger.Error("hook failed", "name", h.Name, "error", err)
			if tl != nil {
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_HOOK, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Hook failed: %s: %v", h.Name, err), nil)
			}
			continue
		}
		if result.Result != nil && result.Result.IsError {
			logger.Error("hook returned error", "name", h.Name, "result", result.Result.Result)
			if tl != nil {
				resultPreview := truncateText(result.Result.Result, 200)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_HOOK, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Hook returned error: %s: %s", h.Name, resultPreview), nil)
			}
			continue
		}

		logger.Info("hook completed successfully", "name", h.Name)
		if tl != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_HOOK, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Hook completed: %s", h.Name), nil)
		}

		// Parse TASK_METADATA directives from hook output and update the task.
		if taskClient != nil && result.Result != nil {
			applyHookMetadata(ctx, taskID, result.Result.Result, taskClient)
		}
	}
}

// detectDefaultBranch returns the name of the default branch (main or master)
// by checking which local branch exists. Falls back to "main" if neither exists.
func detectDefaultBranch(ctx context.Context, workDir string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "main")
	cmd.Dir = workDir
	if err := cmd.Run(); err == nil {
		return "main"
	}
	cmd2 := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "--quiet", "master")
	cmd2.Dir = workDir
	if err := cmd2.Run(); err == nil {
		return "master"
	}
	return "main"
}

// warnIfWorktreeEmpty checks whether the current branch in workDir has any
// commits ahead of the default branch (main/master). If not, it logs a warning
// to the task logger so the user knows that hooks like create-pr will fail.
// This catches the common mistake where the agent accidentally committed on
// the main repository branch instead of the worktree branch.
func warnIfWorktreeEmpty(ctx context.Context, workDir string, logger *slog.Logger, tl *taskLogger) {
	defaultBranch := detectDefaultBranch(ctx, workDir)

	// Count commits ahead of the default branch.
	logCmd := exec.CommandContext(ctx, "git", "log", defaultBranch+"..HEAD", "--oneline")
	logCmd.Dir = workDir
	out, err := logCmd.Output()
	if err != nil {
		logger.Warn("warnIfWorktreeEmpty: git log failed", "error", err)
		return
	}

	lines := strings.TrimSpace(string(out))
	if lines == "" {
		msg := fmt.Sprintf("Warning: worktree branch has no commits ahead of %s — hooks that create PRs will not find any diff", defaultBranch)
		logger.Warn(msg, "workDir", workDir)
		if tl != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_HOOK, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN, msg, nil)
		}
	}
}

// taskMetadataRegex matches "TASK_METADATA: key=value" lines in hook output.
var taskMetadataRegex = regexp.MustCompile(`(?m)^TASK_METADATA:\s*(\S+?)=(.+)$`)

// applyHookMetadata extracts TASK_METADATA directives from hook output and
// updates the task's metadata via the TaskService API.
func applyHookMetadata(ctx context.Context, taskID string, output string, taskClient taskguildv1connect.TaskServiceClient) {
	logger := clog.LoggerFromContext(ctx)

	matches := taskMetadataRegex.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return
	}

	meta := make(map[string]string)
	for _, m := range matches {
		key := strings.TrimSpace(m[1])
		value := strings.TrimSpace(m[2])
		meta[key] = value
		logger.Debug("hook metadata", "key", key, "value", value)
	}

	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: meta,
	}))
	if err != nil {
		logger.Error("failed to update task metadata from hook", "error", err)
	}
}

// listLocalWorktrees returns directory names under {workDir}/.claude/worktrees/.
func listLocalWorktrees(workDir string) []string {
	dir := filepath.Join(workDir, ".claude", "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

var slugMultiHyphen = regexp.MustCompile(`-{2,}`)

// slugifyASCII extracts ASCII alphanumeric characters from a string, lowercased,
// with non-ASCII/non-alnum replaced by hyphens (collapsed).
func slugifyASCII(s string) string {
	var sb strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			sb.WriteRune('-')
			prevHyphen = true
		}
	}
	slug := strings.Trim(sb.String(), "-")
	return slugMultiHyphen.ReplaceAllString(slug, "-")
}

// ensureWorktree creates a git worktree if the directory does not already exist.
// It uses "git worktree add" with a new branch based on HEAD.
func ensureWorktree(ctx context.Context, workDir, worktreeName, taskID string) (string, error) {
	logger := clog.LoggerFromContext(ctx)

	wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		// Sync .claude/ resources even for existing worktrees in case
		// agent/skill definitions have been updated since creation.
		syncClaudeDirToWorktree(logger, workDir, wtDir)
		return wtDir, nil
	}

	if err := os.MkdirAll(filepath.Join(workDir, ".claude", "worktrees"), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	// Fetch the latest default branch from origin so the worktree starts
	// from the most recent commit rather than a potentially stale local HEAD.
	defaultBranch := detectDefaultBranch(ctx, workDir)
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", defaultBranch)
	fetchCmd.Dir = workDir
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		logger.Warn("git fetch origin failed, creating worktree from local HEAD", "error", err, "output", string(out))
	} else {
		logger.Info("fetched latest default branch from origin", "branch", defaultBranch)
	}

	branchName := "worktree-" + worktreeName
	startPoint := "origin/" + defaultBranch
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branchName, wtDir, startPoint)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch may already exist from a previous run; try without -b.
		cmd2 := exec.CommandContext(ctx, "git", "worktree", "add", wtDir, branchName)
		cmd2.Dir = workDir
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return "", fmt.Errorf("git worktree add: %w: %s / %s", err2, out, out2)
		}
	}
	logger.Info("created worktree", "worktree_dir", wtDir, "branch", branchName)

	// Copy .claude/ resources that are not carried over by git worktree add.
	// These are typically .gitignored so they must be explicitly synced.
	syncClaudeDirToWorktree(logger, workDir, wtDir)

	return wtDir, nil
}

// syncClaudeDirToWorktree copies .claude/ resources from the main repo to the
// worktree directory. git worktree add does not copy .gitignored files, so
// agents/, skills/, and settings.json must be synced explicitly.
// Directories are copied recursively; existing files in the worktree are not
// overwritten so that worktree-local customizations are preserved.
func syncClaudeDirToWorktree(logger *slog.Logger, workDir, wtDir string) {
	srcClaude := filepath.Join(workDir, ".claude")
	dstClaude := filepath.Join(wtDir, ".claude")

	// Directories to copy recursively.
	for _, dir := range []string{"agents", "skills"} {
		src := filepath.Join(srcClaude, dir)
		dst := filepath.Join(dstClaude, dir)
		if err := syncDir(src, dst); err != nil {
			logger.Warn("failed to sync .claude/"+dir, "error", err)
		} else {
			logger.Info("synced .claude/" + dir + " to worktree")
		}
	}

	// Single files to copy.
	for _, name := range []string{"settings.json"} {
		src := filepath.Join(srcClaude, name)
		dst := filepath.Join(dstClaude, name)
		if err := copyFile(src, dst); err != nil {
			logger.Warn("failed to sync .claude/"+name, "error", err)
		} else {
			logger.Info("synced .claude/" + name + " to worktree")
		}
	}
}

// syncDir mirrors the contents of src into dst. Files in src overwrite those
// in dst, and files/directories in dst that do not exist in src are removed.
func syncDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to copy
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	// Remove dst entirely first so stale files are cleaned up.
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("remove old dir: %w", err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

// copyFile copies a single file from src to dst, overwriting dst if it exists.
// If src does not exist, it is silently skipped.
func copyFile(src, dst string) error {
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// generateWorktreeName creates a git-safe worktree/branch name from the task ID and title.
// If the title is mostly non-ASCII (e.g. Japanese), it uses a lightweight Claude call
// to generate an English slug. Format: {taskID first 6 chars}_{slug} (max 50 chars).
func generateWorktreeName(ctx context.Context, taskID, title, workDir string, qr QueryRunner) string {
	id := strings.ToLower(taskID)
	prefix := id
	if len(id) > 6 {
		prefix = id[len(id)-6:]
	}

	slug := slugifyASCII(title)

	// If the ASCII slug is too short (title was mostly non-ASCII), ask Claude to translate.
	if len(slug) < 4 && title != "" {
		if englishSlug := translateToEnglishSlug(ctx, title, workDir, qr); englishSlug != "" {
			slug = englishSlug
		}
	}

	if slug == "" {
		return prefix
	}

	name := prefix + "_" + slug
	if len(name) > 50 {
		name = name[:50]
		name = strings.TrimRight(name, "-_")
	}
	return name
}

// translateToEnglishSlug uses a lightweight Claude call to convert a non-English
// title into a short English slug suitable for a git branch name.
func translateToEnglishSlug(ctx context.Context, title, workDir string, qr QueryRunner) string {
	prompt := fmt.Sprintf(
		"Translate the following title into a short English slug for a git branch name. "+
			"Output ONLY the slug (lowercase, hyphens, no spaces, max 30 chars). No explanation.\n\nTitle: %s",
		title,
	)
	maxTurns := 1
	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt: "You are a translation assistant. Output only the requested slug, nothing else.",
		Cwd:          workDir,
		MaxTurns:     &maxTurns,
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := qr.RunQuerySync(timeoutCtx, prompt, opts, workDir, "", "translate_slug")
	if err != nil || result.Result == nil {
		slog.Warn("translateToEnglishSlug failed", "error", err)
		return ""
	}

	raw := strings.TrimSpace(result.Result.Result)
	return slugifyASCII(raw)
}
