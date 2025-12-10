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
		ID:       "TEST-001",
		Title:    "Test Task",
		Type:     "test",
		Worktree: ".taskguild/worktrees/TEST-001",
		Branch:   "test/TEST-001",
		Processes: map[string]*ProcessState{
			"implement": {Status: ProcessStatusPending},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
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

	// Test Update - update process status
	retrievedTask.Processes["implement"].Status = ProcessStatusInProgress
	if err := repo.Update(retrievedTask); err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	updatedTask, err := repo.GetByID("TEST-001")
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}

	if updatedTask.Processes["implement"].Status != ProcessStatusInProgress {
		t.Errorf("Expected status in_progress, got %s", updatedTask.Processes["implement"].Status)
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
	service := NewService(repo, nil) // Pass nil for eventBus in test

	// Test CreateTask
	createReq := &CreateTaskRequest{
		Title: "Test Task",
		Type:  "feature",
	}
	task, err := service.CreateTask(createReq)
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

	// Check initial process status
	overallStatus := task.GetOverallStatus()
	if overallStatus != "PENDING" {
		t.Errorf("Expected overall status 'PENDING', got %s", overallStatus)
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

	// Test process acquisition
	acquireReq := &TryAcquireProcessRequest{
		TaskID:      "TASK-001",
		ProcessName: "implement",
		AgentID:     "agent-001",
	}
	acquiredTask, err := service.TryAcquireProcess(acquireReq)
	if err != nil {
		t.Fatalf("Failed to acquire process: %v", err)
	}

	if acquiredTask.Processes["implement"].Status != ProcessStatusInProgress {
		t.Errorf("Expected status in_progress, got %s", acquiredTask.Processes["implement"].Status)
	}

	if acquiredTask.Processes["implement"].AssignedTo != "agent-001" {
		t.Errorf("Expected assigned to agent-001, got %s", acquiredTask.Processes["implement"].AssignedTo)
	}

	// Test complete process
	err = service.CompleteProcess("TASK-001", "implement", "agent-001")
	if err != nil {
		t.Fatalf("Failed to complete process: %v", err)
	}

	completedTask, err := service.GetTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to get task after complete: %v", err)
	}

	if completedTask.Processes["implement"].Status != ProcessStatusCompleted {
		t.Errorf("Expected status completed, got %s", completedTask.Processes["implement"].Status)
	}

	// Test CloseTask
	closedTask, err := service.CloseTask("TASK-001")
	if err != nil {
		t.Fatalf("Failed to close task: %v", err)
	}

	overallStatus = closedTask.GetOverallStatus()
	if overallStatus != "CLOSED" {
		t.Errorf("Expected status CLOSED, got %s", overallStatus)
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
	service := NewService(repo, nil) // Pass nil for eventBus in test

	// Test CreateTask with empty title
	_, err = service.CreateTask(&CreateTaskRequest{
		Title: "",
		Type:  "feature",
	})
	if err == nil {
		t.Error("Expected error for empty title, but got none")
	}

	// Test GetTask with empty ID
	_, err = service.GetTask("")
	if err == nil {
		t.Error("Expected error for empty ID, but got none")
	}

	// Test UpdateTask with empty ID
	_, err = service.UpdateTask(&UpdateTaskRequest{
		ID: "",
	})
	if err == nil {
		t.Error("Expected error for empty ID, but got none")
	}
}

func TestProcessRejectWithCascade(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "taskguild-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	repo := NewYAMLRepository(filepath.Join(tempDir, "test-tasks.yaml"))
	service := NewService(repo, nil)

	// Create task
	task, err := service.CreateTask(&CreateTaskRequest{
		Title: "Test Task",
		Type:  "feature",
	})
	if err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// Acquire and complete implement process
	_, err = service.TryAcquireProcess(&TryAcquireProcessRequest{
		TaskID:      task.ID,
		ProcessName: "implement",
		AgentID:     "agent-001",
	})
	if err != nil {
		t.Fatalf("Failed to acquire implement: %v", err)
	}

	err = service.CompleteProcess(task.ID, "implement", "agent-001")
	if err != nil {
		t.Fatalf("Failed to complete implement: %v", err)
	}

	// Acquire and complete review process
	_, err = service.TryAcquireProcess(&TryAcquireProcessRequest{
		TaskID:      task.ID,
		ProcessName: "review",
		AgentID:     "agent-002",
	})
	if err != nil {
		t.Fatalf("Failed to acquire review: %v", err)
	}

	// Reject review - this should cascade reset to implement
	err = service.RejectProcess(task.ID, "review", "agent-002", "code quality issues")
	if err != nil {
		t.Fatalf("Failed to reject review: %v", err)
	}

	// Verify implement was reset to pending
	taskAfterReject, err := service.GetTask(task.ID)
	if err != nil {
		t.Fatalf("Failed to get task after reject: %v", err)
	}

	if taskAfterReject.Processes["implement"].Status != ProcessStatusPending {
		t.Errorf("Expected implement to be reset to pending, got %s", taskAfterReject.Processes["implement"].Status)
	}

	// Note: The rejected process is also reset to pending (not rejected status)
	// because it needs to be re-executed after its dependencies are fixed
	if taskAfterReject.Processes["review"].Status != ProcessStatusPending {
		t.Errorf("Expected review to be reset to pending, got %s", taskAfterReject.Processes["review"].Status)
	}
}
