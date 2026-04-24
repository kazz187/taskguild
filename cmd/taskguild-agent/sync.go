package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// syncAgents calls the SyncAgents RPC and writes .claude/agents/*.md files locally.
// By default, existing local files are preserved (not overwritten).
// forceOverwriteNames controls which agents are overwritten even if local files exist.
// If forceOverwriteNames is nil, no agents are force-overwritten.
// forceAll overrides all agents unconditionally (used by --override-agent-md).
func syncAgents(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, forceOverwriteNames map[string]bool, forceAll bool) {
	if cfg.ProjectName == "" {
		slog.Info("skipping agent sync: no project name configured")
		return
	}

	resp, err := client.SyncAgents(ctx, connect.NewRequest(&v1.SyncAgentsRequest{
		ProjectName: cfg.ProjectName,
	}))
	if err != nil {
		slog.Error("agent sync failed", "error", err)
		return
	}

	agents := resp.Msg.GetAgents()
	slog.Info("syncing agents from server", "count", len(agents))

	agentsDir := filepath.Join(cfg.WorkDir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		slog.Error("failed to create agents directory", "error", err)
		return
	}

	// serverFiles tracks filenames known to the server (regardless of whether we wrote them).
	serverFiles := make(map[string]bool)

	for _, ag := range agents {
		name := ag.GetName()

		// Skip agents with unsafe names.
		if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			slog.Warn("skipping agent with unsafe name", "name", name)
			continue
		}

		filename := name + ".md"
		filePath := filepath.Join(agentsDir, filename)
		serverFiles[filename] = true

		// Check if the file already exists.
		if _, err := os.Stat(filePath); err == nil {
			// File exists — only overwrite if forced.
			if forceAll {
				slog.Debug("force-overwriting all: overwriting existing agent", "filename", filename)
			} else if forceOverwriteNames != nil && forceOverwriteNames[name] {
				slog.Debug("force-overwriting existing agent", "filename", filename, "agent_name", name)
			} else {
				slog.Debug("agent file already exists, preserving local version", "filename", filename)
				continue
			}
		}

		content := buildAgentMDContent(ag)

		err := os.WriteFile(filePath, []byte(content), 0o644)
		if err != nil {
			slog.Error("failed to write agent file", "path", filePath, "error", err)
			continue
		}

		slog.Debug("synced agent", "filename", filename)
	}

	cleanupStaleAgentFiles(agentsDir, serverFiles)
}

// buildAgentMDContent generates markdown content with YAML frontmatter
// matching the Claude Code sub-agent .md file format.
func buildAgentMDContent(ag *v1.AgentDefinition) string {
	var sb strings.Builder

	sb.WriteString("---\n")

	if ag.GetName() != "" {
		fmt.Fprintf(&sb, "name: %s\n", ag.GetName())
	}

	if ag.GetDescription() != "" {
		writeYAMLStringField(&sb, "description", ag.GetDescription())
	}

	if len(ag.GetTools()) > 0 {
		fmt.Fprintf(&sb, "tools: %s\n", strings.Join(ag.GetTools(), ", "))
	}

	if len(ag.GetDisallowedTools()) > 0 {
		fmt.Fprintf(&sb, "disallowedTools: %s\n", strings.Join(ag.GetDisallowedTools(), ", "))
	}

	if ag.GetModel() != "" {
		fmt.Fprintf(&sb, "model: %s\n", ag.GetModel())
	}

	if ag.GetPermissionMode() != "" {
		fmt.Fprintf(&sb, "permissionMode: %s\n", ag.GetPermissionMode())
	}

	if len(ag.GetSkills()) > 0 {
		sb.WriteString("skills:\n")

		for _, skill := range ag.GetSkills() {
			fmt.Fprintf(&sb, "  - %s\n", skill)
		}
	}

	if ag.GetMemory() != "" {
		fmt.Fprintf(&sb, "memory: %s\n", ag.GetMemory())
	}

	sb.WriteString("---\n")

	if prompt := ag.GetPrompt(); prompt != "" {
		sb.WriteString("\n")
		sb.WriteString(prompt)
		sb.WriteString("\n")
	}

	return sb.String()
}

// writeYAMLStringField writes a YAML key-value pair to the builder.
// If the value contains newlines, it uses YAML block scalar (|) notation.
func writeYAMLStringField(sb *strings.Builder, key, value string) {
	if strings.Contains(value, "\n") {
		sb.WriteString(key + ": |\n")

		for line := range strings.SplitSeq(value, "\n") {
			if line == "" {
				sb.WriteString("\n")
			} else {
				fmt.Fprintf(sb, "  %s\n", line)
			}
		}
	} else {
		fmt.Fprintf(sb, "%s: %s\n", key, value)
	}
}

// cleanupStaleAgentFiles removes .md files from the agents directory
// that were not found on the server during the current sync.
func cleanupStaleAgentFiles(agentsDir string, serverFiles map[string]bool) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		if !serverFiles[entry.Name()] {
			filePath := filepath.Join(agentsDir, entry.Name())

			err := os.Remove(filePath)
			if err != nil {
				slog.Error("failed to remove stale agent file", "path", filePath, "error", err)
			} else {
				slog.Info("removed stale agent file", "filename", entry.Name())
			}
		}
	}
}
