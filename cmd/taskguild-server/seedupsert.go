package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/kazz187/taskguild/internal/config"
	"github.com/kazz187/taskguild/internal/project"
	skillrepo "github.com/kazz187/taskguild/internal/skill/repositoryimpl"
	workflowrepo "github.com/kazz187/taskguild/internal/workflow/repositoryimpl"
	"github.com/kazz187/taskguild/pkg/clog"
	"github.com/kazz187/taskguild/pkg/storage"
)

// runSeedUpsert wires up minimal dependencies and calls Seeder.UpsertSkills
// for the given project ID. It is intended to be run from a shell while the
// main server is stopped (or immediately before a restart), so that the
// running server picks up any newly created skill files on its next index
// build.
func runSeedUpsert(projectID string) {
	env, err := config.LoadEnv()
	if err != nil {
		slog.Error("failed to load env", "error", err)
		os.Exit(1)
	}

	handler := clog.NewConnectTextHandler(os.Stderr, clog.WithLevel(env.SlogLevel()))
	slog.SetDefault(slog.New(clog.NewAttributesHandler(handler)))

	var store storage.Storage

	switch env.Type {
	case "s3":
		store, err = storage.NewS3Storage(context.Background(), env.S3Bucket, env.S3Prefix, env.S3Region)
		if err != nil {
			slog.Error("failed to create S3 storage", "error", err)
			os.Exit(1)
		}
	default:
		store, err = storage.NewLocalStorage(env.BaseDir)
		if err != nil {
			slog.Error("failed to create local storage", "error", err)
			os.Exit(1)
		}
	}

	workflowRepo := workflowrepo.NewYAMLRepository(store)
	skillRepo := skillrepo.NewYAMLRepository(store)
	seeder := project.NewSeeder(workflowRepo, skillRepo)

	ctx := context.Background()
	if err := seeder.UpsertSkills(ctx, projectID); err != nil {
		slog.Error("failed to upsert skills", "project_id", projectID, "error", err)
		os.Exit(1)
	}

	slog.Info("skills upserted", "project_id", projectID)
}
