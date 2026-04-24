package script

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- fakes ---

type fakeRepository struct {
	mu      sync.Mutex
	scripts map[string]*Script
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{scripts: make(map[string]*Script)}
}

func (r *fakeRepository) Create(_ context.Context, s *Script) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts[s.ID] = s
	return nil
}

func (r *fakeRepository) Get(_ context.Context, id string) (*Script, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.scripts[id]
	if !ok {
		return nil, fmt.Errorf("script not found: %s", id)
	}
	return s, nil
}

func (r *fakeRepository) List(_ context.Context, projectID string, limit, offset int) ([]*Script, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*Script
	for _, s := range r.scripts {
		if s.ProjectID == projectID {
			result = append(result, s)
		}
	}
	return result, len(result), nil
}

func (r *fakeRepository) FindByName(_ context.Context, projectID, name string) (*Script, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.scripts {
		if s.ProjectID == projectID && s.Name == name {
			return s, nil
		}
	}
	return nil, errors.New("script not found")
}

func (r *fakeRepository) Update(_ context.Context, s *Script) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts[s.ID] = s
	return nil
}

func (r *fakeRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.scripts, id)
	return nil
}

type fakeExecutionRequester struct {
	mu           sync.Mutex
	execRequests []execRequest
	stopRequests []stopRequest
	execErr      error
	stopErr      error
}

type execRequest struct {
	RequestID string
	ProjectID string
	Script    *Script
}

type stopRequest struct {
	ProjectID string
	RequestID string
}

func (f *fakeExecutionRequester) RequestScriptExecution(requestID string, projectID string, sc *Script) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execRequests = append(f.execRequests, execRequest{
		RequestID: requestID,
		ProjectID: projectID,
		Script:    sc,
	})
	return f.execErr
}

func (f *fakeExecutionRequester) RequestScriptStop(projectID string, requestID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopRequests = append(f.stopRequests, stopRequest{
		ProjectID: projectID,
		RequestID: requestID,
	})
	return f.stopErr
}

// helpers

func seedScript(repo *fakeRepository, id, projectID, name string) *Script {
	sc := &Script{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		Filename:  name + ".sh",
		Content:   "#!/bin/sh\necho hello",
	}
	repo.scripts[id] = sc
	return sc
}

func newTestServer() (*Server, *fakeRepository, *fakeExecutionRequester, *ScriptExecutionBroker) {
	repo := newFakeRepository()
	execReq := &fakeExecutionRequester{}
	broker := NewScriptExecutionBroker()
	srv := NewServer(repo, execReq, broker, nil, nil)
	return srv, repo, execReq, broker
}

// --- ExecuteScript ---

func TestExecuteScript_Success(t *testing.T) {
	srv, repo, execReq, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	resp, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}

	requestID := resp.Msg.GetRequestId()
	if requestID == "" {
		t.Fatal("expected non-empty requestID")
	}

	// Verify broker has registered the execution
	ch, unsub := broker.Subscribe(requestID)
	defer unsub()
	if ch == nil {
		t.Fatal("expected execution to be registered in broker")
	}

	// Verify execution request was sent to agent
	execReq.mu.Lock()
	defer execReq.mu.Unlock()
	if len(execReq.execRequests) != 1 {
		t.Fatalf("expected 1 exec request, got %d", len(execReq.execRequests))
	}
	if execReq.execRequests[0].RequestID != requestID {
		t.Errorf("exec request has wrong requestID: %q", execReq.execRequests[0].RequestID)
	}
	if execReq.execRequests[0].ProjectID != "proj-1" {
		t.Errorf("exec request has wrong projectID: %q", execReq.execRequests[0].ProjectID)
	}
}

func TestExecuteScript_ScriptNotFound(t *testing.T) {
	srv, _, _, _ := newTestServer()

	_, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected error for missing script")
	}
}

func TestExecuteScript_ExecRequestFails_BrokerCleaned(t *testing.T) {
	srv, repo, execReq, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	execReq.execErr = errors.New("agent not connected")

	_, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err == nil {
		t.Fatal("expected error when execReq fails")
	}

	// Broker should have been cleaned up — no active executions
	if broker.ActiveCount() != 0 {
		t.Fatalf("expected 0 active executions after failure, got %d", broker.ActiveCount())
	}
}

func TestExecuteScript_RejectedWhileDraining(t *testing.T) {
	srv, repo, _, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	broker.SetDraining(true)

	_, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err == nil {
		t.Fatal("expected error when draining")
	}
}

// --- StopScriptExecution ---

