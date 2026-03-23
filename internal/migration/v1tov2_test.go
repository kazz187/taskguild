package migration

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/kazz187/taskguild/pkg/storage"
)

func setupStorage(t *testing.T) storage.Storage {
	t.Helper()
	store, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	return store
}

func writeYAML(t *testing.T, store storage.Storage, path string, data map[string]interface{}) {
	t.Helper()
	b, err := yaml.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal yaml: %v", err)
	}
	if err := store.Write(context.Background(), path, b); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func assertExists(t *testing.T, store storage.Storage, path string) {
	t.Helper()
	exists, err := store.Exists(context.Background(), path)
	if err != nil {
		t.Fatalf("failed to check existence of %s: %v", path, err)
	}
	if !exists {
		t.Errorf("expected %s to exist", path)
	}
}

func assertNotExists(t *testing.T, store storage.Storage, path string) {
	t.Helper()
	exists, err := store.Exists(context.Background(), path)
	if err != nil {
		t.Fatalf("failed to check existence of %s: %v", path, err)
	}
	if exists {
		t.Errorf("expected %s to not exist", path)
	}
}

func readYAML(t *testing.T, store storage.Storage, path string) map[string]interface{} {
	t.Helper()
	data, err := store.Read(context.Background(), path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal %s: %v", path, err)
	}
	return m
}

func TestMigrateProjects(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "projects/proj1.yaml", map[string]interface{}{
		"id":   "proj1",
		"name": "Project One",
	})

	m := NewV1ToV2Migrator(store, false)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/project.yaml")
	assertNotExists(t, store, "projects/proj1.yaml")
	assertExists(t, store, ".storage-version")

	if result.FilesMigrated != 1 {
		t.Errorf("expected 1 file migrated, got %d", result.FilesMigrated)
	}
}

func TestMigrateTasks(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "tasks/task1.yaml", map[string]interface{}{
		"id":         "task1",
		"project_id": "proj1",
		"title":      "Test Task",
	})

	m := NewV1ToV2Migrator(store, false)
	_, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/task1/task.yaml")
	assertNotExists(t, store, "tasks/task1.yaml")
}

func TestMigrateArchivedTasks(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "tasks/archived/task2.yaml", map[string]interface{}{
		"id":         "task2",
		"project_id": "proj1",
		"title":      "Archived Task",
	})

	m := NewV1ToV2Migrator(store, false)
	_, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/archived/task2/task.yaml")
	assertNotExists(t, store, "tasks/archived/task2.yaml")
}

func TestMigrateEntityTypes(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	for _, entityType := range []string{"agents", "workflows", "skills", "scripts", "singlecommandpermissions"} {
		writeYAML(t, store, entityType+"/ent1.yaml", map[string]interface{}{
			"id":         "ent1",
			"project_id": "proj1",
		})
	}

	m := NewV1ToV2Migrator(store, false)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	for _, entityType := range []string{"agents", "workflows", "skills", "scripts", "singlecommandpermissions"} {
		assertExists(t, store, "projects/proj1/"+entityType+"/ent1.yaml")
		assertNotExists(t, store, entityType+"/ent1.yaml")
	}

	if result.FilesMigrated != 5 {
		t.Errorf("expected 5 files migrated, got %d", result.FilesMigrated)
	}
}

func TestMigratePerProjectSingleton(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "permissions/proj1.yaml", map[string]interface{}{
		"allow": []string{"read"},
	})
	writeYAML(t, store, "claude_settings/proj1.yaml", map[string]interface{}{
		"model": "claude-3",
	})

	m := NewV1ToV2Migrator(store, false)
	_, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/permissions.yaml")
	assertNotExists(t, store, "permissions/proj1.yaml")
	assertExists(t, store, "projects/proj1/claude_settings.yaml")
	assertNotExists(t, store, "claude_settings/proj1.yaml")
}

func TestMigrateInteractions(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	// Need a task first for the taskProjectMap.
	writeYAML(t, store, "tasks/task1.yaml", map[string]interface{}{
		"id":         "task1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "interactions/int1.yaml", map[string]interface{}{
		"id":      "int1",
		"task_id": "task1",
	})

	m := NewV1ToV2Migrator(store, false)
	_, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/task1/interactions/int1.yaml")
	assertNotExists(t, store, "interactions/int1.yaml")

	// Verify project_id was injected.
	data := readYAML(t, store, "projects/proj1/task1/interactions/int1.yaml")
	if data["project_id"] != "proj1" {
		t.Errorf("expected project_id=proj1, got %v", data["project_id"])
	}
}

func TestMigrateTaskLogs(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "tasks/task1.yaml", map[string]interface{}{
		"id":         "task1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "task_logs/log1.yaml", map[string]interface{}{
		"id":      "log1",
		"task_id": "task1",
	})

	m := NewV1ToV2Migrator(store, false)
	_, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	assertExists(t, store, "projects/proj1/task1/logs/log1.yaml")
	assertNotExists(t, store, "task_logs/log1.yaml")

	data := readYAML(t, store, "projects/proj1/task1/logs/log1.yaml")
	if data["project_id"] != "proj1" {
		t.Errorf("expected project_id=proj1, got %v", data["project_id"])
	}
}

