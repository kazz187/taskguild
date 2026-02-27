package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

func init() {
	// The SDK closes stdin after this timeout, breaking the control protocol
	// (permission responses can no longer be sent to Claude CLI).
	// Set to 30 days so user input waits are effectively unlimited.
	if os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT") == "" {
		os.Setenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT", "2592000000") // 30 days in ms
	}

}

const (
	maxConsecutiveErrors = 5
	initialBackoff       = 5 * time.Second
	maxBackoff           = 5 * time.Minute
)

// interactionWaiter dispatches streamed interaction responses to waiting goroutines.
// It buffers responses that arrive before a waiter registers (race condition between
// CreateInteraction and Register).
type interactionWaiter struct {
	mu      sync.Mutex
	waiters map[string]chan *v1.Interaction // interaction_id -> ch
	pending map[string]*v1.Interaction      // arrived before Register()
}

func newInteractionWaiter() *interactionWaiter {
	return &interactionWaiter{
		waiters: make(map[string]chan *v1.Interaction),
		pending: make(map[string]*v1.Interaction),
	}
}

// Register returns a channel that will receive the responded interaction.
// If a response already arrived (buffered in pending), it is sent immediately.
func (w *interactionWaiter) Register(id string) <-chan *v1.Interaction {
	w.mu.Lock()
	defer w.mu.Unlock()

	ch := make(chan *v1.Interaction, 1)
	if inter, ok := w.pending[id]; ok {
		ch <- inter
		delete(w.pending, id)
	} else {
		w.waiters[id] = ch
	}
	return ch
}

// Unregister removes the waiter for the given interaction ID.
func (w *interactionWaiter) Unregister(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.waiters, id)
	delete(w.pending, id)
}

// Deliver is called by the stream listener. If a waiter exists, the interaction
// is sent on the channel; otherwise it is buffered in pending.
func (w *interactionWaiter) Deliver(inter *v1.Interaction) {
	w.mu.Lock()
	defer w.mu.Unlock()

	id := inter.GetId()
	if ch, ok := w.waiters[id]; ok {
		ch <- inter
		delete(w.waiters, id)
	} else {
		w.pending[id] = inter
	}
}

// runInteractionListener subscribes to interaction events for a task and delivers
// responded interactions to the waiter. It returns when the stream ends or ctx is cancelled.
func runInteractionListener(ctx context.Context, interClient taskguildv1connect.InteractionServiceClient, taskID string, waiter *interactionWaiter) {
	stream, err := interClient.SubscribeInteractions(ctx, connect.NewRequest(&v1.SubscribeInteractionsRequest{
		TaskId: taskID,
	}))
	if err != nil {
		log.Printf("[task:%s] interaction stream error: %v", taskID, err)
		return
	}
	defer stream.Close()

	log.Printf("[task:%s] interaction stream connected", taskID)

	for stream.Receive() {
		event := stream.Msg()
		inter := event.GetInteraction()
		if inter == nil {
			continue
		}
		switch inter.GetStatus() {
		case v1.InteractionStatus_INTERACTION_STATUS_RESPONDED:
			log.Printf("[task:%s] interaction %s responded via stream", taskID, inter.GetId())
			waiter.Deliver(inter)
		case v1.InteractionStatus_INTERACTION_STATUS_EXPIRED:
			log.Printf("[task:%s] interaction %s expired via stream", taskID, inter.GetId())
			waiter.Deliver(inter)
		}
	}

	if err := stream.Err(); err != nil && ctx.Err() == nil {
		log.Printf("[task:%s] interaction stream ended: %v", taskID, err)
	}
}

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
// hook sequentially via claudeagent.RunQuerySync. Failures are logged but do
// not block the main task.
// If taskClient is provided, hook results containing TASK_METADATA directives
// will be used to update the task's metadata.
func executeHooks(ctx context.Context, taskID string, trigger string, metadata map[string]string, workDir string, taskClient taskguildv1connect.TaskServiceClient, tl *taskLogger) {
	hooksJSON := metadata["_hooks"]
	if hooksJSON == "" {
		return
	}

	var hooks []hookEntry
	if err := json.Unmarshal([]byte(hooksJSON), &hooks); err != nil {
		log.Printf("[task:%s] failed to parse _hooks metadata: %v", taskID, err)
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

	log.Printf("[task:%s] executing %d hook(s) for trigger %s", taskID, len(filtered), trigger)

	for _, h := range filtered {
		log.Printf("[task:%s] running hook %q (id=%s, skill=%s)", taskID, h.Name, h.ID, h.SkillID)
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

		result, err := claudeagent.RunQuerySync(hookCtx, h.Content, opts)
		cancel()

		if err != nil {
			log.Printf("[task:%s] hook %q failed: %v", taskID, h.Name, err)
			continue
		}
		if result.Result != nil && result.Result.IsError {
			log.Printf("[task:%s] hook %q returned error: %s", taskID, h.Name, result.Result.Result)
			continue
		}

		log.Printf("[task:%s] hook %q completed successfully", taskID, h.Name)

		// Parse TASK_METADATA directives from hook output and update the task.
		if taskClient != nil && result.Result != nil {
			applyHookMetadata(ctx, taskID, result.Result.Result, taskClient)
		}
	}
}

// taskMetadataRegex matches "TASK_METADATA: key=value" lines in hook output.
var taskMetadataRegex = regexp.MustCompile(`(?m)^TASK_METADATA:\s*(\S+?)=(.+)$`)

// applyHookMetadata extracts TASK_METADATA directives from hook output and
// updates the task's metadata via the TaskService API.
func applyHookMetadata(ctx context.Context, taskID string, output string, taskClient taskguildv1connect.TaskServiceClient) {
	matches := taskMetadataRegex.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return
	}

	meta := make(map[string]string)
	for _, m := range matches {
		key := strings.TrimSpace(m[1])
		value := strings.TrimSpace(m[2])
		meta[key] = value
		log.Printf("[task:%s] hook metadata: %s=%s", taskID, key, value)
	}

	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: meta,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to update task metadata from hook: %v", taskID, err)
	}
}

