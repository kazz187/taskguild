package script

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/sourcegraph/conc"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

const (
	// executionTTL is how long completed executions are kept in memory
	// so that page reloads can still retrieve results.
	executionTTL = 10 * time.Minute

	// cleanupInterval is how often the broker checks for expired executions.
	cleanupInterval = 1 * time.Minute
)

// ScriptExecutionBroker manages per-request channels for streaming script
// execution output to frontend subscribers. It supports late joiners by
// buffering all events for each execution.
type ScriptExecutionBroker struct {
	mu         sync.Mutex
	executions map[string]*executionState
	draining   bool
	drainCh    chan struct{} // closed when all executions complete during draining
}

type executionState struct {
	mu          sync.Mutex
	scriptID    string
	projectID   string
	subscribers []chan *taskguildv1.ScriptExecutionEvent
	buffer      []*taskguildv1.ScriptExecutionEvent
	completed   bool
	completedAt time.Time
	success     bool
	exitCode    int32
	errMessage  string
}

// NewScriptExecutionBroker creates a new broker for streaming script output.
func NewScriptExecutionBroker() *ScriptExecutionBroker {
	return &ScriptExecutionBroker{
		executions: make(map[string]*executionState),
	}
}

// StartCleanup starts a background goroutine that periodically removes
// expired completed executions. It stops when the context is cancelled.
func (b *ScriptExecutionBroker) StartCleanup(ctx context.Context) {
	var wg conc.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				b.cleanupExpired()
			case <-ctx.Done():
				return
			}
		}
	})
}

// cleanupExpired removes completed executions older than executionTTL.
func (b *ScriptExecutionBroker) cleanupExpired() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for id, es := range b.executions {
		es.mu.Lock()
		if es.completed && !es.completedAt.IsZero() && now.Sub(es.completedAt) > executionTTL {
			es.mu.Unlock()
			delete(b.executions, id)
			slog.Debug("cleaned up expired script execution", "request_id", id)
			continue
		}
		es.mu.Unlock()
	}
}

// RegisterExecution registers a new script execution. Must be called before
// PushOutput or CompleteExecution for the given requestID.
func (b *ScriptExecutionBroker) RegisterExecution(requestID, scriptID, projectID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.executions[requestID] = &executionState{
		scriptID:  scriptID,
		projectID: projectID,
	}
}

// RemoveExecution removes a registered execution that was never started
// (e.g. when sending the command to the agent fails after registration).
func (b *ScriptExecutionBroker) RemoveExecution(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.executions, requestID)
}

// IsDraining returns true if the broker is in draining mode (rejecting new
// executions in preparation for graceful shutdown).
func (b *ScriptExecutionBroker) IsDraining() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.draining
}

// GetProjectID returns the projectID for a given requestID, or empty string if unknown.
func (b *ScriptExecutionBroker) GetProjectID(requestID string) string {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		return ""
	}
	es.mu.Lock()
	defer es.mu.Unlock()
	return es.projectID
}

// PushOutput sends an output chunk to all subscribers and buffers it for
// late joiners. It is a no-op if the execution is not registered or already
// completed.
func (b *ScriptExecutionBroker) PushOutput(requestID string, entries []*taskguildv1.ScriptLogEntry) {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		slog.Warn("PushOutput: execution not registered, dropping entries",
			"request_id", requestID, "entry_count", len(entries))
		return
	}

	event := &taskguildv1.ScriptExecutionEvent{
		Event: &taskguildv1.ScriptExecutionEvent_Output{
			Output: &taskguildv1.ScriptOutputChunk{
				Entries: entries,
			},
		},
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if es.completed {
		slog.Warn("[STREAM-TRACE] broker: PushOutput called on completed execution, ignoring", "request_id", requestID)
		return
	}
	es.buffer = append(es.buffer, event)
	slog.Info("[STREAM-TRACE] broker: PushOutput buffered event", "request_id", requestID, "entry_count", len(entries), "buffer_size", len(es.buffer), "subscriber_count", len(es.subscribers))
	for i, ch := range es.subscribers {
		select {
		case ch <- event:
			slog.Info("[STREAM-TRACE] broker: PushOutput sent to subscriber", "request_id", requestID, "subscriber_index", i)
		default:
			// Drop if subscriber is full; they can catch up via buffer.
			slog.Warn("[STREAM-TRACE] broker: subscriber channel full, dropping event", "request_id", requestID, "subscriber_index", i)
		}
	}
}

