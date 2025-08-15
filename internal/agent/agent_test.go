package agent

import (
	"context"
	"testing"
	"time"
)

func TestNewAgent(t *testing.T) {
	agent := NewAgent("developer", "claude-code")

	if agent.Name != "developer" {
		t.Errorf("Expected name 'developer', got %s", agent.Name)
	}

	if agent.Type != "claude-code" {
		t.Errorf("Expected type 'claude-code', got %s", agent.Type)
	}

	if agent.Status != StatusIdle {
		t.Errorf("Expected status 'IDLE', got %s", agent.Status)
	}
}

func TestAgentStatusUpdate(t *testing.T) {
	agent := NewAgent("developer", "claude-code")

	agent.UpdateStatus(StatusBusy)

	if agent.GetStatus() != StatusBusy {
		t.Errorf("Expected status 'BUSY', got %s", agent.GetStatus())
	}
}

func TestAgentTaskAssignment(t *testing.T) {
	agent := NewAgent("developer", "claude-code")

	agent.AssignTask("TASK-001", "/path/to/worktree")

	if agent.TaskID != "TASK-001" {
		t.Errorf("Expected task ID 'TASK-001', got %s", agent.TaskID)
	}

	if agent.WorktreePath != "/path/to/worktree" {
		t.Errorf("Expected worktree path '/path/to/worktree', got %s", agent.WorktreePath)
	}

	if !agent.IsAssigned() {
		t.Error("Expected agent to be assigned")
	}

	agent.ClearTask()

	if agent.IsAssigned() {
		t.Error("Expected agent to not be assigned after clearing task")
	}
}

func TestAgentStartStop(t *testing.T) {
	agent := NewAgent("developer", "claude-code")
	ctx := context.Background()

	// Test start
	if err := agent.Start(ctx); err != nil {
		t.Errorf("Failed to start agent: %v", err)
	}

	// Wait a bit to ensure agent is running
	time.Sleep(100 * time.Millisecond)

	// Test stop
	if err := agent.Stop(); err != nil {
		t.Errorf("Failed to stop agent: %v", err)
	}

	if agent.GetStatus() != StatusStopped {
		t.Errorf("Expected status 'STOPPED', got %s", agent.GetStatus())
	}
}
