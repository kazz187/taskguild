package main

import (
	"context"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// QueryRunner abstracts synchronous SDK query execution so that tests can
// substitute a mock without launching a real Claude CLI subprocess.
type QueryRunner interface {
	RunQuerySync(
		ctx context.Context,
		prompt string,
		options *claudeagent.ClaudeAgentOptions,
		workDir, taskID, label string,
	) (*claudeagent.QueryResult, error)
}

// subprocessQueryRunner is the production implementation that delegates to
// runQuerySyncWithLog to launch a real Claude CLI subprocess.
type subprocessQueryRunner struct {
	projectID  string
	projectDir string // main project directory, used as log base
}

func (r subprocessQueryRunner) RunQuerySync(
	ctx context.Context,
	prompt string,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	// Always use the main project directory for turn log output so that
	// logs from hooks/harness running in a worktree are co-located with
	// the task's other logs instead of scattered under the worktree.
	logBaseDir := workDir
	if r.projectDir != "" {
		logBaseDir = r.projectDir
	}
	return runQuerySyncWithLog(ctx, prompt, options, logBaseDir, r.projectID, taskID, label)
}
