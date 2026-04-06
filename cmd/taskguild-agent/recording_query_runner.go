package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	claudeagent "github.com/kazz187/claude-agent-sdk-go"
)

// RecordedEntry represents a single recorded QueryRunner call and its result.
type RecordedEntry struct {
	Prompt    any    `json:"prompt"` // string or []map[string]any for image content blocks
	Label     string `json:"label"`
	TaskID    string `json:"task_id"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	SessionID string `json:"session_id"`
	Error     string `json:"error,omitempty"`
}

// RecordingQueryRunner wraps a QueryRunner and records all calls for later replay.
type RecordingQueryRunner struct {
	Inner   QueryRunner
	mu      sync.Mutex
	entries []RecordedEntry
}

// RunQuerySync delegates to the inner QueryRunner and records the call.
func (r *RecordingQueryRunner) RunQuerySync(
	ctx context.Context,
	prompt any,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	result, err := r.Inner.RunQuerySync(ctx, prompt, options, workDir, taskID, label)

	entry := RecordedEntry{
		Prompt: prompt,
		Label:  label,
		TaskID: taskID,
	}

	if err != nil {
		entry.Error = err.Error()
	}
	if result != nil && result.Result != nil {
		entry.Result = result.Result.Result
		entry.IsError = result.Result.IsError
		entry.SessionID = result.Result.SessionID
	}

	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()

	return result, err
}

// Entries returns a copy of all recorded entries.
func (r *RecordingQueryRunner) Entries() []RecordedEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]RecordedEntry, len(r.entries))
	copy(cp, r.entries)
	return cp
}

// SaveToFile serializes all recorded entries to a JSON file.
func (r *RecordingQueryRunner) SaveToFile(path string) error {
	r.mu.Lock()
	entries := make([]RecordedEntry, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ReplayQueryRunner replays recorded entries in sequence.
// It implements the QueryRunner interface.
type ReplayQueryRunner struct {
	entries []RecordedEntry
	mu      sync.Mutex
	idx     int
}

// LoadReplayQueryRunner loads recorded entries from a JSON file.
func LoadReplayQueryRunner(path string) (*ReplayQueryRunner, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	var entries []RecordedEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &ReplayQueryRunner{entries: entries}, nil
}

// RunQuerySync returns the next recorded entry as a QueryResult.
func (r *ReplayQueryRunner) RunQuerySync(
	ctx context.Context,
	prompt any,
	options *claudeagent.ClaudeAgentOptions,
	workDir, taskID, label string,
) (*claudeagent.QueryResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.idx >= len(r.entries) {
		return nil, fmt.Errorf("replay exhausted: no more recorded entries (called %d times, have %d entries)", r.idx+1, len(r.entries))
	}

	entry := r.entries[r.idx]
	r.idx++

	if entry.Error != "" {
		return &claudeagent.QueryResult{
			Result: &claudeagent.ResultMessage{
				Result:    entry.Result,
				IsError:   entry.IsError,
				SessionID: entry.SessionID,
			},
		}, fmt.Errorf("%s", entry.Error)
	}

	return &claudeagent.QueryResult{
		Messages: []claudeagent.Message{},
		Result: &claudeagent.ResultMessage{
			Result:    entry.Result,
			IsError:   entry.IsError,
			SessionID: entry.SessionID,
		},
	}, nil
}