// CompleteExecution marks an execution as complete and sends the completion
// event to all subscribers. Subscriber channels are closed after sending.
func (b *ScriptExecutionBroker) CompleteExecution(requestID string, success bool, exitCode int32, logEntries []*taskguildv1.ScriptLogEntry, errorMessage string, stoppedByUser bool) {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		slog.Warn("CompleteExecution: execution not registered, dropping completion",
			"request_id", requestID, "log_entry_count", len(logEntries))
		return
	}
	slog.Info("[STREAM-TRACE] broker: CompleteExecution called",
		"request_id", requestID, "success", success, "log_entry_count", len(logEntries))

	event := &taskguildv1.ScriptExecutionEvent{
		Event: &taskguildv1.ScriptExecutionEvent_Complete{
			Complete: &taskguildv1.ScriptExecutionComplete{
				Success:       success,
				ExitCode:      exitCode,
				LogEntries:    logEntries,
				ErrorMessage:  errorMessage,
				StoppedByUser: stoppedByUser,
			},
		},
	}

	es.mu.Lock()
	es.buffer = append(es.buffer, event)
	es.completed = true
	es.completedAt = time.Now()
	es.success = success
	es.exitCode = exitCode
	es.errMessage = errorMessage
	subs := es.subscribers
	es.subscribers = nil
	es.mu.Unlock()

	slog.Info("[STREAM-TRACE] broker: CompleteExecution sending to subscribers", "request_id", requestID, "subscriber_count", len(subs))
	for i, ch := range subs {
		select {
		case ch <- event:
			slog.Info("[STREAM-TRACE] broker: CompleteExecution sent to subscriber", "request_id", requestID, "subscriber_index", i)
		default:
			slog.Warn("[STREAM-TRACE] broker: CompleteExecution subscriber full, dropping", "request_id", requestID, "subscriber_index", i)
		}
		close(ch)
	}

	// Check if draining is complete.
	b.mu.Lock()
	if b.draining && b.activeCountLocked() == 0 {
		select {
		case <-b.drainCh:
		default:
			close(b.drainCh)
		}
	}
	b.mu.Unlock()
}

// Subscribe returns a channel that receives ScriptExecutionEvents for the
// given requestID. The channel is closed when the execution completes. A
// cleanup function is returned to unsubscribe. Returns nil channel if the
// requestID is unknown.
//
// Late joiners receive all buffered events immediately before live events.
func (b *ScriptExecutionBroker) Subscribe(requestID string) (<-chan *taskguildv1.ScriptExecutionEvent, func()) {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		return nil, func() {}
	}

	es.mu.Lock()
	defer es.mu.Unlock()

	// Size the channel to hold all buffered events plus room for future
	// events. This prevents a deadlock where the replay loop blocks because
	// the channel is full and nobody is reading yet (the caller hasn't
	// started its read loop).
	chanSize := len(es.buffer) + 128
	ch := make(chan *taskguildv1.ScriptExecutionEvent, chanSize)

	slog.Info("script execution stream subscriber connected", "request_id", requestID, "buffered_events", len(es.buffer), "completed", es.completed)

	// Replay buffered events (guaranteed not to block).
	for _, evt := range es.buffer {
		ch <- evt
	}

	if es.completed {
		close(ch)
		return ch, func() {}
	}

	es.subscribers = append(es.subscribers, ch)

	unsubscribe := func() {
		es.mu.Lock()
		defer es.mu.Unlock()
		for i, sub := range es.subscribers {
			if sub == ch {
				es.subscribers = append(es.subscribers[:i], es.subscribers[i+1:]...)
				break
			}
		}
	}

	return ch, unsubscribe
}

// ListExecutions returns information about all executions for a given project
// (both active and recently completed within TTL).
func (b *ScriptExecutionBroker) ListExecutions(projectID string) []*taskguildv1.ScriptExecutionInfo {
	b.mu.Lock()
	defer b.mu.Unlock()

	var result []*taskguildv1.ScriptExecutionInfo
	for reqID, es := range b.executions {
		es.mu.Lock()
		if es.projectID != projectID {
			es.mu.Unlock()
			continue
		}
		info := &taskguildv1.ScriptExecutionInfo{
			RequestId:    reqID,
			ScriptId:     es.scriptID,
			Completed:    es.completed,
			Success:      es.success,
			ExitCode:     es.exitCode,
			ErrorMessage: es.errMessage,
		}
		es.mu.Unlock()
		result = append(result, info)
	}
	return result
}

// ActiveCount returns the number of currently active (non-completed) executions.
func (b *ScriptExecutionBroker) ActiveCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.activeCountLocked()
}

// activeCountLocked returns the active count with the lock already held.
func (b *ScriptExecutionBroker) activeCountLocked() int {
	count := 0
	for _, es := range b.executions {
		es.mu.Lock()
		if !es.completed {
			count++
		}
		es.mu.Unlock()
	}
	return count
}

// SetDraining sets the draining flag. When draining, the broker signals
// when all active executions have completed.
func (b *ScriptExecutionBroker) SetDraining(draining bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.draining = draining
	if draining {
		b.drainCh = make(chan struct{})
		if b.activeCountLocked() == 0 {
			close(b.drainCh)
		}
	}
}

// Drain blocks until all active executions have completed or the context
// is cancelled (e.g. timeout). Must call SetDraining(true) before calling
// Drain. Returns the context error if the context was cancelled before all
// executions completed, or nil if all executions completed successfully.
func (b *ScriptExecutionBroker) Drain(ctx context.Context) error {
	b.mu.Lock()
	ch := b.drainCh
	b.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
