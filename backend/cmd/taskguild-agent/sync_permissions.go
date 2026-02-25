package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// syncPermissions reads local .claude/settings.json permissions, merges them
// with the backend's stored permissions via the SyncPermissions RPC, and writes
// the merged result back to .claude/settings.json.
func syncPermissions(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config) {
	if cfg.ProjectName == "" {
		log.Println("skipping permission sync: no project name configured")
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
		log.Printf("permission sync failed: %v", err)
		return
	}

	merged := resp.Msg.GetPermissions()
	log.Printf("permission sync: allow=%d, ask=%d, deny=%d",
		len(merged.GetAllow()), len(merged.GetAsk()), len(merged.GetDeny()))

	// Write merged permissions back to settings.json.
	writeLocalPermissions(settingsPath, rawSettings, merged)
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
		log.Printf("failed to parse settings.json: %v", err)
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
		log.Printf("failed to create .claude directory: %v", err)
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
		log.Printf("failed to marshal settings.json: %v", err)
		return
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("failed to write settings.json: %v", err)
		return
	}
	log.Printf("updated %s with merged permissions", path)
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
