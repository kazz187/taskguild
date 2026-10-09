package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// ---------------------------------------------------------------------------
// Mock QueryRunner
// ---------------------------------------------------------------------------

type mockQueryRunnerCall struct {
	Prompt  any
	Label   string
	TaskID  string
	WorkDir string
}

type mockQueryRunnerResult struct {
	Result *claudeagent.QueryResult
	Err    error
}

// mockQueryRunner returns pre-configured results in sequence.
// If more calls are made than results available, the last result is repeated.
type mockQueryRunner struct {
	mu      sync.Mutex
	calls   []mockQueryRunnerCall
	results []mockQueryRunnerResult
}

func (m *mockQueryRunner) RunQuerySync(
	ctx context.Context,
	prompt any,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, mockQueryRunnerCall{
		Prompt:  prompt,
		Label:   label,
		TaskID:  taskID,
		WorkDir: workDir,
	})

	idx := len(m.calls) - 1
	if idx >= len(m.results) {
		idx = len(m.results) - 1
	}

	if idx < 0 {
		return &claudeagent.QueryResult{
			Result: &claudeagent.ResultMessage{
				SessionID: "test-session",
			},
		}, nil
	}

	return m.results[idx].Result, m.results[idx].Err
}

func (m *mockQueryRunner) getCalls() []mockQueryRunnerCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	cp := make([]mockQueryRunnerCall, len(m.calls))
	copy(cp, m.calls)

	return cp
}

// makeResult builds a QueryResult from a simple text string.
func makeResult(text string) *claudeagent.QueryResult {
	return &claudeagent.QueryResult{
		Messages: []claudeagent.Message{},
		Result: &claudeagent.ResultMessage{
			Result:    text,
			SessionID: "test-session",
		},
	}
}

// makeErrorResult builds a QueryResult with IsError=true.
func makeErrorResult(text string) *claudeagent.QueryResult {
	return &claudeagent.QueryResult{
		Messages: []claudeagent.Message{},
		Result: &claudeagent.ResultMessage{
			Result:    text,
			IsError:   true,
			SessionID: "test-session",
		},
	}
}

// ---------------------------------------------------------------------------
// Mock connectrpc Handlers (httptest-based)
// ---------------------------------------------------------------------------

// testAgentManagerHandler embeds UnimplementedAgentManagerServiceHandler and
// overrides methods as needed via function fields.
type testAgentManagerHandler struct {
	taskguildv1connect.UnimplementedAgentManagerServiceHandler

	mu                    sync.Mutex
	reportAgentStatusReqs []*v1.ReportAgentStatusRequest
	reportTaskResultReqs  []*v1.ReportTaskResultRequest
	reportTaskLogReqs     []*v1.ReportTaskLogRequest
	createInteractionReqs []*v1.CreateInteractionRequest
}

func (h *testAgentManagerHandler) ReportAgentStatus(ctx context.Context, req *v1.ReportAgentStatusRequest) (*v1.ReportAgentStatusResponse, error) {
	h.mu.Lock()
	h.reportAgentStatusReqs = append(h.reportAgentStatusReqs, req)
	h.mu.Unlock()

	return &v1.ReportAgentStatusResponse{}, nil
}

func (h *testAgentManagerHandler) ReportTaskResult(ctx context.Context, req *v1.ReportTaskResultRequest) (*v1.ReportTaskResultResponse, error) {
	h.mu.Lock()
	h.reportTaskResultReqs = append(h.reportTaskResultReqs, req)
	h.mu.Unlock()

	return &v1.ReportTaskResultResponse{}, nil
}

func (h *testAgentManagerHandler) ReportTaskLog(ctx context.Context, req *v1.ReportTaskLogRequest) (*v1.ReportTaskLogResponse, error) {
	h.mu.Lock()
	h.reportTaskLogReqs = append(h.reportTaskLogReqs, req)
	h.mu.Unlock()

	return &v1.ReportTaskLogResponse{}, nil
}

func (h *testAgentManagerHandler) CreateInteraction(ctx context.Context, req *v1.CreateInteractionRequest) (*v1.CreateInteractionResponse, error) {
	h.mu.Lock()
	h.createInteractionReqs = append(h.createInteractionReqs, req)
	h.mu.Unlock()

	return &v1.CreateInteractionResponse{
		Interaction: &v1.Interaction{Id: "test-interaction"},
	}, nil
}

