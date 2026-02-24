package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// syncAgents calls the SyncAgents RPC and writes .claude/agents/*.md files locally.
// It also removes stale .md files that no longer exist on the server.
func syncAgents(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config) {
	if cfg.ProjectName == "" {
		log.Println("skipping agent sync: no project name configured")
		return
	}

	resp, err := client.SyncAgents(ctx, connect.NewRequest(&v1.SyncAgentsRequest{
		ProjectName: cfg.ProjectName,
	}))
	if err != nil {
		log.Printf("agent sync failed: %v", err)
		return
	}

	agents := resp.Msg.GetAgents()
	log.Printf("syncing %d agent(s) from server", len(agents))

	agentsDir := filepath.Join(cfg.WorkDir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		log.Printf("failed to create agents directory: %v", err)
		return
	}

	writtenFiles := make(map[string]bool)
	for _, ag := range agents {
		name := ag.GetName()

		// Skip agents with unsafe names.
		if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			log.Printf("skipping agent with unsafe name: %q", name)
			continue
		}

		filename := name + ".md"
		filePath := filepath.Join(agentsDir, filename)
		content := buildAgentMDContent(ag)

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			log.Printf("failed to write agent file %s: %v", filePath, err)
			continue
		}
		log.Printf("synced agent: %s", filename)
		writtenFiles[filename] = true
	}

	cleanupStaleAgentFiles(agentsDir, writtenFiles)
}

// buildAgentMDContent generates markdown content with YAML frontmatter
// matching the Claude Code sub-agent .md file format.
func buildAgentMDContent(ag *v1.AgentDefinition) string {
	var sb strings.Builder

	sb.WriteString("---\n")

	if ag.GetName() != "" {
		sb.WriteString(fmt.Sprintf("name: %s\n", ag.GetName()))
	}
	if ag.GetDescription() != "" {
		sb.WriteString(fmt.Sprintf("description: %s\n", ag.GetDescription()))
	}
	if len(ag.GetTools()) > 0 {
		sb.WriteString(fmt.Sprintf("tools: %s\n", strings.Join(ag.GetTools(), ", ")))
	}
	if len(ag.GetDisallowedTools()) > 0 {
		sb.WriteString(fmt.Sprintf("disallowedTools: %s\n", strings.Join(ag.GetDisallowedTools(), ", ")))
	}
	if ag.GetModel() != "" {
		sb.WriteString(fmt.Sprintf("model: %s\n", ag.GetModel()))
	}
	if ag.GetPermissionMode() != "" {
		sb.WriteString(fmt.Sprintf("permissionMode: %s\n", ag.GetPermissionMode()))
	}
	if len(ag.GetSkills()) > 0 {
		sb.WriteString("skills:\n")
		for _, skill := range ag.GetSkills() {
			sb.WriteString(fmt.Sprintf("  - %s\n", skill))
		}
	}
	if ag.GetMemory() != "" {
		sb.WriteString(fmt.Sprintf("memory: %s\n", ag.GetMemory()))
	}

	sb.WriteString("---\n")

	if prompt := ag.GetPrompt(); prompt != "" {
		sb.WriteString("\n")
		sb.WriteString(prompt)
		sb.WriteString("\n")
	}

	return sb.String()
}

// cleanupStaleAgentFiles removes .md files from the agents directory
// that were not written during the current sync.
func cleanupStaleAgentFiles(agentsDir string, writtenFiles map[string]bool) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if !writtenFiles[entry.Name()] {
			filePath := filepath.Join(agentsDir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				log.Printf("failed to remove stale agent file %s: %v", filePath, err)
			} else {
				log.Printf("removed stale agent: %s", entry.Name())
			}
		}
	}
}
