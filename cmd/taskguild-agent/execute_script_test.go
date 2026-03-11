package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// --- mock client ---

type scriptMockClient struct {
	taskguildv1connect.UnimplementedAgentManagerServiceHandler

	mu     sync.Mutex
	chunks []*v1.ReportScriptOutputChunkRequest
	result *v1.ReportScriptExecutionResultRequest
}

func (m *scriptMockClient) Subscribe(_ context.Context, _ *connect.Request[v1.AgentManagerSubscribeRequest]) (*connect.ServerStreamForClient[v1.AgentCommand], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, nil)
}

func (m *scriptMockClient) ReportScriptOutputChunk(_ context.Context, req *connect.Request[v1.ReportScriptOutputChunkRequest]) (*connect.Response[v1.ReportScriptOutputChunkResponse], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks = append(m.chunks, req.Msg)
	return connect.NewResponse(&v1.ReportScriptOutputChunkResponse{}), nil
}

func (m *scriptMockClient) ReportScriptExecutionResult(_ context.Context, req *connect.Request[v1.ReportScriptExecutionResultRequest]) (*connect.Response[v1.ReportScriptExecutionResultResponse], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.result = req.Msg
	return connect.NewResponse(&v1.ReportScriptExecutionResultResponse{}), nil
}

func (m *scriptMockClient) getChunks() []*v1.ReportScriptOutputChunkRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*v1.ReportScriptOutputChunkRequest, len(m.chunks))
	copy(result, m.chunks)
	return result
}

func (m *scriptMockClient) getResult() *v1.ReportScriptExecutionResultRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.result
}

func (m *scriptMockClient) allChunkEntries() []*v1.ScriptLogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var entries []*v1.ScriptLogEntry
	for _, c := range m.chunks {
		entries = append(entries, c.Entries...)
	}
	return entries
}

// --- chunkBuffer ---

func TestChunkBuffer_AppendAndDrain(t *testing.T) {
	var cb chunkBuffer
	cb.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, "line1\n")
	cb.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, "err1\n")

	entries := cb.drain()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Text != "line1\n" || entries[0].Stream != v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT {
		t.Errorf("entry 0: unexpected %v", entries[0])
	}
	if entries[1].Text != "err1\n" || entries[1].Stream != v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR {
		t.Errorf("entry 1: unexpected %v", entries[1])
	}

	// Drain again should be empty
	entries = cb.drain()
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after second drain, got %d", len(entries))
	}
}

func TestChunkBuffer_DrainEmpty(t *testing.T) {
	var cb chunkBuffer
	entries := cb.drain()
	if entries != nil {
		t.Errorf("expected nil from draining empty buffer, got %v", entries)
	}
}

// --- logEntryBuffer ---

func TestLogEntryBuffer_AppendAndEntries(t *testing.T) {
	var lb logEntryBuffer
	lb.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, "a\n")
	lb.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, "b\n")

	entries := lb.entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// entries() returns a new slice (shallow copy of pointers)
	if len(lb.entries()) != 2 {
		t.Error("calling entries() again should still return 2 entries")
	}
}

// --- flushLogEntries ---

func TestFlushLogEntries_SendsChunk(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var cb chunkBuffer
	cb.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, "hello\n")

	flushLogEntries(context.Background(), mock, cfg, "req-1", &cb)

	chunks := mock.getChunks()
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].RequestId != "req-1" {
		t.Errorf("expected requestID %q, got %q", "req-1", chunks[0].RequestId)
	}
	if chunks[0].ProjectName != "test-proj" {
		t.Errorf("expected projectName %q, got %q", "test-proj", chunks[0].ProjectName)
	}
	if len(chunks[0].Entries) != 1 || chunks[0].Entries[0].Text != "hello\n" {
		t.Errorf("unexpected entries: %v", chunks[0].Entries)
	}
}

func TestFlushLogEntries_EmptyBuffer_NoRPC(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var cb chunkBuffer

	flushLogEntries(context.Background(), mock, cfg, "req-1", &cb)

	if len(mock.getChunks()) != 0 {
		t.Error("expected no RPC call for empty buffer")
	}
}

// --- streamOutput ---

