package task

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestYAMLRepository(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "taskguild-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo := NewYAMLRepository(filepath.Join(tempDir, "test-tasks.yaml"))

	// Test Create
	task := &Task{
		ID:             "TEST-001",
		Title:          "Test Task",
		Type:           "test",
		Status:         "CREATED",
		Worktree:       ".taskguild/worktrees/TEST-001",
		Branch:         "test/TEST-001",
		AssignedAgents: []string{"agent1"},
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := repo.Create(task); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Test GetByID
	retrievedTask, err := repo.GetByID("TEST-001")
	if err != nil {
		t.Fatalf("Failed to get task by ID: %v", err)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected ID %s, got %s", task.ID, retrievedTask.ID)
	}

	if retrievedTask.Title != task.Title {
		t.Errorf("Expected title %s, got %s", task.Title, retrievedTask.Title)
	}

	// Test GetAll
	tasks, err := repo.GetAll()
	if err != nil {
		t.Fatalf("Failed to get all tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Test Update
	retrievedTask.Status = "IN_PROGRESS"
	if err := repo.Update(retrievedTask); err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	updatedTask, err := repo.GetByID("TEST-001")
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}

	if updatedTask.Status != "IN_PROGRESS" {
		t.Errorf("Expected status IN_PROGRESS, got %s", updatedTask.Status)
	}

	// Test Delete
	if err := repo.Delete("TEST-001"); err != nil {
		t.Fatalf("Failed to delete task: %v", err)
	}

	_, err = repo.GetByID("TEST-001")
	if err == nil {
		t.Error("Expected error when getting deleted task, but got none")
	}
}

func TestTaskService(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "taskguild-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo := NewYAMLRepository(filepath.Join(tempDir, "test-tasks.yaml"))
	service := NewTaskService(repo)

	// Test CreateTask
	task, err := service.CreateTask("Test Task", "feature")
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	if task.ID != "TASK-001" {
		t.Errorf("Expected ID TASK-001, got %s", task.ID)
	}

	if task.Title != "Test Task" {
		t.Errorf("Expected title 'Test Task', got %s", task.Title)
	}

	if task.Type != "feature" {
		t.Errorf("Expected type 'feature', got %s", task.Type)
	}

	if task.Status != "CREATED" {
		t.Errorf("Expected status 'CREATED', got %s", task.Status)
	}

	// Test GetTask
	retrievedTask, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected ID %s, got %s", task.ID, retrievedTask.ID)
	}

	// Test ListTasks
	tasks, err := service.ListTasks()
	if err != nil {
		t.Fatalf("Failed to list tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Test UpdateTaskStatus
	if err := service.UpdateTaskStatus("TASK-001", "IN_PROGRESS"); err != nil {
		t.Fatalf("Failed to update task status: %v", err)
	}

	updatedTask, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}

	if updatedTask.Status != "IN_PROGRESS" {
		t.Errorf("Expected status IN_PROGRESS, got %s", updatedTask.Status)
	}

	// Test AssignAgent
	if err := service.AssignAgent("TASK-001", "agent1"); err != nil {
		t.Fatalf("Failed to assign agent: %v", err)
	}

	taskWithAgent, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get task with agent: %v", err)
	}

	if len(taskWithAgent.AssignedAgents) != 1 {
		t.Errorf("Expected 1 assigned agent, got %d", len(taskWithAgent.AssignedAgents))
	}

	if taskWithAgent.AssignedAgents[0] != "agent1" {
		t.Errorf("Expected agent1, got %s", taskWithAgent.AssignedAgents[0])
	}

	// Test UnassignAgent
	if err := service.UnassignAgent("TASK-001", "agent1"); err != nil {
		t.Fatalf("Failed to unassign agent: %v", err)
	}

	taskWithoutAgent, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get task without agent: %v", err)
	}

	if len(taskWithoutAgent.AssignedAgents) != 0 {
		t.Errorf("Expected 0 assigned agents, got %d", len(taskWithoutAgent.AssignedAgents))
	}

	// Test CloseTask
	if err := service.CloseTask("TASK-001"); err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	closedTask, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get closed task: %v", err)
	}

	if closedTask.Status != "CLOSED" {
		t.Errorf("Expected status CLOSED, got %s", closedTask.Status)
	}
}

func TestTaskServiceValidation(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "taskguild-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo := NewYAMLRepository(filepath.Join(tempDir, "test-tasks.yaml"))
	service := NewTaskService(repo)

	// Test CreateTask with empty title
	_, err = service.CreateTask("", "feature")
	if err == nil {
		t.Error("Expected error for empty title, but got none")
	}

	// Test GetTask with empty ID
	_, err = service.GetTask("")
	if err == nil {
		t.Error("Expected error for empty ID, but got none")
	}

	// Test UpdateTaskStatus with empty ID
	err = service.UpdateTaskStatus("", "IN_PROGRESS")
	if err == nil {
		t.Error("Expected error for empty ID, but got none")
	}

	// Test UpdateTaskStatus with empty status
	err = service.UpdateTaskStatus("TASK-001", "")
	if err == nil {
		t.Error("Expected error for empty status, but got none")
	}
}
