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

// syncSkills calls the SyncSkills RPC and writes .claude/skills/{name}/SKILL.md files locally.
// It only writes new files (files that don't exist yet on the agent).
// Existing files are preserved to protect local modifications.
// When forceOverwriteIDs is non-empty, those specific skills are overwritten
// regardless of whether the local file already exists.
func syncSkills(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, forceOverwriteIDs map[string]bool) {
	if cfg.ProjectName == "" {
		slog.Info("skipping skill sync: no project name configured")
		return
	}

	resp, err := client.SyncSkills(ctx, connect.NewRequest(&v1.SyncSkillsRequest{
		ProjectName: cfg.ProjectName,
	}))
	if err != nil {
		slog.Error("skill sync failed", "error", err)
		return
	}

	skills := resp.Msg.GetSkills()
	slog.Info("syncing skills from server", "count", len(skills))

	skillsDir := filepath.Join(cfg.WorkDir, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		slog.Error("failed to create skills directory", "error", err)
		return
	}

	// serverSkillNames tracks skill directory names known to the server.
	serverSkillNames := make(map[string]bool)

	var written, skipped int

	for _, sk := range skills {
		name := sk.GetName()
		if name == "" {
			slog.Warn("skipping skill with empty name")
			continue
		}

		// Skip skills with unsafe names.
		if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
			slog.Warn("skipping skill with unsafe name", "name", name)
			continue
		}

		serverSkillNames[name] = true
		skillDir := filepath.Join(skillsDir, name)
		filePath := filepath.Join(skillDir, "SKILL.md")

		// Check if the file already exists.
		if _, err := os.Stat(filePath); err == nil {
			// File exists — only overwrite if this skill ID is in the force list.
			if forceOverwriteIDs != nil && forceOverwriteIDs[sk.GetId()] {
				slog.Debug("force-overwriting existing skill", "name", name, "skill_id", sk.GetId())
			} else {
				slog.Debug("skill file already exists, preserving local version", "name", name)

				skipped++

				continue
			}
		}

		// Ensure the skill subdirectory exists.
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			slog.Error("failed to create skill directory", "path", skillDir, "error", err)
			continue
		}

		content := buildSkillMDContent(sk)
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			slog.Error("failed to write skill file", "path", filePath, "error", err)
			continue
		}

		slog.Debug("synced skill", "name", name)

		written++
	}

	cleanupStaleSkillDirs(skillsDir, serverSkillNames)
	slog.Info("skill sync complete", "written", written, "skipped_existing", skipped)
}

// buildSkillMDContent generates SKILL.md content with YAML frontmatter
// matching the Claude Code skill SKILL.md file format.
func buildSkillMDContent(sk *v1.SkillDefinition) string {
	var sb strings.Builder

	sb.WriteString("---\n")

	if sk.GetName() != "" {
		sb.WriteString(fmt.Sprintf("name: %s\n", sk.GetName()))
	}

	if sk.GetDescription() != "" {
		writeYAMLStringField(&sb, "description", sk.GetDescription())
	}

	if sk.GetDisableModelInvocation() {
		sb.WriteString("disable-model-invocation: true\n")
	}

	if !sk.GetUserInvocable() {
		sb.WriteString("user-invocable: false\n")
	}

	if len(sk.GetAllowedTools()) > 0 {
		sb.WriteString("allowed-tools:\n")

		for _, tool := range sk.GetAllowedTools() {
			sb.WriteString(fmt.Sprintf("  - %s\n", tool))
		}
	}

	if sk.GetModel() != "" {
		sb.WriteString(fmt.Sprintf("model: %s\n", sk.GetModel()))
	}

	if sk.GetContext() != "" {
		sb.WriteString(fmt.Sprintf("context: %s\n", sk.GetContext()))
	}

	if sk.GetAgent() != "" {
		sb.WriteString(fmt.Sprintf("agent: %s\n", sk.GetAgent()))
	}

	if sk.GetArgumentHint() != "" {
		sb.WriteString(fmt.Sprintf("argument-hint: %s\n", sk.GetArgumentHint()))
	}

	sb.WriteString("---\n")

	if content := sk.GetContent(); content != "" {
		sb.WriteString("\n")
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	return sb.String()
}

// cleanupStaleSkillDirs removes skill subdirectories from the skills directory
// that were not found on the server during the current sync.
func cleanupStaleSkillDirs(skillsDir string, serverSkillNames map[string]bool) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		if !serverSkillNames[entry.Name()] {
			dirPath := filepath.Join(skillsDir, entry.Name())
			if err := os.RemoveAll(dirPath); err != nil {
				slog.Error("failed to remove stale skill directory", "path", dirPath, "error", err)
			} else {
				slog.Info("removed stale skill directory", "name", entry.Name())
			}
		}
	}
}