func runTask(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskClient taskguildv1connect.TaskServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	agentManagerID string,
	taskID string,
	instructions string,
	metadata map[string]string,
	workDir string,
) {
	reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "starting task")

	// Initialize task logger for structured log streaming.
	tl := newTaskLogger(ctx, client, taskID)
	defer tl.Close()
	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO, "Task started", nil)

	// Resolve worktree name: reuse persisted name or generate a new one.
	worktreeName := metadata["worktree"]
	if worktreeName == "" && metadata["_use_worktree"] == "true" {
		worktreeName = generateWorktreeName(ctx, taskID, metadata["_task_title"], workDir)
		saveWorktreeName(ctx, taskClient, taskID, worktreeName)
		metadata["worktree"] = worktreeName // keep local metadata in sync for buildUserPrompt
	}

	// Ensure the worktree directory exists before launching Claude so that
	// Cwd is set to the worktree from the very first turn.
	if metadata["_use_worktree"] == "true" && worktreeName != "" {
		if _, err := ensureWorktree(ctx, workDir, worktreeName, taskID); err != nil {
			log.Printf("[task:%s] WARNING: failed to ensure worktree: %v", taskID, err)
		}
	}

	// resolveHookDir returns the worktree directory if it exists, otherwise workDir.
	resolveHookDir := func() string {
		if metadata["_use_worktree"] == "true" && worktreeName != "" {
			wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				return wtDir
			}
		}
		return workDir
	}

	// afterHooks runs after_task_execution hooks exactly once.
	// It is called explicitly before status transitions and deferred as a
	// safety-net for all other return paths so hooks always execute.
	afterHooksExecuted := false
	afterHooks := func() {
		if !afterHooksExecuted {
			afterHooksExecuted = true
			executeHooks(ctx, taskID, "after_task_execution", metadata, resolveHookDir(), taskClient, tl)
		}
	}
	defer afterHooks()

	// Execute before_task_execution hooks.
	executeHooks(ctx, taskID, "before_task_execution", metadata, workDir, taskClient, tl)

	// Start interaction stream listener for this task.
	waiter := newInteractionWaiter()
	go runInteractionListener(ctx, interClient, taskID, waiter)

	sessionID := metadata["_session_id"]
	prompt := buildUserPrompt(metadata)
	hasTransitions := metadata["_available_transitions"] != ""

	const maxResumeRetries = 2 // after this many consecutive resume failures, start fresh

	worktreeHookFired := false
	consecutiveErrors := 0
	backoff := initialBackoff

	for turn := 0; ; turn++ {
		opts := buildClaudeOptions(instructions, workDir, metadata, sessionID, worktreeName, client, ctx, taskID, agentManagerID, waiter)
		// Override StderrCallback to also send to task logger.
		opts.StderrCallback = func(line string) {
			log.Printf("[task:%s] [claude-stderr] %s", taskID, line)
			tl.LogStderr(line)
		}

		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Turn %d started", turn),
			map[string]string{"turn": fmt.Sprintf("%d", turn)})
		log.Printf("[task:%s] === Claude SDK Input (turn %d) ===", taskID, turn)
		if turn == 0 {
			log.Printf("[task:%s] SystemPrompt:\n%s", taskID, instructions)
			log.Printf("[task:%s] Metadata: %v", taskID, metadata)
			log.Printf("[task:%s] WorkDir: %s", taskID, workDir)
		}
		log.Printf("[task:%s] UserPrompt:\n%s", taskID, prompt)
		if sessionID != "" {
			log.Printf("[task:%s] Resume: %s", taskID, sessionID)
		}
		log.Printf("[task:%s] === End Claude SDK Input (turn %d) ===", taskID, turn)

		result, err := claudeagent.RunQuerySync(ctx, prompt, opts)

		log.Printf("[task:%s] === Claude SDK Output (turn %d) ===", taskID, turn)
		if err != nil {
			log.Printf("[task:%s] Error: %v", taskID, err)
		} else if result.Result != nil {
			log.Printf("[task:%s] IsError: %v", taskID, result.Result.IsError)
			log.Printf("[task:%s] SessionID: %s", taskID, result.Result.SessionID)
			log.Printf("[task:%s] Result: %s", taskID, result.Result.Result)
		} else {
			log.Printf("[task:%s] Result is nil", taskID)
		}
		log.Printf("[task:%s] === End Claude SDK Output (turn %d) ===", taskID, turn)

		if err != nil {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END, v1.TaskLogLevel_TASK_LOG_LEVEL_ERROR,
				fmt.Sprintf("Turn %d error: %v", turn, err),
				map[string]string{"turn": fmt.Sprintf("%d", turn)})
		} else {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Turn %d completed", turn),
				map[string]string{"turn": fmt.Sprintf("%d", turn)})
		}

		// Save session ID for resume.
		if result.Result != nil && result.Result.SessionID != "" {
			sessionID = result.Result.SessionID
			saveSessionID(ctx, taskClient, taskID, sessionID)
		}

		// Handle errors with backoff retry.
		isError := false
		var errMsg string

		if err != nil {
			isError = true
			errMsg = err.Error()
		} else if result.Result != nil && result.Result.IsError {
			isError = true
			errMsg = result.Result.Result
			if errMsg == "" {
				errMsg = "Claude returned an error"
			}
		}

		if isError {
			consecutiveErrors++
			log.Printf("[task:%s] error (%d/%d): %s", taskID, consecutiveErrors, maxConsecutiveErrors, errMsg)

			// If resume keeps failing, clear session and start fresh.
			if sessionID != "" && consecutiveErrors >= maxResumeRetries {
				log.Printf("[task:%s] resume failed %d times, clearing session to start fresh", taskID, consecutiveErrors)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_STATUS_CHANGE, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					"Resume failed, restarting with fresh session", nil)
				sessionID = ""
				consecutiveErrors = 0
				backoff = initialBackoff
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING,
					"resume failed, restarting with fresh session")
				continue
			}

			if consecutiveErrors >= maxConsecutiveErrors {
				log.Printf("[task:%s] max consecutive errors reached, giving up", taskID)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_ERROR, v1.TaskLogLevel_TASK_LOG_LEVEL_ERROR,
					fmt.Sprintf("Max consecutive errors reached, giving up: %s", errMsg), nil)
				reportTaskResult(ctx, client, taskID, "", errMsg)
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_ERROR, errMsg)
				return
			}

			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_ERROR,
				fmt.Sprintf("error, retrying in %s: %s", backoff, errMsg))

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			// Exponential backoff, capped.
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		// Success — reset error tracking.
		consecutiveErrors = 0
		backoff = initialBackoff

		// Extract and persist task description updates from agent output.
		if result.Result != nil {
			if newDesc := parseTaskDescription(result.Result.Result); newDesc != "" {
				log.Printf("[task:%s] detected TASK_DESCRIPTION update (turn %d)", taskID, turn)
				saveTaskDescription(ctx, taskClient, taskID, newDesc)
				// Update local metadata so subsequent prompts reflect the new description.
				metadata["_task_description"] = newDesc
			}
		}

		// Fire after_worktree_creation hook once, after the first successful turn
		// when a worktree directory exists.
		if !worktreeHookFired && metadata["_use_worktree"] == "true" && worktreeName != "" {
			wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				worktreeHookFired = true
				executeHooks(ctx, taskID, "after_worktree_creation", metadata, wtDir, taskClient, tl)
			}
		}

		summary := ""
		if result.Result != nil {
			summary = stripTaskDescription(result.Result.Result)
		}

		// Check completion: NEXT_STATUS present means task is done.
		if parseNextStatus(summary) != "" {
			log.Printf("[task:%s] completed with NEXT_STATUS (turn %d)", taskID, turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Task completed with status transition (turn %d)", turn),
				map[string]string{"next_status": parseNextStatus(summary)})
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			// Run after hooks before transitioning status so that hooks
			// still observe the current status and the transition happens
			// only after all hooks complete.
			afterHooks()
			handleStatusTransition(ctx, taskClient, taskID, summary, metadata)
			return
		}

		// No transitions available (terminal status) means task is done.
		if !hasTransitions {
			log.Printf("[task:%s] completed at terminal status (turn %d)", taskID, turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Task completed at terminal status (turn %d)", turn), nil)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			return
		}

		// Claude hasn't completed — wait for user input.
		log.Printf("[task:%s] waiting for user input (turn %d)", taskID, turn)
		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "waiting for user input")

		userResponse, err := waitForUserResponse(ctx, client, taskID, agentManagerID, summary, waiter)
		if err != nil {
			log.Printf("[task:%s] user response error: %v, completing task", taskID, err)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (no user response)")
			return
		}

		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "continuing task")
		prompt = userResponse
	}
}

