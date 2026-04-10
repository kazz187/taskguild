package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunTask_PlanToDevelop verifies that when the agent outputs
// "NEXT_STATUS: Develop" while in Plan status, the task transitions to Develop.
func TestRunTask_PlanToDevelop(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Plan", `[{"name":"Develop"}]`)

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Plan is complete.\nNEXT_STATUS: Develop")},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-1", "system instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify status transition
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1, "expected exactly one status transition")
	assert.Equal(t, "Develop", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

// TestRunTask_DevelopToReview verifies transition from Develop to Review.
func TestRunTask_DevelopToReview(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Develop", `[{"name":"Review"}]`)

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Code is complete.\nNEXT_STATUS: Review")},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-2", "system instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Review", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

// TestRunTask_FullWorkflow_PlanDevelopReview verifies the complete 3-step
// workflow: Plan -> Develop -> Review -> terminal (no transitions).
func TestRunTask_FullWorkflow_PlanDevelopReview(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)
	workDir := t.TempDir()

	// Phase 1: Plan -> Develop
	ctx1, cancel1 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel1()

	metadata1 := baseMetadata("Plan", `[{"name":"Develop"}]`)
	qr1 := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Plan done.\nNEXT_STATUS: Develop")},
		},
	}
	runTask(ctx1, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-full", "instructions", metadata1,
		workDir, permCache, scpCache, qr1, func() bool { return false })

	// Phase 2: Develop -> Review
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	metadata2 := baseMetadata("Develop", `[{"name":"Review"}]`)
	qr2 := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Code done.\nNEXT_STATUS: Review")},
		},
	}
	runTask(ctx2, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-full", "instructions", metadata2,
		workDir, permCache, scpCache, qr2, func() bool { return false })

	// Phase 3: Review (terminal - no transitions)
	ctx3, cancel3 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel3()

	metadata3 := baseMetadata("Review", "")
	qr3 := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Review complete. All looks good.")},
		},
	}
	runTask(ctx3, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-full", "instructions", metadata3,
		workDir, permCache, scpCache, qr3, func() bool { return false })

	// Verify: exactly 2 status transitions (Plan->Develop, Develop->Review)
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 2, "expected 2 status transitions")
	assert.Equal(t, "Develop", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
	assert.Equal(t, "Review", tc.taskHandler.updateTaskStatusReqs[1].StatusId)
}

// TestRunTask_InvalidTransitionRetry verifies that when the agent outputs an
// invalid NEXT_STATUS, it gets a retry prompt and can correct itself.
func TestRunTask_InvalidTransitionRetry(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Plan", `[{"name":"Develop"}]`)

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("NEXT_STATUS: InvalidStatus")},
			{Result: makeResult("NEXT_STATUS: Develop")},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-retry", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify QueryRunner was called twice
	calls := qr.getCalls()
	require.Len(t, calls, 2, "expected 2 QueryRunner calls (original + retry)")
	// Second call should contain the corrective prompt
	assert.Contains(t, calls[1].Prompt, "failed")

	// Verify final transition was to Develop
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Develop", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

// TestRunTask_AutoTransition_SingleTarget verifies that when the agent does
// not output NEXT_STATUS but there is exactly one available transition,
// the system auto-transitions.
func TestRunTask_AutoTransition_SingleTarget(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Plan", `[{"name":"Develop"}]`)

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("I've completed the planning phase.")},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-auto", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify auto-transition to Develop
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Develop", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

// TestRunTask_TerminalStatus verifies that when the task is at a terminal
// status (no transitions), it completes without attempting a transition.
// The subtests cover both an empty string and "null" (what json.Marshal
// produces for a nil Go slice) to guard against the bug where "null" was
// treated as non-empty, causing hasTransitions to be incorrectly true.
func TestRunTask_TerminalStatus(t *testing.T) {
	for _, tt := range []struct {
		name        string
		transitions string
	}{
		{"empty", ""},
		{"null", "null"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := newTestClients()
			defer tc.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			metadata := baseMetadata("Done", tt.transitions)

			qr := &mockQueryRunner{
				results: []mockQueryRunnerResult{
					{Result: makeResult("Task is complete.")},
				},
			}

			permCache := newPermissionCache("test", tc.agentClient)
			scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

			runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
				"agent-mgr-1", "task-terminal", "instructions", metadata,
				t.TempDir(), permCache, scpCache, qr, func() bool { return false })

			// No status transitions should have been attempted
			tc.taskHandler.mu.Lock()
			defer tc.taskHandler.mu.Unlock()
			assert.Empty(t, tc.taskHandler.updateTaskStatusReqs, "no transitions expected at terminal status")

			// But task result should have been reported
			tc.agentHandler.mu.Lock()
			defer tc.agentHandler.mu.Unlock()
			assert.NotEmpty(t, tc.agentHandler.reportTaskResultReqs, "task result should be reported")
		})
	}
}

