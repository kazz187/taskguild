package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
	"github.com/oklog/ulid/v2"
)

type config struct {
	ServerURL          string
	APIKey             string
	AgentManagerID     string
	MaxConcurrentTasks int
	WorkDir            string
	ProjectName        string
}

func loadConfig() (*config, error) {
	cfg := &config{
		ServerURL:          "http://localhost:3100",
		AgentManagerID:     ulid.Make().String(),
		MaxConcurrentTasks: 10,
		WorkDir:            ".",
	}

	if v := os.Getenv("TASKGUILD_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	cfg.APIKey = os.Getenv("TASKGUILD_API_KEY")
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("TASKGUILD_API_KEY is required")
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

	return cfg, nil
}

// runAgent is the entry point for the "run" subcommand.
// It contains the original main() logic: connects to the TaskGuild server,
// subscribes for task assignments, and executes tasks.
func runAgent() {
	// Set up log file: write to both stderr and file.
	logFile, err := os.OpenFile("agent-manager.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer logFile.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, logFile))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("agent-manager starting (id: %s, server: %s, max_tasks: %d, work_dir: %s, project_name: %s)",
		cfg.AgentManagerID, cfg.ServerURL, cfg.MaxConcurrentTasks, cfg.WorkDir, cfg.ProjectName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()
	}()

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

	// Task tracking
	var (
		mu          sync.Mutex
		activeTasks = make(map[string]context.CancelFunc)
		wg          sync.WaitGroup
		sem         = make(chan struct{}, cfg.MaxConcurrentTasks)
	)

	// Start heartbeat goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		heartbeat(ctx, client, cfg.AgentManagerID)
	}()

	// Subscribe loop with reconnection
	for {
		if ctx.Err() != nil {
			break
		}

		// Re-sync agents, permissions, and scripts on each reconnection so local files stay up-to-date.
		syncAgents(ctx, client, cfg)
		syncPermissions(ctx, client, cfg)
		syncScripts(ctx, client, cfg)

		err := runSubscribeLoop(ctx, client, taskClient, interClient, cfg, &mu, activeTasks, &wg, sem)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			log.Printf("subscribe stream error: %v, reconnecting in 5s...", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
		}
	}

	log.Println("waiting for active tasks to finish...")
	// Cancel all active tasks
	mu.Lock()
	for taskID, cancelFn := range activeTasks {
		log.Printf("cancelling task %s", taskID)
		cancelFn()
	}
	mu.Unlock()

	wg.Wait()
	log.Println("agent-manager stopped")
}

