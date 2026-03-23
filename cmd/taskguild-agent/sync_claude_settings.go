package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// syncClaudeSettings reads local .claude/settings.json settings (language, etc.),
// merges them with the backend's stored settings via the SyncClaudeSettings RPC,
// and writes the merged result back to .claude/settings.json.
func syncClaudeSettings(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config) {
	if cfg.ProjectName == "" {
		slog.Info("skipping claude settings sync: no project name configured")
		return
	}

	settingsPath := filepath.Join(cfg.WorkDir, ".claude", "settings.json")

	// Read existing settings.json (may not exist).
	localLanguage, rawSettings := readLocalClaudeSettings(settingsPath)

	// Call SyncClaudeSettings RPC.
	resp, err := client.SyncClaudeSettings(ctx, connect.NewRequest(&v1.SyncClaudeSettingsAgentRequest{
		ProjectName:   cfg.ProjectName,
		LocalLanguage: localLanguage,
	}))
	if err != nil {
		slog.Error("claude settings sync failed", "error", err)
		return
	}

	merged := resp.Msg.GetSettings()
	slog.Info("claude settings sync complete",
		"language", merged.GetLanguage(),
	)

	// Write merged settings back to settings.json.
	writeLocalClaudeSettings(settingsPath, rawSettings, merged)
}

// readLocalClaudeSettings reads the settings fields from a .claude/settings.json file.
// Returns empty values and an empty map if the file doesn't exist or has no settings.
func readLocalClaudeSettings(path string) (language string, raw map[string]interface{}) {
	raw = make(map[string]interface{})

	data, err := os.ReadFile(path)
	if err != nil {
		return "", raw
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("failed to parse settings.json for claude settings", "error", err)
		return "", raw
	}

	language, _ = raw["language"].(string)
	return language, raw
}

// writeLocalClaudeSettings writes the merged settings back to settings.json,
// preserving all other sections (permissions, env, hooks, etc.).
func writeLocalClaudeSettings(path string, raw map[string]interface{}, merged *v1.ClaudeSettings) {
	if raw == nil {
		raw = make(map[string]interface{})
	}

	// Ensure .claude directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create .claude directory", "error", err)
		return
	}

	// Update only the settings fields (language).
	if merged.GetLanguage() != "" {
		raw["language"] = merged.GetLanguage()
	}

	data, err := json.MarshalIndent(raw, "", "    ")
	if err != nil {
		slog.Error("failed to marshal settings.json", "error", err)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Error("failed to write settings.json", "error", err)
		return
	}
	slog.Info("updated settings.json with merged claude settings", "path", path)
}
