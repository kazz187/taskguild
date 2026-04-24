package script

import (
	"context"
	"testing"
	"time"

	"github.com/sourcegraph/conc"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// helpers

func newBroker() *ScriptExecutionBroker {
	return NewScriptExecutionBroker()
}

func makeEntries(text string) []*taskguildv1.ScriptLogEntry {
	return []*taskguildv1.ScriptLogEntry{
		{Stream: taskguildv1.ScriptLogStream_SCRIPT_LOG_STREAM_STDOUT, Text: text},
	}
}

func receiveWithTimeout(t *testing.T, ch <-chan *taskguildv1.ScriptExecutionEvent, timeout time.Duration) *taskguildv1.ScriptExecutionEvent {
	t.Helper()
	select {
	case evt, ok := <-ch:
		if !ok {
			t.Fatal("channel closed unexpectedly")
		}
		return evt
	case <-time.After(timeout):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

func expectClosed(t *testing.T, ch <-chan *taskguildv1.ScriptExecutionEvent, timeout time.Duration) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed, but received an event")
		}
	case <-time.After(timeout):
		t.Fatal("timed out waiting for channel to close")
	}
}

// --- RegisterExecution / RemoveExecution ---

func TestRegisterExecution_SubscribeSucceeds(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	ch, unsub := b.Subscribe("req-1")
	defer unsub()
	if ch == nil {
		t.Fatal("expected non-nil channel after RegisterExecution")
	}
}

func TestRemoveExecution_SubscribeReturnsNil(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")
	b.RemoveExecution("req-1")

	ch, _ := b.Subscribe("req-1")
	if ch != nil {
		t.Fatal("expected nil channel after RemoveExecution")
	}
}

func TestPushOutput_UnregisteredExecution(t *testing.T) {
	b := newBroker()
	// Should not panic
	b.PushOutput("unknown", makeEntries("hello"))
}

func TestCompleteExecution_UnregisteredExecution(t *testing.T) {
	b := newBroker()
	// Should not panic
	b.CompleteExecution("unknown", true, 0, nil, "", false)
}

// --- PushOutput ---

func TestPushOutput_DeliveredToSubscriber(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	ch, unsub := b.Subscribe("req-1")
	defer unsub()

	b.PushOutput("req-1", makeEntries("line1"))

	evt := receiveWithTimeout(t, ch, time.Second)
	out, ok := evt.GetEvent().(*taskguildv1.ScriptExecutionEvent_Output)
	if !ok {
		t.Fatal("expected output event")
	}
	if len(out.Output.GetEntries()) != 1 || out.Output.GetEntries()[0].GetText() != "line1" {
		t.Fatalf("unexpected entry: %v", out.Output.GetEntries())
	}
}

func TestPushOutput_DeliveredToMultipleSubscribers(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	ch1, unsub1 := b.Subscribe("req-1")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("req-1")
	defer unsub2()

	b.PushOutput("req-1", makeEntries("hello"))

	for i, ch := range []<-chan *taskguildv1.ScriptExecutionEvent{ch1, ch2} {
		evt := receiveWithTimeout(t, ch, time.Second)
		out, ok := evt.GetEvent().(*taskguildv1.ScriptExecutionEvent_Output)
		if !ok {
			t.Fatalf("subscriber %d: expected output event", i)
		}
		if out.Output.GetEntries()[0].GetText() != "hello" {
			t.Fatalf("subscriber %d: unexpected text %q", i, out.Output.GetEntries()[0].GetText())
		}
	}
}

func TestPushOutput_IgnoredAfterComplete(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")
	b.CompleteExecution("req-1", true, 0, nil, "", false)

	// Subscribe to the completed execution to check buffer size
	ch, _ := b.Subscribe("req-1")
	// Drain the completion event
	receiveWithTimeout(t, ch, time.Second)

	// PushOutput after completion should be ignored
	b.PushOutput("req-1", makeEntries("late"))

	// Subscribe again - should only see the completion event, not "late"
	ch2, _ := b.Subscribe("req-1")
	evt := receiveWithTimeout(t, ch2, time.Second)
	if _, ok := evt.GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete); !ok {
		t.Fatal("expected only completion event, got something else")
	}
	expectClosed(t, ch2, time.Second)
}

// --- CompleteExecution ---

