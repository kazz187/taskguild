package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

// mockAgentManagerClient is a minimal mock for testing handlePermissionRequest.
// It embeds the unimplemented handler to satisfy the interface.
type mockAgentManagerClient struct {
	taskguildv1connect.UnimplementedAgentManagerServiceHandler

	// interactions stores created interactions for inspection.
	interactions []*v1.CreateInteractionRequest

	// addedPermissions stores AddSingleCommandPermission calls.
	addedPermissions []*v1.AddSingleCommandPermissionRequest

	// listPermissions is returned by ListSingleCommandPermissions.
	listPermissions []*v1.SingleCommandPermission
}

func (m *mockAgentManagerClient) CreateInteraction(_ context.Context, req *connect.Request[v1.CreateInteractionRequest]) (*connect.Response[v1.CreateInteractionResponse], error) {
	m.interactions = append(m.interactions, req.Msg)
	return connect.NewResponse(&v1.CreateInteractionResponse{
		Interaction: &v1.Interaction{
			Id: "test-interaction-id",
		},
	}), nil
}

func (m *mockAgentManagerClient) AddSingleCommandPermission(_ context.Context, req *connect.Request[v1.AddSingleCommandPermissionRequest]) (*connect.Response[v1.AddSingleCommandPermissionResponse], error) {
	m.addedPermissions = append(m.addedPermissions, req.Msg)
	return connect.NewResponse(&v1.AddSingleCommandPermissionResponse{
		Permission: &v1.SingleCommandPermission{
			Id:      "perm-" + req.Msg.GetPattern(),
			Pattern: req.Msg.GetPattern(),
			Type:    req.Msg.GetType(),
			Label:   req.Msg.GetLabel(),
		},
	}), nil
}

func (m *mockAgentManagerClient) ListSingleCommandPermissions(_ context.Context, _ *connect.Request[v1.ListSingleCommandPermissionsAgentRequest]) (*connect.Response[v1.ListSingleCommandPermissionsAgentResponse], error) {
	return connect.NewResponse(&v1.ListSingleCommandPermissionsAgentResponse{
		Permissions: m.listPermissions,
	}), nil
}

func (m *mockAgentManagerClient) GetInteractionResponse(_ context.Context, _ *connect.Request[v1.GetInteractionResponseRequest]) (*connect.Response[v1.GetInteractionResponseResponse], error) {
	return connect.NewResponse(&v1.GetInteractionResponseResponse{}), nil
}

