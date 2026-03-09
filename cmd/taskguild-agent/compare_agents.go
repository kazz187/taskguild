package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/gen/proto/taskguild/v1/taskguildv1connect"
)

// handleCompareAgents compares local .claude/agents/*.md files with server-side
// agent content and reports differences back to the server.
func handleCompareAgents(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.CompareAgentsCommand) {
	requestID := cmd.GetRequestId()
	serverAgents := cmd.GetAgents()

	slog.Info("comparing agents with server", "request_id", requestID, "server_count", len(serverAgents))

	agentsDir := filepath.Join(cfg.WorkDir, ".claude", "agents")

	// Read all local agent files.
	localAgents := readLocalAgents(agentsDir)

	// Build a map of server agents by name for fast lookup.
	serverByName := make(map[string]*v1.AgentDefinition, len(serverAgents))
	for _, ag := range serverAgents {
		serverByName[ag.GetName()] = ag
	}

	var diffs []*v1.AgentDiff

	// Check each local file against server versions.
	for filename, localContent := range localAgents {
		agentName := strings.TrimSuffix(filename, ".md")
		if server, ok := serverByName[agentName]; ok {
			// Both exist — compare content.
			serverContent := buildAgentMDContent(server)
			if localContent != serverContent {
				diffs = append(diffs, &v1.AgentDiff{
					AgentId:       server.GetId(),
					AgentName:     agentName,
					Filename:      filename,
					ServerContent: serverContent,
					AgentContent:  localContent,
					DiffType:      v1.AgentDiffType_AGENT_DIFF_TYPE_MODIFIED,
				})
			}
			// Remove from server map; remaining entries are server-only.
			delete(serverByName, agentName)
		} else {
			// Agent-only: exists locally but not on server.
			diffs = append(diffs, &v1.AgentDiff{
				AgentName:    agentName,
				Filename:     filename,
				AgentContent: localContent,
				DiffType:     v1.AgentDiffType_AGENT_DIFF_TYPE_AGENT_ONLY,
			})
		}
	}

	// Remaining server agents not found locally.
	for agentName, server := range serverByName {
		serverContent := buildAgentMDContent(server)
		diffs = append(diffs, &v1.AgentDiff{
			AgentId:       server.GetId(),
			AgentName:     agentName,
			Filename:      agentName + ".md",
			ServerContent: serverContent,
			DiffType:      v1.AgentDiffType_AGENT_DIFF_TYPE_SERVER_ONLY,
		})
	}

	slog.Info("agent comparison complete", "request_id", requestID, "total_diffs", len(diffs))

	// Report diffs to server.
	_, err := client.ReportAgentComparison(ctx, connect.NewRequest(&v1.ReportAgentComparisonRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		Diffs:       diffs,
	}))
	if err != nil {
		slog.Error("failed to report agent comparison", "request_id", requestID, "error", err)
	}
}

// readLocalAgents reads all .md files from the agents directory
// and returns a map of filename → content.
func readLocalAgents(agentsDir string) map[string]string {
	result := make(map[string]string)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("failed to read agents directory", "error", err)
		}
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(agentsDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			slog.Error("failed to read local agent file", "path", filePath, "error", err)
			continue
		}

		result[entry.Name()] = string(content)
	}

	return result
}