func TestStopScriptExecution_Success(t *testing.T) {
	srv, _, execReq, broker := newTestServer()
	broker.RegisterExecution("req-1", "sc-1", "proj-1")

	_, err := srv.StopScriptExecution(context.Background(), connect.NewRequest(&taskguildv1.StopScriptExecutionRequest{
		RequestId: "req-1",
	}))
	if err != nil {
		t.Fatalf("StopScriptExecution failed: %v", err)
	}

	execReq.mu.Lock()
	defer execReq.mu.Unlock()
	if len(execReq.stopRequests) != 1 {
		t.Fatalf("expected 1 stop request, got %d", len(execReq.stopRequests))
	}
	if execReq.stopRequests[0].ProjectID != "proj-1" {
		t.Errorf("stop request has wrong projectID: %q", execReq.stopRequests[0].ProjectID)
	}
	if execReq.stopRequests[0].RequestID != "req-1" {
		t.Errorf("stop request has wrong requestID: %q", execReq.stopRequests[0].RequestID)
	}
}

func TestStopScriptExecution_EmptyRequestID(t *testing.T) {
	srv, _, _, _ := newTestServer()

	_, err := srv.StopScriptExecution(context.Background(), connect.NewRequest(&taskguildv1.StopScriptExecutionRequest{
		RequestId: "",
	}))
	if err == nil {
		t.Fatal("expected error for empty request_id")
	}
}

func TestStopScriptExecution_UnknownRequestID(t *testing.T) {
	srv, _, _, _ := newTestServer()

	_, err := srv.StopScriptExecution(context.Background(), connect.NewRequest(&taskguildv1.StopScriptExecutionRequest{
		RequestId: "unknown",
	}))
	if err == nil {
		t.Fatal("expected error for unknown request_id")
	}
}

// --- ListActiveExecutions ---

func TestListActiveExecutions(t *testing.T) {
	srv, _, _, broker := newTestServer()
	broker.RegisterExecution("req-1", "sc-1", "proj-1")
	broker.RegisterExecution("req-2", "sc-2", "proj-1")
	broker.RegisterExecution("req-3", "sc-3", "proj-2")
	broker.CompleteExecution("req-2", true, 0, nil, "", false)

	resp, err := srv.ListActiveExecutions(context.Background(), connect.NewRequest(&taskguildv1.ListActiveExecutionsRequest{
		ProjectId: "proj-1",
	}))
	if err != nil {
		t.Fatalf("ListActiveExecutions failed: %v", err)
	}
	if len(resp.Msg.GetExecutions()) != 2 {
		t.Fatalf("expected 2 executions for proj-1, got %d", len(resp.Msg.GetExecutions()))
	}
}

// --- End-to-end: ExecuteScript → agent reports via broker → subscriber receives ---

func TestEndToEnd_ExecuteAndReceiveViaSubscriber(t *testing.T) {
	srv, repo, _, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	// 1. Frontend triggers execution
	execResp, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}
	requestID := execResp.Msg.GetRequestId()

	// 2. Frontend subscribes to the stream (via broker directly, since
	//    connect.ServerStream requires HTTP infrastructure to construct)
	ch, unsub := broker.Subscribe(requestID)
	defer unsub()
	if ch == nil {
		t.Fatal("expected non-nil channel for new execution")
	}

	// 3. Simulate agent sending output chunks (ReportScriptOutputChunk → broker.PushOutput)
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "deploying...\n"},
	})
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, Text: "warning: disk low\n"},
	})
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "done!\n"},
	})

	// 4. Simulate agent reporting completion (ReportScriptExecutionResult → broker.CompleteExecution)
	fullLog := []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "deploying...\n"},
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, Text: "warning: disk low\n"},
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "done!\n"},
	}
	broker.CompleteExecution(requestID, true, 0, fullLog, "", false)

	// 5. Collect all events from the subscriber channel
	var events []*taskguildv1.ScriptExecutionEvent
	timeout := time.After(2 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, evt)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	// 6. Verify: 3 output events + 1 completion event = 4 total
	if len(events) != 4 {
		t.Fatalf("expected 4 events (3 output + 1 complete), got %d", len(events))
	}

	// Verify output events
	expectedTexts := []string{"deploying...\n", "warning: disk low\n", "done!\n"}
	expectedStreams := []taskguildv1.ScriptLogStream{
		taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT,
		taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR,
		taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT,
	}
	for i := range 3 {
		out, ok := events[i].GetEvent().(*taskguildv1.ScriptExecutionEvent_Output)
		if !ok {
			t.Fatalf("event %d: expected output event", i)
		}
		if len(out.Output.GetEntries()) != 1 {
			t.Fatalf("event %d: expected 1 entry, got %d", i, len(out.Output.GetEntries()))
		}
		if out.Output.GetEntries()[0].GetText() != expectedTexts[i] {
			t.Errorf("event %d: expected text %q, got %q", i, expectedTexts[i], out.Output.GetEntries()[0].GetText())
		}
		if out.Output.GetEntries()[0].GetStream() != expectedStreams[i] {
			t.Errorf("event %d: expected stream %v, got %v", i, expectedStreams[i], out.Output.GetEntries()[0].GetStream())
		}
	}

	// Verify completion event
	comp, ok := events[3].GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete)
	if !ok {
		t.Fatal("event 3: expected complete event")
	}
	if !comp.Complete.GetSuccess() {
		t.Error("expected success=true")
	}
	if comp.Complete.GetExitCode() != 0 {
		t.Errorf("expected exitCode=0, got %d", comp.Complete.GetExitCode())
	}
	if len(comp.Complete.GetLogEntries()) != 3 {
		t.Errorf("expected 3 log entries in completion, got %d", len(comp.Complete.GetLogEntries()))
	}

	// 7. Verify execution shows as completed in list
	listResp, err := srv.ListActiveExecutions(context.Background(), connect.NewRequest(&taskguildv1.ListActiveExecutionsRequest{
		ProjectId: "proj-1",
	}))
	if err != nil {
		t.Fatalf("ListActiveExecutions failed: %v", err)
	}
	if len(listResp.Msg.GetExecutions()) != 1 {
		t.Fatalf("expected 1 execution in list, got %d", len(listResp.Msg.GetExecutions()))
	}
	if !listResp.Msg.GetExecutions()[0].GetCompleted() {
		t.Error("expected execution to be marked completed")
	}
}