func TestCompleteExecution_DeliveredAndChannelClosed(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	ch, unsub := b.Subscribe("req-1")
	defer unsub()

	b.CompleteExecution("req-1", true, 0, nil, "", false)

	evt := receiveWithTimeout(t, ch, time.Second)
	comp, ok := evt.GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete)
	if !ok {
		t.Fatal("expected complete event")
	}
	if !comp.Complete.GetSuccess() {
		t.Fatal("expected success=true")
	}

	expectClosed(t, ch, time.Second)
}

func TestCompleteExecution_FieldsStoredCorrectly(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")
	b.CompleteExecution("req-1", false, 42, nil, "something failed", false)

	execs := b.ListExecutions("proj-1")
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	e := execs[0]
	if e.GetSuccess() {
		t.Error("expected success=false")
	}
	if e.GetExitCode() != 42 {
		t.Errorf("expected exitCode=42, got %d", e.GetExitCode())
	}
	if e.GetErrorMessage() != "something failed" {
		t.Errorf("expected errorMessage=%q, got %q", "something failed", e.GetErrorMessage())
	}
}

// --- Subscribe (late joiner) ---

func TestSubscribe_LateJoinerReceivesBufferedEvents(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	b.PushOutput("req-1", makeEntries("line1"))
	b.PushOutput("req-1", makeEntries("line2"))
	b.PushOutput("req-1", makeEntries("line3"))

	// Subscribe after 3 pushes
	ch, unsub := b.Subscribe("req-1")
	defer unsub()

	for _, expected := range []string{"line1", "line2", "line3"} {
		evt := receiveWithTimeout(t, ch, time.Second)
		out, ok := evt.GetEvent().(*taskguildv1.ScriptExecutionEvent_Output)
		if !ok {
			t.Fatalf("expected output event for %q", expected)
		}
		if out.Output.GetEntries()[0].GetText() != expected {
			t.Errorf("expected %q, got %q", expected, out.Output.GetEntries()[0].GetText())
		}
	}
}

func TestSubscribe_CompletedExecution(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")
	b.PushOutput("req-1", makeEntries("output"))
	b.CompleteExecution("req-1", true, 0, nil, "", false)

	// Subscribe after completion
	ch, _ := b.Subscribe("req-1")

	// Should receive output event
	evt1 := receiveWithTimeout(t, ch, time.Second)
	if _, ok := evt1.GetEvent().(*taskguildv1.ScriptExecutionEvent_Output); !ok {
		t.Fatal("expected output event")
	}

	// Should receive complete event
	evt2 := receiveWithTimeout(t, ch, time.Second)
	if _, ok := evt2.GetEvent().(*taskguildv1.ScriptExecutionEvent_Complete); !ok {
		t.Fatal("expected complete event")
	}

	// Channel should be closed
	expectClosed(t, ch, time.Second)
}

func TestSubscribe_UnsubscribeStopsDelivery(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "script-1", "proj-1")

	ch, unsub := b.Subscribe("req-1")
	unsub()

	b.PushOutput("req-1", makeEntries("after-unsub"))

	// Channel should not receive anything
	select {
	case <-ch:
		t.Fatal("expected no event after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}

// --- ListExecutions ---

func TestListExecutions_FiltersByProjectID(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "proj-A")
	b.RegisterExecution("req-2", "s2", "proj-B")
	b.RegisterExecution("req-3", "s3", "proj-A")

	execs := b.ListExecutions("proj-A")
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions for proj-A, got %d", len(execs))
	}

	execs = b.ListExecutions("proj-B")
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution for proj-B, got %d", len(execs))
	}

	execs = b.ListExecutions("proj-C")
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions for proj-C, got %d", len(execs))
	}
}

func TestListExecutions_ActiveAndCompleted(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "proj-1")
	b.RegisterExecution("req-2", "s2", "proj-1")
	b.CompleteExecution("req-2", true, 0, nil, "", false)

	execs := b.ListExecutions("proj-1")
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions, got %d", len(execs))
	}

	var active, completed int
	for _, e := range execs {
		if e.GetCompleted() {
			completed++
		} else {
			active++
		}
	}
	if active != 1 || completed != 1 {
		t.Errorf("expected 1 active and 1 completed, got %d active and %d completed", active, completed)
	}
}