// buildClaudeOptions constructs ClaudeAgentOptions for each turn.
func buildClaudeOptions(
	instructions string,
	workDir string,
	metadata map[string]string,
	sessionID string,
	worktreeName string,
	client taskguildv1connect.AgentManagerServiceClient,
	ctx context.Context,
	taskID string,
	agentManagerID string,
	waiter *interactionWaiter,
) *claudeagent.ClaudeAgentOptions {
	// Permission mode from agent config (default if empty)
	permMode := claudeagent.PermissionModeDefault
	if pm := metadata["_permission_mode"]; pm != "" {
		permMode = claudeagent.PermissionMode(pm)
	}

	cwd := workDir

	// If worktree is enabled and the worktree directory already exists, use it as Cwd.
	// This ensures both fresh and resumed sessions work inside the worktree.
	if metadata["_use_worktree"] == "true" && worktreeName != "" {
		wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
		if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
			cwd = wtDir
			log.Printf("[task:%s] using existing worktree directory: %s", taskID, wtDir)
		}
	}

	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   instructions,
		Cwd:            cwd,
		PermissionMode: permMode,
		CanUseTool: func(toolName string, input map[string]any, toolCtx claudeagent.ToolPermissionContext) (claudeagent.PermissionResult, error) {
			return handlePermissionRequest(ctx, client, taskID, agentManagerID, toolName, input, waiter, permMode, toolCtx)
		},
		StderrCallback: func(line string) {
			log.Printf("[task:%s] [claude-stderr] %s", taskID, line)
		},
	}

	// Parse and pass sub-agents from metadata.
	if subAgentsJSON := metadata["_sub_agents"]; subAgentsJSON != "" {
		var subAgents map[string]*claudeagent.AgentDefinition
		if err := json.Unmarshal([]byte(subAgentsJSON), &subAgents); err == nil && len(subAgents) > 0 {
			opts.Agents = subAgents
		}
	}

	if sessionID != "" {
		opts.Resume = sessionID
	}

	// Set --worktree flag whenever worktree is enabled. This ensures Claude CLI
	// creates or enters the worktree on both fresh and resumed sessions.
	if metadata["_use_worktree"] == "true" && worktreeName != "" {
		opts.Worktree = &worktreeName
	}

	return opts
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
	wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
	if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
		return wtDir, nil
	}

	if err := os.MkdirAll(filepath.Join(workDir, ".claude", "worktrees"), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	branchName := "worktree-" + worktreeName
	cmd := exec.CommandContext(ctx, "git", "worktree", "add", "-b", branchName, wtDir)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch may already exist from a previous run; try without -b.
		cmd2 := exec.CommandContext(ctx, "git", "worktree", "add", wtDir, branchName)
		cmd2.Dir = workDir
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			return "", fmt.Errorf("git worktree add: %w: %s / %s", err2, out, out2)
		}
	}
	log.Printf("[task:%s] created worktree at %s (branch: %s)", taskID, wtDir, branchName)
	return wtDir, nil
}

