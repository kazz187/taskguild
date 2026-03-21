package agentmanager

import (
	"testing"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

func TestUnregisterIfMatch_MatchingChannel(t *testing.T) {
	r := NewRegistry()
	ch := r.Register("agent-1", 2, "proj")

	if !r.UnregisterIfMatch("agent-1", ch) {
		t.Fatal("expected UnregisterIfMatch to return true for matching channel")
	}

	// Connection should be gone.
	if r.SendCommand("agent-1", &taskguildv1.AgentCommand{}) {
		t.Fatal("expected SendCommand to return false after unregister")
	}
}

func TestUnregisterIfMatch_MismatchedChannel(t *testing.T) {
	r := NewRegistry()

	// Register handler A.
	chA := r.Register("agent-1", 2, "proj")

	// Register handler B (same ID) — replaces A, closes chA.
	chB := r.Register("agent-1", 2, "proj")

	// Verify chA was closed.
	select {
	case _, ok := <-chA:
		if ok {
			t.Fatal("expected chA to be closed")
		}
	default:
		t.Fatal("expected chA to be closed (should not block)")
	}

	// Handler A's deferred cleanup: should NOT close chB.
	if r.UnregisterIfMatch("agent-1", chA) {
		t.Fatal("expected UnregisterIfMatch to return false for superseded channel")
	}

	// Handler B's channel should still be open and functional.
	cmd := &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_Ping{
			Ping: &taskguildv1.PingCommand{},
		},
	}
	if !r.SendCommand("agent-1", cmd) {
		t.Fatal("expected SendCommand to succeed — handler B's channel should be alive")
	}

	// Read the command from chB to verify.
	received := <-chB
	if received.GetPing() == nil {
		t.Fatal("expected to receive ping command on chB")
	}
}

func TestUnregisterIfMatch_AlreadyRemoved(t *testing.T) {
	r := NewRegistry()
	ch := r.Register("agent-1", 2, "proj")

	// Unregister normally first.
	r.Unregister("agent-1")

	// UnregisterIfMatch should return false (already gone).
	if r.UnregisterIfMatch("agent-1", ch) {
		t.Fatal("expected UnregisterIfMatch to return false for already-removed connection")
	}
}

func TestUnregisterIfMatch_UnknownAgent(t *testing.T) {
	r := NewRegistry()
	ch := make(chan *taskguildv1.AgentCommand, 1)

	if r.UnregisterIfMatch("unknown", ch) {
		t.Fatal("expected UnregisterIfMatch to return false for unknown agent")
	}
}

func TestRaceCondition_ConcurrentRegisterUnregister(t *testing.T) {
	// Simulates the exact race: two handlers for the same agent, old handler's
	// deferred cleanup must not destroy new handler's connection.
	r := NewRegistry()

	// Handler A registers.
	chA := r.Register("agent-1", 2, "proj")

	// Handler B registers (reconnection) — closes chA.
	chB := r.Register("agent-1", 2, "proj")

	// Handler A exits (channel closed) and runs deferred cleanup.
	wasActive := r.UnregisterIfMatch("agent-1", chA)
	if wasActive {
		t.Fatal("handler A should be superseded")
	}

	// Verify handler B's channel is still open by sending a command.
	cmd := &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_Ping{
			Ping: &taskguildv1.PingCommand{},
		},
	}
	if !r.SendCommand("agent-1", cmd) {
		t.Fatal("handler B's connection should still be active")
	}
	received := <-chB
	if received.GetPing() == nil {
		t.Fatal("expected ping on handler B's channel")
	}

	// Handler B eventually disconnects normally.
	wasActive = r.UnregisterIfMatch("agent-1", chB)
	if !wasActive {
		t.Fatal("handler B should be the active handler")
	}

	// Verify connection is fully cleaned up.
	if r.SendCommand("agent-1", cmd) {
		t.Fatal("connection should be gone after handler B unregisters")
	}

	// Verify chA is closed (from Register step).
	select {
	case _, ok := <-chA:
		if ok {
			t.Fatal("chA should be closed")
		}
	default:
		t.Fatal("chA read should not block")
	}

	// Verify chB is closed (from UnregisterIfMatch).
	select {
	case _, ok := <-chB:
		if ok {
			t.Fatal("chB should be closed")
		}
	default:
		t.Fatal("chB read should not block")
	}
}
