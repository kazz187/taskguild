package event

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHookExecutor_Execute(t *testing.T) {
	// Create a temporary file to write hook output
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "hook_output.txt")

	hooks := []Hook{
		{
			Name:    "test-hook",
			Event:   TaskCreated,
			Command: "echo \"Task created event received\" > " + outputFile,
			Timeout: 5,
		},
	}

	executor := NewHookExecutor(hooks)

	// Create an event with struct data
	event := NewEvent("test", TaskCreatedData{
		TaskID:      "TASK-001",
		Title:       "Test Task",
		Description: "Test Description",
		Type:        "feature",
	})
	eventMsg, err := event.ToMessage()
	if err != nil {
		t.Fatalf("Failed to convert event to message: %v", err)
	}

	// Execute hooks
	ctx := context.Background()
	err = executor.Execute(ctx, eventMsg)
	if err != nil {
		t.Fatalf("Failed to execute hook: %v", err)
	}

	// Verify hook output
	time.Sleep(100 * time.Millisecond) // Give time for the hook to complete

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read hook output: %v", err)
	}

	expected := "Task created event received\n"
	if string(data) != expected {
		t.Errorf("Expected hook output %q, got %q", expected, string(data))
	}
}

func TestHookExecutor_MultipleHooks(t *testing.T) {
	// Create temporary files for hook outputs
	tmpDir := t.TempDir()
	outputFile1 := filepath.Join(tmpDir, "hook1_output.txt")
	outputFile2 := filepath.Join(tmpDir, "hook2_output.txt")

	hooks := []Hook{
		{
			Name:    "hook1",
			Event:   TaskStatusChanged,
			Command: "echo \"Hook1 executed\" > " + outputFile1,
			Timeout: 5,
		},
		{
			Name:    "hook2",
			Event:   TaskStatusChanged,
			Command: "echo \"Hook2 executed\" > " + outputFile2,
			Timeout: 5,
		},
	}

	executor := NewHookExecutor(hooks)

	// Create an event with struct data
	event := NewEvent("test", TaskStatusChangedData{
		TaskID:    "TASK-001",
		OldStatus: "CREATED",
		NewStatus: "IN_PROGRESS",
	})
	eventMsg, err := event.ToMessage()
	if err != nil {
		t.Fatalf("Failed to convert event to message: %v", err)
	}

	// Execute hooks
	ctx := context.Background()
	err = executor.Execute(ctx, eventMsg)
	if err != nil {
		t.Fatalf("Failed to execute hooks: %v", err)
	}

	// Verify hook outputs
	time.Sleep(100 * time.Millisecond)

	// Check hook1 output
	data1, err := os.ReadFile(outputFile1)
	if err != nil {
		t.Fatalf("Failed to read hook1 output: %v", err)
	}
	if string(data1) != "Hook1 executed\n" {
		t.Errorf("Unexpected hook1 output: %q", string(data1))
	}

	// Check hook2 output
	data2, err := os.ReadFile(outputFile2)
	if err != nil {
		t.Fatalf("Failed to read hook2 output: %v", err)
	}
	if string(data2) != "Hook2 executed\n" {
		t.Errorf("Unexpected hook2 output: %q", string(data2))
	}
}

func TestHookExecutor_Timeout(t *testing.T) {
	hooks := []Hook{
		{
			Name:    "timeout-hook",
			Event:   TaskClosed,
			Command: "sleep 10", // This should timeout
			Timeout: 1,          // 1 second timeout
		},
	}

	executor := NewHookExecutor(hooks)

	// Create an event
	event := NewEvent("test", TaskClosedData{
		TaskID: "TASK-001",
	})
	eventMsg, err := event.ToMessage()
	if err != nil {
		t.Fatalf("Failed to convert event to message: %v", err)
	}

	// Execute hooks
	ctx := context.Background()
	start := time.Now()
	err = executor.Execute(ctx, eventMsg)
	duration := time.Since(start)

	// Should fail due to timeout
	if err == nil {
		t.Error("Expected timeout error, got nil")
	}

	// Should complete within 2 seconds (1 second timeout + some overhead)
	if duration > 2*time.Second {
		t.Errorf("Hook execution took too long: %v", duration)
	}
}