// generateWorktreeName creates a git-safe worktree/branch name from the task ID and title.
// If the title is mostly non-ASCII (e.g. Japanese), it uses a lightweight Claude call
// to generate an English slug. Format: {taskID first 6 chars}_{slug} (max 50 chars).
func generateWorktreeName(ctx context.Context, taskID, title, workDir string) string {
	id := strings.ToLower(taskID)
	prefix := id
	if len(id) > 6 {
		prefix = id[len(id)-6:]
	}

	slug := slugifyASCII(title)

	// If the ASCII slug is too short (title was mostly non-ASCII), ask Claude to translate.
	if len(slug) < 4 && title != "" {
		if englishSlug := translateToEnglishSlug(ctx, title, workDir); englishSlug != "" {
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
func translateToEnglishSlug(ctx context.Context, title, workDir string) string {
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

	result, err := claudeagent.RunQuerySync(timeoutCtx, prompt, opts)
	if err != nil || result.Result == nil {
		log.Printf("translateToEnglishSlug failed: %v", err)
		return ""
	}

	raw := strings.TrimSpace(result.Result.Result)
	return slugifyASCII(raw)
}

// waitForUserResponse creates a QUESTION interaction and waits for a response via the event stream.
func waitForUserResponse(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentManagerID string,
	claudeOutput string,
	waiter *interactionWaiter,
) (string, error) {
	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentManagerID,
		Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
		Title:       "Agent needs your input",
		Description: claudeOutput,
	}))
	if err != nil {
		return "", fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for user response (interaction: %s)", taskID, interactionID)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			return "", fmt.Errorf("interaction expired")
		}
		log.Printf("[task:%s] user responded to interaction %s", taskID, interactionID)
		return inter.GetResponse(), nil
	}
}

