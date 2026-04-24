package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/sourcegraph/conc"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// mockAgentManagerClient is a minimal mock for testing handlePermissionRequest.
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

func (m *mockAgentManagerClient) SyncClaudeSettings(_ context.Context, _ *connect.Request[v1.SyncClaudeSettingsAgentRequest]) (*connect.Response[v1.SyncClaudeSettingsAgentResponse], error) {
	return connect.NewResponse(&v1.SyncClaudeSettingsAgentResponse{
		Settings: &v1.ClaudeSettings{},
	}), nil
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
		nil, scpCache, nil,
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

	ctx := t.Context()
	waiter := newInteractionWaiter()

	// Run in goroutine since it blocks waiting for response.
	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)

	var wg conc.WaitGroup
	wg.Go(func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "cd /home && npm test"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache, nil,
		)
		resultCh <- result

		errCh <- err
	})

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	inter := mock.interactions[0]

	// Bash tools should have 3 options.
	if len(inter.GetOptions()) != 3 {
		t.Errorf("expected 3 options, got %d", len(inter.GetOptions()))
	}

	if inter.GetOptions()[0].GetValue() != "allow" {
		t.Errorf("expected first option 'allow', got %q", inter.GetOptions()[0].GetValue())
	}

	if inter.GetOptions()[1].GetValue() != "always_allow_command" {
		t.Errorf("expected second option 'always_allow_command', got %q", inter.GetOptions()[1].GetValue())
	}

	if inter.GetOptions()[2].GetValue() != "deny" {
		t.Errorf("expected third option 'deny', got %q", inter.GetOptions()[2].GetValue())
	}

	// Verify metadata contains parsed command info.
	if inter.GetMetadata() == "" {
		t.Fatal("expected metadata to be set")
	}

	var meta bashPermissionMetadata

	err := json.Unmarshal([]byte(inter.GetMetadata()), &meta)
	if err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}

	if len(meta.ParsedCommands) != 2 {
		t.Errorf("expected 2 parsed commands, got %d", len(meta.ParsedCommands))
	}

	if !meta.ParsedCommands[0].Matched {
		t.Error("expected first command (cd /home) to be matched")
	}

	if meta.ParsedCommands[1].Matched {
		t.Error("expected second command (npm test) to be unmatched")
	}

	if meta.ParsedCommands[1].SuggestedPattern == "" {
		t.Error("expected suggested pattern for unmatched command")
	}

	// Respond with "allow".
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "allow",
	})

	result := <-resultCh

	if err = <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}
}

func TestHandlePermissionRequest_NonBashToolOptions(t *testing.T) {
	mock := &mockAgentManagerClient{}

	ctx := t.Context()
	waiter := newInteractionWaiter()

	var wg conc.WaitGroup
	wg.Go(func() {
		handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Write", map[string]any{"file_path": "/tmp/test.txt"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, nil, nil,
		)
	})

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	inter := mock.interactions[0]

	// Non-Bash tools should have 2 options: Allow and Deny.
	if len(inter.GetOptions()) != 2 {
		t.Errorf("expected 2 options for non-Bash tool, got %d", len(inter.GetOptions()))
	}

	if inter.GetOptions()[0].GetValue() != "allow" {
		t.Errorf("expected first option 'allow', got %q", inter.GetOptions()[0].GetValue())
	}

	if inter.GetOptions()[1].GetValue() != "deny" {
		t.Errorf("expected second option 'deny', got %q", inter.GetOptions()[1].GetValue())
	}

	// Should have no metadata.
	if inter.GetMetadata() != "" {
		t.Errorf("expected empty metadata for non-Bash tool, got %q", inter.GetMetadata())
	}
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

	var wg conc.WaitGroup
	wg.Go(func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "cd /home && npm test"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache, nil,
		)
		resultCh <- result

		errCh <- err
	})

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	// Build JSON response that the frontend would send.
	aacResp := alwaysAllowCommandResponse{
		Action: "always_allow_command",
		Rules: []alwaysAllowCommandResponseRule{
			{Pattern: "npm test", Type: "command"},
		},
	}
	respBytes, _ := json.Marshal(aacResp)

	// Simulate response.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: string(respBytes),
	})

	result := <-resultCh

	err := <-errCh
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}

	// Wait briefly for async RPC calls.
	time.Sleep(50 * time.Millisecond)

	// Verify AddSingleCommandPermission was called.
	if len(mock.addedPermissions) != 1 {
		t.Fatalf("expected 1 added permission, got %d", len(mock.addedPermissions))
	}

	perm := mock.addedPermissions[0]
	if perm.GetPattern() != "npm test" {
		t.Errorf("expected pattern 'npm test', got %q", perm.GetPattern())
	}

	if perm.GetType() != "command" {
		t.Errorf("expected type 'command', got %q", perm.GetType())
	}

	if perm.GetProjectName() != "test-project" {
		t.Errorf("expected project 'test-project', got %q", perm.GetProjectName())
	}
}

