package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// userInputMsg mirrors the SDK's internal userInputMessage for sending
// the initial prompt via the control protocol.
type userInputMsg struct {
	Type      string        `json:"type"`
	Message   userInputBody `json:"message"`
	SessionID string        `json:"sessionId"`
}

type userInputBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// turnLog writes log entries to a single file per turn in real-time.
// Each method appends to the file immediately so that partial logs
// are available even if the process is interrupted.
type turnLog struct {
	mu   sync.Mutex
	file *os.File
	idx  int // message counter
}

// newTurnLog creates a new turn log file and writes the header.
// If workDir is empty, logging is disabled (all methods become no-ops).
func newTurnLog(workDir, taskID, label string) *turnLog {
	if workDir == "" {
		return &turnLog{}
	}

	dir := taskID
	if dir == "" {
		dir = "_system"
	}

	logDir := filepath.Join(workDir, ".claude", "cmd-logs", dir)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("failed to create cmd-log directory", "dir", logDir, "error", err)
		return &turnLog{}
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	filename := fmt.Sprintf("%s_%s.log", ts, label)
	logPath := filepath.Join(logDir, filename)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.Warn("failed to open turn-log file", "path", logPath, "error", err)
		return &turnLog{}
	}

	tl := &turnLog{file: f}
	tl.write(fmt.Sprintf("# Turn log: %s\n# Task: %s\n# Label: %s\n", ts, taskID, label))
	return tl
}

// Close closes the underlying file.
func (tl *turnLog) Close() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.file != nil {
		tl.file.Close()
		tl.file = nil
	}
}

// write appends text to the file. Caller must not hold tl.mu.
func (tl *turnLog) write(s string) {
	if tl.file == nil {
		return
	}
	if _, err := tl.file.WriteString(s); err != nil {
		slog.Warn("failed to write to turn-log", "error", err)
	}
}

// writeLocked appends text while holding the lock.
func (tl *turnLog) writeLocked(s string) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.write(s)
}

func separator() string {
	return "\n" + strings.Repeat("=", 60) + "\n"
}

func ts() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

// LogCommandArgs writes command arguments immediately.
func (tl *turnLog) LogCommandArgs(args []string) {
	if tl.file == nil || len(args) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString(separator())
	sb.WriteString(fmt.Sprintf("[%s] ## Command Args\n\n", ts()))
	for _, arg := range args {
		sb.WriteString(arg)
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("\n# Full command:\n%s\n", strings.Join(args, " ")))
	tl.writeLocked(sb.String())
}

// LogStdinMessage writes the stdin JSON message immediately.
func (tl *turnLog) LogStdinMessage(jsonData []byte) {
	if tl.file == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString(separator())
	sb.WriteString(fmt.Sprintf("[%s] ## Stdin Message\n\n", ts()))

	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, jsonData, "", "  "); err == nil {
		sb.WriteString(prettyJSON.String())
	} else {
		sb.Write(jsonData)
	}
	sb.WriteString("\n")
	tl.writeLocked(sb.String())
}

// LogMessage writes a single received message immediately with a timestamp.
func (tl *turnLog) LogMessage(msg claudeagent.Message) {
	if tl.file == nil {
		return
	}
	tl.mu.Lock()
	idx := tl.idx
	tl.idx++
	tl.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		tl.writeLocked(fmt.Sprintf("\n[%s] ## Message[%d] (marshal error: %v)\n", ts(), idx, err))
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n[%s] ## Message[%d]\n", ts(), idx))
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, data, "", "  "); err == nil {
		sb.WriteString(prettyJSON.String())
	} else {
		sb.Write(data)
	}
	sb.WriteString("\n")
	tl.writeLocked(sb.String())
}

// LogResult writes the final result summary immediately.
func (tl *turnLog) LogResult(result *claudeagent.ResultMessage, queryErr error) {
	if tl.file == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString(separator())
	sb.WriteString(fmt.Sprintf("[%s] ## Final Result\n\n", ts()))

	if queryErr != nil {
		sb.WriteString(fmt.Sprintf("### Error\n%v\n\n", queryErr))
	}

	if result == nil {
		sb.WriteString("Result: nil\n")
	} else {
		sb.WriteString(fmt.Sprintf("Session ID: %s\n", result.SessionID))
		sb.WriteString(fmt.Sprintf("Is Error: %v\n", result.IsError))
		sb.WriteString(fmt.Sprintf("Result Text (%d chars):\n", len(result.Result)))
		sb.WriteString(result.Result)
		sb.WriteString("\n")
	}
	tl.writeLocked(sb.String())
}

