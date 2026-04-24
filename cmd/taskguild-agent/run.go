package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/version"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// scriptTracker tracks running script executions for graceful hot-reload.
// When the sentinel sends SIGUSR1, the agent-manager sets rejectScripts to
// prevent new script executions and waits for running ones to complete
// (via scriptWg) before shutting down.
var scriptTracker struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	reject bool // true once SIGUSR1 is received; prevents new script starts
}

// userStoppedTasks tracks which tasks were explicitly stopped by the user
// (via CancelTaskCommand) as opposed to system-initiated cancellations
// (SIGINT/SIGTERM, task re-assignment). This prevents false "stopped by user"
// reports when context is canceled for non-user reasons.
var userStoppedTasks struct {
	mu      sync.Mutex
	stopped map[string]bool
}

func init() {
	userStoppedTasks.stopped = make(map[string]bool)
}

// safeGo launches f in a new goroutine with panic recovery using conc.WaitGroup.
func safeGo(name string, f func()) {
	var wg conc.WaitGroup
	wg.Go(f)
	go func() {
		if r := wg.WaitAndRecover(); r != nil {
			slog.Error("goroutine panicked", "handler", name,
				"panic", fmt.Sprintf("%v", r.Value), "stack", string(r.Stack))
		}
	}()
}

type config struct {
	ServerURL          string
	APIKey             string
	AgentManagerID     string
	MaxConcurrentTasks int
	WorkDir            string
	ProjectName        string
	Env                string
	LogLevel           string
}

func loadConfig() (*config, error) {
	cfg := &config{
		ServerURL:          "http://localhost:3100",
		AgentManagerID:     ulid.Make().String(),
		MaxConcurrentTasks: 10,
		WorkDir:            ".",
		Env:                "local",
		LogLevel:           "debug",
	}

	if v := os.Getenv("TASKGUILD_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	cfg.APIKey = os.Getenv("TASKGUILD_API_KEY")
	if cfg.APIKey == "" {
		return nil, errors.New("TASKGUILD_API_KEY is required")
	}
	if v := os.Getenv("TASKGUILD_AGENT_MANAGER_ID"); v != "" {
		cfg.AgentManagerID = v
	}
	if v := os.Getenv("TASKGUILD_MAX_CONCURRENT_TASKS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid TASKGUILD_MAX_CONCURRENT_TASKS: %w", err)
		}
		cfg.MaxConcurrentTasks = n
	}
	if v := os.Getenv("TASKGUILD_WORK_DIR"); v != "" {
		cfg.WorkDir = v
	}

	// Resolve WorkDir to an absolute path so child processes inherit a stable CWD.
	if abs, err := filepath.Abs(cfg.WorkDir); err == nil {
		cfg.WorkDir = abs
	}

	// Derive project name from env var or working directory basename.
	if v := os.Getenv("TASKGUILD_PROJECT_NAME"); v != "" {
		cfg.ProjectName = v
	} else {
		absDir, err := filepath.Abs(cfg.WorkDir)
		if err == nil {
			cfg.ProjectName = filepath.Base(absDir)
		}
	}

	if v := os.Getenv("TASKGUILD_ENV"); v != "" {
		cfg.Env = v
	}
	if v := os.Getenv("TASKGUILD_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	return cfg, nil
}

