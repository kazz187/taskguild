package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kazz187/taskguild/backend/internal/agent"
	agentrepo "github.com/kazz187/taskguild/backend/internal/agent/repositoryimpl"
	"github.com/kazz187/taskguild/backend/internal/agentmanager"
	"github.com/kazz187/taskguild/backend/internal/config"
	"github.com/kazz187/taskguild/backend/internal/event"
	"github.com/kazz187/taskguild/backend/internal/eventbus"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	interactionrepo "github.com/kazz187/taskguild/backend/internal/interaction/repositoryimpl"
	"github.com/kazz187/taskguild/backend/internal/orchestrator"
	"github.com/kazz187/taskguild/backend/internal/project"
	projectrepo "github.com/kazz187/taskguild/backend/internal/project/repositoryimpl"
	"github.com/kazz187/taskguild/backend/internal/task"
	taskrepo "github.com/kazz187/taskguild/backend/internal/task/repositoryimpl"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	workflowrepo "github.com/kazz187/taskguild/backend/internal/workflow/repositoryimpl"
	"github.com/kazz187/taskguild/backend/pkg/clog"
	"github.com/kazz187/taskguild/backend/pkg/storage"

	server "github.com/kazz187/taskguild/backend/internal"
)

func main() {
	env, err := config.LoadEnv()
	if err != nil {
		slog.Error("failed to load env", "error", err)
		os.Exit(1)
	}

	// Setup logger
	level := env.SlogLevel()
	var handler slog.Handler
	if env.Env == "local" {
		handler = clog.NewConnectTextHandler(os.Stderr, clog.WithLevel(level))
	} else {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	slog.SetDefault(slog.New(clog.NewAttributesHandler(handler)))

	// Setup storage
	var store storage.Storage
	switch env.StorageEnv.Type {
	case "s3":
		store, err = storage.NewS3Storage(context.Background(), env.StorageEnv.S3Bucket, env.StorageEnv.S3Prefix, env.StorageEnv.S3Region)
		if err != nil {
			slog.Error("failed to create S3 storage", "error", err)
			os.Exit(1)
		}
	default:
		store, err = storage.NewLocalStorage(env.StorageEnv.BaseDir)
		if err != nil {
			slog.Error("failed to create local storage", "error", err)
			os.Exit(1)
		}
	}

	// Setup event bus
	bus := eventbus.New()

	// Setup repositories
	projectRepo := projectrepo.NewYAMLRepository(store)
	workflowRepo := workflowrepo.NewYAMLRepository(store)
	taskRepo := taskrepo.NewYAMLRepository(store)
	interactionRepo := interactionrepo.NewYAMLRepository(store)
	agentRepo := agentrepo.NewYAMLRepository(store)

	// Setup agent-manager registry
	agentManagerRegistry := agentmanager.NewRegistry()

	// Setup servers
	projectServer := project.NewServer(projectRepo)
	workflowServer := workflow.NewServer(workflowRepo)
	taskServer := task.NewServer(taskRepo, workflowRepo, bus)
	interactionServer := interaction.NewServer(interactionRepo, taskRepo, bus)
	agentManagerServer := agentmanager.NewServer(agentManagerRegistry, taskRepo, workflowRepo, agentRepo, interactionRepo, bus)
	agentServer := agent.NewServer(agentRepo)
	eventServer := event.NewServer(bus)

	srv := server.NewServer(
		env,
		projectServer,
		workflowServer,
		taskServer,
		interactionServer,
		agentManagerServer,
		agentServer,
		eventServer,
	)

	// Setup orchestrator
	orch := orchestrator.New(bus, taskRepo, workflowRepo, agentManagerRegistry)

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	go orch.Start(ctx)

	go func() {
		if err := srv.ListenAndServe(ctx); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")

	// Give active connections time to finish after stream contexts are cancelled.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