// --- ActiveCount ---

func TestActiveCount(t *testing.T) {
	b := newBroker()
	if b.ActiveCount() != 0 {
		t.Fatalf("expected 0, got %d", b.ActiveCount())
	}

	b.RegisterExecution("req-1", "s1", "p1")
	b.RegisterExecution("req-2", "s2", "p1")
	b.RegisterExecution("req-3", "s3", "p1")
	if b.ActiveCount() != 3 {
		t.Fatalf("expected 3, got %d", b.ActiveCount())
	}

	b.CompleteExecution("req-1", true, 0, nil, "", false)
	if b.ActiveCount() != 2 {
		t.Fatalf("expected 2 after completing 1, got %d", b.ActiveCount())
	}

	b.CompleteExecution("req-2", true, 0, nil, "", false)
	b.CompleteExecution("req-3", true, 0, nil, "", false)
	if b.ActiveCount() != 0 {
		t.Fatalf("expected 0 after completing all, got %d", b.ActiveCount())
	}
}

// --- Draining ---

func TestIsDraining(t *testing.T) {
	b := newBroker()
	if b.IsDraining() {
		t.Fatal("expected not draining initially")
	}
	b.SetDraining(true)
	if !b.IsDraining() {
		t.Fatal("expected draining after SetDraining(true)")
	}
}

func TestDrain_NoActiveExecutions(t *testing.T) {
	b := newBroker()
	b.SetDraining(true)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := b.Drain(ctx); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDrain_WaitsForCompletion(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "p1")
	b.SetDraining(true)

	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var drainWg conc.WaitGroup
	drainWg.Go(func() {
		done <- b.Drain(ctx)
	})

	// Drain should be blocking
	select {
	case <-done:
		t.Fatal("Drain returned before execution completed")
	case <-time.After(50 * time.Millisecond):
		// OK, still blocking
	}

	// Complete the execution
	b.CompleteExecution("req-1", true, 0, nil, "", false)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Drain did not return after execution completed")
	}
}

func TestDrain_ContextCancelled(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "p1")
	b.SetDraining(true)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := b.Drain(ctx)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

// --- cleanupExpired ---

func TestCleanupExpired_RemovesOldCompletedExecutions(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "p1")

	// Complete and manually set completedAt to the past
	b.CompleteExecution("req-1", true, 0, nil, "", false)

	b.mu.Lock()
	es := b.executions["req-1"]
	b.mu.Unlock()
	es.mu.Lock()
	es.completedAt = time.Now().Add(-(executionTTL + time.Minute))
	es.mu.Unlock()

	b.cleanupExpired()

	ch, _ := b.Subscribe("req-1")
	if ch != nil {
		t.Fatal("expected nil channel after cleanup of expired execution")
	}
}

func TestCleanupExpired_KeepsActiveAndRecentExecutions(t *testing.T) {
	b := newBroker()

	// Active execution (not completed)
	b.RegisterExecution("active", "s1", "p1")

	// Recently completed execution
	b.RegisterExecution("recent", "s2", "p1")
	b.CompleteExecution("recent", true, 0, nil, "", false)

	b.cleanupExpired()

	// Both should still exist
	ch1, _ := b.Subscribe("active")
	if ch1 == nil {
		t.Fatal("active execution should not be cleaned up")
	}
	ch2, _ := b.Subscribe("recent")
	if ch2 == nil {
		t.Fatal("recently completed execution should not be cleaned up")
	}
}

// --- GetProjectID ---

func TestGetProjectID(t *testing.T) {
	b := newBroker()
	b.RegisterExecution("req-1", "s1", "proj-42")

	if got := b.GetProjectID("req-1"); got != "proj-42" {
		t.Errorf("GetProjectID(%q) = %q, want %q", "req-1", got, "proj-42")
	}

	if got := b.GetProjectID("unknown"); got != "" {
		t.Errorf("GetProjectID(%q) = %q, want empty string", "unknown", got)
	}
}

// --- Subscribe to unknown requestID ---

func TestSubscribe_UnknownRequestID(t *testing.T) {
	b := newBroker()
	ch, unsub := b.Subscribe("nonexistent")
	if ch != nil {
		t.Fatal("expected nil channel for unknown requestID")
	}
	// unsub should be safe to call
	unsub()
}
