package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/sourcegraph/conc"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	"github.com/kazz187/taskguild/pkg/clog"
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
	scpCache *singleCommandPermissionCache,
	queryRunner QueryRunner,
	isUserStopped func() bool,
) {
	// Create task-scoped logger and embed in context.
	logger := slog.Default().With("task_id", taskID)
	ctx = clog.ContextWithLogger(ctx, logger)

	// Track the current Claude operating mode (plan, acceptEdits, bypassPermissions, default).
	// Initialized from metadata and updated dynamically via hook callbacks.
	currentMode := metadata["_permission_mode"]
	if currentMode == "" {
		currentMode = string(claudeagent.PermissionModeDefault)
	}

	var modeMu sync.Mutex

	logger.Info("runTask started", "agent_name", metadata["_agent_name"], "use_worktree", metadata["_use_worktree"], "claude_mode", currentMode)
	saveClaudeMode(ctx, taskClient, taskID, currentMode)
	reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "starting task")

	// Initialize task logger for structured log streaming.
	tl := newTaskLogger(ctx, client, taskID)
	defer tl.Close()

	tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO, "Task started", nil)

	// Resolve worktree name: reuse persisted name or generate a new one.
	worktreeName := metadata["worktree"]
	if worktreeName == "" && metadata["_use_worktree"] == "true" {
		logger.Info("generating worktree name")

		worktreeName = generateWorktreeName(ctx, taskID, metadata["_task_title"], workDir, queryRunner)
		logger.Info("worktree name generated", "worktree_name", worktreeName)
		saveWorktreeName(ctx, taskClient, taskID, worktreeName)
		metadata["worktree"] = worktreeName // keep local metadata in sync for buildUserPrompt
	}

	// Execute before_worktree_creation hooks (runs in the main repo directory).
	if metadata["_use_worktree"] == "true" && worktreeName != "" {
		logger.Info("executing before_worktree_creation hooks")
		executeHooks(ctx, taskID, "before_worktree_creation", metadata, workDir, taskClient, tl, queryRunner, "")
		logger.Info("before_worktree_creation hooks completed")
	}

	// Ensure the worktree directory exists before launching Claude so that
	// Cwd is set to the worktree from the very first turn.
	if metadata["_use_worktree"] == "true" && worktreeName != "" {
		logger.Info("ensuring worktree directory", "worktree_name", worktreeName)

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

	// Resolve session early so the afterHooks closure can capture sessionID.
	sessionID := resolveSession(metadata)

	// afterHooks runs after_task_execution hooks exactly once.
	// It is called explicitly before status transitions and deferred as a
	// safety-net for all other return paths so hooks always execute.
	afterHooksExecuted := false

	afterHooks := func() {
		if !afterHooksExecuted {
			afterHooksExecuted = true

			executeHooks(ctx, taskID, "after_task_execution", metadata, resolveHookDir(), taskClient, tl, queryRunner, sessionID)
		}
	}
	defer afterHooks()

	// Defense-in-depth: if context is canceled by user stop, report
	// the cancellation with a fresh context so the RPC can still succeed.
	// Only report "stopped by user" when the user explicitly stopped the task;
	// system-initiated cancellations (e.g. task re-assignment after status
	// transition) should not produce this error.
	// This defer runs after afterHooks (LIFO) but before tl.Close.
	defer func() {
		if ctx.Err() == context.Canceled && isUserStopped() {
			bgCtx, bgCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer bgCancel()

			reportTaskResult(bgCtx, client, taskID, "", "stopped by user")
			reportAgentStatus(bgCtx, client, agentManagerID, taskID,
				v1.AgentStatus_AGENT_STATUS_IDLE, "stopped by user")
		}
	}()

	// Execute before_task_execution hooks.
	logger.Info("executing before_task_execution hooks")
	executeHooks(ctx, taskID, "before_task_execution", metadata, workDir, taskClient, tl, queryRunner, "")
	logger.Info("before_task_execution hooks completed")

	// Start interaction stream listener for this task.
	waiter := newInteractionWaiter()

	var listenerWg conc.WaitGroup
	listenerWg.Go(func() {
		runInteractionListener(ctx, interClient, taskID, waiter)
	})

	prompt, err := buildUserPromptWithImages(ctx, metadata, workDir, taskClient, taskID)
	if err != nil {
		logger.Error("failed to build prompt with images, falling back to text-only", "error", err)

		prompt = buildUserPrompt(metadata, workDir)
	}

	hasTransitions := metadata["_available_transitions"] != "" && metadata["_available_transitions"] != "null"
	logger.Info("task setup complete, entering turn loop", "has_session", sessionID != "", "has_transitions", hasTransitions)

	const maxResumeRetries = 2 // after this many consecutive resume failures, start fresh

	worktreeHookFired := false
	consecutiveErrors := 0
	backoff := initialBackoff
	userResponseRetries := 0
	statusTransitionRetries := 0

	for turn := 0; ; turn++ {
		opts := buildClaudeOptions(instructions, workDir, metadata, sessionID, worktreeName, client, taskClient, interClient, ctx, taskID, agentManagerID, waiter, permCache, scpCache, tl, func(newMode string) {
			modeMu.Lock()
			old := currentMode
			currentMode = newMode
			modeMu.Unlock()

			if old != newMode {
				logger.Info("claude mode changed", "old_mode", old, "new_mode", newMode)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
					fmt.Sprintf("Mode changed: %s → %s", old, newMode),
					map[string]string{"old_mode": old, "new_mode": newMode})
				saveClaudeMode(ctx, taskClient, taskID, newMode)
			}
		})
		// Override StderrCallback to also send to task logger.
		opts.StderrCallback = func(line string) {
			logger.Debug("claude-stderr", "line", line)
			tl.LogStderr(line)
		}

		modeMu.Lock()
		turnMode := currentMode
		modeMu.Unlock()

		tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_START, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
			fmt.Sprintf("Turn %d started", turn),
			map[string]string{"turn": strconv.Itoa(turn), "claude_mode": turnMode})
		logger.Info("starting Claude CLI", "turn", turn, "session_id", sessionID, "claude_mode", turnMode)
		logger.Debug("Claude SDK input", "turn", turn)

		if turn == 0 {
			// Log the actual system prompt from opts (after buildWorkflowContext appends).
			switch sp := opts.SystemPrompt.(type) {
			case *claudeagent.SystemPromptPreset:
				logger.Debug("append-system-prompt", "prompt", sp.Append)
			case string:
				logger.Debug("system prompt", "prompt", sp)
			default:
				logger.Debug("system prompt", "prompt", fmt.Sprintf("%v", opts.SystemPrompt))
			}

			if opts.Agent != "" {
				logger.Debug("agent", "name", opts.Agent)
			}

			logger.Debug("metadata", "metadata", fmt.Sprintf("%v", metadata))
			logger.Debug("work directory", "work_dir", workDir)
		}

		logger.Debug("user prompt", "prompt", prompt)

		if sessionID != "" {
			logger.Debug("resuming session", "session_id", sessionID)
		}

		result, err := queryRunner.RunQuerySync(ctx, prompt, opts, workDir, taskID, fmt.Sprintf("task_turn%d", turn))

		modeMu.Lock()
		endMode := currentMode
		modeMu.Unlock()

		logger.Info("Claude CLI finished", "turn", turn, "has_error", err != nil, "claude_mode", endMode)

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
				map[string]string{"turn": strconv.Itoa(turn), "claude_mode": endMode})
		} else {
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_TURN_END, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Turn %d completed", turn),
				map[string]string{"turn": strconv.Itoa(turn), "claude_mode": endMode})
		}

		// Save session ID for resume.
		// Prefer ResultMessage.SessionID, but fall back to intermediate
		// messages (StreamEvent, etc.) when the turn was interrupted before
		// the ResultMessage arrived (e.g., user-stopped task).
		newSessionID := ""
		if result.Result != nil && result.Result.SessionID != "" {
			newSessionID = result.Result.SessionID
		} else {
			newSessionID = extractSessionIDFromMessages(result.Messages)
		}

		if newSessionID != "" {
			sessionID = newSessionID
			saveSessionIDBestEffort(ctx, taskClient, taskID, sessionID, metadata)
			// Keep local metadata in sync for subtask session inheritance.
			if statusName := metadata["_current_status_name"]; statusName != "" {
				metadata["session_id_"+statusName] = sessionID
			}
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
					"Max consecutive errors reached, giving up: "+errMsg, nil)
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

		logger.Info("processing successful result", "turn", turn, "result_len", len(result.Result.Result))

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
						"turn":           strconv.Itoa(turn),
					})
				// Emit a RESULT log so description updates appear in the chronological results timeline.
				descPreview := newDesc
				if len(descPreview) > 200 {
					descPreview = descPreview[:200] + "..."
				}

				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_RESULT, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
					descPreview,
					map[string]string{
						"full_text":   newDesc,
						"result_type": "description",
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
					"Task created: "+d.Title,
					map[string]string{
						"directive_type": "CREATE_TASK",
						"task_title":     d.Title,
						"turn":           strconv.Itoa(turn),
					})
			}
		}

		// Fire after_worktree_creation hook once, after the first successful turn
		// when a worktree directory exists.
		if !worktreeHookFired && metadata["_use_worktree"] == "true" && worktreeName != "" {
			wtDir := filepath.Join(workDir, ".claude", "worktrees", worktreeName)
			if info, err := os.Stat(wtDir); err == nil && info.IsDir() {
				worktreeHookFired = true

				executeHooks(ctx, taskID, "after_worktree_creation", metadata, wtDir, taskClient, tl, queryRunner, sessionID)
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
						"turn":      strconv.Itoa(turn),
					})
			}
		}

		// Check completion: NEXT_STATUS present means task is done.
		nextStatusID := parseNextStatus(summary)
		logger.Info("parsed directives", "turn", turn, "next_status", nextStatusID, "summary_len", len(summary))

		if nextStatusID != "" {
			// Log the NEXT_STATUS directive to timeline.
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_DIRECTIVE, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				"Status transition: "+nextStatusID,
				map[string]string{
					"directive_type": "NEXT_STATUS",
					"next_status":    nextStatusID,
					"turn":           strconv.Itoa(turn),
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

					afterHooks()
					reportTaskResult(ctx, client, taskID, displaySummary, "")
					reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (invalid transition after retries)")
					maybeRunSkillHarness(ctx, metadata, taskID, displaySummary, workDir, tl, client, queryRunner, sessionID)

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
			// Skip after_task_execution hooks on self-transitions to avoid
			// wasteful repeated hook execution (e.g. create-pr running every
			// iteration of a Develop→Develop loop).
			isSelfTransition := strings.EqualFold(nextStatusID, metadata["_current_status_name"])
			if isSelfTransition {
				logger.Info("skipping after hooks for self-transition", "status", nextStatusID)

				afterHooksExecuted = true // prevent deferred call from running
			} else {
				// Run after hooks while the task remains ASSIGNED so that hooks
				// still observe the current status.
				logger.Info("running after hooks")
				afterHooks()
				logger.Info("after hooks completed")
			}
			// Run harness synchronously while the task is still ASSIGNED.
			// The agent retains ownership until hooks and harness are complete.
			runSkillHarnessAndWait(ctx, metadata, taskID, displaySummary, workDir, tl, client, queryRunner, sessionID)
			// Now unassign and transition.
			logger.Info("reporting task result")
			reportTaskResult(ctx, client, taskID, displaySummary, "")
			logger.Info("reporting agent status IDLE")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")
			logger.Info("calling handleStatusTransition", "next_status", nextStatusID)

			if err := handleStatusTransition(ctx, taskClient, taskID, nextStatusID, metadata, tl); err != nil {
				logger.Error("status transition failed", "error", err)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Status transition to %q failed: %v", nextStatusID, err), nil)
			} else {
				logger.Info("status transition succeeded", "next_status", nextStatusID)
			}

			return
		}

		// No transitions available (terminal status) means task is done.
		if !hasTransitions {
			logger.Info("completed at terminal status", "turn", turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				fmt.Sprintf("Task completed at terminal status (turn %d)", turn), nil)
			afterHooks()
			runSkillHarnessAndWait(ctx, metadata, taskID, summary, workDir, tl, client, queryRunner, sessionID)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed")

			return
		}

		// No NEXT_STATUS output but transitions exist — try auto-transition
		// if exactly one transition is available.
		if transitions, err := parseAvailableTransitions(metadata); err == nil && len(transitions) == 1 {
			autoName := transitions[0].Name
			logger.Info("no NEXT_STATUS output, auto-transitioning (single transition available)",
				"next_status_name", autoName, "turn", turn)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_INFO,
				"No NEXT_STATUS output; auto-transitioning to "+autoName, nil)

			if strings.EqualFold(autoName, metadata["_current_status_name"]) {
				afterHooksExecuted = true
			} else {
				afterHooks()
			}

			runSkillHarnessAndWait(ctx, metadata, taskID, summary, workDir, tl, client, queryRunner, sessionID)
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (auto-transition)")

			err := handleStatusTransition(ctx, taskClient, taskID, autoName, metadata, tl)
			if err != nil {
				logger.Error("auto status transition failed", "error", err)
				tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
					fmt.Sprintf("Auto status transition to %q failed: %v", autoName, err), nil)
			}

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
				// Attempt auto-transition on force-complete so the task
				// does not remain stuck at the current status.
				afterHooks()
				reportTaskResult(ctx, client, taskID, summary, "")
				reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task force-completed (no NEXT_STATUS after retries)")

				err := handleStatusTransition(ctx, taskClient, taskID, "", metadata, tl)
				if err != nil {
					logger.Warn("auto-transition on force-complete failed", "error", err)
					tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
						fmt.Sprintf("Auto-transition on force-complete failed: %v", err), nil)
				}

				return
			}

			logger.Warn("user response timeout, prompting for NEXT_STATUS", "retry", userResponseRetries, "max_retries", maxUserResponseRetries)
			tl.Log(v1.TaskLogCategory_TASK_LOG_CATEGORY_SYSTEM, v1.TaskLogLevel_TASK_LOG_LEVEL_WARN,
				fmt.Sprintf("User response timeout, retrying with NEXT_STATUS prompt (retry %d/%d)", userResponseRetries, maxUserResponseRetries), nil)
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "retrying: requesting NEXT_STATUS")

			prompt = "You appear to have completed your work but did not output a NEXT_STATUS directive. " +
				"Please review the available status transitions and output your chosen next status on the LAST LINE of your response in the format:\n" +
				"NEXT_STATUS: <status>\n\n" +
				"If you still need user input, clearly state what you need."

			continue
		}

		if err != nil {
			logger.Error("user response error, completing task", "error", err)
			// Attempt auto-transition so the task does not remain stuck.
			afterHooks()
			reportTaskResult(ctx, client, taskID, summary, "")
			reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_IDLE, "task completed (no user response)")

			transErr := handleStatusTransition(ctx, taskClient, taskID, "", metadata, tl)
			if transErr != nil {
				logger.Warn("auto-transition on user response error failed", "error", transErr)
			}

			return
		}

		// Got a valid user response — reset the retry counter.
		userResponseRetries = 0

		reportAgentStatus(ctx, client, agentManagerID, taskID, v1.AgentStatus_AGENT_STATUS_RUNNING, "continuing task")

		prompt = userResponse
	}
}

