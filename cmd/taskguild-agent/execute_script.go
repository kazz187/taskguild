package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
	"github.com/sourcegraph/conc"
	"github.com/sourcegraph/conc/pool"
)

const (
	scriptExecutionTimeout = 30 * time.Minute
	outputFlushInterval    = 200 * time.Millisecond
)

// runningScripts tracks cancel functions for running script executions
// so they can be stopped via StopScriptCommand.
var runningScripts struct {
	mu          sync.Mutex
	cancels     map[string]context.CancelFunc // requestID → cancel
	userStopped map[string]bool               // requestID → true if stopped by user (not hot-reload)
}

func init() {
	runningScripts.cancels = make(map[string]context.CancelFunc)
	runningScripts.userStopped = make(map[string]bool)
}

// handleStopScript cancels a running script execution by its requestID.
func handleStopScript(cmd *v1.StopScriptCommand) {
	requestID := cmd.GetRequestId()
	slog.Info("received stop script command", "request_id", requestID)

	runningScripts.mu.Lock()
	cancel, ok := runningScripts.cancels[requestID]
	if ok {
		runningScripts.userStopped[requestID] = true
	}
	runningScripts.mu.Unlock()

	if ok {
		cancel()
		slog.Info("script execution cancelled", "request_id", requestID)
	} else {
		slog.Warn("script execution not found for stop", "request_id", requestID)
	}
}

