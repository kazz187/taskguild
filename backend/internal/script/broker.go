package script

import (
	"sync"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
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
	subscribers []chan *taskguildv1.ScriptExecutionEvent
	buffer      []*taskguildv1.ScriptExecutionEvent
	completed   bool
}

// NewScriptExecutionBroker creates a new broker for streaming script output.
func NewScriptExecutionBroker() *ScriptExecutionBroker {
	return &ScriptExecutionBroker{
		executions: make(map[string]*executionState),
	}
}

// RegisterExecution registers a new script execution. Must be called before
// PushOutput or CompleteExecution for the given requestID.
func (b *ScriptExecutionBroker) RegisterExecution(requestID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.executions[requestID] = &executionState{}
}

// IsDraining returns true if the broker is in draining mode (rejecting new
// executions in preparation for graceful shutdown).
func (b *ScriptExecutionBroker) IsDraining() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.draining
}

// PushOutput sends an output chunk to all subscribers and buffers it for
// late joiners. It is a no-op if the execution is not registered or already
// completed.
func (b *ScriptExecutionBroker) PushOutput(requestID, stdoutChunk, stderrChunk string) {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		return
	}

	event := &taskguildv1.ScriptExecutionEvent{
		Event: &taskguildv1.ScriptExecutionEvent_Output{
			Output: &taskguildv1.ScriptOutputChunk{
				Stdout: stdoutChunk,
				Stderr: stderrChunk,
			},
		},
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if es.completed {
		return
	}
	es.buffer = append(es.buffer, event)
	for _, ch := range es.subscribers {
		select {
		case ch <- event:
		default:
			// Drop if subscriber is full; they can catch up via buffer.
		}
	}
}

// CompleteExecution marks an execution as complete and sends the completion
// event to all subscribers. Subscriber channels are closed after sending.
func (b *ScriptExecutionBroker) CompleteExecution(requestID string, success bool, exitCode int32, stdout, stderr, errorMessage string) {
	b.mu.Lock()
	es, ok := b.executions[requestID]
	b.mu.Unlock()
	if !ok {
		return
	}

	event := &taskguildv1.ScriptExecutionEvent{
		Event: &taskguildv1.ScriptExecutionEvent_Complete{
			Complete: &taskguildv1.ScriptExecutionComplete{
				Success:      success,
				ExitCode:     exitCode,
				Stdout:       stdout,
				Stderr:       stderr,
				ErrorMessage: errorMessage,
			},
		},
	}

	es.mu.Lock()
	es.buffer = append(es.buffer, event)
	es.completed = true
	subs := es.subscribers
	es.subscribers = nil
	es.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- event:
		default:
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

	ch := make(chan *taskguildv1.ScriptExecutionEvent, 128)

	es.mu.Lock()
	defer es.mu.Unlock()

	// Replay buffered events.
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

// Drain blocks until all active executions have completed. Must call
// SetDraining(true) before calling Drain.
func (b *ScriptExecutionBroker) Drain() {
	b.mu.Lock()
	ch := b.drainCh
	b.mu.Unlock()
	if ch == nil {
		return
	}
	<-ch
}
