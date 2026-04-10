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
	"github.com/kazz187/taskguild/internal/script"
)

// handleCompareScripts compares local .taskguild/scripts/* files with server-side
// script content and reports differences back to the server.
func handleCompareScripts(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.CompareScriptsCommand) {
	requestID := cmd.GetRequestId()
	serverScripts := cmd.GetScripts()

	slog.Info("comparing scripts with server", "request_id", requestID, "server_count", len(serverScripts))

	scriptsDir := filepath.Join(cfg.WorkDir, ".taskguild", "scripts")

	// Read all local script files.
	localScripts := readLocalScripts(scriptsDir)

	// Build a map of server scripts by filename for fast lookup.
	serverByFilename := make(map[string]*v1.ScriptDefinition, len(serverScripts))
	for _, sc := range serverScripts {
		filename := sc.GetFilename()
		if filename == "" {
			filename = sc.GetName() + ".sh"
		}
		serverByFilename[filename] = sc
	}

	var diffs []*v1.ScriptDiff

	// Check each local file against server versions.
	for filename, localContent := range localScripts {
		if server, ok := serverByFilename[filename]; ok {
			// Both exist — compare content.
			if localContent != server.GetContent() {
				diffs = append(diffs, &v1.ScriptDiff{
					ScriptId:      server.GetId(),
					ScriptName:    server.GetName(),
					Filename:      filename,
					ServerContent: server.GetContent(),
					AgentContent:  localContent,
					DiffType:      v1.ScriptDiffType_SCRIPT_DIFF_TYPE_MODIFIED,
				})
			}
			// Remove from server map; remaining entries are server-only.
			delete(serverByFilename, filename)
		} else {
			// Agent-only script: exists locally but not on server.
			name := strings.TrimSuffix(filename, filepath.Ext(filename))
			diffs = append(diffs, &v1.ScriptDiff{
				ScriptName:   name,
				Filename:     filename,
				AgentContent: localContent,
				DiffType:     v1.ScriptDiffType_SCRIPT_DIFF_TYPE_AGENT_ONLY,
			})
		}
	}

	// Remaining server scripts not found locally.
	for filename, server := range serverByFilename {
		diffs = append(diffs, &v1.ScriptDiff{
			ScriptId:      server.GetId(),
			ScriptName:    server.GetName(),
			Filename:      filename,
			ServerContent: server.GetContent(),
			DiffType:      v1.ScriptDiffType_SCRIPT_DIFF_TYPE_SERVER_ONLY,
		})
	}

	slog.Info("script comparison complete", "request_id", requestID, "total_diffs", len(diffs))

	// Report diffs to server.
	_, err := client.ReportScriptComparison(ctx, connect.NewRequest(&v1.ReportScriptComparisonRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		Diffs:       diffs,
	}))
	if err != nil {
		slog.Error("failed to report script comparison", "request_id", requestID, "error", err)
	}
}

// readLocalScripts reads all non-directory files from the scripts directory
// and returns a map of filename → content.
func readLocalScripts(scriptsDir string) map[string]string {
	result := make(map[string]string)

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("failed to read scripts directory", "error", err)
		}
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		// Skip editor swap files, backups, and other temporary files.
		if script.ShouldSkipScriptFile(filename) {
			continue
		}

		filePath := filepath.Join(scriptsDir, filename)
		content, err := os.ReadFile(filePath)
		if err != nil {
			slog.Error("failed to read local script file", "path", filePath, "error", err)
			continue
		}

		result[filename] = string(content)
	}

	return result
}