// TestRunTask_CreateTask verifies that CREATE_TASK directives in agent output
// result in CreateTask RPC calls.
func TestRunTask_CreateTask(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Plan", `[{"name":"Develop"}]`)

	agentOutput := `I've created a subtask.

CREATE_TASK_START
title: Implement authentication
status: Plan

Implement user authentication with OAuth2.
CREATE_TASK_END

NEXT_STATUS: Develop`

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult(agentOutput)},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-create", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify CreateTask was called
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()
	require.Len(t, tc.taskHandler.createTaskReqs, 1, "expected one CreateTask call")
	assert.Equal(t, "Implement authentication", tc.taskHandler.createTaskReqs[0].Title)

	// Verify status still transitioned
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Develop", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

// TestRunTask_TaskDescriptionUpdate verifies that TASK_DESCRIPTION directives
// in agent output result in UpdateTask calls with the new description.
func TestRunTask_TaskDescriptionUpdate(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Plan", `[{"name":"Develop"}]`)

	agentOutput := `I've updated the task description.

TASK_DESCRIPTION_START
This is the updated description with more details.
TASK_DESCRIPTION_END

NEXT_STATUS: Develop`

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult(agentOutput)},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-desc", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify UpdateTask was called with the description
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()

	// Find the description update among all UpdateTask calls
	var foundDesc bool
	for _, req := range tc.taskHandler.updateTaskReqs {
		if req.Description != "" {
			assert.Equal(t, "This is the updated description with more details.", req.Description)
			foundDesc = true
			break
		}
	}
	assert.True(t, foundDesc, "expected a description update via UpdateTask")
}

// TestRunTask_SessionPerStatusOnTransition verifies that per-status session IDs
// are preserved across status transitions (session_id_{StatusName} keys are
// never cleared).
func TestRunTask_SessionPerStatusOnTransition(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := baseMetadata("Develop", `[{"name":"Review"}]`)
	metadata["session_id_Plan"] = "plan-session-id"

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("Code done.\nNEXT_STATUS: Review")},
		},
	}

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-session", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()

	// Verify global session_id is NOT written (only per-status keys).
	for _, req := range tc.taskHandler.updateTaskReqs {
		_, hasGlobal := req.Metadata["session_id"]
		assert.False(t, hasGlobal, "global session_id should not be written")
	}

	// Verify status transition still happened.
	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Review", tc.taskHandler.updateTaskStatusReqs[0].StatusId)
}

func TestResolveSession(t *testing.T) {
	tests := []struct {
		name          string
		metadata      map[string]string
		wantSessionID string
	}{
		{
			name:          "new task, no session",
			metadata:      map[string]string{},
			wantSessionID: "",
		},
		{
			name: "per-status session resume",
			metadata: map[string]string{
				"_current_status_name": "Develop",
				"session_id_Develop":   "dev-sess",
			},
			wantSessionID: "dev-sess",
		},
		{
			name: "inherit from previous status",
			metadata: map[string]string{
				"_current_status_name":  "Develop",
				"_inherit_session_from": "Plan",
				"session_id_Plan":       "plan-sess",
			},
			wantSessionID: "plan-sess",
		},
		{
			name: "subtask inherits parent session",
			metadata: map[string]string{
				"_current_status_name": "Develop",
				"session_id_Develop":   "parent-dev-sess",
			},
			wantSessionID: "parent-dev-sess",
		},
		{
			name: "inherit takes priority over per-status",
			metadata: map[string]string{
				"_current_status_name":  "Develop",
				"_inherit_session_from": "Plan",
				"session_id_Plan":       "plan-sess",
				"session_id_Develop":    "dev-sess",
			},
			wantSessionID: "plan-sess",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID := resolveSession(tt.metadata)
			assert.Equal(t, tt.wantSessionID, sessionID)
		})
	}
}