// buildUserPrompt constructs the user prompt from enriched metadata.
func buildUserPrompt(metadata map[string]string) string {
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
				sb.WriteString(fmt.Sprintf("- %s: %s\n", t.ID, t.Name))
			}
			sb.WriteString("\nAfter completing the task, output your chosen next status on the last line:\nNEXT_STATUS: <status_id>\n")
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

// parseNextStatus extracts a "NEXT_STATUS: <id>" directive from the result text.
func parseNextStatus(resultText string) string {
	lines := strings.Split(resultText, "\n")
	// Scan from the end to find the last NEXT_STATUS directive.
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "NEXT_STATUS:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "NEXT_STATUS:"))
		}
	}
	return ""
}

// parseTaskDescription extracts a task description update from the result text.
// The description is enclosed between TASK_DESCRIPTION_START and TASK_DESCRIPTION_END markers.
// Returns the extracted description (trimmed) or empty string if no markers found.
func parseTaskDescription(resultText string) string {
	const startMarker = "TASK_DESCRIPTION_START"
	const endMarker = "TASK_DESCRIPTION_END"

	startIdx := strings.Index(resultText, startMarker)
	if startIdx == -1 {
		return ""
	}
	contentStart := startIdx + len(startMarker)

	endIdx := strings.Index(resultText[contentStart:], endMarker)
	if endIdx == -1 {
		return ""
	}

	return strings.TrimSpace(resultText[contentStart : contentStart+endIdx])
}

// stripTaskDescription removes the TASK_DESCRIPTION block from the result text
// so it doesn't clutter the reported summary.
func stripTaskDescription(resultText string) string {
	const startMarker = "TASK_DESCRIPTION_START"
	const endMarker = "TASK_DESCRIPTION_END"

	startIdx := strings.Index(resultText, startMarker)
	if startIdx == -1 {
		return resultText
	}

	endIdx := strings.Index(resultText[startIdx:], endMarker)
	if endIdx == -1 {
		return resultText
	}

	// Find the start of the line containing the start marker.
	lineStart := strings.LastIndex(resultText[:startIdx], "\n")
	if lineStart == -1 {
		lineStart = 0
	}

	fullEndIdx := startIdx + endIdx + len(endMarker)
	// Skip trailing newline if present.
	if fullEndIdx < len(resultText) && resultText[fullEndIdx] == '\n' {
		fullEndIdx++
	}

	return strings.TrimSpace(resultText[:lineStart] + resultText[fullEndIdx:])
}

// handleStatusTransition parses the agent result and transitions the task status.
func handleStatusTransition(
	ctx context.Context,
	taskClient taskguildv1connect.TaskServiceClient,
	taskID string,
	resultText string,
	metadata map[string]string,
) {
	transitionsJSON := metadata["_available_transitions"]
	if transitionsJSON == "" {
		return
	}

	type transitionEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var transitions []transitionEntry
	if err := json.Unmarshal([]byte(transitionsJSON), &transitions); err != nil || len(transitions) == 0 {
		return
	}

	nextStatusID := parseNextStatus(resultText)

	if nextStatusID == "" {
		// Auto-transition if exactly one transition is available.
		if len(transitions) == 1 {
			nextStatusID = transitions[0].ID
			log.Printf("[task:%s] no NEXT_STATUS found, auto-transitioning to %s (%s)", taskID, nextStatusID, transitions[0].Name)
		} else {
			log.Printf("[task:%s] WARNING: no NEXT_STATUS found and %d transitions available, skipping transition", taskID, len(transitions))
			return
		}
	} else {
		// Validate the chosen status is in available transitions.
		valid := false
		for _, t := range transitions {
			if t.ID == nextStatusID {
				valid = true
				break
			}
		}
		if !valid {
			log.Printf("[task:%s] WARNING: NEXT_STATUS %q is not a valid transition, skipping", taskID, nextStatusID)
			return
		}
	}

	_, err := taskClient.UpdateTaskStatus(ctx, connect.NewRequest(&v1.UpdateTaskStatusRequest{
		Id:       taskID,
		StatusId: nextStatusID,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to transition status to %s: %v", taskID, nextStatusID, err)
		return
	}
	log.Printf("[task:%s] status transitioned to %s", taskID, nextStatusID)
}

func saveSessionID(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, sessionID string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"_session_id": sessionID},
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save session_id: %v", taskID, err)
	}
}

func saveWorktreeName(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, name string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:       taskID,
		Metadata: map[string]string{"worktree": name},
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save worktree_name: %v", taskID, err)
	} else {
		log.Printf("[task:%s] worktree name: %s", taskID, name)
	}
}

func saveTaskDescription(ctx context.Context, taskClient taskguildv1connect.TaskServiceClient, taskID, description string) {
	_, err := taskClient.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		Id:          taskID,
		Description: description,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to save task description: %v", taskID, err)
	} else {
		log.Printf("[task:%s] task description updated (%d chars)", taskID, len(description))
	}
}