// handleExecuteScript executes a script on the agent-manager machine and reports the result.
// Output is streamed to the server in real-time via ReportScriptOutputChunk RPCs.
// The script is read from the local .claude/scripts/{filename} file.
func handleExecuteScript(ctx context.Context, client taskguildv1connect.AgentManagerServiceClient, cfg *config, cmd *v1.ExecuteScriptCommand) {
	requestID := cmd.GetRequestId()
	scriptID := cmd.GetScriptId()
	filename := cmd.GetFilename()

	slog.Info("executing script", "script_id", scriptID, "request_id", requestID, "filename", filename)

	reportResult := func(success bool, exitCode int32, logEntries []*v1.ScriptLogEntry, errMsg string, stoppedByUser bool) {
		slog.Info("[STREAM-TRACE] agent: reporting execution result to backend", "request_id", requestID, "success", success, "exit_code", exitCode, "log_entry_count", len(logEntries), "error_message", errMsg)
		_, err := client.ReportScriptExecutionResult(context.Background(), connect.NewRequest(&v1.ReportScriptExecutionResultRequest{
			RequestId:     requestID,
			ProjectName:   cfg.ProjectName,
			ScriptId:      scriptID,
			Success:       success,
			ExitCode:      exitCode,
			LogEntries:    logEntries,
			ErrorMessage:  errMsg,
			StoppedByUser: stoppedByUser,
		}))
		if err != nil {
			slog.Error("failed to report script execution result", "request_id", requestID, "error", err)
		}
	}

	// Register this script execution with the tracker so the SIGUSR1
	// (hot-reload) handler waits for it to complete before shutting down.
	scriptTracker.mu.Lock()
	if scriptTracker.reject {
		scriptTracker.mu.Unlock()
		slog.Warn("script rejected: agent is shutting down for hot reload", "filename", filename, "request_id", requestID)
		reportResult(false, -1, nil, "script execution rejected: agent is shutting down for hot reload", false)
		return
	}
	scriptTracker.wg.Add(1)
	scriptTracker.mu.Unlock()
	defer scriptTracker.wg.Done()

	// Resolve the script file path from the local .claude/scripts/ directory.
	scriptPath, err := resolveScriptPath(cfg.WorkDir, filename)
	if err != nil {
		reportResult(false, -1, nil, err.Error(), false)
		return
	}

	// Execute the script with piped stdout/stderr for streaming.
	execCtx, cancel := context.WithTimeout(ctx, scriptExecutionTimeout)
	defer cancel()

	// Register the cancel function so StopScriptCommand can cancel this execution.
	runningScripts.mu.Lock()
	runningScripts.cancels[requestID] = cancel
	runningScripts.mu.Unlock()
	defer func() {
		runningScripts.mu.Lock()
		delete(runningScripts.cancels, requestID)
		delete(runningScripts.userStopped, requestID)
		runningScripts.mu.Unlock()
	}()

	execCmd := exec.CommandContext(execCtx, "/bin/sh", scriptPath)
	execCmd.Dir = cfg.WorkDir
	execCmd.Env = append(os.Environ(),
		"TASKGUILD_PROJECT_NAME="+cfg.ProjectName,
		"TASKGUILD_SCRIPT_ID="+scriptID,
		"TASKGUILD_SCRIPT_FILENAME="+filename,
		"TASKGUILD_WORK_DIR="+cfg.WorkDir,
	)
	// Set process group so we can kill the entire tree on stop.
	execCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Override the default kill behavior of CommandContext to kill the
	// entire process group instead of just the main process.
	execCmd.Cancel = func() error {
		if execCmd.Process != nil {
			// Kill the entire process group (negative PID).
			return syscall.Kill(-execCmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	// Use manual os.Pipe() instead of StdoutPipe/StderrPipe so that
	// Wait() does not close the read ends. This lets us run Wait()
	// concurrently with streamOutput and force-close the pipes after
	// the process exits, preventing a deadlock when background processes
	// inherit the file descriptors and keep them open.
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		reportResult(false, -1, nil, fmt.Sprintf("failed to create stdout pipe: %v", err), false)
		return
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		stdoutR.Close()
		stdoutW.Close()
		reportResult(false, -1, nil, fmt.Sprintf("failed to create stderr pipe: %v", err), false)
		return
	}
	execCmd.Stdout = stdoutW
	execCmd.Stderr = stderrW

	if err := execCmd.Start(); err != nil {
		stdoutR.Close()
		stdoutW.Close()
		stderrR.Close()
		stderrW.Close()
		reportResult(false, -1, nil, fmt.Sprintf("failed to start script: %v", err), false)
		return
	}
	// Close write ends in the parent so the read ends get EOF when the
	// child (and any processes that did NOT inherit these fds) exits.
	stdoutW.Close()
	stderrW.Close()

	slog.Info("script process started", "request_id", requestID, "pid", execCmd.Process.Pid, "filename", filename)

	// Run Wait() concurrently. When the main process exits, give scanners
	// a short window to drain buffered pipe data, then force-close the
	// read ends. This prevents indefinite blocking when scripts spawn
	// background processes that inherit stdout/stderr.
	waitPool := pool.NewWithResults[error]().WithMaxGoroutines(1)
	waitPool.Go(func() error {
		waitErr := execCmd.Wait()
		// Allow scanners up to 500ms to drain any data still in the
		// kernel pipe buffer after the main process exits.
		time.Sleep(500 * time.Millisecond)
		stdoutR.Close()
		stderrR.Close()
		return waitErr
	})

	// Stream output in real-time.
	var fullLog logEntryBuffer
	streamOutput(ctx, client, cfg, requestID, stdoutR, stderrR, &fullLog)

	// Collect the Wait result (available immediately or shortly after
	// streamOutput returns).
	waitResults := waitPool.Wait()
	var cmdErr error
	if len(waitResults) > 0 {
		cmdErr = waitResults[0]
	}

	// Check if this was a user-initiated stop (via StopScriptCommand).
	// Do not rely on execCtx.Err() == context.Canceled because the context
	// can also be canceled by SIGUSR1 (hot-reload) or SIGINT/SIGTERM,
	// which are not user-initiated stops.
	runningScripts.mu.Lock()
	stoppedByUser := runningScripts.userStopped[requestID]
	runningScripts.mu.Unlock()

	logEntries := fullLog.entries()

	if cmdErr != nil {
		exitCode := int32(-1)
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		}
		if stoppedByUser {
			slog.Info("script stopped by user", "filename", filename, "request_id", requestID)
			reportResult(false, exitCode, logEntries, "Stopped by user", true)
		} else {
			slog.Error("script failed", "filename", filename, "exit_code", exitCode, "request_id", requestID)
			reportResult(false, exitCode, logEntries, "", false)
		}
		return
	}

	slog.Info("script succeeded", "filename", filename, "request_id", requestID)
	reportResult(true, 0, logEntries, "", false)
}

// resolveScriptPath returns the path to the script file to execute.
// It looks up .claude/scripts/{filename} under workDir. If the file does not
// exist, it returns an error instructing the user to run sync first.
func resolveScriptPath(workDir, filename string) (string, error) {
	localPath := filepath.Join(workDir, ".claude", "scripts", filename)
	info, err := os.Stat(localPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("script file not found locally: %s; run sync first", filename)
	}
	// Ensure the local file is executable.
	if info.Mode()&0111 == 0 {
		if chmodErr := os.Chmod(localPath, 0755); chmodErr != nil {
			slog.Warn("failed to set execute permission on script", "path", localPath, "error", chmodErr)
		}
	}
	slog.Info("using local script file", "path", localPath)
	return localPath, nil
}

// logEntryBuffer accumulates log entries in order for the final report.
type logEntryBuffer struct {
	mu      sync.Mutex
	buf     []*v1.ScriptLogEntry
}

func (b *logEntryBuffer) append(stream v1.ScriptLogStream, text string) {
	b.mu.Lock()
	b.buf = append(b.buf, &v1.ScriptLogEntry{Stream: stream, Text: text})
	b.mu.Unlock()
}

func (b *logEntryBuffer) entries() []*v1.ScriptLogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := make([]*v1.ScriptLogEntry, len(b.buf))
	copy(result, b.buf)
	return result
}