func TestEndToEnd_ExecuteFailure(t *testing.T) {
	srv, repo, _, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	// Execute
	execResp, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}
	requestID := execResp.Msg.GetRequestId()

	// Subscribe
	ch, unsub := broker.Subscribe(requestID)
	defer unsub()

	// Agent sends some output then fails
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, Text: "error: permission denied\n"},
	})
	broker.CompleteExecution(requestID, false, 126, nil, "permission denied", false)

	// Collect events
	var events []*taskguildv1.ScriptExecutionEvent
	timeout := time.After(2 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, evt)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	comp, ok := events[1].GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete)
	if !ok {
		t.Fatal("event 1: expected complete event")
	}
	if comp.Complete.GetSuccess() {
		t.Error("expected success=false")
	}
	if comp.Complete.GetExitCode() != 126 {
		t.Errorf("expected exitCode=126, got %d", comp.Complete.GetExitCode())
	}
	if comp.Complete.GetErrorMessage() != "permission denied" {
		t.Errorf("expected errorMessage=%q, got %q", "permission denied", comp.Complete.GetErrorMessage())
	}
}

func TestEndToEnd_StoppedByUser(t *testing.T) {
	srv, repo, _, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	// Execute
	execResp, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}
	requestID := execResp.Msg.GetRequestId()

	// Subscribe
	ch, unsub := broker.Subscribe(requestID)
	defer unsub()

	// Agent reports user-stopped execution
	broker.CompleteExecution(requestID, false, -1, nil, "Stopped by user", true)

	// Collect events
	var events []*taskguildv1.ScriptExecutionEvent
	timeout := time.After(2 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, evt)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	comp, ok := events[0].GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete)
	if !ok {
		t.Fatal("event 0: expected complete event")
	}
	if comp.Complete.GetSuccess() {
		t.Error("expected success=false")
	}
	if !comp.Complete.GetStoppedByUser() {
		t.Error("expected stoppedByUser=true")
	}
}

func TestEndToEnd_LateJoinerGetsFullReplay(t *testing.T) {
	srv, repo, _, broker := newTestServer()
	seedScript(repo, "sc-1", "proj-1", "deploy")

	// Execute
	execResp, err := srv.ExecuteScript(context.Background(), connect.NewRequest(&taskguildv1.ExecuteScriptRequest{
		ScriptId: "sc-1",
	}))
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}
	requestID := execResp.Msg.GetRequestId()

	// Agent sends output and completes BEFORE anyone subscribes
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "line1\n"},
	})
	broker.PushOutput(requestID, []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: "line2\n"},
	})
	broker.CompleteExecution(requestID, true, 0, nil, "", false)

	// Late joiner subscribes after completion (e.g. page reload)
	ch, _ := broker.Subscribe(requestID)
	if ch == nil {
		t.Fatal("expected non-nil channel for completed execution")
	}

	var events []*taskguildv1.ScriptExecutionEvent
	timeout := time.After(2 * time.Second)
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				goto done
			}
			events = append(events, evt)
		case <-timeout:
			t.Fatal("timed out waiting for events")
		}
	}
done:

	// Should get all 3 events: 2 output + 1 complete
	if len(events) != 3 {
		t.Fatalf("expected 3 events for late joiner, got %d", len(events))
	}

	if _, ok := events[0].GetEvent().(*taskguildv1.ScriptExecutionEvent_Output); !ok {
		t.Error("event 0: expected output")
	}
	if _, ok := events[1].GetEvent().(*taskguildv1.ScriptExecutionEvent_Output); !ok {
		t.Error("event 1: expected output")
	}
	if _, ok := events[2].GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete); !ok {
		t.Error("event 2: expected complete")
	}
}
