package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// syncScripts calls the SyncScripts RPC and writes .taskguild/scripts/* files locally.
// It only writes new files (files that don't exist yet on the agent).
// Existing files are preserved to protect local modifications.
// When forceOverwriteIDs is non-empty, those specific scripts are overwritten
// regardless of whether the local file already exists.
func syncScripts(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, forceOverwriteIDs map[string]bool) {
	if cfg.ProjectName == "" {
		slog.Info("skipping script sync: no project name configured")
		return
	}

	resp, err := client.SyncScripts(ctx, connect.NewRequest(&v1.SyncScriptsRequest{
		ProjectName: cfg.ProjectName,
	}))
	if err != nil {
		slog.Error("script sync failed", "error", err)
		return
	}

	scripts := resp.Msg.GetScripts()
	slog.Info("syncing scripts from server", "count", len(scripts))

	scriptsDir := filepath.Join(cfg.WorkDir, ".taskguild", "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		slog.Error("failed to create scripts directory", "error", err)
		return
	}

	var written, skipped int
	for _, sc := range scripts {
		filename := sc.GetFilename()
		if filename == "" {
			filename = sc.GetName() + ".sh"
		}

		// Skip scripts with unsafe names.
		if strings.Contains(filename, "/") || strings.Contains(filename, "\\") || strings.Contains(filename, "..") {
			slog.Warn("skipping script with unsafe filename", "filename", filename)
			continue
		}

		filePath := filepath.Join(scriptsDir, filename)

		// Check if the file already exists.
		if _, err := os.Stat(filePath); err == nil {
			// File exists — only overwrite if this script ID is in the force list.
			if forceOverwriteIDs != nil && forceOverwriteIDs[sc.GetId()] {
				slog.Debug("force-overwriting existing script", "filename", filename, "script_id", sc.GetId())
			} else {
				slog.Debug("script file already exists, preserving local version", "filename", filename)
				skipped++
				continue
			}
		}

		if err := os.WriteFile(filePath, []byte(sc.GetContent()), 0755); err != nil {
			slog.Error("failed to write script file", "path", filePath, "error", err)
			continue
		}
		slog.Debug("synced script", "filename", filename)
		written++
	}

	slog.Info("script sync complete", "written", written, "skipped_existing", skipped)
}
