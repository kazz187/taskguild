package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sourcegraph/conc/pool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/server"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/panicerr"
)

// Daemon represents the TaskGuild daemon process
type Daemon struct {
	config       *Config
	httpServer   *http.Server
	taskService  task.Service
	agentManager *agent.Manager
	eventBus     *event.EventBus
}

// Config holds daemon configuration
type Config struct {
	Address string `yaml:"address"`
	Port    int    `yaml:"port"`
}

// DefaultConfig returns default daemon configuration
func DefaultConfig() *Config {
	return &Config{
		Address: "localhost",
		Port:    8080,
	}
}

// New creates a new daemon instance
func New(config *Config) (*Daemon, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Initialize event bus
	eventBus := event.NewEventBus()

	// Initialize task service
	taskRepo := task.NewYAMLRepository(".taskguild/task.yaml")

	taskService := task.NewService(taskRepo, eventBus)

	// Initialize agent manager
	agentConfig, err := agent.LoadConfig("")
	if err != nil {
		return nil, fmt.Errorf("failed to load agent config: %w", err)
	}

	agentManager := agent.NewManager(agentConfig, eventBus)

	return &Daemon{
		config:       config,
		taskService:  taskService,
		agentManager: agentManager,
		eventBus:     eventBus,
	}, nil
}

// Start starts the daemon with parallel execution using pool and panic protection
func (d *Daemon) Start(ctx context.Context) error {
	// Create server handlers
	taskHandler := server.NewTaskServiceHandler(d.taskService)
	agentHandler := server.NewAgentServiceHandler(d.agentManager)
	eventHandler := server.NewEventServiceHandler(d.eventBus)

	// Setup HTTP mux
	mux := http.NewServeMux()

	// Add Connect handlers
	path, handler := taskHandler.PathAndHandler()
	mux.Handle(path, handler)

	path, handler = agentHandler.PathAndHandler()
	mux.Handle(path, handler)

	path, handler = eventHandler.PathAndHandler()
	mux.Handle(path, handler)

	// Add health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Create HTTP server with h2c support for gRPC-Web compatibility
	addr := fmt.Sprintf("%s:%d", d.config.Address, d.config.Port)
	d.httpServer = &http.Server{
		Addr:         addr,
		Handler:      h2c.NewHandler(mux, &http2.Server{}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	fmt.Printf("TaskGuild daemon starting on %s\n", addr)

	// Use pool for parallel execution with error cancellation and panic protection
	p := pool.New().WithContext(ctx).WithCancelOnError()

	// Start eventBus wrapped with SafeContext for panic protection
	p.Go(panicerr.SafeContext(d.eventBus.Start))

	// Start agentManager wrapped with SafeContext for panic protection
	p.Go(panicerr.SafeContext(d.agentManager.Start))

	// Start HTTP server wrapped with SafeContext for panic protection
	p.Go(panicerr.SafeContext(func(ctx context.Context) error {
		// Start server in a goroutine
		errCh := make(chan error, 1)
		go func() {
			if err := d.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("HTTP server error: %w", err)
			} else {
				errCh <- nil
			}
		}()

		// Wait for context cancellation or server error
		select {
		case <-ctx.Done():
			// Context cancelled, shutdown server gracefully
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			return d.httpServer.Shutdown(shutdownCtx)
		case err := <-errCh:
			return err
		}
	}))

	// Wait for all services to complete or for context cancellation
	return p.Wait()
}