// collectStatusSkills returns the set of skill names that should be
// auto-allowed when invoked via the Skill tool for the current turn:
//
//  1. The current status's execution skills (metadata["_skill_names"]).
//  2. Skills registered as hooks for the current status
//     (metadata["_hooks"], entries whose action_type is "skill" — or blank
//     for legacy hooks that omit the field).
//
// Both are pre-approved by the TaskGuild workflow definition, so the agent
// does not need to prompt the user when Claude decides to launch one as a
// sub-skill during task execution.
func collectStatusSkills(metadata map[string]string) map[string]bool {
	out := map[string]bool{}

	if names := metadata["_skill_names"]; names != "" {
		for n := range strings.SplitSeq(names, ",") {
			if n = strings.TrimSpace(n); n != "" {
				out[n] = true
			}
		}
	}

	if hooksJSON := metadata["_hooks"]; hooksJSON != "" {
		var hooks []struct {
			Name       string `json:"name"`
			ActionType string `json:"action_type"`
		}
		err := json.Unmarshal([]byte(hooksJSON), &hooks)
		if err == nil {
			for _, h := range hooks {
				if h.Name == "" {
					continue
				}

				if h.ActionType == "" || h.ActionType == "skill" {
					out[h.Name] = true
				}
			}
		}
	}

	return out
}