// Stub remaining methods that may be called.
func (m *mockAgentManagerClient) Subscribe(_ context.Context, _ *connect.Request[v1.AgentManagerSubscribeRequest]) (*connect.ServerStreamForClient[v1.AgentCommand], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (m *mockAgentManagerClient) Heartbeat(_ context.Context, _ *connect.Request[v1.HeartbeatRequest]) (*connect.Response[v1.HeartbeatResponse], error) {
	return connect.NewResponse(&v1.HeartbeatResponse{}), nil
}

func (m *mockAgentManagerClient) ClaimTask(_ context.Context, _ *connect.Request[v1.ClaimTaskRequest]) (*connect.Response[v1.ClaimTaskResponse], error) {
	return connect.NewResponse(&v1.ClaimTaskResponse{}), nil
}

func (m *mockAgentManagerClient) ReportTaskResult(_ context.Context, _ *connect.Request[v1.ReportTaskResultRequest]) (*connect.Response[v1.ReportTaskResultResponse], error) {
	return connect.NewResponse(&v1.ReportTaskResultResponse{}), nil
}

func (m *mockAgentManagerClient) ReportAgentStatus(_ context.Context, _ *connect.Request[v1.ReportAgentStatusRequest]) (*connect.Response[v1.ReportAgentStatusResponse], error) {
	return connect.NewResponse(&v1.ReportAgentStatusResponse{}), nil
}

func (m *mockAgentManagerClient) SyncPermissions(_ context.Context, _ *connect.Request[v1.SyncPermissionsRequest]) (*connect.Response[v1.SyncPermissionsResponse], error) {
	return connect.NewResponse(&v1.SyncPermissionsResponse{
		Permissions: &v1.PermissionSet{},
	}), nil
}

func (m *mockAgentManagerClient) SyncAgents(_ context.Context, _ *connect.Request[v1.SyncAgentsRequest]) (*connect.Response[v1.SyncAgentsResponse], error) {
	return connect.NewResponse(&v1.SyncAgentsResponse{}), nil
}

func (m *mockAgentManagerClient) SyncScripts(_ context.Context, _ *connect.Request[v1.SyncScriptsRequest]) (*connect.Response[v1.SyncScriptsResponse], error) {
	return connect.NewResponse(&v1.SyncScriptsResponse{}), nil
}

func (m *mockAgentManagerClient) ReportWorktreeList(_ context.Context, _ *connect.Request[v1.ReportWorktreeListRequest]) (*connect.Response[v1.ReportWorktreeListResponse], error) {
	return connect.NewResponse(&v1.ReportWorktreeListResponse{}), nil
}

func (m *mockAgentManagerClient) ReportWorktreeDeleteResult(_ context.Context, _ *connect.Request[v1.ReportWorktreeDeleteResultRequest]) (*connect.Response[v1.ReportWorktreeDeleteResultResponse], error) {
	return connect.NewResponse(&v1.ReportWorktreeDeleteResultResponse{}), nil
}

func (m *mockAgentManagerClient) ReportGitPullMainResult(_ context.Context, _ *connect.Request[v1.ReportGitPullMainResultRequest]) (*connect.Response[v1.ReportGitPullMainResultResponse], error) {
	return connect.NewResponse(&v1.ReportGitPullMainResultResponse{}), nil
}

func (m *mockAgentManagerClient) ReportCompareScriptsResult(_ context.Context, _ *connect.Request[v1.ReportCompareScriptsResultRequest]) (*connect.Response[v1.ReportCompareScriptsResultResponse], error) {
	return connect.NewResponse(&v1.ReportCompareScriptsResultResponse{}), nil
}

func (m *mockAgentManagerClient) ReportExecuteScriptResult(_ context.Context, _ *connect.Request[v1.ReportExecuteScriptResultRequest]) (*connect.Response[v1.ReportExecuteScriptResultResponse], error) {
	return connect.NewResponse(&v1.ReportExecuteScriptResultResponse{}), nil
}

func (m *mockAgentManagerClient) ListSingleCommandPermissionsAgent(_ context.Context, _ *connect.Request[v1.ListSingleCommandPermissionsAgentRequest]) (*connect.Response[v1.ListSingleCommandPermissionsAgentResponse], error) {
	return connect.NewResponse(&v1.ListSingleCommandPermissionsAgentResponse{
		Permissions: m.listPermissions,
	}), nil
}

func (m *mockAgentManagerClient) StreamTaskLogs(_ context.Context, _ *connect.Request[v1.StreamTaskLogsRequest]) (*connect.Response[v1.StreamTaskLogsResponse], error) {
	return connect.NewResponse(&v1.StreamTaskLogsResponse{}), nil
}

func TestHandlePermissionRequest_BashAutoAllow(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)
	scpCache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "git *", Type: "command"},
		{Id: "2", Pattern: "cd *", Type: "command"},
	})

	ctx := context.Background()
	waiter := newInteractionWaiter()

	result, err := handlePermissionRequest(
		ctx, mock, "task-1", "agent-1",
		"Bash", map[string]any{"command": "cd /home && git status"},
		waiter, claudeagent.PermissionModeDefault,
		claudeagent.ToolPermissionContext{},
		nil, scpCache,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}

	// No interaction should have been created.
	if len(mock.interactions) != 0 {
		t.Errorf("expected no interactions, got %d", len(mock.interactions))
	}
}

