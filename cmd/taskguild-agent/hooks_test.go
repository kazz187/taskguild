package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildHookPrompt verifies the prompt construction logic for various
// combinations of action_type / Content / SkillName / Args.
func TestBuildHookPrompt(t *testing.T) {
	cases := []struct {
		name string
		h    hookEntry
		want string
	}{
		{
			name: "skill with no args (legacy/registered) — Content only",
			h:    hookEntry{ActionType: "skill", Content: "do X"},
			want: "do X",
		},
		{
			name: "skill with args appends args footer",
			h:    hookEntry{ActionType: "skill", Content: "do X", Args: "urgent"},
			want: "do X\n\nArgs: urgent",
		},
		{
			name: "custom skill with args becomes slash command",
			h:    hookEntry{ActionType: "custom_skill", SkillName: "simplify", Args: "foo bar"},
			want: "/simplify foo bar",
		},
		{
			name: "custom skill without args becomes bare slash command",
			h:    hookEntry{ActionType: "custom_skill", SkillName: "simplify"},
			want: "/simplify",
		},
		{
			name: "missing Content with SkillName falls back to slash command",
			h:    hookEntry{ActionType: "skill", SkillName: "review", Args: "src/foo.ts"},
			want: "/review src/foo.ts",
		},
		{
			name: "legacy entry with no action_type, only Content",
			h:    hookEntry{Content: "legacy content"},
			want: "legacy content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildHookPrompt(tc.h)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestExecuteHooks_CustomSkill verifies that a custom_skill hook is invoked
// via QueryRunner with a slash-command prompt assembled from SkillName + Args.
func TestExecuteHooks_CustomSkill(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:         "hook-custom-1",
			ActionType: "custom_skill",
			Trigger:    "after_task_execution",
			Order:      1,
			Name:       "simplify",
			SkillName:  "simplify",
			Args:       "src/foo.ts",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("simplified")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-custom-skill")
	defer tl.Close()

	executeHooks(ctx, "task-custom-skill", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	calls := qr.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "/simplify src/foo.ts", calls[0].Prompt)
	assert.Equal(t, "hook_simplify", calls[0].Label)
}

// TestExecuteHooks_SkillWithArgs verifies that a registered Skill hook with
// args appends the args block to the resolved Skill content.
func TestExecuteHooks_SkillWithArgs(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:         "hook-skill-args",
			ActionType: "skill",
			Trigger:    "after_task_execution",
			Order:      1,
			Name:       "create-pr",
			Content:    "Create a PR for the changes.",
			Args:       "urgent",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("ok")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-skill-args")
	defer tl.Close()

	executeHooks(ctx, "task-skill-args", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	calls := qr.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Create a PR for the changes.\n\nArgs: urgent", calls[0].Prompt)
}

// TestCollectStatusSkills_CustomSkill verifies that hooks with action_type
// "custom_skill" are added to the auto-allow set.
func TestCollectStatusSkills_CustomSkill(t *testing.T) {
	hooks := []map[string]any{
		{"name": "simplify", "action_type": "custom_skill"},
		{"name": "create-pr", "action_type": "skill"},
		{"name": "deploy", "action_type": "script"},
		{"name": "legacy", "action_type": ""},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	got := collectStatusSkills(metadata)

	assert.True(t, got["simplify"], "custom_skill hook should be auto-allowed")
	assert.True(t, got["create-pr"], "skill hook should be auto-allowed")
	assert.True(t, got["legacy"], "legacy hook (empty action_type) should be auto-allowed")
	assert.False(t, got["deploy"], "script hook should NOT be auto-allowed")
}

// TestExecuteHooks_CreatePR verifies that a create-pr hook is executed via
// QueryRunner and that TASK_METADATA directives in the hook output update
// the task's metadata via UpdateTask.
func TestExecuteHooks_CreatePR(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:      "hook-1",
			SkillID: "skill-create-pr",
			Trigger: "after_task_execution",
			Order:   1,
			Name:    "create-pr",
			Content: "Create a PR for the changes made in this task.",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("PR created successfully.\nTASK_METADATA: pr_url=https://github.com/test/repo/pull/42")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-hook-test")
	defer tl.Close()

	executeHooks(ctx, "task-hook-test", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	// Verify QueryRunner was called with the hook content
	calls := qr.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "Create a PR for the changes made in this task.", calls[0].Prompt)
	assert.Equal(t, "hook_create-pr", calls[0].Label)

	// Verify TASK_METADATA was applied via UpdateTask
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()

	var foundMetadata bool

	for _, req := range tc.taskHandler.updateTaskReqs {
		if req.Metadata != nil {
			if url, ok := req.GetMetadata()["pr_url"]; ok {
				assert.Equal(t, "https://github.com/test/repo/pull/42", url)

				foundMetadata = true

				break
			}
		}
	}

	assert.True(t, foundMetadata, "expected pr_url metadata update from hook")
}

// TestExecuteHooks_MultipleHooksOrdered verifies that hooks are executed
// in the order specified by their Order field.
func TestExecuteHooks_MultipleHooksOrdered(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:      "hook-2",
			Trigger: "after_task_execution",
			Order:   2,
			Name:    "second-hook",
			Content: "second hook content",
		},
		{
			ID:      "hook-1",
			Trigger: "after_task_execution",
			Order:   1,
			Name:    "first-hook",
			Content: "first hook content",
		},
		{
			ID:      "hook-3",
			Trigger: "after_task_execution",
			Order:   3,
			Name:    "third-hook",
			Content: "third hook content",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("ok")},
			{Result: makeResult("ok")},
			{Result: makeResult("ok")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-order-test")
	defer tl.Close()

	executeHooks(ctx, "task-order-test", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	// Verify calls were made in order
	calls := qr.getCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, "hook_first-hook", calls[0].Label)
	assert.Equal(t, "hook_second-hook", calls[1].Label)
	assert.Equal(t, "hook_third-hook", calls[2].Label)
}

// TestExecuteHooks_HookFailure_DoesNotBlock verifies that if one hook fails,
// subsequent hooks still execute.
func TestExecuteHooks_HookFailure_DoesNotBlock(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:      "hook-1",
			Trigger: "after_task_execution",
			Order:   1,
			Name:    "failing-hook",
			Content: "this hook will fail",
		},
		{
			ID:      "hook-2",
			Trigger: "after_task_execution",
			Order:   2,
			Name:    "succeeding-hook",
			Content: "this hook will succeed",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: nil, Err: errors.New("hook execution failed")},
			{Result: makeResult("success")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-fail-test")
	defer tl.Close()

	executeHooks(ctx, "task-fail-test", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	// Both hooks should have been attempted
	calls := qr.getCalls()
	require.Len(t, calls, 2, "both hooks should be attempted even if first fails")
	assert.Equal(t, "hook_failing-hook", calls[0].Label)
	assert.Equal(t, "hook_succeeding-hook", calls[1].Label)
}

// TestExecuteHooks_TriggerFilter verifies that only hooks matching the
// specified trigger are executed.
func TestExecuteHooks_TriggerFilter(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:      "hook-before",
			Trigger: "before_task_execution",
			Order:   1,
			Name:    "before-hook",
			Content: "before content",
		},
		{
			ID:      "hook-after",
			Trigger: "after_task_execution",
			Order:   1,
			Name:    "after-hook",
			Content: "after content",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := map[string]string{
		"_hooks": string(hooksJSON),
	}

	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("ok")},
		},
	}

	tl := newTaskLogger(ctx, tc.agentClient, "task-filter-test")
	defer tl.Close()

	// Only run after_task_execution hooks
	executeHooks(ctx, "task-filter-test", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	// Only the after hook should have been called
	calls := qr.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "hook_after-hook", calls[0].Label)
}

// TestExecuteHooks_NoHooksMetadata verifies that executeHooks does nothing
// when _hooks metadata is empty.
func TestExecuteHooks_NoHooksMetadata(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	metadata := map[string]string{} // no _hooks

	qr := &mockQueryRunner{}

	tl := newTaskLogger(ctx, tc.agentClient, "task-no-hooks")
	defer tl.Close()

	executeHooks(ctx, "task-no-hooks", "after_task_execution", metadata, t.TempDir(), tc.taskClient, tl, qr, "")

	calls := qr.getCalls()
	assert.Empty(t, calls, "no hooks should be executed when metadata is empty")
}

// TestRunTask_WithAfterHooks verifies that after_task_execution hooks are
// executed when runTask completes with a status transition.
func TestRunTask_WithAfterHooks(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hooks := []hookEntry{
		{
			ID:      "hook-pr",
			SkillID: "skill-create-pr",
			Trigger: "after_task_execution",
			Order:   1,
			Name:    "create-pr",
			Content: "Create a PR.",
		},
	}
	hooksJSON, err := json.Marshal(hooks)
	require.NoError(t, err)

	metadata := baseMetadata("Develop", `[{"name":"Review"}]`)
	metadata["_hooks"] = string(hooksJSON)

	callIdx := 0
	qr := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			// First call: main task execution
			{Result: makeResult("Done developing.\nNEXT_STATUS: Review")},
			// Second call: create-pr hook
			{Result: makeResult("PR created.\nTASK_METADATA: pr_url=https://github.com/test/repo/pull/99")},
		},
	}
	_ = callIdx

	permCache := newPermissionCache("test", tc.agentClient)
	scpCache := newSingleCommandPermissionCache("test", tc.agentClient)

	runTask(ctx, tc.agentClient, tc.taskClient, tc.interClient,
		"agent-mgr-1", "task-with-hooks", "instructions", metadata,
		t.TempDir(), permCache, scpCache, qr, func() bool { return false })

	// Verify both main task and hook were executed
	calls := qr.getCalls()
	require.GreaterOrEqual(t, len(calls), 2, "expected at least 2 QueryRunner calls")

	// Find the hook call
	var hookCallFound bool

	for _, c := range calls {
		if c.Label == "hook_create-pr" {
			hookCallFound = true
			break
		}
	}

	assert.True(t, hookCallFound, "create-pr hook should have been executed")

	// Verify status transition still happened
	tc.taskHandler.mu.Lock()
	defer tc.taskHandler.mu.Unlock()

	require.Len(t, tc.taskHandler.updateTaskStatusReqs, 1)
	assert.Equal(t, "Review", tc.taskHandler.updateTaskStatusReqs[0].GetStatusId())
}