// resolveSession determines which session ID to use for resume.
// Sessions are always directly resumed (never forked). Only hooks and harness
// fork sessions independently via their own opts.ForkSession = true.
//
// Resolution order:
//  1. _inherit_session_from → look up session_id_{inheritedStatus}
//  2. _current_status_name → look up session_id_{currentStatus} (turn 1+ or subtask)
//  3. No session found → fresh session
func resolveSession(metadata map[string]string) string {
	// 1. Inherit from a previous status (e.g., Develop inheriting from Plan).
	if inheritFrom := metadata["_inherit_session_from"]; inheritFrom != "" {
		if sid := metadata["session_id_"+inheritFrom]; sid != "" {
			return sid
		}
	}

	// 2. Same status resume (turn 1+) or subtask inheriting parent's session.
	if statusName := metadata["_current_status_name"]; statusName != "" {
		if sid := metadata["session_id_"+statusName]; sid != "" {
			return sid
		}
	}

	return ""
}

// extractSessionIDFromMessages scans intermediate messages (StreamEvent,
// RateLimitEvent, etc.) in reverse for a session_id. Used as fallback when
// ResultMessage is not available (e.g., user-stopped turn).
func extractSessionIDFromMessages(messages []claudeagent.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		switch m := messages[i].(type) {
		case *claudeagent.StreamEvent:
			if m.SessionID != "" {
				return m.SessionID
			}
		case *claudeagent.RateLimitEvent:
			if m.SessionID != "" {
				return m.SessionID
			}
		}
	}

	return ""
}