// runAgent is the entry point for the "run" subcommand.
// It contains the original main() logic: connects to the TaskGuild server,
// subscribes for task assignments, and executes tasks.
func runAgent() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	// Initialize slog.
	var slogLevel slog.Level
	if err := slogLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		slogLevel = slog.LevelDebug
	}
	var handler slog.Handler
	if cfg.Env == "local" {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	} else {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	}
	slog.SetDefault(slog.New(handler))

	slog.Info("agent-manager starting",
		"agent_manager_id", cfg.AgentManagerID,
		"version", version.Short(),
		"server_url", cfg.ServerURL,
		"max_tasks", cfg.MaxConcurrentTasks,
		"work_dir", cfg.WorkDir,
		"project_name", cfg.ProjectName,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// taskRootCtx is a separate context tree for tasks. Canceling ctx
	// (for subscribe loop teardown) does NOT cascade to running tasks,
	// allowing them to finish during graceful hot-reload.
	taskRootCtx, taskRootCancel := context.WithCancel(context.Background())
	defer taskRootCancel()

	// rejectTasks is set to true on SIGUSR1 to prevent new task claims.
	var rejectTasks atomic.Bool

	// taskWg tracks task goroutines separately so the SIGUSR1 handler can
	// wait for tasks to drain without waiting for infrastructure goroutines
	// (which depend on ctx and would deadlock).
	var taskWg conc.WaitGroup

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	var sigWg conc.WaitGroup
	sigWg.Go(func() {
		select {
		case sig := <-sigCh:
			slog.Info("received signal, shutting down", "signal", sig)
			cancel()
			taskRootCancel()
		case <-ctx.Done():
		}
	})

	// Handle SIGUSR1 for graceful hot-reload.
	// When the sentinel detects a binary update it sends SIGUSR1 instead of
	// SIGTERM. This handler stops accepting new scripts and tasks, waits for
	// all running scripts and tasks to complete naturally, and then triggers
	// the normal shutdown flow.
	usr1Ch := make(chan os.Signal, 1)
	signal.Notify(usr1Ch, syscall.SIGUSR1)
	sigWg.Go(func() {
		select {
		case <-usr1Ch:
			slog.Info("received SIGUSR1 (hot reload), draining running scripts and tasks")

			// Reject new scripts and tasks.
			scriptTracker.mu.Lock()
			scriptTracker.reject = true
			scriptTracker.mu.Unlock()
			rejectTasks.Store(true)

			// Wait for running scripts to complete naturally (no cancellation).
			slog.Info("waiting for running scripts to complete")
			scriptTracker.wg.Wait()
			slog.Info("all scripts completed")

			// Wait for running tasks to complete naturally (no cancellation).
			slog.Info("waiting for running tasks to complete")
			taskWg.Wait()
			slog.Info("all tasks completed, shutting down for hot reload restart")

			cancel()
			taskRootCancel()
		case <-ctx.Done():
		}
	})

	// Create Connect RPC clients with API key interceptor
	httpClient := http.DefaultClient
	interceptor := newAuthInterceptor(cfg.APIKey)
	client := taskguildv1connect.NewAgentManagerServiceClient(
		httpClient,
		cfg.ServerURL,
		connect.WithInterceptors(interceptor),
	)
	taskClient := taskguildv1connect.NewTaskServiceClient(
		httpClient,
		cfg.ServerURL,
		connect.WithInterceptors(interceptor),
	)
	interClient := taskguildv1connect.NewInteractionServiceClient(
		httpClient,
		cfg.ServerURL,
		connect.WithInterceptors(interceptor),
	)

	// Wait for the server to be ready before making any RPC calls.
	// This is important during sentinel hot-reload restarts where the
	// server may still be starting up.
	if err := waitForServer(ctx, httpClient, cfg.ServerURL); err != nil {
		slog.Error("failed waiting for server", "error", err)
		return
	}

	// Permission cache: shared across all tasks within this agent-manager.
	permCache := newPermissionCache(cfg.ProjectName, client)

	// Single-command permission cache: regex-based per-command rules.
	scpCache := newSingleCommandPermissionCache(cfg.ProjectName, client)
	scpCache.Sync(ctx)

	// Task tracking
	var (
		mu          sync.Mutex
		activeTasks = make(map[string]context.CancelFunc)
		wg          conc.WaitGroup // infrastructure goroutines (heartbeat, etc.)
	)
	// Start heartbeat goroutine
	wg.Go(func() {
		heartbeat(ctx, client, cfg.AgentManagerID)
	})

	// Subscribe loop with reconnection and exponential backoff.
	const (
		subscribeInitialBackoff = 5 * time.Second
		subscribeMaxBackoff     = 5 * time.Minute
		subscribeReceiveTimeout = 3 * time.Minute
	)
	subscribeBackoff := subscribeInitialBackoff

	// On the very first sync, honor the --override-agent-md flag.
	firstSync := true

	for {
		if ctx.Err() != nil {
			break
		}

		// Re-sync agents, permissions, and scripts on each reconnection so local files stay up-to-date.
		forceAll := firstSync && overrideAgentMDFlag != nil && *overrideAgentMDFlag
		syncAgents(ctx, client, cfg, nil, forceAll)
		firstSync = false
		syncPermissions(ctx, client, cfg, permCache)
		syncClaudeSettings(ctx, client, cfg)
		syncScripts(ctx, client, cfg, nil) // nil = don't force-overwrite any existing files
		syncSkills(ctx, client, cfg, nil)

		err := runSubscribeLoop(ctx, client, taskClient, interClient, cfg, &mu, activeTasks, &wg, &taskWg, taskRootCtx, &rejectTasks, permCache, scpCache, subscribeReceiveTimeout)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			slog.Error("subscribe stream error, reconnecting", "error", err, "backoff", subscribeBackoff)
			select {
			case <-time.After(subscribeBackoff):
			case <-ctx.Done():
			}
			// Exponential backoff, capped.
			subscribeBackoff *= 2
			if subscribeBackoff > subscribeMaxBackoff {
				subscribeBackoff = subscribeMaxBackoff
			}
		} else {
			// Successful connection (clean disconnect) — reset backoff but
			// still wait a short delay to prevent reconnection storms when
			// the server forcefully replaces connections in quick succession
			// (e.g. old handler's deferred Unregister racing with new handler).
			subscribeBackoff = subscribeInitialBackoff
			slog.Info("clean disconnect, reconnecting after short delay", "delay", subscribeBackoff)
			select {
			case <-time.After(subscribeBackoff):
			case <-ctx.Done():
			}
		}
	}

	slog.Info("waiting for active tasks to finish")
	// On normal shutdown (SIGINT/SIGTERM), cancel all active tasks.
	// On hot-reload (SIGUSR1), tasks already drained naturally.
	if !rejectTasks.Load() {
		mu.Lock()
		for taskID, cancelFn := range activeTasks {
			slog.Info("canceling task", "task_id", taskID)
			cancelFn()
		}
		mu.Unlock()
	}

	taskWg.Wait()
	wg.Wait()
	sigWg.Wait()
	slog.Info("agent-manager stopped")
}

