package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

const (
	scriptExecutionTimeout = 5 * time.Minute
	outputFlushInterval    = 200 * time.Millisecond
)

// handleExecuteScript executes a script on the agent-manager machine and reports the result.
// Output is streamed to the server in real-time via ReportScriptOutputChunk RPCs.
func handleExecuteScript(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.ExecuteScriptCommand) {
	requestID := cmd.GetRequestId()
	scriptID := cmd.GetScriptId()
	filename := cmd.GetFilename()
	content := cmd.GetContent()

	log.Printf("executing script %s (request_id: %s, filename: %s)", scriptID, requestID, filename)

	reportResult := func(success bool, exitCode int32, stdout, stderr, errMsg string) {
		_, err := client.ReportScriptExecutionResult(ctx, connect.NewRequest(&v1.ReportScriptExecutionResultRequest{
			RequestId:    requestID,
			ProjectName:  cfg.ProjectName,
			ScriptId:     scriptID,
			Success:      success,
			ExitCode:     exitCode,
			Stdout:       stdout,
			Stderr:       stderr,
			ErrorMessage: errMsg,
		}))
		if err != nil {
			log.Printf("failed to report script execution result: %v", err)
		}
	}

	// Register this script execution with the tracker so the SIGUSR1
	// (hot-reload) handler waits for it to complete before shutting down.
	// The mutex ensures atomicity between the reject check and wg.Add,
	// preventing a race where SIGUSR1 handler calls wg.Wait() between
	// our check and Add.
	scriptTracker.mu.Lock()
	if scriptTracker.reject {
		scriptTracker.mu.Unlock()
		log.Printf("script %s rejected: agent is shutting down for hot reload (request_id: %s)", filename, requestID)
		reportResult(false, -1, "", "", "script execution rejected: agent is shutting down for hot reload")
		return
	}
	scriptTracker.wg.Add(1)
	scriptTracker.mu.Unlock()
	defer scriptTracker.wg.Done()

	// Write script to temporary file.
	tmpDir := filepath.Join(cfg.WorkDir, ".claude", "scripts", ".tmp")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		reportResult(false, -1, "", "", fmt.Sprintf("failed to create temp directory: %v", err))
		return
	}

	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("exec_%s_%s", requestID, filename))
	if err := os.WriteFile(tmpFile, []byte(content), 0755); err != nil {
		reportResult(false, -1, "", "", fmt.Sprintf("failed to write temp script: %v", err))
		return
	}
	defer os.Remove(tmpFile)

	// Execute the script with piped stdout/stderr for streaming.
	execCtx, cancel := context.WithTimeout(ctx, scriptExecutionTimeout)
	defer cancel()

	execCmd := exec.CommandContext(execCtx, "/bin/sh", tmpFile)
	execCmd.Dir = cfg.WorkDir
	execCmd.Env = append(os.Environ(),
		"TASKGUILD_PROJECT_NAME="+cfg.ProjectName,
		"TASKGUILD_SCRIPT_ID="+scriptID,
		"TASKGUILD_SCRIPT_FILENAME="+filename,
		"TASKGUILD_WORK_DIR="+cfg.WorkDir,
	)

	stdoutPipe, err := execCmd.StdoutPipe()
	if err != nil {
		reportResult(false, -1, "", "", fmt.Sprintf("failed to create stdout pipe: %v", err))
		return
	}
	stderrPipe, err := execCmd.StderrPipe()
	if err != nil {
		reportResult(false, -1, "", "", fmt.Sprintf("failed to create stderr pipe: %v", err))
		return
	}

	if err := execCmd.Start(); err != nil {
		reportResult(false, -1, "", "", fmt.Sprintf("failed to start script: %v", err))
		return
	}

	// Stream output in real-time.
	var fullStdout, fullStderr bytes.Buffer
	streamOutput(ctx, client, cfg, requestID, stdoutPipe, stderrPipe, &fullStdout, &fullStderr)

	// Wait for the command to finish.
	cmdErr := execCmd.Wait()

	stdout := fullStdout.String()
	stderr := fullStderr.String()

	if cmdErr != nil {
		exitCode := int32(-1)
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		}
		log.Printf("script %s failed (exit_code: %d, request_id: %s)", filename, exitCode, requestID)
		reportResult(false, exitCode, stdout, stderr, "")
		return
	}

	log.Printf("script %s succeeded (request_id: %s)", filename, requestID)
	reportResult(true, 0, stdout, stderr, "")
}

// streamOutput reads from stdout and stderr pipes concurrently, buffers the
// output, and sends chunks to the server every outputFlushInterval (200ms).
// It blocks until both pipes are closed (i.e., the child process has ended).
func streamOutput(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	cfg *config,
	requestID string,
	stdoutPipe, stderrPipe io.ReadCloser,
	fullStdout, fullStderr *bytes.Buffer,
) {
	var mu sync.Mutex
	var stdoutBuf, stderrBuf bytes.Buffer

	// Read pipes into buffers concurrently.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			mu.Lock()
			stdoutBuf.WriteString(line)
			fullStdout.WriteString(line)
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			mu.Lock()
			stderrBuf.WriteString(line)
			fullStderr.WriteString(line)
			mu.Unlock()
		}
	}()

	// Flush buffered output every 200ms until both pipes close.
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	ticker := time.NewTicker(outputFlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			flushOutputChunk(ctx, client, cfg, requestID, &mu, &stdoutBuf, &stderrBuf)
		case <-doneCh:
			// Pipes closed — flush any remaining data.
			flushOutputChunk(ctx, client, cfg, requestID, &mu, &stdoutBuf, &stderrBuf)
			return
		case <-ctx.Done():
			return
		}
	}
}

// flushOutputChunk sends accumulated stdout/stderr data to the server and
// resets the buffers. It is a no-op when both buffers are empty.
func flushOutputChunk(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	cfg *config,
	requestID string,
	mu *sync.Mutex,
	stdoutBuf, stderrBuf *bytes.Buffer,
) {
	mu.Lock()
	stdoutChunk := stdoutBuf.String()
	stderrChunk := stderrBuf.String()
	stdoutBuf.Reset()
	stderrBuf.Reset()
	mu.Unlock()

	if stdoutChunk == "" && stderrChunk == "" {
		return
	}

	_, err := client.ReportScriptOutputChunk(ctx, connect.NewRequest(&v1.ReportScriptOutputChunkRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		StdoutChunk: stdoutChunk,
		StderrChunk: stderrChunk,
	}))
	if err != nil {
		log.Printf("failed to send output chunk (request_id: %s): %v", requestID, err)
	}
}