// readOnlyTools are always auto-allowed regardless of permission mode.
var readOnlyTools = map[string]bool{
	"Read":      true,
	"Glob":      true,
	"Grep":      true,
	"WebSearch": true,
	"WebFetch":  true,
}

// editTools are auto-allowed in acceptEdits and bypassPermissions modes.
var editTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"NotebookEdit": true,
}

// buildPermissionUpdate constructs permission updates for the "always allow" action.
// It prefers CLI-provided suggestions when available, falling back to a manually
// constructed rule from the tool name and input.
func buildPermissionUpdate(toolName string, input map[string]any, suggestions []*claudeagent.PermissionUpdate) []*claudeagent.PermissionUpdate {
	// Prefer CLI suggestions: they contain the exact rule format the CLI expects.
	if len(suggestions) > 0 {
		var updates []*claudeagent.PermissionUpdate
		for _, s := range suggestions {
			if s.Type == claudeagent.PermissionUpdateAddRules &&
				s.Behavior == claudeagent.PermissionBehaviorAllow {
				cp := *s // copy
				cp.Destination = claudeagent.PermissionDestinationLocalSettings
				updates = append(updates, &cp)
			}
		}
		if len(updates) > 0 {
			return updates
		}
	}

	// Fallback: build rule from tool name and input.
	rule := &claudeagent.PermissionRuleValue{
		ToolName: toolName,
	}

	// For Bash, include the command as ruleContent for a specific match.
	if toolName == "Bash" {
		if cmd, ok := input["command"]; ok {
			if cmdStr, ok := cmd.(string); ok && cmdStr != "" {
				rule.RuleContent = cmdStr
			}
		}
	}

	return []*claudeagent.PermissionUpdate{
		{
			Type:        claudeagent.PermissionUpdateAddRules,
			Rules:       []*claudeagent.PermissionRuleValue{rule},
			Behavior:    claudeagent.PermissionBehaviorAllow,
			Destination: claudeagent.PermissionDestinationLocalSettings,
		},
	}
}

// handleAskUserQuestion processes the AskUserQuestion tool by presenting each question
// as an INTERACTION_TYPE_QUESTION with selectable options. Returns PermissionResultAllow
// with the user's answers injected into UpdatedInput.
func handleAskUserQuestion(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	input map[string]any,
	waiter *interactionWaiter,
) (claudeagent.PermissionResult, error) {
	questionsRaw, ok := input["questions"]
	if !ok {
		log.Printf("[task:%s] AskUserQuestion: no questions field", taskID)
		return claudeagent.PermissionResultAllow{}, nil
	}

	questionsSlice, ok := questionsRaw.([]any)
	if !ok {
		log.Printf("[task:%s] AskUserQuestion: questions is not an array", taskID)
		return claudeagent.PermissionResultAllow{}, nil
	}

	answers := make(map[string]any)

	for i, qRaw := range questionsSlice {
		qMap, ok := qRaw.(map[string]any)
		if !ok {
			log.Printf("[task:%s] AskUserQuestion: question[%d] is not a map", taskID, i)
			continue
		}

		questionText, _ := qMap["question"].(string)
		header, _ := qMap["header"].(string)
		if questionText == "" {
			continue
		}

		// Build interaction options from the question's options array.
		var interactionOpts []*v1.InteractionOption
		if optsRaw, ok := qMap["options"].([]any); ok {
			for _, optRaw := range optsRaw {
				optMap, ok := optRaw.(map[string]any)
				if !ok {
					continue
				}
				label, _ := optMap["label"].(string)
				desc, _ := optMap["description"].(string)
				if label == "" {
					continue
				}
				interactionOpts = append(interactionOpts, &v1.InteractionOption{
					Label:       label,
					Value:       label,
					Description: desc,
				})
			}
		}

		// Add "Other" option for free-text input.
		interactionOpts = append(interactionOpts, &v1.InteractionOption{
			Label:       "Other",
			Value:       "__other__",
			Description: "Provide a custom answer",
		})

		// Build description: include header if present.
		description := ""
		if header != "" {
			description = header
		}

		resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
			TaskId:      taskID,
			AgentId:     agentID,
			Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
			Title:       questionText,
			Description: description,
			Options:     interactionOpts,
		}))
		if err != nil {
			return nil, fmt.Errorf("failed to create question interaction: %w", err)
		}

		interactionID := resp.Msg.GetInteraction().GetId()
		log.Printf("[task:%s] AskUserQuestion: waiting for answer to question %d (interaction: %s)", taskID, i, interactionID)

		ch := waiter.Register(interactionID)

		var selectedAnswer string
		select {
		case <-ctx.Done():
			waiter.Unregister(interactionID)
			return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
		case inter := <-ch:
			waiter.Unregister(interactionID)
			if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
				return claudeagent.PermissionResultDeny{Message: "question expired"}, nil
			}
			selectedAnswer = inter.GetResponse()
		}

		// If user chose "Other", create a follow-up interaction for free-text input.
		if selectedAnswer == "__other__" {
			followResp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
				TaskId:      taskID,
				AgentId:     agentID,
				Type:        v1.InteractionType_INTERACTION_TYPE_QUESTION,
				Title:       questionText,
				Description: "Enter your custom answer:",
			}))
			if err != nil {
				return nil, fmt.Errorf("failed to create follow-up interaction: %w", err)
			}

			followID := followResp.Msg.GetInteraction().GetId()
			log.Printf("[task:%s] AskUserQuestion: waiting for free-text answer (interaction: %s)", taskID, followID)

			followCh := waiter.Register(followID)

			select {
			case <-ctx.Done():
				waiter.Unregister(followID)
				return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
			case inter := <-followCh:
				waiter.Unregister(followID)
				if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
					return claudeagent.PermissionResultDeny{Message: "question expired"}, nil
				}
				selectedAnswer = inter.GetResponse()
			}
		}

		answers[questionText] = selectedAnswer
		log.Printf("[task:%s] AskUserQuestion: question %d answered: %q", taskID, i, selectedAnswer)
	}

	// Inject answers into the input and return as UpdatedInput.
	updatedInput := make(map[string]any)
	for k, v := range input {
		updatedInput[k] = v
	}
	updatedInput["answers"] = answers

	return claudeagent.PermissionResultAllow{
		UpdatedInput: updatedInput,
	}, nil
}

