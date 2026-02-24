package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/oklog/ulid/v2"
)

type config struct {
	ServerURL          string
	APIKey             string
	AgentManagerID     string
	MaxConcurrentTasks int
	WorkDir            string
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

	return cfg, nil
}

func main() {
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

	log.Printf("agent-manager starting (id: %s, server: %s, max_tasks: %d, work_dir: %s)",
		cfg.AgentManagerID, cfg.ServerURL, cfg.MaxConcurrentTasks, cfg.WorkDir)

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