func TestIdempotency(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "projects/proj1.yaml", map[string]interface{}{
		"id":   "proj1",
		"name": "Project One",
	})

	// First run.
	m1 := NewV1ToV2Migrator(store, false)
	r1, err := m1.Run(ctx)
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if r1.FilesMigrated != 1 {
		t.Errorf("first run: expected 1 file migrated, got %d", r1.FilesMigrated)
	}

	// Second run — should be a no-op since .storage-version is v2.
	m2 := NewV1ToV2Migrator(store, false)
	r2, err := m2.Run(ctx)
	if err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	if r2.FilesMigrated != 0 {
		t.Errorf("second run: expected 0 files migrated, got %d", r2.FilesMigrated)
	}
}

func TestAlreadyV2(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	if err := store.Write(ctx, ".storage-version", []byte("v2")); err != nil {
		t.Fatalf("failed to write version: %v", err)
	}

	m := NewV1ToV2Migrator(store, false)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	if result.FilesMigrated != 0 {
		t.Errorf("expected 0 files migrated, got %d", result.FilesMigrated)
	}
}

func TestDryRun(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "projects/proj1.yaml", map[string]interface{}{
		"id":   "proj1",
		"name": "Project One",
	})

	m := NewV1ToV2Migrator(store, true)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// File should NOT have been moved.
	assertExists(t, store, "projects/proj1.yaml")
	assertNotExists(t, store, "projects/proj1/project.yaml")
	assertNotExists(t, store, ".storage-version")

	if result.FilesMigrated != 1 {
		t.Errorf("expected 1 file reported as migrated, got %d", result.FilesMigrated)
	}
}

func TestMissingProjectID(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	writeYAML(t, store, "tasks/task1.yaml", map[string]interface{}{
		"id":    "task1",
		"title": "No Project",
	})

	m := NewV1ToV2Migrator(store, false)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Task should not be migrated.
	assertExists(t, store, "tasks/task1.yaml")
	if result.Errors != 1 {
		t.Errorf("expected 1 error, got %d", result.Errors)
	}
}

func TestFullMigration(t *testing.T) {
	store := setupStorage(t)
	ctx := context.Background()

	// Set up a complete v1 dataset.
	writeYAML(t, store, "projects/proj1.yaml", map[string]interface{}{
		"id":   "proj1",
		"name": "Project One",
	})
	writeYAML(t, store, "tasks/task1.yaml", map[string]interface{}{
		"id":         "task1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "tasks/archived/task2.yaml", map[string]interface{}{
		"id":         "task2",
		"project_id": "proj1",
	})
	writeYAML(t, store, "agents/agent1.yaml", map[string]interface{}{
		"id":         "agent1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "workflows/wf1.yaml", map[string]interface{}{
		"id":         "wf1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "skills/skill1.yaml", map[string]interface{}{
		"id":         "skill1",
		"project_id": "proj1",
	})
	writeYAML(t, store, "permissions/proj1.yaml", map[string]interface{}{
		"allow": []string{"read"},
	})
	writeYAML(t, store, "claude_settings/proj1.yaml", map[string]interface{}{
		"model": "claude-3",
	})
	writeYAML(t, store, "interactions/int1.yaml", map[string]interface{}{
		"id":      "int1",
		"task_id": "task1",
	})
	writeYAML(t, store, "task_logs/log1.yaml", map[string]interface{}{
		"id":      "log1",
		"task_id": "task1",
	})

	m := NewV1ToV2Migrator(store, false)
	result, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify all files migrated.
	assertExists(t, store, "projects/proj1/project.yaml")
	assertExists(t, store, "projects/proj1/task1/task.yaml")
	assertExists(t, store, "projects/proj1/archived/task2/task.yaml")
	assertExists(t, store, "projects/proj1/agents/agent1.yaml")
	assertExists(t, store, "projects/proj1/workflows/wf1.yaml")
	assertExists(t, store, "projects/proj1/skills/skill1.yaml")
	assertExists(t, store, "projects/proj1/permissions.yaml")
	assertExists(t, store, "projects/proj1/claude_settings.yaml")
	assertExists(t, store, "projects/proj1/task1/interactions/int1.yaml")
	assertExists(t, store, "projects/proj1/task1/logs/log1.yaml")
	assertExists(t, store, ".storage-version")

	// Verify old files are gone.
	assertNotExists(t, store, "projects/proj1.yaml")
	assertNotExists(t, store, "tasks/task1.yaml")
	assertNotExists(t, store, "agents/agent1.yaml")
	assertNotExists(t, store, "interactions/int1.yaml")
	assertNotExists(t, store, "task_logs/log1.yaml")

	if result.FilesMigrated != 10 {
		t.Errorf("expected 10 files migrated, got %d", result.FilesMigrated)
	}
	if result.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", result.Errors)
	}
}
