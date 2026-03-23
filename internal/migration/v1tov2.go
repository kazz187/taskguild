package migration

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/pkg/storage"
)

// entity is a generic struct to extract common fields from any YAML entity.
type entity struct {
	ID        string `yaml:"id"`
	ProjectID string `yaml:"project_id"`
	TaskID    string `yaml:"task_id"`
}

// MigrationResult holds statistics from the migration run.
type MigrationResult struct {
	FilesMigrated int
	FilesSkipped  int
	Errors        int
}

// V1ToV2Migrator migrates storage from v1 (flat) to v2 (project-scoped hierarchy).
type V1ToV2Migrator struct {
	store  storage.Storage
	dryRun bool
	result MigrationResult
}

// NewV1ToV2Migrator creates a new migrator.
func NewV1ToV2Migrator(store storage.Storage, dryRun bool) *V1ToV2Migrator {
	return &V1ToV2Migrator{store: store, dryRun: dryRun}
}

// Run executes the full v1→v2 migration.
func (m *V1ToV2Migrator) Run(ctx context.Context) (*MigrationResult, error) {
	// Check if already migrated.
	if data, err := m.store.Read(ctx, ".storage-version"); err == nil {
		if strings.TrimSpace(string(data)) == "v2" {
			slog.Info("storage is already at v2, nothing to do")
			return &m.result, nil
		}
	}

	// Build task ID -> project ID mapping for interactions/task logs.
	taskProjectMap := make(map[string]string)

	// 1. Migrate projects.
	m.migrateProjects(ctx)

	// 2. Migrate tasks (also builds taskProjectMap).
	m.migrateTasks(ctx, taskProjectMap)
	m.migrateArchivedTasks(ctx, taskProjectMap)

	// 3. Migrate project-scoped entity types.
	for _, entityType := range []string{"agents", "workflows", "skills", "scripts", "singlecommandpermissions"} {
		m.migrateEntityType(ctx, entityType)
	}

	// 4. Migrate per-project singletons.
	m.migratePerProjectSingleton(ctx, "permissions", "permissions.yaml")
	m.migratePerProjectSingleton(ctx, "claude_settings", "claude_settings.yaml")

	// 5. Migrate interactions.
	m.migrateInteractions(ctx, taskProjectMap)

	// 6. Migrate task logs.
	m.migrateTaskLogs(ctx, taskProjectMap)

	// 7. Write version marker.
	if !m.dryRun {
		if err := m.store.Write(ctx, ".storage-version", []byte("v2")); err != nil {
			return &m.result, fmt.Errorf("failed to write storage version: %w", err)
		}
	}

	slog.Info("migration complete",
		"files_migrated", m.result.FilesMigrated,
		"files_skipped", m.result.FilesSkipped,
		"errors", m.result.Errors,
	)
	return &m.result, nil
}

func (m *V1ToV2Migrator) migrateProjects(ctx context.Context) {
	files, err := m.store.List(ctx, "projects")
	if err != nil {
		slog.Warn("failed to list projects", "error", err)
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".yaml")
		newPath := fmt.Sprintf("projects/%s/project.yaml", id)

		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		data, err := m.store.Read(ctx, f)
		if err != nil {
			slog.Warn("failed to read project file", "path", f, "error", err)
			m.result.Errors++
			continue
		}

		slog.Info("migrate project", "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write project file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migrateTasks(ctx context.Context, taskProjectMap map[string]string) {
	files, err := m.store.List(ctx, "tasks")
	if err != nil {
		slog.Warn("failed to list tasks", "error", err)
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			m.result.Errors++
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			slog.Warn("task missing project_id or id, skipping", "path", f)
			m.result.Errors++
			continue
		}
		taskProjectMap[e.ID] = e.ProjectID

		newPath := fmt.Sprintf("projects/%s/%s/task.yaml", e.ProjectID, e.ID)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate task", "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write task file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migrateArchivedTasks(ctx context.Context, taskProjectMap map[string]string) {
	files, err := m.store.List(ctx, "tasks/archived")
	if err != nil {
		return // no archived tasks
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			m.result.Errors++
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			slog.Warn("archived task missing project_id or id, skipping", "path", f)
			m.result.Errors++
			continue
		}
		taskProjectMap[e.ID] = e.ProjectID

		newPath := fmt.Sprintf("projects/%s/archived/%s/task.yaml", e.ProjectID, e.ID)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate archived task", "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write archived task file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migrateEntityType(ctx context.Context, entityType string) {
	files, err := m.store.List(ctx, entityType)
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			m.result.Errors++
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			slog.Warn("entity missing project_id or id, skipping", "type", entityType, "path", f)
			m.result.Errors++
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/%s.yaml", e.ProjectID, entityType, e.ID)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate entity", "type", entityType, "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write entity file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migratePerProjectSingleton(ctx context.Context, oldPrefix, newFileName string) {
	files, err := m.store.List(ctx, oldPrefix)
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		projectID := strings.TrimSuffix(filepath.Base(f), ".yaml")
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s", projectID, newFileName)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate singleton", "type", oldPrefix, "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write singleton file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migrateInteractions(ctx context.Context, taskProjectMap map[string]string) {
	files, err := m.store.List(ctx, "interactions")
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			m.result.Errors++
			continue
		}
		if e.TaskID == "" || e.ID == "" {
			m.result.Errors++
			continue
		}
		projectID, ok := taskProjectMap[e.TaskID]
		if !ok {
			slog.Warn("interaction references unknown task, skipping", "interaction_id", e.ID, "task_id", e.TaskID)
			m.result.Errors++
			continue
		}

		// Inject project_id into the YAML data.
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			m.result.Errors++
			continue
		}
		raw["project_id"] = projectID
		data, err = yaml.Marshal(raw)
		if err != nil {
			m.result.Errors++
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/interactions/%s.yaml", projectID, e.TaskID, e.ID)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate interaction", "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write interaction file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}

func (m *V1ToV2Migrator) migrateTaskLogs(ctx context.Context, taskProjectMap map[string]string) {
	files, err := m.store.List(ctx, "task_logs")
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(ctx, f)
		if err != nil {
			m.result.Errors++
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			m.result.Errors++
			continue
		}
		if e.TaskID == "" || e.ID == "" {
			m.result.Errors++
			continue
		}
		projectID, ok := taskProjectMap[e.TaskID]
		if !ok {
			slog.Warn("task_log references unknown task, skipping", "log_id", e.ID, "task_id", e.TaskID)
			m.result.Errors++
			continue
		}

		// Inject project_id into the YAML data.
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			m.result.Errors++
			continue
		}
		raw["project_id"] = projectID
		data, err = yaml.Marshal(raw)
		if err != nil {
			m.result.Errors++
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/logs/%s.yaml", projectID, e.TaskID, e.ID)
		if exists, _ := m.store.Exists(ctx, newPath); exists {
			m.result.FilesSkipped++
			continue
		}

		slog.Info("migrate task_log", "from", f, "to", newPath)
		if !m.dryRun {
			if err := m.store.Write(ctx, newPath, data); err != nil {
				slog.Warn("failed to write task_log file", "path", newPath, "error", err)
				m.result.Errors++
				continue
			}
			_ = m.store.Delete(ctx, f)
		}
		m.result.FilesMigrated++
	}
}
