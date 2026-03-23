package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/pkg/storage"
)

// entity is a generic struct to extract ProjectID from any YAML entity.
type entity struct {
	ID        string `yaml:"id"`
	ProjectID string `yaml:"project_id"`
	TaskID    string `yaml:"task_id"`
}

func main() {
	baseDir := flag.String("base-dir", ".taskguild/data", "storage base directory")
	dryRun := flag.Bool("dry-run", false, "print what would be done without making changes")
	flag.Parse()

	ctx := context.Background()

	store, err := storage.NewLocalStorage(*baseDir)
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}

	// Check if already migrated.
	if data, err := store.Read(ctx, ".storage-version"); err == nil {
		if strings.TrimSpace(string(data)) == "v2" {
			log.Println("storage is already at v2, nothing to do")
			return
		}
	}

	m := &migrator{store: store, ctx: ctx, dryRun: *dryRun}

	// Build task ID -> project ID mapping for interactions/tasklogs.
	taskProjectMap := make(map[string]string)

	// 1. Migrate projects: projects/$id.yaml -> projects/$id/project.yaml
	m.migrateProjects()

	// 2. Migrate project-scoped entities.
	taskProjectMap = m.migrateTasks(taskProjectMap)
	m.migrateArchivedTasks(taskProjectMap)
	m.migrateEntityType("agents", taskProjectMap)
	m.migrateEntityType("workflows", taskProjectMap)
	m.migrateEntityType("skills", taskProjectMap)
	m.migrateEntityType("scripts", taskProjectMap)
	m.migrateEntityType("singlecommandpermissions", taskProjectMap)

	// 3. Migrate permissions and claude_settings.
	m.migratePerProjectSingleton("permissions", "permissions.yaml")
	m.migratePerProjectSingleton("claude_settings", "claude_settings.yaml")

	// 4. Migrate interactions.
	m.migrateInteractions(taskProjectMap)

	// 5. Migrate task_logs.
	m.migrateTaskLogs(taskProjectMap)

	// 6. Write version marker.
	if !*dryRun {
		if err := store.Write(ctx, ".storage-version", []byte("v2")); err != nil {
			log.Fatalf("failed to write storage version: %v", err)
		}
	}

	log.Println("migration complete")
}

type migrator struct {
	store  storage.Storage
	ctx    context.Context
	dryRun bool
}

func (m *migrator) migrateProjects() {
	files, err := m.store.List(m.ctx, "projects")
	if err != nil {
		log.Printf("warning: failed to list projects: %v", err)
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(f), ".yaml")
		newPath := fmt.Sprintf("projects/%s/project.yaml", id)

		// Skip if already migrated.
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			log.Printf("warning: failed to read %s: %v", f, err)
			continue
		}

		log.Printf("migrate project: %s -> %s", f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func (m *migrator) migrateTasks(taskProjectMap map[string]string) map[string]string {
	files, err := m.store.List(m.ctx, "tasks")
	if err != nil {
		log.Printf("warning: failed to list tasks: %v", err)
		return taskProjectMap
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			log.Printf("warning: task %s missing project_id or id, skipping", f)
			continue
		}
		taskProjectMap[e.ID] = e.ProjectID

		newPath := fmt.Sprintf("projects/%s/%s/task.yaml", e.ProjectID, e.ID)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate task: %s -> %s", f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
	return taskProjectMap
}

func (m *migrator) migrateArchivedTasks(taskProjectMap map[string]string) {
	files, err := m.store.List(m.ctx, "tasks/archived")
	if err != nil {
		return // no archived tasks
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			continue
		}
		taskProjectMap[e.ID] = e.ProjectID

		newPath := fmt.Sprintf("projects/%s/archived/%s/task.yaml", e.ProjectID, e.ID)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate archived task: %s -> %s", f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func (m *migrator) migrateEntityType(entityType string, taskProjectMap map[string]string) {
	files, err := m.store.List(m.ctx, entityType)
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.ProjectID == "" || e.ID == "" {
			log.Printf("warning: %s/%s missing project_id or id, skipping", entityType, filepath.Base(f))
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/%s.yaml", e.ProjectID, entityType, e.ID)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate %s: %s -> %s", entityType, f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func (m *migrator) migratePerProjectSingleton(oldPrefix, newFileName string) {
	files, err := m.store.List(m.ctx, oldPrefix)
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		projectID := strings.TrimSuffix(filepath.Base(f), ".yaml")
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s", projectID, newFileName)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate %s: %s -> %s", oldPrefix, f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func (m *migrator) migrateInteractions(taskProjectMap map[string]string) {
	files, err := m.store.List(m.ctx, "interactions")
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.TaskID == "" || e.ID == "" {
			continue
		}
		projectID, ok := taskProjectMap[e.TaskID]
		if !ok {
			log.Printf("warning: interaction %s references unknown task %s, skipping", e.ID, e.TaskID)
			continue
		}

		// Inject project_id into the YAML data.
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		raw["project_id"] = projectID
		data, err = yaml.Marshal(raw)
		if err != nil {
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/interactions/%s.yaml", projectID, e.TaskID, e.ID)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate interaction: %s -> %s", f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func (m *migrator) migrateTaskLogs(taskProjectMap map[string]string) {
	files, err := m.store.List(m.ctx, "task_logs")
	if err != nil {
		return
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		data, err := m.store.Read(m.ctx, f)
		if err != nil {
			continue
		}
		var e entity
		if err := yaml.Unmarshal(data, &e); err != nil {
			continue
		}
		if e.TaskID == "" || e.ID == "" {
			continue
		}
		projectID, ok := taskProjectMap[e.TaskID]
		if !ok {
			log.Printf("warning: task_log %s references unknown task %s, skipping", e.ID, e.TaskID)
			continue
		}

		// Inject project_id into the YAML data.
		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		raw["project_id"] = projectID
		data, err = yaml.Marshal(raw)
		if err != nil {
			continue
		}

		newPath := fmt.Sprintf("projects/%s/%s/logs/%s.yaml", projectID, e.TaskID, e.ID)
		if exists, _ := m.store.Exists(m.ctx, newPath); exists {
			continue
		}

		log.Printf("migrate task_log: %s -> %s", f, newPath)
		if !m.dryRun {
			if err := m.store.Write(m.ctx, newPath, data); err != nil {
				log.Printf("error: failed to write %s: %v", newPath, err)
				continue
			}
			_ = m.store.Delete(m.ctx, f)
		}
	}
}

func init() {
	// Suppress unused import warning.
	_ = os.Stderr
}
