package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
	"github.com/kazz187/taskguild/backend/pkg/clog"
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

	// waitForUserResponseTimeout is how long to wait for user input before
	// auto-expiring the interaction and retrying with a prompt that requests
	// NEXT_STATUS output.
	waitForUserResponseTimeout = 5 * time.Minute

	// maxUserResponseRetries is the maximum number of auto-expire + retry
	// cycles before the task is force-completed.
	maxUserResponseRetries = 2

	// maxStatusTransitionRetries is the maximum number of retry attempts
	// when the agent outputs an invalid NEXT_STATUS value.
	maxStatusTransitionRetries = 2
)

const (
	// maxToolOutputSize is the maximum number of characters stored for
	// tool output in task log metadata.
	maxToolOutputSize = 10000
)

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
	permCache *permissionCache,
) {
	// Create task-scoped logger and embed in context.
	logger := slog.Default().With("task_id", taskID)
	ctx = clog.ContextWithLogger(ctx, logger)

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
			logger.Warn("failed to ensure worktree", "error", err)
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

	sessionID := metadata["session_id"]
	prompt := buildUserPrompt(metadata, workDir)
	hasTransitions := metadata["_available_transitions"] != ""

	const maxResumeRetries = 2 // after this many consecutive resume failures, start fresh

	worktreeHookFired := false
	consecutiveErrors := 0
	backoff := initialBackoff
	userResponseRetries := 0
	statusTransitionRetries := 0

	for turn := 0; ; turn++ {
		opts := buildClaudeOptions(instructions, workDir, metadata, sessionID, worktreeName, client, ctx, taskID, agentManagerID, waiter, permCache, tl)
		// Override StderrCallback to also send to task logger.
		opts.StderrCallback = func(line string) {
			logger.Debug("claude-stderr", "line", line)
			tl.LogStderr(line)
		}

		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Turn %d started", turn),
			map[string]string{"turn": fmt.Sprintf("%d", turn)})
		logger.Debug("Claude SDK input", "turn", turn)
		if turn == 0 {
			logger.Debug("system prompt", "prompt", instructions)
			logger.Debug("metadata", "metadata", fmt.Sprintf("%v", metadata))
			logger.Debug("work directory", "work_dir", workDir)
		}
		logger.Debug("user prompt", "prompt", prompt)
		if sessionID != "" {
			logger.Debug("resuming session", "session_id", sessionID)
		}

		result, err := claudeagent.RunQuerySync(ctx, prompt, opts)

		logger.Debug("Claude SDK output", "turn", turn)
		if err != nil {
			logger.Error("Claude SDK error", "turn", turn, "error", err)
		} else if result.Result != nil {
			logger.Debug("Claude SDK result",
				"turn", turn,
				"is_error", result.Result.IsError,
				"session_id", result.Result.SessionID,
				"result", result.Result.Result,
			)
		} else {
			logger.Debug("Claude SDK result is nil", "turn", turn)
		}

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
			// Authentication errors (e.g. expired OAuth token) are not
			// recoverable by retrying. Fail immediately and tell the user
			// to run 'claude login' to re-authenticate.
			if isAuthenticationError(errMsg) {
				authErrMsg := fmt.Sprintf("Authentication failed: %s\nRun 'claude login' to re-authenticate.", errMsg)
				logger.Error("authentication error detected, not retrying", "error", errMsg)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_ERROR, v1.TaskLogLevel_TASK_LOG_LEVEL_ERROR,
					authErrMsg, nil)
				reportTaskResult(ctx, client, taskID, "", authErrMsg)
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_ERROR, authErrMsg)
				return
			}

			consecutiveErrors++
			logger.Error("task error", "consecutive_errors", consecutiveErrors, "max_errors", maxConsecutiveErrors, "error", errMsg)

			// If resume keeps failing, clear session and start fresh.
			if sessionID != "" && consecutiveErrors >= maxResumeRetries {
				logger.Warn("resume failed, clearing session to start fresh", "consecutive_errors", consecutiveErrors)
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
				logger.Error("max consecutive errors reached, giving up")
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
				logger.Info("detected TASK_DESCRIPTION update", "turn", turn)
				saveTaskDescription(ctx, taskClient, taskID, newDesc)
				// Update local metadata so subsequent prompts reflect the new description.
				metadata["_task_description"] = newDesc
				// Log the directive execution to timeline.
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_DIRECTIVE, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
					"Task description updated",
					map[string]string{
						"directive_type": "TASK_DESCRIPTION",
						"turn":           fmt.Sprintf("%d", turn),
					})
			}
		}

		// Parse and execute CREATE_TASK directives from agent output.
		if result.Result != nil {
			ctDirectives := parseCreateTasks(result.Result.Result)
			for _, d := range ctDirectives {
				logger.Info("detected CREATE_TASK directive", "title", d.Title, "turn", turn)
				createTaskFromDirective(ctx, taskClient, taskID, metadata, d)
				// Log the directive execution to timeline.
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_DIRECTIVE, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
					fmt.Sprintf("Task created: %s", d.Title),
					map[string]string{
						"directive_type": "CREATE_TASK",
						"task_title":     d.Title,
						"turn":           fmt.Sprintf("%d", turn),
					})
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
			summary = stripCreateTasks(summary)
		}

		// Log agent text output for this turn.
		if summary != "" {
			// Strip directives for the agent output display.
			agentText := stripNextStatus(summary)
			if agentText != "" {
				preview := truncateText(agentText, 200)
				fullText := agentText
				if len(fullText) > maxToolOutputSize {
					fullText = fullText[:maxToolOutputSize] + "... (truncated)"
				}
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_AGENT_OUTPUT, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
					preview,
					map[string]string{
						"full_text": fullText,
						"turn":      fmt.Sprintf("%d", turn),
					})
			}
		}

		// Check completion: NEXT_STATUS present means task is done.
		nextStatusID := parseNextStatus(summary)
		if nextStatusID != "" {
			// Log the NEXT_STATUS directive to timeline.
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_DIRECTIVE, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Status transition: %s", nextStatusID),
				map[string]string{
					"directive_type": "NEXT_STATUS",
					"next_status":    nextStatusID,
					"turn":           fmt.Sprintf("%d", turn),
				})

			// Validate the transition before reporting completion.
			resolvedID, err := validateAndResolveTransition(nextStatusID, metadata)
			if err != nil && errors.Is(err, errInvalidTransition) {
				statusTransitionRetries++
				logger.Warn("invalid status transition, retrying",
					"next_status", nextStatusID,
					"retry", statusTransitionRetries,
					"max_retries", maxStatusTransitionRetries,
					"error", err)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Invalid status transition to %q (retry %d/%d): %v", nextStatusID, statusTransitionRetries, maxStatusTransitionRetries, err), nil)

				if statusTransitionRetries > maxStatusTransitionRetries {
					logger.Warn("max status transition retries reached, completing without transition")
					tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
						"Max status transition retries reached, completing without transition", nil)
					displaySummary := stripNextStatus(summary)
					reportTaskResult(ctx, client, taskID, displaySummary, "")
					reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (invalid transition after retries)")
					afterHooks()
					maybeRunAgentMDHarness(ctx, metadata, taskID, displaySummary, resolveHookDir(), tl)
					return
				}

				// Retry with a corrective prompt.
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "retrying: invalid status transition")
				prompt = buildTransitionRetryPrompt(nextStatusID, metadata)
				continue
			}

			// Valid transition (or non-retryable error) — proceed with completion.
			if resolvedID != "" {
				nextStatusID = resolvedID
			}
			displaySummary := stripNextStatus(summary)
			logger.Info("completed with NEXT_STATUS", "next_status", nextStatusID, "turn", turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Task completed with status transition (turn %d)", turn),
				map[string]string{"next_status": nextStatusID})
			reportTaskResult(ctx, client, taskID, displaySummary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			// Run after hooks before transitioning status so that hooks
			// still observe the current status and the transition happens
			// only after all hooks complete.
			afterHooks()
			// Launch AGENT.md harness in background goroutine if enabled.
			maybeRunAgentMDHarness(ctx, metadata, taskID, displaySummary, resolveHookDir(), tl)
			if err := handleStatusTransition(ctx, taskClient, taskID, nextStatusID, metadata, tl); err != nil {
				logger.Error("status transition failed", "error", err)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Status transition to %q failed: %v", nextStatusID, err), nil)
			}
			return
		}

		// No transitions available (terminal status) means task is done.
		if !hasTransitions {
			logger.Info("completed at terminal status", "turn", turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Task completed at terminal status (turn %d)", turn), nil)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			// Launch AGENT.md harness in background goroutine if enabled.
			maybeRunAgentMDHarness(ctx, metadata, taskID, summary, resolveHookDir(), tl)
			return
		}

		// Claude hasn't completed — wait for user input.
		logger.Info("waiting for user input", "turn", turn)
		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "waiting for user input")

		userResponse, err := waitForUserResponse(ctx, client, interClient, taskID, agentManagerID, summary, waiter)
		if err == errWaitTimeout {
			userResponseRetries++
			if userResponseRetries > maxUserResponseRetries {
				logger.Warn("max user response retries reached, force-completing task", "retries", userResponseRetries-1)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					"Max user response retries reached, force-completing task", nil)
				reportTaskResult(ctx, client, taskID, summary, "")
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task force-completed (no NEXT_STATUS after retries)")
				return
			}
			logger.Warn("user response timeout, prompting for NEXT_STATUS", "retry", userResponseRetries, "max_retries", maxUserResponseRetries)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
				fmt.Sprintf("User response timeout, retrying with NEXT_STATUS prompt (retry %d/%d)", userResponseRetries, maxUserResponseRetries), nil)
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "retrying: requesting NEXT_STATUS")
			prompt = "You appear to have completed your work but did not output a NEXT_STATUS directive. " +
				"Please review the available status transitions and output your chosen next status on the LAST LINE of your response in the format:\n" +
				"NEXT_STATUS: <status_id>\n\n" +
				"If you still need user input, clearly state what you need."
			continue
		}
		if err != nil {
			logger.Error("user response error, completing task", "error", err)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (no user response)")
			return
		}

		// Got a valid user response — reset the retry counter.
		userResponseRetries = 0
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
	permCache *permissionCache,
	tl *taskLogger,
) *claudeagent.ClaudeAgentOptions {
	logger := clog.LoggerFromContext(ctx)

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
			logger.Debug("using existing worktree directory", "worktree_dir", wtDir)
		}
	}

	opts := &claudeagent.ClaudeAgentOptions{
		SystemPrompt:   instructions,
		Cwd:            cwd,
		PermissionMode: permMode,
		CanUseTool: func(toolName string, input map[string]any, toolCtx claudeagent.ToolPermissionContext) (claudeagent.PermissionResult, error) {
			return handlePermissionRequest(ctx, client, taskID, agentManagerID, toolName, input, waiter, permMode, toolCtx, permCache)
		},
		StderrCallback: func(line string) {
			logger.Debug("claude-stderr", "line", line)
		},
		Hooks: buildToolUseHooks(tl, taskID),
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

func reportTaskResult(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskID string,
	summary string,
	errMsg string,
) {
	logger := clog.LoggerFromContext(ctx)
	_, err := client.ReportTaskResult(ctx, connect.NewRequest(&v1.ReportTaskResultRequest{
		TaskId:       taskID,
		Summary:      summary,
		ErrorMessage: errMsg,
	}))
	if err != nil {
		logger.Error("failed to report task result", "error", err)
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
	logger := clog.LoggerFromContext(ctx)
	_, err := client.ReportAgentStatus(ctx, connect.NewRequest(&v1.ReportAgentStatusRequest{
		AgentManagerId: agentManagerID,
		TaskId:         taskID,
		Status:         status,
		Message:        message,
	}))
	if err != nil {
		logger.Error("failed to report agent status", "error", err)
	}
}

// isAuthenticationError checks whether an error message from Claude CLI
// indicates an authentication failure (e.g. expired OAuth token).
// These errors cannot be resolved by retrying and require the user to
// run 'claude login' to re-authenticate.
func isAuthenticationError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	authPatterns := []string{
		"authentication_error",
		"authentication_failed",
		"oauth token has expired",
		"failed to authenticate",
	}
	for _, pattern := range authPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}