func TestStreamOutput_StdoutAndStderr(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var fullLog logEntryBuffer

	// Create pipes that simulate script output
	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()

	// Write output and close immediately
	stdoutW.WriteString("stdout line1\nstdout line2\n")
	stdoutW.Close()
	stderrW.WriteString("stderr line1\n")
	stderrW.Close()

	streamOutput(context.Background(), mock, cfg, "req-1", stdoutR, stderrR, &fullLog)

	// Verify full log captured all lines
	entries := fullLog.entries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 log entries, got %d", len(entries))
	}

	// Collect all text
	var stdoutTexts, stderrTexts []string
	for _, e := range entries {
		if e.Stream == v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT {
			stdoutTexts = append(stdoutTexts, e.Text)
		} else {
			stderrTexts = append(stderrTexts, e.Text)
		}
	}
	if len(stdoutTexts) != 2 {
		t.Errorf("expected 2 stdout entries, got %d", len(stdoutTexts))
	}
	if len(stderrTexts) != 1 {
		t.Errorf("expected 1 stderr entry, got %d", len(stderrTexts))
	}

	// Verify chunks were sent to server
	chunkEntries := mock.allChunkEntries()
	if len(chunkEntries) != 3 {
		t.Errorf("expected 3 entries sent via RPC, got %d", len(chunkEntries))
	}
}

func TestStreamOutput_EmptyPipes(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var fullLog logEntryBuffer

	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	stdoutW.Close()
	stderrW.Close()

	streamOutput(context.Background(), mock, cfg, "req-1", stdoutR, stderrR, &fullLog)

	entries := fullLog.entries()
	if len(entries) != 0 {
		t.Errorf("expected 0 log entries for empty pipes, got %d", len(entries))
	}
}

func TestStreamOutput_LargeOutput(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var fullLog logEntryBuffer

	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()

	// Write many lines
	go func() {
		for i := 0; i < 100; i++ {
			stdoutW.WriteString("line of output\n")
		}
		stdoutW.Close()
	}()
	stderrW.Close()

	streamOutput(context.Background(), mock, cfg, "req-1", stdoutR, stderrR, &fullLog)

	entries := fullLog.entries()
	if len(entries) != 100 {
		t.Fatalf("expected 100 log entries, got %d", len(entries))
	}

	// All entries should have been sent via chunks
	chunkEntries := mock.allChunkEntries()
	if len(chunkEntries) != 100 {
		t.Errorf("expected 100 entries sent via RPC, got %d", len(chunkEntries))
	}
}

func TestStreamOutput_ContextCancelled(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var fullLog logEntryBuffer

	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		streamOutput(ctx, mock, cfg, "req-1", stdoutR, stderrR, &fullLog)
		close(done)
	}()

	// Cancel context while pipes are still open
	cancel()

	select {
	case <-done:
		// OK, streamOutput returned
	case <-time.After(2 * time.Second):
		t.Fatal("streamOutput did not return after context cancellation")
	}

	// Clean up pipes
	stdoutW.Close()
	stderrW.Close()
	stdoutR.Close()
	stderrR.Close()
}

// --- resolveScriptPath ---

func TestResolveScriptPath_LocalFile(t *testing.T) {
	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, ".claude", "scripts")
	os.MkdirAll(scriptsDir, 0755)
	os.WriteFile(filepath.Join(scriptsDir, "test.sh"), []byte("#!/bin/sh\necho ok"), 0755)

	path, err := resolveScriptPath(workDir, "test.sh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != filepath.Join(scriptsDir, "test.sh") {
		t.Errorf("expected local path, got %q", path)
	}
}

func TestResolveScriptPath_LocalFileWithoutExecPermission(t *testing.T) {
	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, ".claude", "scripts")
	os.MkdirAll(scriptsDir, 0755)
	os.WriteFile(filepath.Join(scriptsDir, "test.sh"), []byte("#!/bin/sh\necho ok"), 0644)

	path, err := resolveScriptPath(workDir, "test.sh")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify execute permission was added
	info, _ := os.Stat(path)
	if info.Mode()&0111 == 0 {
		t.Error("expected execute permission to be set")
	}
	_ = path
}

func TestResolveScriptPath_FileNotFound(t *testing.T) {
	workDir := t.TempDir()

	_, err := resolveScriptPath(workDir, "missing.sh")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "not found locally") {
		t.Errorf("expected 'not found locally' in error, got: %q", err.Error())
	}
}

// --- writeTestScript helper ---

// writeTestScript creates a script file at .claude/scripts/{filename} and returns the scripts dir.
func writeTestScript(t *testing.T, workDir, filename, content string) {
	t.Helper()
	scriptsDir := filepath.Join(workDir, ".claude", "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		t.Fatalf("failed to create scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, filename), []byte(content), 0755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}
}

// --- handleExecuteScript (end-to-end) ---

