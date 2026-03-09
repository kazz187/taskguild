package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/storage"
)

const tasksPrefix = "tasks"

func runMigrate() {
	env, err := config.LoadEnv()
	if err != nil {
		slog.Error("failed to load env", "error", err)
		os.Exit(1)
	}

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

	ctx := context.Background()

	paths, err := store.List(ctx, tasksPrefix)
	if err != nil {
		slog.Error("failed to list tasks", "error", err)
		os.Exit(1)
	}

	migrated := 0
	for _, p := range paths {
		data, err := store.Read(ctx, p)
		if err != nil {
			slog.Warn("failed to read task file", "path", p, "error", err)
			continue
		}

		var t task.Task
		if err := yaml.Unmarshal(data, &t); err != nil {
			slog.Warn("failed to unmarshal task", "path", p, "error", err)
			continue
		}

		oldVal, hasOld := t.Metadata["_session_id"]
		if !hasOld {
			continue
		}

		// Rename _session_id -> session_id
		t.Metadata["session_id"] = oldVal
		delete(t.Metadata, "_session_id")

		out, err := yaml.Marshal(&t)
		if err != nil {
			slog.Warn("failed to marshal task", "path", p, "error", err)
			continue
		}

		if err := store.Write(ctx, p, out); err != nil {
			slog.Warn("failed to write task file", "path", p, "error", err)
			continue
		}

		migrated++
		slog.Info("migrated task metadata", "task_id", t.ID, "session_id", oldVal)
	}

	fmt.Printf("Migration complete: %d/%d tasks migrated (_session_id -> session_id)\n", migrated, len(paths))
}
