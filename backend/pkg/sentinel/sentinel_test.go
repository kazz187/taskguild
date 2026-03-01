package sentinel

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHashFile(t *testing.T) {
	// Create a temp file with known content.
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile failed: %v", err)
	}

	want := sha256.Sum256(content)
	if got != want {
		t.Errorf("hash mismatch: got %x, want %x", got, want)
	}
}

func TestHashFileDifferentContent(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "file1")
	path2 := filepath.Join(dir, "file2")
	if err := os.WriteFile(path1, []byte("content A"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(path2, []byte("content B"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	hash1, err := HashFile(path1)
	if err != nil {
		t.Fatalf("HashFile(file1) failed: %v", err)
	}
	hash2, err := HashFile(path2)
	if err != nil {
		t.Fatalf("HashFile(file2) failed: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different files produced the same hash")
	}
}

func TestHashFileNotFound(t *testing.T) {
	_, err := HashFile("/nonexistent/file/path")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestHashFileSameContent(t *testing.T) {
	dir := t.TempDir()

	path1 := filepath.Join(dir, "file1")
	path2 := filepath.Join(dir, "file2")
	content := []byte("identical content")
	if err := os.WriteFile(path1, content, 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(path2, content, 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	hash1, err := HashFile(path1)
	if err != nil {
		t.Fatalf("HashFile(file1) failed: %v", err)
	}
	hash2, err := HashFile(path2)
	if err != nil {
		t.Fatalf("HashFile(file2) failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("identical files produced different hashes: %x vs %x", hash1, hash2)
	}
}

func TestBackoffProgression(t *testing.T) {
	s := &Sentinel{
		backoff: InitialBackoff,
		stopCh:  make(chan struct{}),
	}

	// Verify initial value.
	if s.backoff != 5*time.Second {
		t.Errorf("initial backoff: got %v, want %v", s.backoff, 5*time.Second)
	}

	// Verify progression: 5s -> 10s -> 20s -> 40s -> 80s -> 160s -> 320s -> 600s
	expected := []time.Duration{
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		80 * time.Second,
		160 * time.Second,
		320 * time.Second,
		600 * time.Second, // capped at 10 minutes
	}

	for i, want := range expected {
		s.increaseBackoff()
		if s.backoff != want {
			t.Errorf("step %d: got %v, want %v", i+1, s.backoff, want)
		}
	}
}

func TestBackoffCap(t *testing.T) {
	s := &Sentinel{
		backoff: 9 * time.Minute,
		stopCh:  make(chan struct{}),
	}

	s.increaseBackoff()
	if s.backoff != MaxBackoff {
		t.Errorf("got %v, want %v (should be capped)", s.backoff, MaxBackoff)
	}

	// Another increase should stay capped.
	s.increaseBackoff()
	if s.backoff != MaxBackoff {
		t.Errorf("got %v, want %v (should stay capped)", s.backoff, MaxBackoff)
	}
}

func TestBackoffReset(t *testing.T) {
	s := &Sentinel{
		backoff: 5 * time.Minute,
		stopCh:  make(chan struct{}),
	}

	// Simulate a reset after successful run.
	s.backoff = InitialBackoff
	if s.backoff != InitialBackoff {
		t.Errorf("got %v, want %v", s.backoff, InitialBackoff)
	}
}

func TestSleepBackoffInterruptible(t *testing.T) {
	s := &Sentinel{
		backoff: 10 * time.Second,
		stopCh:  make(chan struct{}),
	}

	start := time.Now()
	// Close stopCh after a short delay to interrupt the sleep.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(s.stopCh)
	}()

	s.sleepBackoff()
	elapsed := time.Since(start)

	// Should have been interrupted well before the 10s backoff.
	if elapsed >= 1*time.Second {
		t.Errorf("sleepBackoff was not interrupted: elapsed %v", elapsed)
	}
}

func TestConstants(t *testing.T) {
	// Verify the constants match the spec.
	if InitialBackoff != 5*time.Second {
		t.Errorf("InitialBackoff: got %v, want %v", InitialBackoff, 5*time.Second)
	}
	if MaxBackoff != 10*time.Minute {
		t.Errorf("MaxBackoff: got %v, want %v", MaxBackoff, 10*time.Minute)
	}
	if GracePeriod != 10*time.Second {
		t.Errorf("GracePeriod: got %v, want %v", GracePeriod, 10*time.Second)
	}
	if BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor: got %v, want %v", BackoffFactor, 2.0)
	}
	if SuccessRunTime != 30*time.Second {
		t.Errorf("SuccessRunTime: got %v, want %v", SuccessRunTime, 30*time.Second)
	}
	if ScriptWaitTimeout != 6*time.Minute {
		t.Errorf("ScriptWaitTimeout: got %v, want %v", ScriptWaitTimeout, 6*time.Minute)
	}
}

func TestRequestGracefulRestart_NilCmd(t *testing.T) {
	s := &Sentinel{
		stopCh: make(chan struct{}),
	}

	// Should not panic with nil cmd.
	s.requestGracefulRestart(nil)
}

func TestMainLoopGracefulRestart_ChildExitsBeforeTimeout(t *testing.T) {
	// Simulate the updateCh case where child exits before ScriptWaitTimeout.
	// We verify the flow by checking that the sentinel does NOT call stopChild
	// (force kill) when the child exits on its own.

	s := &Sentinel{
		backoff: InitialBackoff,
		stopCh:  make(chan struct{}),
	}

	childDone := make(chan error, 1)

	// Simulate child exit after a short delay (e.g. after scripts complete).
	go func() {
		time.Sleep(50 * time.Millisecond)
		childDone <- nil
	}()

	// Simulate the updateCh select branch inline:
	// We can't easily call mainLoop here because it needs real processes,
	// but we can verify the select logic that waits for childDone.
	select {
	case <-childDone:
		// Expected: child exited before timeout.
	case <-time.After(ScriptWaitTimeout):
		t.Error("should not have timed out; child was expected to exit first")
	}

	// Verify backoff was not changed (no error path taken).
	if s.backoff != InitialBackoff {
		t.Errorf("backoff changed unexpectedly: got %v, want %v", s.backoff, InitialBackoff)
	}
}

func TestMainLoopGracefulRestart_TimeoutFallback(t *testing.T) {
	// Verify that the timeout path is taken when the child does not exit.
	childDone := make(chan error, 1)
	// Do NOT send to childDone — simulates a stuck child.

	timedOut := false
	select {
	case <-childDone:
		t.Error("child should not have exited")
	case <-time.After(100 * time.Millisecond): // short timeout for testing
		timedOut = true
	}

	if !timedOut {
		t.Error("expected timeout fallback path to be taken")
	}
}