// runQuerySyncWithLog executes a synchronous SDK query and logs the CLI
// command arguments, stdin message, each received message, and the final
// result to a single file per turn in real-time.
func runQuerySyncWithLog(
	ctx context.Context,
	prompt string,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	opts := claudeagent.ClaudeAgentOptions{}
	if options != nil {
		opts = *options
	}

	tl := newTurnLog(workDir, taskID, label)
	defer tl.Close()

	// Configure permission prompt tool name (matching RunQuery behavior).
	if opts.CanUseTool != nil {
		if opts.PermissionPromptToolName != "" {
			return nil, fmt.Errorf("CanUseTool callback cannot be used with PermissionPromptToolName")
		}
		opts.PermissionPromptToolName = "stdio"
	}

	// Create a cancellable context for the subprocess so we can terminate
	// it promptly once the ResultMessage has been received.
	transportCtx, transportCancel := context.WithCancel(ctx)
	defer transportCancel()

	// Create and connect the subprocess transport.
	transport := claudeagent.NewSubprocessTransport("", opts)
	if err := transport.Connect(transportCtx); err != nil {
		return nil, err
	}
	defer transport.Close()

	// Log command args.
	if args := transport.CommandArgs(); args != nil {
		tl.LogCommandArgs(args)
	}

	// Calculate initialize timeout (matching RunQuery behavior).
	initTimeout := 60 * time.Second
	if timeoutStr := os.Getenv("CLAUDE_CODE_INIT_TIMEOUT"); timeoutStr != "" {
		if ms, err := strconv.ParseInt(timeoutStr, 10, 64); err == nil {
			initTimeout = time.Duration(ms) * time.Millisecond
		}
	}

	// Create the query handler.
	query := claudeagent.NewQuery(transport, true, claudeagent.QueryOptions{
		CanUseTool:        opts.CanUseTool,
		Hooks:             opts.Hooks,
		Agents:            opts.Agents,
		InitializeTimeout: initTimeout,
	})

	if err := query.Start(ctx); err != nil {
		return nil, err
	}
	defer query.Close()

	if _, err := query.Initialize(); err != nil {
		return nil, err
	}

	// Send the user message via stdin.
	msg := userInputMsg{
		Type: "user",
		Message: userInputBody{
			Role:    "user",
			Content: prompt,
		},
		SessionID: "default",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if err := transport.Write(ctx, string(data)+"\n"); err != nil {
		return nil, err
	}
	tl.LogStdinMessage(data)

	// Handle stdin closure.
	hasHooks := len(opts.Hooks) > 0
	hasCanUseTool := opts.CanUseTool != nil
	if !hasHooks && !hasCanUseTool {
		// No control protocol needed after sending the message.
		transport.EndInput()
	}
	// For hooks/CanUseTool cases, EndInput is called when we receive
	// the ResultMessage below (stdin stays open for the control protocol).

	// Read and collect messages.
	rawMsgChan := query.ReceiveMessages()
	queryErrChan := query.Errors()

	result := &claudeagent.QueryResult{
		Messages: make([]claudeagent.Message, 0),
	}

	stdinClosed := !hasHooks && !hasCanUseTool

	for {
		select {
		case <-ctx.Done():
			tl.LogResult(result.Result, ctx.Err())
			return result, ctx.Err()
		case err, ok := <-queryErrChan:
			if ok && err != nil {
				// Exit code 0 is normal termination.
				if pe, ok := err.(*claudeagent.ProcessError); ok && pe.ExitCode == 0 {
					tl.LogResult(result.Result, nil)
					return result, nil
				}
				tl.LogResult(result.Result, err)
				return result, err
			}
		case rawMsg, ok := <-rawMsgChan:
			if !ok {
				tl.LogResult(result.Result, nil)
				return result, nil
			}

			parsed, err := claudeagent.ParseMessage(rawMsg)
			if err != nil {
				// MessageParseError is non-fatal.
				if _, isParseErr := err.(*claudeagent.MessageParseError); isParseErr {
					continue
				}
				tl.LogResult(result.Result, err)
				return result, err
			}
			if parsed == nil {
				continue
			}

			result.Messages = append(result.Messages, parsed)
			tl.LogMessage(parsed)

			if rm, ok := parsed.(*claudeagent.ResultMessage); ok {
				result.Result = rm
				slog.Info("ResultMessage received, starting cleanup")
				// Close stdin to signal CLI that no more input is coming.
				if !stdinClosed {
					transport.EndInput()
					stdinClosed = true
				}
				slog.Info("EndInput completed, closing transport")
				// Close the transport to terminate the subprocess and its
				// process group, then close its stdout pipe.
				// Without this, the deferred query.Close() deadlocks:
				// it waits for readMessages goroutine which is blocked on
				// scanner.Scan() reading from the still-open stdout pipe.
				//
				// We intentionally do NOT call transportCancel() before
				// transport.Close(). Cancelling the context triggers
				// exec.CommandContext's internal kill which only sends
				// SIGKILL to the main PID — grandchild processes (in their
				// own session via Setsid) survive and keep the stdout pipe
				// open, causing cmd.Wait() inside Close() to block.
				// transport.Close() handles the full cleanup correctly:
				// it kills the entire process group and waits with a
				// SIGKILL fallback timeout.
				transport.Close()
				slog.Info("transport closed, logging result")
				tl.LogResult(result.Result, nil)
				slog.Info("result logged, returning")
				return result, nil
			}
		}
	}
}
