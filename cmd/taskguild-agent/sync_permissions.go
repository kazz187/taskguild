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

// syncPermissions reads local .claude/settings.json permissions, merges them
// with the backend's stored permissions via the SyncPermissions RPC, writes
// the merged result back to .claude/settings.json, and updates the in-memory
// permission cache so that subsequent tool calls can be auto-allowed.
func syncPermissions(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cache *permissionCache) {
	if cfg.ProjectName == "" {
		slog.Info("skipping permission sync: no project name configured")
		return
	}

	settingsPath := filepath.Join(cfg.WorkDir, ".claude", "settings.json")

	// Read existing settings.json (may not exist).
	localAllow, localAsk, localDeny, rawSettings := readLocalPermissions(settingsPath)

	// Call SyncPermissions RPC.
	resp, err := client.SyncPermissions(ctx, connect.NewRequest(&v1.SyncPermissionsRequest{
		ProjectName: cfg.ProjectName,
		LocalAllow:  localAllow,
		LocalAsk:    localAsk,
		LocalDeny:   localDeny,
	}))
	if err != nil {
		slog.Error("permission sync failed", "error", err)
		return
	}

	merged := resp.Msg.GetPermissions()
	slog.Info("permission sync complete",
		"allow", len(merged.GetAllow()),
		"ask", len(merged.GetAsk()),
		"deny", len(merged.GetDeny()),
	)

	// Write merged permissions back to settings.json.
	writeLocalPermissions(settingsPath, rawSettings, merged)

	// Update the in-memory permission cache with the merged allow rules.
	if cache != nil {
		cache.Update(merged.GetAllow())
	}
}

// readLocalPermissions reads the permissions section from a .claude/settings.json file.
// Returns empty slices and an empty map if the file doesn't exist or has no permissions.
func readLocalPermissions(path string) (allow, ask, deny []string, raw map[string]interface{}) {
	raw = make(map[string]interface{})

	data, err := os.ReadFile(path)
	if err != nil {
		// File doesn't exist or unreadable -- return empty.
		return nil, nil, nil, raw
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Warn("failed to parse settings.json", "error", err)
		return nil, nil, nil, raw
	}

	permsRaw, ok := raw["permissions"]
	if !ok {
		return nil, nil, nil, raw
	}
	permsMap, ok := permsRaw.(map[string]interface{})
	if !ok {
		return nil, nil, nil, raw
	}

	allow = toStringSlice(permsMap["allow"])
	ask = toStringSlice(permsMap["ask"])
	deny = toStringSlice(permsMap["deny"])
	return allow, ask, deny, raw
}

// writeLocalPermissions writes the merged permissions back to settings.json,
// preserving all other sections (env, hooks, etc.).
func writeLocalPermissions(path string, raw map[string]interface{}, merged *v1.PermissionSet) {
	if raw == nil {
		raw = make(map[string]interface{})
	}

	// Ensure .claude directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("failed to create .claude directory", "error", err)
		return
	}

	// Build the permissions section, using empty arrays instead of nil
	// to produce clean JSON output.
	allowList := merged.GetAllow()
	if allowList == nil {
		allowList = []string{}
	}
	askList := merged.GetAsk()
	if askList == nil {
		askList = []string{}
	}
	denyList := merged.GetDeny()
	if denyList == nil {
		denyList = []string{}
	}

	// Update only the permissions section.
	raw["permissions"] = map[string]interface{}{
		"allow": allowList,
		"ask":   askList,
		"deny":  denyList,
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
	slog.Info("updated settings.json with merged permissions", "path", path)
}

// toStringSlice converts an interface{} (expected to be []interface{}) to []string.
func toStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