func runSubscribeLoop(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	taskClient taskguildv1connect.TaskServiceClient,
	interClient taskguildv1connect.InteractionServiceClient,
	cfg *config,
	mu *sync.Mutex,
	activeTasks map[string]context.CancelFunc,
	wg *sync.WaitGroup,
	sem chan struct{},
) error {
	stream, err := client.Subscribe(ctx, connect.NewRequest(&v1.AgentManagerSubscribeRequest{
		AgentManagerId:     cfg.AgentManagerID,
		MaxConcurrentTasks: int32(cfg.MaxConcurrentTasks),
		ProjectName:        cfg.ProjectName,
	}))
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer stream.Close()

	log.Println("subscribe stream connected")

	for stream.Receive() {
		cmd := stream.Msg()

		switch c := cmd.GetCommand().(type) {
		case *v1.AgentCommand_TaskAvailable:
			taskAvail := c.TaskAvailable
			taskID := taskAvail.GetTaskId()
			log.Printf("task available: %s (title: %s)", taskID, taskAvail.GetTitle())

			// Skip if this task is already running (prevents semaphore deadlock on re-assignment).
			mu.Lock()
			if prevCancel, ok := activeTasks[taskID]; ok {
				mu.Unlock()
				log.Printf("task %s already active, cancelling previous run and re-claiming", taskID)
				prevCancel()
			} else {
				mu.Unlock()
			}

			// Try to claim the task
			claimResp, err := client.ClaimTask(ctx, connect.NewRequest(&v1.ClaimTaskRequest{
				TaskId:         taskID,
				AgentManagerId: cfg.AgentManagerID,
			}))
			if err != nil {
				log.Printf("failed to claim task %s: %v", taskID, err)
				continue
			}
			if !claimResp.Msg.GetSuccess() {
				log.Printf("task %s already claimed by another agent", taskID)
				continue
			}

			log.Printf("claimed task %s", taskID)
			instructions := claimResp.Msg.GetInstructions()
			metadata := claimResp.Msg.GetMetadata()

			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			taskCtx, taskCancel := context.WithCancel(ctx)
			mu.Lock()
			activeTasks[taskID] = taskCancel
			mu.Unlock()

			wg.Add(1)
			go func(tID string) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					mu.Lock()
					delete(activeTasks, tID)
					mu.Unlock()
					taskCancel()
				}()

				runTask(taskCtx, client, taskClient, interClient, cfg.AgentManagerID, tID, instructions, metadata, cfg.WorkDir)
			}(taskID)

		case *v1.AgentCommand_ListWorktrees:
			listCmd := c.ListWorktrees
			log.Printf("received list worktrees command (request_id: %s)", listCmd.GetRequestId())
			go handleListWorktrees(ctx, client, cfg, listCmd.GetRequestId())

		case *v1.AgentCommand_DeleteWorktree:
			deleteCmd := c.DeleteWorktree
			log.Printf("received delete worktree command (request_id: %s, name: %s, force: %v)",
				deleteCmd.GetRequestId(), deleteCmd.GetWorktreeName(), deleteCmd.GetForce())
			go handleDeleteWorktree(ctx, client, cfg, deleteCmd)

		case *v1.AgentCommand_GitPullMain:
			pullCmd := c.GitPullMain
			log.Printf("received git pull main command (request_id: %s)", pullCmd.GetRequestId())
			go handleGitPullMain(ctx, client, cfg, pullCmd.GetRequestId())

		case *v1.AgentCommand_SyncAgents:
			log.Println("received sync agents command, re-syncing...")
			syncAgents(ctx, client, cfg)

		case *v1.AgentCommand_SyncPermissions:
			log.Println("received sync permissions command, re-syncing...")
			syncPermissions(ctx, client, cfg)

		case *v1.AgentCommand_SyncScripts:
			log.Println("received sync scripts command, re-syncing...")
			syncScripts(ctx, client, cfg)

		case *v1.AgentCommand_ExecuteScript:
			execCmd := c.ExecuteScript
			log.Printf("received execute script command (request_id: %s, script_id: %s, filename: %s)",
				execCmd.GetRequestId(), execCmd.GetScriptId(), execCmd.GetFilename())
			go handleExecuteScript(ctx, client, cfg, execCmd)

		case *v1.AgentCommand_CancelTask:
			cancelCmd := c.CancelTask
			taskID := cancelCmd.GetTaskId()
			reason := cancelCmd.GetReason()
			log.Printf("cancel request for task %s: %s", taskID, reason)

			mu.Lock()
			if cancelFn, ok := activeTasks[taskID]; ok {
				cancelFn()
			}
			mu.Unlock()

		case *v1.AgentCommand_AssignTask:
			assignCmd := c.AssignTask
			taskID := assignCmd.GetTaskId()
			log.Printf("direct task assignment: %s", taskID)

			// Cancel previous run if the same task is re-assigned.
			mu.Lock()
			if prevCancel, ok := activeTasks[taskID]; ok {
				mu.Unlock()
				log.Printf("task %s already active, cancelling previous run for re-assignment", taskID)
				prevCancel()
			} else {
				mu.Unlock()
			}

			instructions := assignCmd.GetInstructions()
			metadata := assignCmd.GetMetadata()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return ctx.Err()
			}

			taskCtx, taskCancel := context.WithCancel(ctx)
			mu.Lock()
			activeTasks[taskID] = taskCancel
			mu.Unlock()

			wg.Add(1)
			go func(tID string) {
				defer wg.Done()
				defer func() { <-sem }()
				defer func() {
					mu.Lock()
					delete(activeTasks, tID)
					mu.Unlock()
					taskCancel()
				}()

				runTask(taskCtx, client, taskClient, interClient, cfg.AgentManagerID, tID, instructions, metadata, cfg.WorkDir)
			}(taskID)

		default:
			log.Printf("unknown command type: %T", cmd.GetCommand())
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
				log.Printf("heartbeat error: %v", err)
			}
		}
	}
}

// handleListWorktrees scans the .claude/worktrees/ directory and reports
// available worktrees to the backend.
func handleListWorktrees(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, requestID string) {
	worktreesDir := filepath.Join(cfg.WorkDir, ".claude", "worktrees")
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("no worktrees directory found at %s", worktreesDir)
		} else {
			log.Printf("failed to read worktrees directory: %v", err)
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
		var hasChanges bool
		var changedFiles []string
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = wtDir
		if out, err := statusCmd.Output(); err == nil {
			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				hasChanges = true
				// git status --porcelain format: "XY filename" (filename starts at position 3)
				if len(line) > 3 {
					changedFiles = append(changedFiles, line[3:])
				}
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
		log.Printf("failed to report worktree list: %v", err)
	} else {
		log.Printf("reported %d worktrees (request_id: %s)", len(worktrees), requestID)
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
			log.Printf("failed to report worktree delete result: %v", err)
		}
	}

	wtDir := filepath.Join(cfg.WorkDir, ".claude", "worktrees", worktreeName)

	// Verify worktree exists.
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		reportResult(false, fmt.Sprintf("worktree %q does not exist", worktreeName))
		return
	}

	// Check for uncommitted changes (unless force).
	if !force {
		statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
		statusCmd.Dir = wtDir
		if out, err := statusCmd.Output(); err == nil {
			if strings.TrimSpace(string(out)) != "" {
				reportResult(false, "worktree has uncommitted changes; use force delete")
				return
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

	// Remove the git worktree.
	var removeCmd *exec.Cmd
	if force {
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
		log.Printf("warning: failed to delete branch %s: %v: %s", branchName, err, strings.TrimSpace(string(out)))
		// Not fatal – worktree is already removed.
	}

	log.Printf("deleted worktree %s (branch: %s, force: %v)", worktreeName, branchName, force)
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
			log.Printf("failed to report git pull main result: %v", err)
		}
	}

	cmd := exec.CommandContext(ctx, "git", "pull", "origin", "main")
	cmd.Dir = cfg.WorkDir
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))

	if err != nil {
		log.Printf("git pull origin main failed: %v: %s", err, output)
		reportResult(false, output, fmt.Sprintf("git pull origin main failed: %v", err))
		return
	}

	log.Printf("git pull origin main succeeded: %s", output)
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
