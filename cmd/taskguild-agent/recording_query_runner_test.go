package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// TestRecordingQueryRunner_RecordAndReplay verifies that a RecordingQueryRunner
// can capture calls and save them to a file, which can then be loaded by a
// ReplayQueryRunner to produce the same results.
func TestRecordingQueryRunner_RecordAndReplay(t *testing.T) {
	// Create a simple inner QueryRunner that returns sequential results.
	inner := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeResult("first result\nNEXT_STATUS: Develop")},
			{Result: makeResult("second result")},
		},
	}

	recorder := &RecordingQueryRunner{Inner: inner}

	ctx := context.Background()
	opts := &claudeagent.ClaudeAgentOptions{}

	// Record two calls
	res1, err := recorder.RunQuerySync(ctx, "prompt 1", opts, "/tmp", "task-1", "label-1")
	require.NoError(t, err)
	assert.Equal(t, "first result\nNEXT_STATUS: Develop", res1.Result.Result)

	res2, err := recorder.RunQuerySync(ctx, "prompt 2", opts, "/tmp", "task-1", "label-2")
	require.NoError(t, err)
	assert.Equal(t, "second result", res2.Result.Result)

	// Save to file
	entries := recorder.Entries()
	require.Len(t, entries, 2)

	savePath := filepath.Join(t.TempDir(), "recording.json")
	err = recorder.SaveToFile(savePath)
	require.NoError(t, err)

	// Load and replay
	replayer, err := LoadReplayQueryRunner(savePath)
	require.NoError(t, err)

	replay1, err := replayer.RunQuerySync(ctx, "ignored", nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "first result\nNEXT_STATUS: Develop", replay1.Result.Result)
	assert.Equal(t, "test-session", replay1.Result.SessionID)

	replay2, err := replayer.RunQuerySync(ctx, "ignored", nil, "", "", "")
	require.NoError(t, err)
	assert.Equal(t, "second result", replay2.Result.Result)

	// Third call should fail (exhausted)
	_, err = replayer.RunQuerySync(ctx, "extra", nil, "", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "replay exhausted")
}

// TestRecordingQueryRunner_RecordError verifies that errors from the inner
// QueryRunner are properly recorded and replayed.
func TestRecordingQueryRunner_RecordError(t *testing.T) {
	inner := &mockQueryRunner{
		results: []mockQueryRunnerResult{
			{Result: makeErrorResult("something went wrong"), Err: nil},
		},
	}

	recorder := &RecordingQueryRunner{Inner: inner}

	ctx := context.Background()
	res, err := recorder.RunQuerySync(ctx, "prompt", nil, "/tmp", "task-1", "label")
	require.NoError(t, err)
	assert.True(t, res.Result.IsError)

	savePath := filepath.Join(t.TempDir(), "error_recording.json")
	err = recorder.SaveToFile(savePath)
	require.NoError(t, err)

	replayer, err := LoadReplayQueryRunner(savePath)
	require.NoError(t, err)

	replay, err := replayer.RunQuerySync(ctx, "ignored", nil, "", "", "")
	require.NoError(t, err)
	assert.True(t, replay.Result.IsError)
	assert.Equal(t, "something went wrong", replay.Result.Result)
}