func TestHandlePermissionRequest_BashPartialMatch_CreatesInteraction(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)
	scpCache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "cd *", Type: "command"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	waiter := newInteractionWaiter()

	// Run handlePermissionRequest in a goroutine because it blocks waiting for response.
	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "cd /home && npm test"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache,
		)
		resultCh <- result
		errCh <- err
	}()

	// Wait briefly for the interaction to be created.
	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	inter := mock.interactions[0]

	// Verify interaction has 3 options for Bash.
	if len(inter.Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(inter.Options))
	}
	if inter.Options[0].Value != "allow" {
		t.Errorf("expected first option 'allow', got %q", inter.Options[0].Value)
	}
	if inter.Options[1].Value != "always_allow_command" {
		t.Errorf("expected second option 'always_allow_command', got %q", inter.Options[1].Value)
	}
	if inter.Options[2].Value != "deny" {
		t.Errorf("expected third option 'deny', got %q", inter.Options[2].Value)
	}

	// Verify metadata contains parsed command info.
	if inter.Metadata == "" {
		t.Error("expected metadata to be set")
	} else {
		var meta bashPermissionMetadata
		if err := json.Unmarshal([]byte(inter.Metadata), &meta); err != nil {
			t.Errorf("failed to parse metadata: %v", err)
		} else {
			if len(meta.ParsedCommands) != 2 {
				t.Errorf("expected 2 parsed commands, got %d", len(meta.ParsedCommands))
			}
			// First command (cd /home) should be matched.
			if !meta.ParsedCommands[0].Matched {
				t.Error("expected first command to be matched")
			}
			// Second command (npm test) should NOT be matched.
			if meta.ParsedCommands[1].Matched {
				t.Error("expected second command to be unmatched")
			}
			if meta.ParsedCommands[1].SuggestedPattern == "" {
				t.Error("expected suggested pattern for unmatched command")
			}
		}
	}

	// Simulate "allow" response.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "allow",
	})

	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}

	cancel()
}

func TestHandlePermissionRequest_NonBashToolOptions(t *testing.T) {
	mock := &mockAgentManagerClient{}

	ctx, cancel := context.WithCancel(context.Background())
	waiter := newInteractionWaiter()

	go func() {
		handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Write", map[string]any{"file_path": "/tmp/test.txt"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, nil,
		)
	}()

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		cancel()
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	inter := mock.interactions[0]

	// Non-Bash tools should have 2 options: Allow and Deny.
	if len(inter.Options) != 2 {
		t.Errorf("expected 2 options for non-Bash tool, got %d", len(inter.Options))
	}
	if inter.Options[0].Value != "allow" {
		t.Errorf("expected first option 'allow', got %q", inter.Options[0].Value)
	}
	if inter.Options[1].Value != "deny" {
		t.Errorf("expected second option 'deny', got %q", inter.Options[1].Value)
	}

	// Should have no metadata.
	if inter.Metadata != "" {
		t.Errorf("expected empty metadata for non-Bash tool, got %q", inter.Metadata)
	}

	cancel()
}

func TestHandlePermissionRequest_AlwaysAllowCommand(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)
	scpCache.Update([]*v1.SingleCommandPermission{
		{Id: "1", Pattern: "cd *", Type: "command"},
	})

	ctx := context.Background()
	waiter := newInteractionWaiter()

	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "cd /home && npm test"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache,
		)
		resultCh <- result
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	// Build the metadata that the frontend would return (with suggested patterns).
	meta := bashPermissionMetadata{
		ParsedCommands: []commandCheckResult{
			{Command: "cd /home", Matched: true, MatchedPattern: "cd *"},
			{Command: "npm test", Matched: false, SuggestedPattern: "npm test"},
		},
	}
	metaBytes, _ := json.Marshal(meta)

	// Simulate "always_allow_command" response with metadata.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "always_allow_command",
		Metadata: string(metaBytes),
	})

	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}

	// Verify AddSingleCommandPermission was called for the unmatched command.
	if len(mock.addedPermissions) != 1 {
		t.Fatalf("expected 1 added permission, got %d", len(mock.addedPermissions))
	}
	perm := mock.addedPermissions[0]
	if perm.Pattern != "npm test" {
		t.Errorf("expected pattern 'npm test', got %q", perm.Pattern)
	}
	if perm.Type != "command" {
		t.Errorf("expected type 'command', got %q", perm.Type)
	}
	if perm.ProjectName != "test-project" {
		t.Errorf("expected project 'test-project', got %q", perm.ProjectName)
	}
}

func TestHandlePermissionRequest_AlwaysAllowCommand_InvalidMetadata(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)

	ctx := context.Background()
	waiter := newInteractionWaiter()

	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "echo hello"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache,
		)
		resultCh <- result
		errCh <- err
	}()

	time.Sleep(50 * time.Millisecond)

	// Simulate "always_allow_command" response with invalid metadata.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "always_allow_command",
		Metadata: "invalid-json",
	})

	result := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should fall back to simple allow.
	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow fallback, got %T", result)
	}

	// No permissions should have been added.
	if len(mock.addedPermissions) != 0 {
		t.Errorf("expected no added permissions on invalid metadata, got %d", len(mock.addedPermissions))
	}
}