func runSubscribeLoop(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskClient taskguildv1connect.TaskServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	cfg *config,
	mu *sync.Mutex,
	activeTasks map[string]context.CancelFunc,
	wg *conc.WaitGroup,
	taskWg *conc.WaitGroup,
	taskRootCtx context.Context,
	rejectTasks *atomic.Bool,
	permCache *permissionCache,
	scpCache *singleCommandPermissionCache,
	receiveTimeout time.Duration,
) error {
	// Collect active task IDs so the server knows which tasks are still running
	// and should NOT be released during reconnection.
	mu.Lock()
	activeTaskIDs := make([]string, 0, len(activeTasks))
	for taskID := range activeTasks {
		activeTaskIDs = append(activeTaskIDs, taskID)
	}
	mu.Unlock()

	if len(activeTaskIDs) > 0 {
		slog.Info("reconnecting with active tasks", "count", len(activeTaskIDs), "task_ids", activeTaskIDs)
	}

	// Use a derived context so the watchdog can cancel the stream
	// without tearing down the entire agent.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	stream, err := client.Subscribe(streamCtx, connect.NewRequest(&v1.AgentManagerSubscribeRequest{
		AgentManagerId:     cfg.AgentManagerID,
		MaxConcurrentTasks: int32(cfg.MaxConcurrentTasks),
		ProjectName:        cfg.ProjectName,
		ActiveTaskIds:      activeTaskIDs,
		AgentVersion:       version.Short(),
		WorkDir:            cfg.WorkDir,
	}))
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer stream.Close()

	slog.Info("subscribe stream connected")

	// Watchdog: force reconnect if no message received within timeout.
	var lastReceive atomic.Int64
	lastReceive.Store(time.Now().UnixNano())

	watchdogDone := make(chan struct{})
	var watchdogWg conc.WaitGroup
	watchdogWg.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-watchdogDone:
				return
			case <-ticker.C:
				elapsed := time.Since(time.Unix(0, lastReceive.Load()))
				if elapsed > receiveTimeout {
					slog.Warn("subscribe stream receive timeout, forcing reconnect",
						"elapsed", elapsed, "timeout", receiveTimeout)
					streamCancel()
					return
				}
			}
		}
	})
	defer func() {
		close(watchdogDone)
		watchdogWg.Wait()
	}()

	for stream.Receive() {
		lastReceive.Store(time.Now().UnixNano())
		cmd := stream.Msg()

		// Skip empty commands (e.g. caused by proxy-injected frames or
		// partial envelope reads from intermediaries).
		if cmd.GetCommand() == nil {
			slog.Debug("skipping empty command (nil oneof)")
			continue
		}

		switch c := cmd.GetCommand().(type) {
		case *v1.AgentCommand_TaskAvailable:
			taskAvail := c.TaskAvailable
			taskID := taskAvail.GetTaskId()
			slog.Info("task available", "task_id", taskID, "title", taskAvail.GetTitle())

			// Reject new tasks during hot-reload drain.
			if rejectTasks.Load() {
				slog.Info("rejecting task: agent is shutting down for hot reload", "task_id", taskID)
				continue
			}

			// Skip if this task is already running.
			mu.Lock()
			if prevCancel, ok := activeTasks[taskID]; ok {
				mu.Unlock()
				slog.Info("task already active, canceling previous run and re-claiming", "task_id", taskID)
				prevCancel()
			} else {
				mu.Unlock()
			}

			// Check capacity using the activeTasks map.
			mu.Lock()
			if len(activeTasks) >= cfg.MaxConcurrentTasks {
				mu.Unlock()
				slog.Info("at max capacity, skipping task", "task_id", taskID)
				continue
			}
			mu.Unlock()

			// Try to claim the task
			claimResp, err := client.ClaimTask(ctx, connect.NewRequest(&v1.ClaimTaskRequest{
				TaskId:         taskID,
				AgentManagerId: cfg.AgentManagerID,
			}))
			if err != nil {
				slog.Error("failed to claim task", "task_id", taskID, "error", err)
				continue
			}
			if !claimResp.Msg.GetSuccess() {
				slog.Info("task already claimed by another agent", "task_id", taskID)
				continue
			}

			slog.Info("claimed task", "task_id", taskID)
			instructions := claimResp.Msg.GetInstructions()
			metadata := claimResp.Msg.GetMetadata()

			taskCtx, taskCancel := context.WithCancel(taskRootCtx)
			mu.Lock()
			activeTasks[taskID] = taskCancel
			mu.Unlock()

			taskWg.Go(func() {
				tID := taskID
				defer func() {
					mu.Lock()
					delete(activeTasks, tID)
					mu.Unlock()
					userStoppedTasks.mu.Lock()
					delete(userStoppedTasks.stopped, tID)
					userStoppedTasks.mu.Unlock()
					taskCancel()
				}()
				defer func() {
					if r := recover(); r != nil {
						slog.Error("runTask panicked", "task_id", tID, "panic", fmt.Sprintf("%v", r))
					}
				}()

				isUserStopped := func() bool {
					userStoppedTasks.mu.Lock()
					defer userStoppedTasks.mu.Unlock()
					return userStoppedTasks.stopped[tID]
				}

				slog.Info("launching runTask goroutine", "task_id", tID)
				runTask(taskCtx, client, taskClient, interClient, cfg.AgentManagerID, tID, instructions, metadata, cfg.WorkDir, permCache, scpCache, subprocessQueryRunner{projectID: metadata["_project_id"], projectDir: cfg.WorkDir}, isUserStopped)
				slog.Info("runTask goroutine finished", "task_id", tID)
			})

		case *v1.AgentCommand_ListWorktrees:
			listCmd := c.ListWorktrees
			slog.Info("received list worktrees command", "request_id", listCmd.GetRequestId())
			safeGo("handleListWorktrees", func() { handleListWorktrees(ctx, client, cfg, listCmd.GetRequestId()) })

		case *v1.AgentCommand_DeleteWorktree:
			deleteCmd := c.DeleteWorktree
			slog.Info("received delete worktree command",
				"request_id", deleteCmd.GetRequestId(),
				"worktree_name", deleteCmd.GetWorktreeName(),
				"force", deleteCmd.GetForce(),
			)
			safeGo("handleDeleteWorktree", func() { handleDeleteWorktree(ctx, client, cfg, deleteCmd) })

		case *v1.AgentCommand_GitPullMain:
			pullCmd := c.GitPullMain
			slog.Info("received git pull main command", "request_id", pullCmd.GetRequestId())
			safeGo("handleGitPullMain", func() { handleGitPullMain(ctx, client, cfg, pullCmd.GetRequestId()) })

		case *v1.AgentCommand_SyncAgents:
			syncCmd := c.SyncAgents
			forceNames := make(map[string]bool, len(syncCmd.GetForceOverwriteAgentNames()))
			for _, n := range syncCmd.GetForceOverwriteAgentNames() {
				forceNames[n] = true
			}
			slog.Info("received sync agents command, re-syncing", "force_overwrite_count", len(forceNames))
			syncAgents(ctx, client, cfg, forceNames, false)

		case *v1.AgentCommand_SyncPermissions:
			slog.Info("received sync permissions command, re-syncing")
			syncPermissions(ctx, client, cfg, permCache)
			scpCache.Sync(ctx)

		case *v1.AgentCommand_SyncScripts:
			syncCmd := c.SyncScripts
			forceIDs := make(map[string]bool, len(syncCmd.GetForceOverwriteScriptIds()))
			for _, id := range syncCmd.GetForceOverwriteScriptIds() {
				forceIDs[id] = true
			}
			slog.Info("received sync scripts command, re-syncing", "force_overwrite_count", len(forceIDs))
			syncScripts(ctx, client, cfg, forceIDs)

		case *v1.AgentCommand_CompareScripts:
			compareCmd := c.CompareScripts
			slog.Info("received compare scripts command", "request_id", compareCmd.GetRequestId())
			safeGo("handleCompareScripts", func() { handleCompareScripts(ctx, client, cfg, compareCmd) })

		case *v1.AgentCommand_ExecuteScript:
			execCmd := c.ExecuteScript
			slog.Info("received execute script command",
				"request_id", execCmd.GetRequestId(),
				"script_id", execCmd.GetScriptId(),
				"filename", execCmd.GetFilename(),
			)
			safeGo("handleExecuteScript", func() { handleExecuteScript(ctx, client, cfg, execCmd) })

		case *v1.AgentCommand_StopScript:
			stopCmd := c.StopScript
			slog.Info("received stop script command", "request_id", stopCmd.GetRequestId())
			handleStopScript(stopCmd)

		case *v1.AgentCommand_CompareAgents:
			compareCmd := c.CompareAgents
			slog.Info("received compare agents command", "request_id", compareCmd.GetRequestId())
			safeGo("handleCompareAgents", func() { handleCompareAgents(ctx, client, cfg, compareCmd) })

		case *v1.AgentCommand_SyncSkills:
			syncCmd := c.SyncSkills
			forceIDs := make(map[string]bool, len(syncCmd.GetForceOverwriteSkillIds()))
			for _, id := range syncCmd.GetForceOverwriteSkillIds() {
				forceIDs[id] = true
			}
			slog.Info("received sync skills command, re-syncing", "force_overwrite_count", len(forceIDs))
			syncSkills(ctx, client, cfg, forceIDs)

		case *v1.AgentCommand_CompareSkills:
			compareCmd := c.CompareSkills
			slog.Info("received compare skills command", "request_id", compareCmd.GetRequestId())
			safeGo("handleCompareSkills", func() { handleCompareSkills(ctx, client, cfg, compareCmd) })

		case *v1.AgentCommand_SyncClaudeSettings:
			slog.Info("received sync claude settings command, re-syncing")
			syncClaudeSettings(ctx, client, cfg)

		case *v1.AgentCommand_CancelTask:
			cancelCmd := c.CancelTask
			taskID := cancelCmd.GetTaskId()
			reason := cancelCmd.GetReason()
			slog.Info("cancel request for task", "task_id", taskID, "reason", reason)

			userStoppedTasks.mu.Lock()
			userStoppedTasks.stopped[taskID] = true
			userStoppedTasks.mu.Unlock()

			mu.Lock()
			if cancelFn, ok := activeTasks[taskID]; ok {
				cancelFn()
			}
			mu.Unlock()

		case *v1.AgentCommand_AssignTask:
			assignCmd := c.AssignTask
			taskID := assignCmd.GetTaskId()
			slog.Info("direct task assignment", "task_id", taskID)

			// Reject new tasks during hot-reload drain.
			if rejectTasks.Load() {
				slog.Info("rejecting assigned task: agent is shutting down for hot reload", "task_id", taskID)
				continue
			}

			// Cancel previous run if the same task is re-assigned.
			mu.Lock()
			if prevCancel, ok := activeTasks[taskID]; ok {
				mu.Unlock()
				slog.Info("task already active, canceling previous run for re-assignment", "task_id", taskID)
				prevCancel()
			} else {
				mu.Unlock()
			}

			instructions := assignCmd.GetInstructions()
			metadata := assignCmd.GetMetadata()

			// Check capacity using the activeTasks map.
			mu.Lock()
			if len(activeTasks) >= cfg.MaxConcurrentTasks {
				mu.Unlock()
				slog.Warn("at max capacity, cannot run assigned task", "task_id", taskID)
				continue
			}
			mu.Unlock()

			taskCtx, taskCancel := context.WithCancel(taskRootCtx)
			mu.Lock()
			activeTasks[taskID] = taskCancel
			mu.Unlock()

			taskWg.Go(func() {
				tID := taskID
				defer func() {
					mu.Lock()
					delete(activeTasks, tID)
					mu.Unlock()
					userStoppedTasks.mu.Lock()
					delete(userStoppedTasks.stopped, tID)
					userStoppedTasks.mu.Unlock()
					taskCancel()
				}()
				defer func() {
					if r := recover(); r != nil {
						slog.Error("runTask panicked", "task_id", tID, "panic", fmt.Sprintf("%v", r))
					}
				}()

				isUserStopped := func() bool {
					userStoppedTasks.mu.Lock()
					defer userStoppedTasks.mu.Unlock()
					return userStoppedTasks.stopped[tID]
				}

				slog.Info("launching runTask goroutine (assigned)", "task_id", tID)
				runTask(taskCtx, client, taskClient, interClient, cfg.AgentManagerID, tID, instructions, metadata, cfg.WorkDir, permCache, scpCache, subprocessQueryRunner{projectID: metadata["_project_id"], projectDir: cfg.WorkDir}, isUserStopped)
				slog.Info("runTask goroutine finished (assigned)", "task_id", tID)
			})

		case *v1.AgentCommand_InteractionResponse:
			// Interaction responses are handled per-task via SubscribeInteractions.
			// Log and ignore if received on the global subscribe stream.
			slog.Debug("received interaction_response on subscribe stream, ignoring",
				"interaction_id", c.InteractionResponse.GetInteractionId(),
			)

		case *v1.AgentCommand_Ping:
			// Server-side keepalive ping — silently ignore.

		default:
			// Nil should be caught by the guard above; if it still reaches
			// here, silently skip to avoid noisy logs from proxy artifacts.
			if cmd.GetCommand() == nil {
				continue
			}
			slog.Warn("unknown command type", "type", fmt.Sprintf("%T", cmd.GetCommand()))
		}
	}

	if err := stream.Err(); err != nil {
		return fmt.Errorf("stream error: %w", err)
	}
	return nil
}