func TestHandlePermissionRequest_AlwaysAllowCommand_InvalidJSON(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)

	ctx := context.Background()
	waiter := newInteractionWaiter()

	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)

	var wg conc.WaitGroup
	wg.Go(func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "echo hello"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache, nil,
		)
		resultCh <- result

		errCh <- err
	})

	time.Sleep(50 * time.Millisecond)

	// Simulate non-JSON response that doesn't match any known action → deny.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "invalid-response",
	})

	result := <-resultCh

	err := <-errCh
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unknown response falls through to deny.
	if _, ok := result.(claudeagent.PermissionResultDeny); !ok {
		t.Fatalf("expected PermissionResultDeny for unknown response, got %T", result)
	}

	// No permissions should have been added.
	if len(mock.addedPermissions) != 0 {
		t.Errorf("expected no added permissions, got %d", len(mock.addedPermissions))
	}
}

func TestHandlePermissionRequest_AlwaysAllowCommand_WithRedirects(t *testing.T) {
	mock := &mockAgentManagerClient{}
	scpCache := newSingleCommandPermissionCache("test-project", mock)
	// No patterns cached.

	ctx := context.Background()
	waiter := newInteractionWaiter()

	resultCh := make(chan claudeagent.PermissionResult, 1)
	errCh := make(chan error, 1)

	var wg conc.WaitGroup
	wg.Go(func() {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Bash", map[string]any{"command": "echo hello > /dev/null"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, scpCache, nil,
		)
		resultCh <- result

		errCh <- err
	})

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 interaction, got %d", len(mock.interactions))
	}

	// Verify metadata includes redirect info.
	inter := mock.interactions[0]
	if inter.GetMetadata() == "" {
		t.Fatal("expected metadata to be set")
	}

	var meta bashPermissionMetadata

	err := json.Unmarshal([]byte(inter.GetMetadata()), &meta)
	if err != nil {
		t.Fatalf("failed to parse metadata: %v", err)
	}

	if len(meta.Redirects) != 1 {
		t.Errorf("expected 1 redirect, got %d", len(meta.Redirects))
	}

	if meta.Redirects[0].Path != "/dev/null" {
		t.Errorf("expected redirect path '/dev/null', got %q", meta.Redirects[0].Path)
	}

	// Respond with always_allow_command including both command and redirect rules.
	aacResp := alwaysAllowCommandResponse{
		Action: "always_allow_command",
		Rules: []alwaysAllowCommandResponseRule{
			{Pattern: "echo *", Type: "command"},
			{Pattern: "/dev/null", Type: "redirect"},
		},
	}
	respBytes, _ := json.Marshal(aacResp)

	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: string(respBytes),
	})

	result := <-resultCh

	if err = <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
		t.Fatalf("expected PermissionResultAllow, got %T", result)
	}

	time.Sleep(50 * time.Millisecond)

	// Two permissions should have been registered (command + redirect).
	if len(mock.addedPermissions) != 2 {
		t.Fatalf("expected 2 added permissions, got %d", len(mock.addedPermissions))
	}

	if mock.addedPermissions[0].GetType() != "command" {
		t.Errorf("expected first permission type 'command', got %q", mock.addedPermissions[0].GetType())
	}

	if mock.addedPermissions[1].GetType() != "redirect" {
		t.Errorf("expected second permission type 'redirect', got %q", mock.addedPermissions[1].GetType())
	}
}

func TestHandlePermissionRequest_SkillToolAutoAllow(t *testing.T) {
	mock := &mockAgentManagerClient{}
	ctx := context.Background()
	waiter := newInteractionWaiter()

	statusSkills := map[string]bool{
		"architect": true,
		"create-pr": true,
	}

	// Status execution skill and hook skill should be auto-allowed.
	for _, skill := range []string{"architect", "create-pr"} {
		result, err := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Skill", map[string]any{"skill": skill},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, nil, statusSkills,
		)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", skill, err)
		}

		if _, ok := result.(claudeagent.PermissionResultAllow); !ok {
			t.Fatalf("expected PermissionResultAllow for %s, got %T", skill, result)
		}
	}

	if len(mock.interactions) != 0 {
		t.Errorf("expected no permission interactions, got %d", len(mock.interactions))
	}

	// A skill that is not configured for this status should still go through
	// the permission flow (blocks waiting for a response).
	blockedResultCh := make(chan claudeagent.PermissionResult, 1)

	var wg conc.WaitGroup
	wg.Go(func() {
		result, _ := handlePermissionRequest(
			ctx, mock, "task-1", "agent-1",
			"Skill", map[string]any{"skill": "some-other-skill"},
			waiter, claudeagent.PermissionModeDefault,
			claudeagent.ToolPermissionContext{},
			nil, nil, statusSkills,
		)
		blockedResultCh <- result
	})

	time.Sleep(50 * time.Millisecond)

	if len(mock.interactions) != 1 {
		t.Fatalf("expected 1 permission interaction for non-status skill, got %d", len(mock.interactions))
	}
	// Respond so the goroutine exits cleanly.
	waiter.Deliver(&v1.Interaction{
		Id:       "test-interaction-id",
		Status:   v1.InteractionStatus_INTERACTION_STATUS_RESPONDED,
		Response: "deny",
	})
	<-blockedResultCh
}
