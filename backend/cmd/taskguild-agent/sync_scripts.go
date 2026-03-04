package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// syncScripts calls the SyncScripts RPC and writes .claude/scripts/* files locally.
// It also removes stale script files that no longer exist on the server.
func syncScripts(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config) {
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

	scriptsDir := filepath.Join(cfg.WorkDir, ".claude", "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		slog.Error("failed to create scripts directory", "error", err)
		return
	}

	writtenFiles := make(map[string]bool)
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

		if err := os.WriteFile(filePath, []byte(sc.GetContent()), 0755); err != nil {
			slog.Error("failed to write script file", "path", filePath, "error", err)
			continue
		}
		slog.Debug("synced script", "filename", filename)
		writtenFiles[filename] = true
	}

	cleanupStaleScriptFiles(scriptsDir, writtenFiles)
}

// cleanupStaleScriptFiles removes script files from the scripts directory
// that were not written during the current sync.
func cleanupStaleScriptFiles(scriptsDir string, writtenFiles map[string]bool) {
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !writtenFiles[entry.Name()] {
			filePath := filepath.Join(scriptsDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				slog.Error("failed to remove stale script file", "path", filePath, "error", err)
			} else {
				slog.Debug("removed stale script", "filename", entry.Name())
			}
		}
	}
}