func heartbeat(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, agentManagerID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := client.Heartbeat(ctx, connect.NewRequest(&v1.HeartbeatRequest{
				AgentManagerId: agentManagerID,
			}))
			if err != nil {
				slog.Warn("heartbeat error", "error", err)
			}
		}
	}
}

// waitForServer polls the server's /health endpoint until it returns 200 OK.
// This prevents the agent from attempting RPC calls before the server is ready,
// which is common during sentinel hot-reload restarts.
func waitForServer(ctx context.Context, httpClient *http.Client, serverURL string) error {
	const (
		initialBackoff = 1 * time.Second
		maxBackoff     = 30 * time.Second
		requestTimeout = 5 * time.Second
	)

	healthURL := serverURL + "/health"
	backoff := initialBackoff
	waited := false

	for {
		reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, healthURL, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("creating health request: %w", err)
		}

		resp, err := httpClient.Do(req)
		cancel()

		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				if waited {
					slog.Info("server is ready")
				}
				return nil
			}
			err = fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		if !waited {
			slog.Info("waiting for server to become ready", "url", healthURL)
			waited = true
		}
		slog.Info("server not ready, retrying", "backoff", backoff, "error", err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// isClaudeInternalPath returns true if the given file path is under the .claude/ directory.
// These files are managed by Claude Code and should be ignored when checking for
// uncommitted changes in worktrees.
func isClaudeInternalPath(path string) bool {
	return strings.HasPrefix(path, ".claude/") || path == ".claude"
}

// handleListWorktrees scans the .claude/worktrees/ directory and reports
// available worktrees to the backend.
func handleListWorktrees(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, requestID string) {
	worktreesDir := filepath.Join(cfg.WorkDir, ".claude", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Info("no worktrees directory found", "path", worktreesDir)
		} else {
			slog.Error("failed to read worktrees directory", "error", err)
		}
		// Report empty list so frontend knows the scan completed.
		_, _ = client.ReportWorktreeList(ctx, connect.NewRequest(&v1.ReportWorktreeListRequest{
			RequestId:   requestID,
			ProjectName: cfg.ProjectName,
		}))
		return
	}

	var worktrees []*v1.WorktreeInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		wtDir := filepath.Join(worktreesDir, name)

		// Get branch name.
		branch := "worktree-" + name
		cmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
		cmd.Dir = wtDir
		if out, err := cmd.Output(); err == nil {
			if b := strings.TrimSpace(string(out)); b != "" {
				branch = b
			}
		}

		// Detect uncommitted changes using git status --porcelain.
		// Changes under .claude/ are ignored as they are managed by Claude Code.
		var hasChanges bool
		var changedFiles []string
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = wtDir
		if out, err := statusCmd.Output(); err == nil {
			lines := strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n")
			for line := range lines {
				// git status --porcelain format: "XY filename" (filename starts at position 3)
				// Do not TrimSpace: the leading space is part of the status code format
				// and trimming shifts the offset, corrupting the filename (e.g. ".claude/" → "claude/").
				if len(line) < 4 {
					continue
				}
				filePath := line[3:]
				if isClaudeInternalPath(filePath) {
					continue
				}
				changedFiles = append(changedFiles, filePath)
				hasChanges = true
			}
		}

		worktrees = append(worktrees, &v1.WorktreeInfo{
			Name:         name,
			Branch:       branch,
			HasChanges:   hasChanges,
			ChangedFiles: changedFiles,
		})
	}

	_, err = client.ReportWorktreeList(ctx, connect.NewRequest(&v1.ReportWorktreeListRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		Worktrees:   worktrees,
	}))
	if err != nil {
		slog.Error("failed to report worktree list", "error", err)
	} else {
		slog.Info("reported worktrees", "count", len(worktrees), "request_id", requestID)
	}
}