func handlePermissionRequest(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	toolName string,
	input map[string]any,
	waiter *interactionWaiter,
	permMode claudeagent.PermissionMode,
	toolCtx claudeagent.ToolPermissionContext,
) (claudeagent.PermissionResult, error) {
	// bypassPermissions: allow everything
	if permMode == claudeagent.PermissionModeBypassPermissions {
		log.Printf("[task:%s] auto-allowing %s (bypassPermissions)", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Read-only tools: always allowed
	if readOnlyTools[toolName] {
		log.Printf("[task:%s] auto-allowing read-only tool %s", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// Edit tools: allowed in acceptEdits mode
	if editTools[toolName] && permMode == claudeagent.PermissionModeAcceptEdits {
		log.Printf("[task:%s] auto-allowing edit tool %s (acceptEdits)", taskID, toolName)
		return claudeagent.PermissionResultAllow{}, nil
	}

	// AskUserQuestion: present as QUESTION interactions instead of permission request
	if toolName == "AskUserQuestion" {
		return handleAskUserQuestion(ctx, client, taskID, agentID, input, waiter)
	}

	description := formatToolDescription(toolName, input)

	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentID,
		Type:        v1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST,
		Title:       fmt.Sprintf("Permission request: %s", toolName),
		Description: description,
		Options: []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
			{Label: "Always Allow", Value: "always_allow", Description: "Allow and remember the rule for future uses"},
			{Label: "Deny", Value: "deny", Description: "Deny this tool use"},
		},
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to create interaction: %w", err)
	}

	interactionID := resp.Msg.GetInteraction().GetId()
	log.Printf("[task:%s] waiting for permission response (interaction: %s, tool: %s)", taskID, interactionID, toolName)

	ch := waiter.Register(interactionID)
	defer waiter.Unregister(interactionID)

	select {
	case <-ctx.Done():
		return claudeagent.PermissionResultDeny{Message: "context cancelled"}, nil
	case inter := <-ch:
		if inter.GetStatus() == v1.InteractionStatus_INTERACTION_STATUS_EXPIRED {
			log.Printf("[task:%s] permission request expired for %s", taskID, toolName)
			return claudeagent.PermissionResultDeny{Message: "permission request expired"}, nil
		}
		switch inter.GetResponse() {
		case "allow":
			log.Printf("[task:%s] permission granted for %s", taskID, toolName)
			return claudeagent.PermissionResultAllow{}, nil
		case "always_allow":
			updates := buildPermissionUpdate(toolName, input, toolCtx.Suggestions)
			log.Printf("[task:%s] permission granted (always allow) for %s, updating %d rule(s)", taskID, toolName, len(updates))
			return claudeagent.PermissionResultAllow{
				UpdatedPermissions: updates,
			}, nil
		default:
			log.Printf("[task:%s] permission denied for %s", taskID, toolName)
			return claudeagent.PermissionResultDeny{Message: "user denied permission"}, nil
		}
	}
}

// inferLanguageFromPath returns a code-fence language tag based on file extension.
func inferLanguageFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".rb":
		return "ruby"
	case ".java":
		return "java"
	case ".kt":
		return "kotlin"
	case ".sh", ".bash":
		return "bash"
	case ".sql":
		return "sql"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".proto":
		return "protobuf"
	case ".dockerfile":
		return "dockerfile"
	default:
		if strings.HasSuffix(path, "Dockerfile") {
			return "dockerfile"
		}
		return ""
	}
}