func TestHandleExecuteScript_Success(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "hello.sh", "#!/bin/sh\necho hello\necho world")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-test-1",
		ScriptId:  "sc-1",
		Filename:  "hello.sh",
	}

	handleExecuteScript(context.Background(), mock, cfg, cmd)

	// Verify result was reported
	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.RequestId != "req-test-1" {
		t.Errorf("expected requestID %q, got %q", "req-test-1", result.RequestId)
	}
	if result.ProjectName != "test-proj" {
		t.Errorf("expected projectName %q, got %q", "test-proj", result.ProjectName)
	}
	if result.ScriptId != "sc-1" {
		t.Errorf("expected scriptID %q, got %q", "sc-1", result.ScriptId)
	}
	if !result.Success {
		t.Errorf("expected success=true, got false. error: %s", result.ErrorMessage)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exitCode=0, got %d", result.ExitCode)
	}

	// Verify output was streamed
	chunkEntries := mock.allChunkEntries()
	var allText string
	for _, e := range chunkEntries {
		allText += e.Text
	}
	if !strings.Contains(allText, "hello") || !strings.Contains(allText, "world") {
		t.Errorf("expected stdout to contain 'hello' and 'world', got: %q", allText)
	}

	// Verify full log in result
	var resultText string
	for _, e := range result.LogEntries {
		resultText += e.Text
	}
	if !strings.Contains(resultText, "hello") || !strings.Contains(resultText, "world") {
		t.Errorf("expected result log to contain 'hello' and 'world', got: %q", resultText)
	}

	// Verify the local script file was NOT deleted
	scriptPath := filepath.Join(workDir, ".claude", "scripts", "hello.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Error("local script file should not be deleted after execution")
	}
}

func TestHandleExecuteScript_ScriptFails(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "fail.sh", "#!/bin/sh\necho failing >&2\nexit 42")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-fail",
		ScriptId:  "sc-2",
		Filename:  "fail.sh",
	}

	handleExecuteScript(context.Background(), mock, cfg, cmd)

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false")
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exitCode=42, got %d", result.ExitCode)
	}
	if result.StoppedByUser {
		t.Error("expected stoppedByUser=false")
	}

	// Verify stderr was captured
	var stderrText string
	for _, e := range mock.allChunkEntries() {
		if e.Stream == v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR {
			stderrText += e.Text
		}
	}
	if !strings.Contains(stderrText, "failing") {
		t.Errorf("expected stderr to contain 'failing', got: %q", stderrText)
	}
}

func TestHandleExecuteScript_StdoutAndStderrInterleaved(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "mixed.sh", "#!/bin/sh\necho out1\necho err1 >&2\necho out2\necho err2 >&2")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-interleave",
		ScriptId:  "sc-3",
		Filename:  "mixed.sh",
	}

	handleExecuteScript(context.Background(), mock, cfg, cmd)

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if !result.Success {
		t.Errorf("expected success=true, error: %s", result.ErrorMessage)
	}

	// Verify both streams captured
	var stdoutCount, stderrCount int
	for _, e := range result.LogEntries {
		switch e.Stream {
		case v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT:
			stdoutCount++
		case v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR:
			stderrCount++
		}
	}
	if stdoutCount != 2 {
		t.Errorf("expected 2 stdout entries in result, got %d", stdoutCount)
	}
	if stderrCount != 2 {
		t.Errorf("expected 2 stderr entries in result, got %d", stderrCount)
	}
}

func TestHandleExecuteScript_ParentContextCancelled(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "long.sh", "#!/bin/sh\nsleep 60")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-parent-cancel",
		ScriptId:  "sc-4",
		Filename:  "long.sh",
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		handleExecuteScript(ctx, mock, cfg, cmd)
		close(done)
	}()

	// Wait a moment for the script to start, then cancel the parent context
	// (simulating SIGINT/SIGTERM or hot-reload — NOT a user-initiated stop).
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("handleExecuteScript did not return after context cancellation")
	}

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false for cancelled script")
	}
	if result.StoppedByUser {
		t.Error("expected stoppedByUser=false for parent context cancellation (not user-initiated)")
	}
}

func TestHandleExecuteScript_StoppedByUser(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "long.sh", "#!/bin/sh\nsleep 60")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-user-stop",
		ScriptId:  "sc-4b",
		Filename:  "long.sh",
	}

	done := make(chan struct{})
	go func() {
		handleExecuteScript(context.Background(), mock, cfg, cmd)
		close(done)
	}()

	// Wait for the script to register in runningScripts
	time.Sleep(200 * time.Millisecond)

	// Stop via handleStopScript (user-initiated stop)
	handleStopScript(&v1.StopScriptCommand{RequestId: "req-user-stop"})

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("handleExecuteScript did not return after user stop")
	}

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false for stopped script")
	}
	if !result.StoppedByUser {
		t.Error("expected stoppedByUser=true for user-initiated stop")
	}
}

