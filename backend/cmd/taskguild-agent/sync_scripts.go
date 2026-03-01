package main

import (
	"context"
	"log"
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
		log.Println("skipping script sync: no project name configured")
		return
	}

	resp, err := client.SyncScripts(ctx, connect.NewRequest(&v1.SyncScriptsRequest{
		ProjectName: cfg.ProjectName,
	}))
	if err != nil {
		log.Printf("script sync failed: %v", err)
		return
	}

	scripts := resp.Msg.GetScripts()
	log.Printf("syncing %d script(s) from server", len(scripts))

	scriptsDir := filepath.Join(cfg.WorkDir, ".claude", "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		log.Printf("failed to create scripts directory: %v", err)
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
			log.Printf("skipping script with unsafe filename: %q", filename)
			continue
		}

		filePath := filepath.Join(scriptsDir, filename)

		if err := os.WriteFile(filePath, []byte(sc.GetContent()), 0755); err != nil {
			log.Printf("failed to write script file %s: %v", filePath, err)
			continue
		}
		log.Printf("synced script: %s", filename)
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
				log.Printf("failed to remove stale script file %s: %v", filePath, err)
			} else {
				log.Printf("removed stale script: %s", entry.Name())
			}
		}
	}
}