// testTaskHandler embeds UnimplementedTaskServiceHandler and overrides methods.
type testTaskHandler struct {
	taskguildv1connect.UnimplementedTaskServiceHandler

	mu                   sync.Mutex
	updateTaskReqs       []*v1.UpdateTaskRequest
	updateTaskStatusReqs []*v1.UpdateTaskStatusRequest
	createTaskReqs       []*v1.CreateTaskRequest
}

func (h *testTaskHandler) UpdateTask(ctx context.Context, req *v1.UpdateTaskRequest) (*v1.UpdateTaskResponse, error) {
	h.mu.Lock()
	h.updateTaskReqs = append(h.updateTaskReqs, req)
	h.mu.Unlock()

	return &v1.UpdateTaskResponse{}, nil
}

func (h *testTaskHandler) UpdateTaskStatus(ctx context.Context, req *v1.UpdateTaskStatusRequest) (*v1.UpdateTaskStatusResponse, error) {
	h.mu.Lock()
	h.updateTaskStatusReqs = append(h.updateTaskStatusReqs, req)
	h.mu.Unlock()

	return &v1.UpdateTaskStatusResponse{}, nil
}

func (h *testTaskHandler) CreateTask(ctx context.Context, req *v1.CreateTaskRequest) (*v1.CreateTaskResponse, error) {
	h.mu.Lock()
	h.createTaskReqs = append(h.createTaskReqs, req)
	h.mu.Unlock()

	return &v1.CreateTaskResponse{
		Task: &v1.Task{Id: "new-task-1"},
	}, nil
}

// testInteractionHandler embeds UnimplementedInteractionServiceHandler.
type testInteractionHandler struct {
	taskguildv1connect.UnimplementedInteractionServiceHandler
}

// SubscribeInteractions blocks until the context is canceled (simulating an
// idle stream with no events).
func (h *testInteractionHandler) SubscribeInteractions(ctx context.Context, req *v1.SubscribeInteractionsRequest, stream taskguildv1connect.InteractionServiceSubscribeInteractionsServerStream) error {
	<-ctx.Done()
	return ctx.Err()
}

// ---------------------------------------------------------------------------
// Test server setup
// ---------------------------------------------------------------------------

type testClients struct {
	agentClient taskguildv1connect.AgentManagerServiceClient
	taskClient  taskguildv1connect.TaskServiceClient
	interClient taskguildv1connect.InteractionServiceClient

	agentHandler *testAgentManagerHandler
	taskHandler  *testTaskHandler
	interHandler *testInteractionHandler

	server *httptest.Server
}

func newTestClients() *testClients {
	agentHandler := &testAgentManagerHandler{}
	taskHandler := &testTaskHandler{}
	interHandler := &testInteractionHandler{}

	rpcServer := connect.NewServer()
	taskguildv1connect.RegisterAgentManagerServiceHandler(rpcServer, agentHandler)
	taskguildv1connect.RegisterTaskServiceHandler(rpcServer, taskHandler)
	taskguildv1connect.RegisterInteractionServiceHandler(rpcServer, interHandler)

	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpcServer)

	server := httptest.NewUnstartedServer(mux)
	server.EnableHTTP2 = true
	server.StartTLS()

	rpcClient := connect.NewClient(connecthttp.NewTransport(server.Client(), server.URL))

	return &testClients{
		agentClient:  taskguildv1connect.NewAgentManagerServiceClient(rpcClient),
		taskClient:   taskguildv1connect.NewTaskServiceClient(rpcClient),
		interClient:  taskguildv1connect.NewInteractionServiceClient(rpcClient),
		agentHandler: agentHandler,
		taskHandler:  taskHandler,
		interHandler: interHandler,
		server:       server,
	}
}

func (tc *testClients) Close() {
	tc.server.Close()
}

// ---------------------------------------------------------------------------
// Test metadata helpers
// ---------------------------------------------------------------------------

func baseMetadata(statusName string, transitions string) map[string]string {
	return map[string]string{
		"_task_title":            "Test Task",
		"_task_description":      "Test description",
		"_current_status_name":   statusName,
		"_available_transitions": transitions,
		"_workflow_id":           "wf-test",
		"_project_id":            "proj-test",
		"_workflow_statuses":     `[{"name":"Plan"},{"name":"Develop"},{"name":"Review"},{"name":"Done"}]`,
	}
}
