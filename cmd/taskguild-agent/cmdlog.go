package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// userInputMsg mirrors the SDK's internal userInputMessage for sending
// the initial prompt via the control protocol.
type userInputMsg struct {
	Type    string          `json:"type"`
	Message userInputBody   `json:"message"`
	SessionID string        `json:"sessionId"`
}

type userInputBody struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// logCommandArgs writes the CLI command arguments to a timestamped log file
// under {workDir}/.claude/cmd-logs/{taskID}/.
// Errors are non-fatal and logged as warnings.
func logCommandArgs(workDir, taskID, label string, args []string) {
	if workDir == "" {
		return
	}

	dir := taskID
	if dir == "" {
		dir = "_system"
	}

	logDir := filepath.Join(workDir, ".claude", "cmd-logs", dir)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.Warn("failed to create cmd-log directory", "dir", logDir, "error", err)
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	filename := fmt.Sprintf("%s_%s.log", ts, label)
	logPath := filepath.Join(logDir, filename)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# Command: %s\n", ts))
	sb.WriteString(fmt.Sprintf("# Task: %s\n", taskID))
	sb.WriteString(fmt.Sprintf("# Label: %s\n\n", label))

	for _, arg := range args {
		sb.WriteString(arg)
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\n# Full command:\n%s\n", strings.Join(args, " ")))

	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		slog.Warn("failed to write cmd-log", "path", logPath, "error", err)
	}
}

// runQuerySyncWithLog executes a synchronous SDK query and logs the CLI
// command arguments to an individual file. It mirrors the behavior of
// claudeagent.RunQuerySync but uses lower-level SDK APIs to access the
// transport's CommandArgs().
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

	// Configure permission prompt tool name (matching RunQuery behavior).
	if opts.CanUseTool != nil {
		if opts.PermissionPromptToolName != "" {
			return nil, fmt.Errorf("CanUseTool callback cannot be used with PermissionPromptToolName")
		}
		opts.PermissionPromptToolName = "stdio"
	}

	// Create and connect the subprocess transport.
	transport := claudeagent.NewSubprocessTransport("", opts)
	if err := transport.Connect(ctx); err != nil {
		return nil, err
	}
	defer transport.Close()

	// Log command args to file.
	if args := transport.CommandArgs(); args != nil {
		logCommandArgs(workDir, taskID, label, args)
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
			return result, ctx.Err()
		case err, ok := <-queryErrChan:
			if ok && err != nil {
				// Exit code 0 is normal termination.
				if pe, ok := err.(*claudeagent.ProcessError); ok && pe.ExitCode == 0 {
					return result, nil
				}
				return result, err
			}
		case rawMsg, ok := <-rawMsgChan:
			if !ok {
				return result, nil
			}

			parsed, err := claudeagent.ParseMessage(rawMsg)
			if err != nil {
				// MessageParseError is non-fatal.
				if _, isParseErr := err.(*claudeagent.MessageParseError); isParseErr {
					continue
				}
				return result, err
			}
			if parsed == nil {
				continue
			}

			result.Messages = append(result.Messages, parsed)

			if rm, ok := parsed.(*claudeagent.ResultMessage); ok {
				result.Result = rm
				// Close stdin to signal CLI that no more input is coming.
				if !stdinClosed {
					transport.EndInput()
					stdinClosed = true
				}
				return result, nil
			}
		}
	}
}