func TestHandleExecuteScript_HotReloadDoesNotSetStoppedByUser(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "long.sh", "#!/bin/sh\nsleep 60")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-hotreload",
		ScriptId:  "sc-4c",
		Filename:  "long.sh",
	}

	done := make(chan struct{})
	go func() {
		handleExecuteScript(context.Background(), mock, cfg, cmd)
		close(done)
	}()

	// Wait for the script to register in runningScripts
	time.Sleep(200 * time.Millisecond)

	// Cancel via runningScripts.cancels directly (mimicking SIGUSR1 handler
	// which iterates cancels without setting userStopped)
	runningScripts.mu.Lock()
	cancelFn, ok := runningScripts.cancels["req-hotreload"]
	runningScripts.mu.Unlock()
	if !ok {
		t.Fatal("expected script to be tracked in runningScripts")
	}
	cancelFn()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("handleExecuteScript did not return after hot-reload cancellation")
	}

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false for cancelled script")
	}
	if result.StoppedByUser {
		t.Error("expected stoppedByUser=false for hot-reload cancellation")
	}
}

func TestHandleExecuteScript_RunningScriptsTracked(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	writeTestScript(t, workDir, "slow.sh", "#!/bin/sh\nsleep 60")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-track",
		ScriptId:  "sc-5",
		Filename:  "slow.sh",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		// Signal when goroutine has entered handleExecuteScript
		close(started)
		handleExecuteScript(ctx, mock, cfg, cmd)
		close(done)
	}()
	<-started
	// Give it a moment to register in runningScripts
	time.Sleep(200 * time.Millisecond)

	// Verify the script is registered in runningScripts
	runningScripts.mu.Lock()
	_, tracked := runningScripts.cancels["req-track"]
	runningScripts.mu.Unlock()
	if !tracked {
		t.Error("expected script to be tracked in runningScripts")
	}

	// Stop via handleStopScript
	handleStopScript(&v1.StopScriptCommand{RequestId: "req-track"})

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("handleExecuteScript did not return after stop command")
	}

	// Verify cleanup
	runningScripts.mu.Lock()
	_, stillTracked := runningScripts.cancels["req-track"]
	runningScripts.mu.Unlock()
	if stillTracked {
		t.Error("expected script to be removed from runningScripts after completion")
	}
}

func TestHandleExecuteScript_RejectDuringHotReload(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	// Set reject mode
	scriptTracker.mu.Lock()
	scriptTracker.reject = true
	scriptTracker.mu.Unlock()
	defer func() {
		scriptTracker.mu.Lock()
		scriptTracker.reject = false
		scriptTracker.mu.Unlock()
	}()

	writeTestScript(t, workDir, "rejected.sh", "#!/bin/sh\necho should not run")

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-reject",
		ScriptId:  "sc-6",
		Filename:  "rejected.sh",
	}

	handleExecuteScript(context.Background(), mock, cfg, cmd)

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false for rejected script")
	}
	if !strings.Contains(result.ErrorMessage, "hot reload") {
		t.Errorf("expected error about hot reload, got: %q", result.ErrorMessage)
	}

	// No chunks should have been sent (script never ran)
	if len(mock.getChunks()) != 0 {
		t.Error("expected no output chunks for rejected script")
	}
}

func TestHandleExecuteScript_LocalFileNotFound(t *testing.T) {
	mock := &scriptMockClient{}
	workDir := t.TempDir()
	cfg := &config{
		ProjectName: "test-proj",
		WorkDir:     workDir,
	}

	cmd := &v1.ExecuteScriptCommand{
		RequestId: "req-missing",
		ScriptId:  "sc-7",
		Filename:  "missing.sh",
	}

	handleExecuteScript(context.Background(), mock, cfg, cmd)

	result := mock.getResult()
	if result == nil {
		t.Fatal("expected result to be reported")
	}
	if result.Success {
		t.Error("expected success=false for missing script")
	}
	if !strings.Contains(result.ErrorMessage, "not found locally") {
		t.Errorf("expected error about script not found, got: %q", result.ErrorMessage)
	}
}

// --- streamOutput with slow producer (verifies periodic flushing) ---

func TestStreamOutput_PeriodicFlush(t *testing.T) {
	mock := &scriptMockClient{}
	cfg := &config{ProjectName: "test-proj"}
	var fullLog logEntryBuffer

	stdoutR, stdoutW, _ := os.Pipe()
	stderrR, stderrW, _ := os.Pipe()
	stderrW.Close()

	done := make(chan struct{})
	go func() {
		streamOutput(context.Background(), mock, cfg, "req-1", stdoutR, stderrR, &fullLog)
		close(done)
	}()

	// Write a line, wait for flush interval, then write another
	io.WriteString(stdoutW, "first\n")
	time.Sleep(300 * time.Millisecond) // > outputFlushInterval (200ms)
	io.WriteString(stdoutW, "second\n")
	time.Sleep(300 * time.Millisecond)
	stdoutW.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("streamOutput did not return")
	}

	// Should have been sent in at least 2 separate chunks
	chunks := mock.getChunks()
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 separate chunk flushes, got %d", len(chunks))
	}
}
