package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
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
func executeHooks(ctx context.Context, taskID string, trigger string, metadata map[string]string, workDir string) {
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

		hookCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		maxTurns := 3
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

	// Execute after_task_execution hooks when runTask exits (success or failure).
	defer executeHooks(ctx, taskID, "after_task_execution", metadata, workDir)

	// Execute before_task_execution hooks.
	executeHooks(ctx, taskID, "before_task_execution", metadata, workDir)

	// Start interaction stream listener for this task.
	waiter := newInteractionWaiter()
	go runInteractionListener(ctx, interClient, taskID, waiter)

	sessionID := metadata["_session_id"]
	prompt := buildUserPrompt(metadata)
	hasTransitions := metadata["_available_transitions"] != ""

	// Resolve worktree name: reuse persisted name or generate a new one.
	worktreeName := metadata["worktree"]
	if worktreeName == "" && metadata["_use_worktree"] == "true" {
		worktreeName = generateWorktreeName(ctx, taskID, metadata["_task_title"], workDir)
		saveWorktreeName(ctx, taskClient, taskID, worktreeName)
	}

	const maxResumeRetries = 2 // after this many consecutive resume failures, start fresh

	worktreeHookFired := false
	consecutiveErrors := 0
	backoff := initialBackoff

	for turn := 0; ; turn++ {
		opts := buildClaudeOptions(instructions, workDir, metadata, sessionID, worktreeName, client, ctx, taskID, agentManagerID, waiter)

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
				sessionID = ""
				consecutiveErrors = 0
				backoff = initialBackoff
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING,
					"resume failed, restarting with fresh session")
				continue
			}

			if consecutiveErrors >= maxConsecutiveErrors {
				log.Printf("[task:%s] max consecutive errors reached, giving up", taskID)
				reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_FAILED, "", errMsg)
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

		// Fire after_worktree_creation hook once, after the first successful turn
		// when a worktree directory exists.
		if !worktreeHookFired && metadata["_use_worktree"] == "true" && worktreeName != "" {
			wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				worktreeHookFired = true
				executeHooks(ctx, taskID, "after_worktree_creation", metadata, wtDir)
			}
		}

		summary := ""
		if result.Result != nil {
			summary = result.Result.Result
		}

		// Check completion: NEXT_STATUS present means task is done.
		if parseNextStatus(summary) != "" {
			log.Printf("[task:%s] completed with NEXT_STATUS (turn %d)", taskID, turn)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			handleStatusTransition(ctx, taskClient, taskID, summary, metadata)
			return
		}

		// No transitions available (terminal status) means task is done.
		if !hasTransitions {
			log.Printf("[task:%s] completed at terminal status (turn %d)", taskID, turn)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			return
		}

		// Claude hasn't completed — wait for user input.
		log.Printf("[task:%s] waiting for user input (turn %d)", taskID, turn)
		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "waiting for user input")

		userResponse, err := waitForUserResponse(ctx, client, taskID, agentManagerID, summary, waiter)
		if err != nil {
			log.Printf("[task:%s] user response error: %v, completing task", taskID, err)
			reportTaskResult(ctx, client, taskID, v1.TaskResultStatus_TASK_RESULT_STATUS_COMPLETED, summary, "")
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
			return handlePermissionRequest(ctx, client, taskID, agentManagerID, toolName, input, waiter, permMode)
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

	// Set --worktree flag when starting a fresh session (no resume) and worktree is enabled.
	// This tells Claude CLI to create the worktree if it doesn't exist yet.
	if sessionID == "" && metadata["_use_worktree"] == "true" && worktreeName != "" {
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

func handlePermissionRequest(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	agentID string,
	toolName string,
	input map[string]any,
	waiter *interactionWaiter,
	permMode claudeagent.PermissionMode,
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

	description := formatToolDescription(toolName, input)

	resp, err := client.CreateInteraction(ctx, connect.NewRequest(&v1.CreateInteractionRequest{
		TaskId:      taskID,
		AgentId:     agentID,
		Type:        v1.InteractionType_INTERACTION_TYPE_PERMISSION_REQUEST,
		Title:       fmt.Sprintf("Permission request: %s", toolName),
		Description: description,
		Options: []*v1.InteractionOption{
			{Label: "Allow", Value: "allow", Description: "Allow this tool use"},
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
		if inter.GetResponse() == "allow" {
			log.Printf("[task:%s] permission granted for %s", taskID, toolName)
			return claudeagent.PermissionResultAllow{}, nil
		}
		log.Printf("[task:%s] permission denied for %s", taskID, toolName)
		return claudeagent.PermissionResultDeny{Message: "user denied permission"}, nil
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
			sb.WriteString("\n```bash\n")
			sb.WriteString(cmd)
			sb.WriteString("\n```\n")
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
			sb.WriteString("\n```diff\n")
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
			sb.WriteString("```\n")
		}

	case "Write":
		sb.WriteString("**Tool:** `Write`\n")
		filePath := str("file_path")
		if filePath != "" {
			sb.WriteString(fmt.Sprintf("**File:** `%s`\n", filePath))
		}
		if content := str("content"); content != "" {
			lang := inferLanguageFromPath(filePath)
			sb.WriteString(fmt.Sprintf("\n```%s\n", lang))
			sb.WriteString(content)
			sb.WriteString("\n```\n")
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
				sb.WriteString(fmt.Sprintf("**%s:**\n\n```\n%s\n```\n", k, s))
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
	status v1.TaskResultStatus,
	summary string,
	errMsg string,
) {
	_, err := client.ReportTaskResult(ctx, connect.NewRequest(&v1.ReportTaskResultRequest{
		TaskId:       taskID,
		Status:       status,
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