// codeBlockFence returns a backtick fence string that is safe to use around the
// given content. If the content itself contains triple-backtick fences, a longer
// fence is returned so the inner fences don't close the outer block.
func codeBlockFence(content string) string {
	maxRun := 0
	cur := 0
	for _, r := range content {
		if r == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	n := maxRun + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}

// formatToolDescription renders a structured markdown description for a tool invocation.
func formatToolDescription(toolName string, input map[string]any) string {
	str := func(key string) string {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	var sb strings.Builder

	switch toolName {
	case "Bash":
		sb.WriteString("**Tool:** `Bash`\n")
		if desc := str("description"); desc != "" {
			sb.WriteString(fmt.Sprintf("**Description:** %s\n", desc))
		}
		if cmd := str("command"); cmd != "" {
			fence := codeBlockFence(cmd)
			sb.WriteString(fmt.Sprintf("\n%sbash\n", fence))
			sb.WriteString(cmd)
			sb.WriteString(fmt.Sprintf("\n%s\n", fence))
		}

	case "Edit":
		sb.WriteString("**Tool:** `Edit`\n")
		filePath := str("file_path")
		if filePath != "" {
			sb.WriteString(fmt.Sprintf("**File:** `%s`\n", filePath))
		}
		oldStr := str("old_string")
		newStr := str("new_string")
		if oldStr != "" || newStr != "" {
			combined := oldStr + newStr
			fence := codeBlockFence(combined)
			sb.WriteString(fmt.Sprintf("\n%sdiff\n", fence))
			for _, line := range strings.Split(oldStr, "\n") {
				sb.WriteString("- ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			for _, line := range strings.Split(newStr, "\n") {
				sb.WriteString("+ ")
				sb.WriteString(line)
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("%s\n", fence))
		}

	case "Write":
		sb.WriteString("**Tool:** `Write`\n")
		filePath := str("file_path")
		if filePath != "" {
			sb.WriteString(fmt.Sprintf("**File:** `%s`\n", filePath))
		}
		if content := str("content"); content != "" {
			lang := inferLanguageFromPath(filePath)
			fence := codeBlockFence(content)
			sb.WriteString(fmt.Sprintf("\n%s%s\n", fence, lang))
			sb.WriteString(content)
			sb.WriteString(fmt.Sprintf("\n%s\n", fence))
		}

	case "Read":
		sb.WriteString("**Tool:** `Read`\n")
		if filePath := str("file_path"); filePath != "" {
			sb.WriteString(fmt.Sprintf("**File:** `%s`\n", filePath))
		}

	case "Glob":
		sb.WriteString("**Tool:** `Glob`\n")
		if pattern := str("pattern"); pattern != "" {
			sb.WriteString(fmt.Sprintf("**Pattern:** `%s`\n", pattern))
		}
		if path := str("path"); path != "" {
			sb.WriteString(fmt.Sprintf("**Path:** `%s`\n", path))
		}

	case "Grep":
		sb.WriteString("**Tool:** `Grep`\n")
		if pattern := str("pattern"); pattern != "" {
			sb.WriteString(fmt.Sprintf("**Pattern:** `%s`\n", pattern))
		}
		if path := str("path"); path != "" {
			sb.WriteString(fmt.Sprintf("**Path:** `%s`\n", path))
		}

	default:
		sb.WriteString(fmt.Sprintf("**Tool:** `%s`\n", toolName))
		// Render remaining keys sorted, with multiline values in code blocks.
		keys := make([]string, 0, len(input))
		for k := range input {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := input[k]
			s := fmt.Sprintf("%v", v)
			if strings.Contains(s, "\n") {
				fence := codeBlockFence(s)
				sb.WriteString(fmt.Sprintf("**%s:**\n\n%s\n%s\n%s\n", k, fence, s, fence))
			} else {
				sb.WriteString(fmt.Sprintf("**%s:** `%s`\n", k, s))
			}
		}
	}

	return sb.String()
}

func reportTaskResult(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	summary string,
	errMsg string,
) {
	_, err := client.ReportTaskResult(ctx, connect.NewRequest(&v1.ReportTaskResultRequest{
		TaskId:       taskID,
		Summary:      summary,
		ErrorMessage: errMsg,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to report task result: %v", taskID, err)
	}
}

func reportAgentStatus(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	agentManagerID string,
	taskID string,
	status v1.AgentStatus,
	message string,
) {
	_, err := client.ReportAgentStatus(ctx, connect.NewRequest(&v1.ReportAgentStatusRequest{
		AgentManagerId: agentManagerID,
		TaskId:         taskID,
		Status:         status,
		Message:        message,
	}))
	if err != nil {
		log.Printf("[task:%s] failed to report agent status: %v", taskID, err)
	}
}
