package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

const scriptExecutionTimeout = 5 * time.Minute

// handleExecuteScript executes a script on the agent-manager machine and reports the result.
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

	// Execute the script.
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

	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	err := execCmd.Run()

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	if err != nil {
		exitCode := int32(-1)
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int32(exitErr.ExitCode())
		}
		log.Printf("script %s failed (exit_code: %d, request_id: %s)", filename, exitCode, requestID)
		reportResult(false, exitCode, stdout, stderr, "")
		return
	}

	log.Printf("script %s succeeded (request_id: %s)", filename, requestID)
	reportResult(true, 0, stdout, stderr, "")
}