// handleDeleteWorktree removes a git worktree and its associated branch.
func handleDeleteWorktree(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.DeleteWorktreeCommand) {
	requestID := cmd.GetRequestId()
	worktreeName := cmd.GetWorktreeName()
	force := cmd.GetForce()

	reportResult := func(success bool, errMsg string) {
		_, err := client.ReportWorktreeDeleteResult(ctx, connect.NewRequest(&v1.ReportWorktreeDeleteResultRequest{
			RequestId:    requestID,
			ProjectName:  cfg.ProjectName,
			WorktreeName: worktreeName,
			Success:      success,
			ErrorMessage: errMsg,
		}))
		if err != nil {
			slog.Error("failed to report worktree delete result", "error", err)
		}
	}

	wtDir := filepath.Join(cfg.WorkDir, ".claude", "worktrees", worktreeName)

	// Verify worktree exists.
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		reportResult(false, fmt.Sprintf("worktree %q does not exist", worktreeName))
		return
	}

	// Check for uncommitted changes (unless force).
	// Changes under .claude/ are ignored as they are managed by Claude Code.
	hasClaudeOnlyChanges := false
	if !force {
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = wtDir
		if out, err := statusCmd.Output(); err == nil {
			raw := strings.TrimRight(string(out), "\n")
			if raw != "" {
				hasNonClaudeChanges := false
				for line := range strings.SplitSeq(raw, "\n") {
					// Do not TrimSpace: the leading space is part of the porcelain status code format
					// and trimming shifts the offset, corrupting the filename (e.g. ".claude/" → "claude/").
					if len(line) < 4 {
						continue
					}
					if isClaudeInternalPath(line[3:]) {
						hasClaudeOnlyChanges = true
						continue
					}
					hasNonClaudeChanges = true
				}
				if hasNonClaudeChanges {
					reportResult(false, "worktree has uncommitted changes; use force delete")
					return
				}
			}
		}
	}

	// Determine the branch name before removing the worktree.
	branchName := "worktree-" + worktreeName
	branchCmd := exec.CommandContext(ctx, "git", "branch", "--show-current")
	branchCmd.Dir = wtDir
	if out, err := branchCmd.Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			branchName = b
		}
	}

	// Remove the git worktree. Use --force when there are .claude/-only changes
	// since git worktree remove also checks for uncommitted changes.
	var removeCmd *exec.Cmd
	if force || hasClaudeOnlyChanges {
		removeCmd = exec.CommandContext(ctx, "git", "worktree", "remove", "--force", wtDir)
	} else {
		removeCmd = exec.CommandContext(ctx, "git", "worktree", "remove", wtDir)
	}
	removeCmd.Dir = cfg.WorkDir
	if out, err := removeCmd.CombinedOutput(); err != nil {
		reportResult(false, fmt.Sprintf("git worktree remove failed: %v: %s", err, strings.TrimSpace(string(out))))
		return
	}

	// Delete the associated branch (best-effort).
	deleteBranchCmd := exec.CommandContext(ctx, "git", "branch", "-D", branchName)
	deleteBranchCmd.Dir = cfg.WorkDir
	if out, err := deleteBranchCmd.CombinedOutput(); err != nil {
		slog.Warn("failed to delete branch", "branch", branchName, "error", err, "output", strings.TrimSpace(string(out)))
		// Not fatal – worktree is already removed.
	}

	slog.Info("deleted worktree", "worktree_name", worktreeName, "branch", branchName, "force", force)
	reportResult(true, "")
}