// chunkBuffer accumulates log entries for the next flush interval.
type chunkBuffer struct {
	mu  sync.Mutex
	buf []*v1.ScriptLogEntry
}

func (c *chunkBuffer) append(stream v1.ScriptLogStream, text string) {
	c.mu.Lock()
	c.buf = append(c.buf, &v1.ScriptLogEntry{Stream: stream, Text: text})
	c.mu.Unlock()
}

func (c *chunkBuffer) drain() []*v1.ScriptLogEntry {
	c.mu.Lock()
	entries := c.buf
	c.buf = nil
	c.mu.Unlock()
	return entries
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
	fullLog *logEntryBuffer,
) {
	var chunk chunkBuffer

	// Read pipes into buffers concurrently.
	var pipeWg conc.WaitGroup

	pipeWg.Go(func() {
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			lineCount++
			slog.Info("[STREAM-TRACE] agent: read stdout line", "request_id", requestID, "line_num", lineCount, "len", len(line))
			chunk.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, line)
			fullLog.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, line)
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("[STREAM-TRACE] agent: stdout scanner error", "request_id", requestID, "error", err)
		}
		slog.Info("[STREAM-TRACE] agent: stdout pipe closed", "request_id", requestID, "total_lines", lineCount)
	})

	pipeWg.Go(func() {
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineCount := 0
		for scanner.Scan() {
			line := scanner.Text() + "\n"
			lineCount++
			slog.Info("[STREAM-TRACE] agent: read stderr line", "request_id", requestID, "line_num", lineCount, "len", len(line))
			chunk.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, line)
			fullLog.append(v1.ScriptLogStream_SCRIPT_LOG_STREAM_STDERR, line)
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("[STREAM-TRACE] agent: stderr scanner error", "request_id", requestID, "error", err)
		}
		slog.Info("[STREAM-TRACE] agent: stderr pipe closed", "request_id", requestID, "total_lines", lineCount)
	})

	// Periodic flushing in background until pipes close.
	flushDone := make(chan struct{})
	var flushWg conc.WaitGroup
	flushWg.Go(func() {
		ticker := time.NewTicker(outputFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				flushLogEntries(ctx, client, cfg, requestID, &chunk)
			case <-flushDone:
				return
			case <-ctx.Done():
				return
			}
		}
	})

	// Wait for both pipe readers to finish, then stop the flusher.
	pipeWg.Wait()
	close(flushDone)
	flushWg.Wait()

	// Final flush for any remaining data.
	flushLogEntries(ctx, client, cfg, requestID, &chunk)

	if ctx.Err() != nil {
		slog.Warn("script output streaming cancelled by context", "request_id", requestID, "error", ctx.Err())
	} else {
		slog.Info("script output streaming finished", "request_id", requestID)
	}
}

// flushLogEntries sends accumulated log entries to the server and
// clears the buffer. It is a no-op when the buffer is empty.
func flushLogEntries(
	ctx context.Context,
	client taskguildv1connect.AgentManagerServiceClient,
	cfg *config,
	requestID string,
	chunk *chunkBuffer,
) {
	entries := chunk.drain()
	if len(entries) == 0 {
		return
	}

	_, err := client.ReportScriptOutputChunk(ctx, connect.NewRequest(&v1.ReportScriptOutputChunkRequest{
		RequestId:   requestID,
		ProjectName: cfg.ProjectName,
		Entries:     entries,
	}))
	if err != nil {
		slog.Error("[STREAM-TRACE] agent: failed to send output chunk to backend", "request_id", requestID, "error", err)
	} else {
		slog.Info("[STREAM-TRACE] agent: sent output chunk to backend", "request_id", requestID, "entry_count", len(entries))
	}
}
