package daemon

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/server"
	"github.com/kazz187/taskguild/internal/task"
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

// Start starts the daemon
func (d *Daemon) Start(ctx context.Context) error {
	// Start event bus
	if err := d.eventBus.Start(ctx); err != nil {
		return fmt.Errorf("failed to start event bus: %w", err)
	}

	// Start agent manager
	if err := d.agentManager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent manager: %w", err)
	}

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
		w.Write([]byte("OK"))
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

	// Start HTTP server in a goroutine
	go func() {
		if err := d.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the daemon
func (d *Daemon) Stop(ctx context.Context) error {
	// Stop HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown HTTP server: %w", err)
		}
	}

	// Stop agent manager
	if err := d.agentManager.Stop(); err != nil {
		return fmt.Errorf("failed to stop agent manager: %w", err)
	}

	// Stop event bus
	if err := d.eventBus.Stop(); err != nil {
		return fmt.Errorf("failed to stop event bus: %w", err)
	}

	return nil
}

// WaitForShutdown waits for shutdown signal
func (d *Daemon) WaitForShutdown(ctx context.Context) error {
	<-ctx.Done()
	return d.Stop(context.Background())
}
