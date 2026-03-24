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
	projectID string
}

func (r subprocessQueryRunner) RunQuerySync(
	ctx context.Context,
	prompt string,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	return runQuerySyncWithLog(ctx, prompt, options, workDir, r.projectID, taskID, label)
}