// handleGitPullMain executes `git pull origin main` in the main repository working directory.
func handleGitPullMain(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, requestID string) {
	reportResult := func(success bool, output, errMsg string) {
		_, err := client.ReportGitPullMainResult(ctx, connect.NewRequest(&v1.ReportGitPullMainResultRequest{
			RequestId:    requestID,
			ProjectName:  cfg.ProjectName,
			Success:      success,
			Output:       output,
			ErrorMessage: errMsg,
		}))
		if err != nil {
			slog.Error("failed to report git pull main result", "error", err)
		}
	}

	cmd := exec.CommandContext(ctx, "git", "pull", "origin", "main")
	cmd.Dir = cfg.WorkDir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		slog.Error("git pull origin main failed", "error", err, "output", output)
		reportResult(false, output, fmt.Sprintf("git pull origin main failed: %v", err))
		return
	}

	slog.Info("git pull origin main succeeded", "output", output)
	reportResult(true, output, "")
}

// authInterceptor adds the API key to outgoing requests.
type authInterceptor struct {
	apiKey string
}

func newAuthInterceptor(apiKey string) *authInterceptor {
	return &authInterceptor{apiKey: apiKey}
}

func (i *authInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		req.Header().Set("Authorization", "Bearer "+i.apiKey)
		return next(ctx, req)
	}
}

func (i *authInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		conn := next(ctx, spec)
		conn.RequestHeader().Set("Authorization", "Bearer "+i.apiKey)
		return conn
	}
}

func (i *authInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}
