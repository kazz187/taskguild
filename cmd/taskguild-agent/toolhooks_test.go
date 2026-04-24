package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// TestLogToolUse_StringToolResponseNotDoubleEncoded verifies that when
// ToolResponse is already a JSON string, logToolUse stores it verbatim
// (no second json.Marshal pass). Regression for the bug where `<` became
// `\u003c` and newlines became literal `\n` in the persisted metadata.
func TestLogToolUse_StringToolResponseNotDoubleEncoded(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tl := newTaskLogger(ctx, tc.agentClient, "task-1")
	defer tl.Close()

	raw := `{"filePath":"/a.ts","newString":"<div>\n</div>"}`
	input := claudeagent.HookInput{
		ToolName:     "Edit",
		ToolInput:    map[string]any{"file_path": "/a.ts"},
		ToolResponse: raw,
	}

	logToolUse(tl, "task-1", input, false)

	// Wait for the async ReportTaskLog RPC to land.
	require.Eventually(t, func() bool {
		tc.agentHandler.mu.Lock()
		defer tc.agentHandler.mu.Unlock()
		return len(tc.agentHandler.reportTaskLogReqs) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	tc.agentHandler.mu.Lock()
	defer tc.agentHandler.mu.Unlock()
	req := tc.agentHandler.reportTaskLogReqs[0]
	out, ok := req.Metadata["tool_output"]
	require.True(t, ok, "tool_output should be present")

	assert.Equal(t, raw, out, "tool_output must be stored verbatim (no double encoding)")
	assert.False(t, strings.Contains(out, `\u003c`), "tool_output must not unicode-escape `<`")
	// It must also not be wrapped in an extra pair of JSON quotes.
	assert.False(t, strings.HasPrefix(out, `"`) && strings.HasSuffix(out, `"`),
		"tool_output must not be wrapped in extra JSON quotes")
}

// TestLogToolUse_NonStringToolResponseMarshaled verifies that non-string
// ToolResponse values are still json.Marshaled (the existing behaviour).
func TestLogToolUse_NonStringToolResponseMarshaled(t *testing.T) {
	tc := newTestClients()
	defer tc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tl := newTaskLogger(ctx, tc.agentClient, "task-2")
	defer tl.Close()

	input := claudeagent.HookInput{
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "/a.ts"},
		ToolResponse: map[string]any{
			"filePath": "/a.ts",
			"count":    7,
		},
	}

	logToolUse(tl, "task-2", input, false)

	require.Eventually(t, func() bool {
		tc.agentHandler.mu.Lock()
		defer tc.agentHandler.mu.Unlock()
		return len(tc.agentHandler.reportTaskLogReqs) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	tc.agentHandler.mu.Lock()
	defer tc.agentHandler.mu.Unlock()
	req := tc.agentHandler.reportTaskLogReqs[0]
	out, ok := req.Metadata["tool_output"]
	require.True(t, ok, "tool_output should be present")

	// Should be valid JSON containing the map fields.
	assert.Contains(t, out, `"filePath":"/a.ts"`)
	assert.Contains(t, out, `"count":7`)
}
