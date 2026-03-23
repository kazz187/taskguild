package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/migration"
	"github.com/kazz187/taskguild/pkg/storage"
)

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

	migrator := migration.NewV1ToV2Migrator(store, *migrateDryRun)
	result, err := migrator.Run(ctx)
	if err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Migration complete: %d migrated, %d skipped, %d errors\n",
		result.FilesMigrated, result.FilesSkipped, result.Errors)
}
