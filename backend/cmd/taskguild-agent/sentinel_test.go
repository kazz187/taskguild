package main

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

	got, err := hashFile(path)
	if err != nil {
		t.Fatalf("hashFile failed: %v", err)
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

	hash1, err := hashFile(path1)
	if err != nil {
		t.Fatalf("hashFile(file1) failed: %v", err)
	}
	hash2, err := hashFile(path2)
	if err != nil {
		t.Fatalf("hashFile(file2) failed: %v", err)
	}

	if hash1 == hash2 {
		t.Error("different files produced the same hash")
	}
}

func TestHashFileNotFound(t *testing.T) {
	_, err := hashFile("/nonexistent/file/path")
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

	hash1, err := hashFile(path1)
	if err != nil {
		t.Fatalf("hashFile(file1) failed: %v", err)
	}
	hash2, err := hashFile(path2)
	if err != nil {
		t.Fatalf("hashFile(file2) failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("identical files produced different hashes: %x vs %x", hash1, hash2)
	}
}

func TestBackoffProgression(t *testing.T) {
	s := &sentinel{
		backoff: sentinelInitialBackoff,
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
	s := &sentinel{
		backoff: 9 * time.Minute,
		stopCh:  make(chan struct{}),
	}

	s.increaseBackoff()
	if s.backoff != sentinelMaxBackoff {
		t.Errorf("got %v, want %v (should be capped)", s.backoff, sentinelMaxBackoff)
	}

	// Another increase should stay capped.
	s.increaseBackoff()
	if s.backoff != sentinelMaxBackoff {
		t.Errorf("got %v, want %v (should stay capped)", s.backoff, sentinelMaxBackoff)
	}
}

func TestBackoffReset(t *testing.T) {
	s := &sentinel{
		backoff: 5 * time.Minute,
		stopCh:  make(chan struct{}),
	}

	// Simulate a reset after successful run.
	s.backoff = sentinelInitialBackoff
	if s.backoff != sentinelInitialBackoff {
		t.Errorf("got %v, want %v", s.backoff, sentinelInitialBackoff)
	}
}

func TestSleepBackoffInterruptible(t *testing.T) {
	s := &sentinel{
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
	if sentinelInitialBackoff != 5*time.Second {
		t.Errorf("sentinelInitialBackoff: got %v, want %v", sentinelInitialBackoff, 5*time.Second)
	}
	if sentinelMaxBackoff != 10*time.Minute {
		t.Errorf("sentinelMaxBackoff: got %v, want %v", sentinelMaxBackoff, 10*time.Minute)
	}
	if sentinelGracePeriod != 10*time.Second {
		t.Errorf("sentinelGracePeriod: got %v, want %v", sentinelGracePeriod, 10*time.Second)
	}
	if sentinelBackoffFactor != 2.0 {
		t.Errorf("sentinelBackoffFactor: got %v, want %v", sentinelBackoffFactor, 2.0)
	}
	if sentinelSuccessRunTime != 30*time.Second {
		t.Errorf("sentinelSuccessRunTime: got %v, want %v", sentinelSuccessRunTime, 30*time.Second)
	}
}