// buildClaudeOptions constructs ClaudeAgentOptions for each turn.
func buildClaudeOptions(
	instructions string,
	workDir string,
	metadata map[string]string,
	sessionID string,
	worktreeName string,
	client taskguildv1connect.AgentManagerServiceClient,
	taskClient taskguildv1connect.TaskServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	ctx context.Context,
	taskID string,
	agentManagerID string,
	waiter *interactionWaiter,
	permCache *permissionCache,
	scpCache *singleCommandPermissionCache,
	tl *taskLogger,
	onModeChange func(newMode string),
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

	// Append workflow context to instructions for the system prompt.
	if wfCtx := buildWorkflowContext(metadata, workDir); wfCtx != "" {
		if instructions != "" {
			instructions = instructions + "\n\n" + wfCtx
		} else {
			instructions = wfCtx
		}
	}

	// Build the set of skill names that should be auto-allowed when invoked
	// via the Skill tool: the current status's execution skills plus any
	// skills registered as hooks for the status. Both are pre-approved by
	// the TaskGuild workflow definition.
	statusSkills := collectStatusSkills(metadata)

	opts := &claudeagent.ClaudeAgentOptions{
		Cwd:            cwd,
		PermissionMode: permMode,
		CanUseTool: func(toolName string, input map[string]any, toolCtx claudeagent.ToolPermissionContext) (claudeagent.PermissionResult, error) {
			return handlePermissionRequest(ctx, client, taskID, agentManagerID, toolName, input, waiter, permMode, toolCtx, permCache, scpCache, statusSkills)
		},
		StderrCallback: func(line string) {
			logger.Debug("claude-stderr", "line", line)
		},
		Hooks: buildToolUseHooks(tl, taskID, onModeChange, client, interClient, agentManagerID, waiter),
	}

	// Skill-based mode: set model/tools/disallowedTools from status metadata.
	if skillNames := metadata["_skill_names"]; skillNames != "" {
		// Skill-based execution: no --agent flag. System prompt is workflow context.
		opts.SystemPrompt = instructions

		if m := metadata["_model"]; m != "" {
			opts.Model = m
		}

		if toolsJSON := metadata["_tools"]; toolsJSON != "" {
			var tools []string
			if json.Unmarshal([]byte(toolsJSON), &tools) == nil {
				opts.AllowedTools = tools
			}
		}

		if dtJSON := metadata["_disallowed_tools"]; dtJSON != "" {
			var dt []string
			if json.Unmarshal([]byte(dtJSON), &dt) == nil {
				opts.DisallowedTools = dt
			}
		}
	} else if agentName := metadata["_agent_name"]; agentName != "" {
		// Agent-based execution (fallback): use --agent flag.
		opts.Agent = agentName
		if instructions != "" {
			opts.SystemPrompt = &claudeagent.SystemPromptPreset{
				Type:   "preset",
				Append: instructions,
			}
		}
	} else {
		opts.SystemPrompt = instructions
	}

	// Effort setting (applies to both skill-based and agent-based modes).
	if effort := metadata["_effort"]; effort != "" {
		opts.Effort = &effort
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
