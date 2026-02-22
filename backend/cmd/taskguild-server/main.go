package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/kazz187/taskguild/backend/internal/agentmanager"
	"github.com/kazz187/taskguild/backend/internal/event"
	"github.com/kazz187/taskguild/backend/internal/interaction"
	"github.com/kazz187/taskguild/backend/internal/project"
	"github.com/kazz187/taskguild/backend/internal/project/repositoryimpl"
	"github.com/kazz187/taskguild/backend/internal/task"
	"github.com/kazz187/taskguild/backend/internal/workflow"
	"github.com/kazz187/taskguild/backend/internal/config"
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

	// Setup repositories
	projectRepo := repositoryimpl.NewYAMLRepository(store)

	// Setup servers
	projectServer := project.NewServer(projectRepo)
	workflowServer := workflow.NewServer()
	taskServer := task.NewServer()
	interactionServer := interaction.NewServer()
	agentManagerServer := agentmanager.NewServer()
	eventServer := event.NewServer()

	srv := server.NewServer(
		env,
		projectServer,
		workflowServer,
		taskServer,
		interactionServer,
		agentManagerServer,
		eventServer,
	)

	// Graceful shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")
	if err := srv.Shutdown(context.Background()); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
