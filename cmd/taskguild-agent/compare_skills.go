package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"connectrpc.com/connect"

	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// handleCompareSkills compares local .claude/skills/*/SKILL.md files with server-side
// skill content and reports differences back to the server.
func handleCompareSkills(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.CompareSkillsCommand) {
	requestID := cmd.GetRequestId()
	serverSkills := cmd.GetSkills()

	slog.Info("comparing skills with server", "request_id", requestID, "server_count", len(serverSkills))

	skillsDir := filepath.Join(cfg.WorkDir, ".claude", "skills")

	// Read all local skill files.
	localSkills := readLocalSkills(skillsDir)

	// Build a map of server skills by name for fast lookup.
	serverByName := make(map[string]*v1.SkillDefinition, len(serverSkills))
	for _, sk := range serverSkills {
		serverByName[sk.GetName()] = sk
	}

	var diffs []*v1.SkillDiff

	// Check each local skill against server versions.
	for name, localContent := range localSkills {
		filename := filepath.Join(name, "SKILL.md")
		if server, ok := serverByName[name]; ok {
			// Both exist — compare content.
			serverContent := buildSkillMDContent(server)
			if localContent != serverContent {
				diffs = append(diffs, &v1.SkillDiff{
					SkillId:       server.GetId(),
					SkillName:     name,
					Filename:      filename,
					ServerContent: serverContent,
					AgentContent:  localContent,
					DiffType:      v1.SkillDiffType_SKILL_DIFF_TYPE_MODIFIED,
				})
			}
			// Remove from server map; remaining entries are server-only.
			delete(serverByName, name)
		} else {
			// Agent-only skill: exists locally but not on server.
			diffs = append(diffs, &v1.SkillDiff{
				SkillName:    name,
				Filename:     filename,
				AgentContent: localContent,
				DiffType:     v1.SkillDiffType_SKILL_DIFF_TYPE_AGENT_ONLY,
			})
		}
	}

	// Remaining server skills not found locally.
	for name, server := range serverByName {
		serverContent := buildSkillMDContent(server)
		diffs = append(diffs, &v1.SkillDiff{
			SkillId:       server.GetId(),
			SkillName:     name,
			Filename:      filepath.Join(name, "SKILL.md"),
			ServerContent: serverContent,
			DiffType:      v1.SkillDiffType_SKILL_DIFF_TYPE_SERVER_ONLY,
		})
	}

	slog.Info("skill comparison complete", "request_id", requestID, "total_diffs", len(diffs))

	// Report diffs to server.
	_, err := client.ReportSkillComparison(ctx, connect.NewRequest(&v1.ReportSkillComparisonRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		Diffs:       diffs,
	}))
	if err != nil {
		slog.Error("failed to report skill comparison", "request_id", requestID, "error", err)
	}
}

// readLocalSkills reads all .claude/skills/*/SKILL.md files from the skills directory
// and returns a map of skill_name → content.
func readLocalSkills(skillsDir string) map[string]string {
	result := make(map[string]string)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("failed to read skills directory", "error", err)
		}
		return result
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		filePath := filepath.Join(skillsDir, name, "SKILL.md")
		content, err := os.ReadFile(filePath)
		if err != nil {
			if !os.IsNotExist(err) {
				slog.Error("failed to read local skill file", "path", filePath, "error", err)
			}
			continue
		}

		result[name] = string(content)
	}

	return result
}
