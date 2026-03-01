package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// sentinelGracePeriod is the time to wait after SIGTERM before sending SIGKILL.
	sentinelGracePeriod = 10 * time.Second

	// sentinelInitialBackoff is the initial delay before restarting after an abnormal exit.
	sentinelInitialBackoff = 5 * time.Second

	// sentinelMaxBackoff is the maximum delay between restarts.
	sentinelMaxBackoff = 10 * time.Minute

	// sentinelBackoffFactor is the multiplier for each successive backoff.
	sentinelBackoffFactor = 2.0

	// sentinelSuccessRunTime is how long the child must run before backoff resets.
	sentinelSuccessRunTime = 30 * time.Second

	// sentinelDebounceInterval is the delay after an fsnotify event before checking the checksum.
	sentinelDebounceInterval = 100 * time.Millisecond
)

// sentinel manages the lifecycle of a child "taskguild-agent run" process.
type sentinel struct {
	binaryPath string
	lastHash   [sha256.Size]byte
	backoff    time.Duration
	stopCh     chan struct{} // closed when sentinel should exit
}

// runSentinel is the entry point for the "sentinel" subcommand.
func runSentinel() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.SetPrefix("[sentinel] ")

	// Resolve own binary path.
	binaryPath, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to resolve executable path: %v", err)
	}
	// Resolve symlinks so we watch the real file location.
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		log.Fatalf("failed to resolve symlinks for binary: %v", err)
	}

	log.Printf("starting sentinel (binary: %s)", binaryPath)

	s := &sentinel{
		binaryPath: binaryPath,
		backoff:    sentinelInitialBackoff,
		stopCh:     make(chan struct{}),
	}

	// Compute initial SHA256 hash of the binary.
	s.lastHash, err = hashFile(binaryPath)
	if err != nil {
		log.Fatalf("failed to hash binary: %v", err)
	}
	log.Printf("initial binary hash: %x", s.lastHash[:8])

	// Set up OS signal handler.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start fsnotify watcher in a goroutine.
	updateCh := make(chan struct{}, 1)
	go s.watchBinary(updateCh)

	// Run the main supervision loop.
	s.mainLoop(sigCh, updateCh)
}

// mainLoop is the core supervision loop that manages the child process lifecycle.
func (s *sentinel) mainLoop(sigCh <-chan os.Signal, updateCh <-chan struct{}) {
	for {
		// Check if we should stop before starting a new child.
		select {
		case <-s.stopCh:
			log.Println("sentinel stopping (stopCh closed)")
			return
		default:
		}

		// Start child process.
		child, err := s.startChild()
		if err != nil {
			log.Printf("failed to start child: %v", err)
			s.sleepBackoff()
			s.increaseBackoff()
			continue
		}

		startTime := time.Now()

		// Wait for child exit in a goroutine.
		childDone := make(chan error, 1)
		go func() {
			childDone <- child.Wait()
		}()

		// Wait for one of: child exit, binary update, or OS signal.
		select {
		case err := <-childDone:
			// Child exited on its own.
			elapsed := time.Since(startTime)
			if err != nil {
				log.Printf("child exited with error after %v: %v", elapsed, err)
				if elapsed >= sentinelSuccessRunTime {
					// Ran long enough — reset backoff.
					s.backoff = sentinelInitialBackoff
				}
				s.sleepBackoff()
				s.increaseBackoff()
			} else {
				// Clean exit. The "run" subcommand normally runs forever,
				// so a clean exit is unexpected and warrants a restart.
				log.Printf("child exited cleanly after %v", elapsed)
				s.backoff = sentinelInitialBackoff
				time.Sleep(1 * time.Second)
			}

		case <-updateCh:
			// Binary was updated on disk.
			log.Println("binary update detected, restarting child...")
			s.stopChild(child)
			// Drain childDone so we don't leak the goroutine.
			<-childDone
			// Refresh the hash for the new binary.
			if h, err := hashFile(s.binaryPath); err == nil {
				s.lastHash = h
				log.Printf("new binary hash: %x", s.lastHash[:8])
			}
			s.backoff = sentinelInitialBackoff

		case sig := <-sigCh:
			// Sentinel received SIGINT/SIGTERM — forward to child and shut down.
			log.Printf("received %v, forwarding to child and shutting down...", sig)
			s.stopChild(child)
			// Wait for child to actually exit.
			<-childDone
			log.Println("sentinel exiting")
			return
		}
	}
}

// startChild launches a new "taskguild-agent run" child process.
func (s *sentinel) startChild() (*exec.Cmd, error) {
	cmd := exec.Command(s.binaryPath, "run")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Child inherits environment (env vars like TASKGUILD_*).
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("exec %s run: %w", s.binaryPath, err)
	}

	log.Printf("started child process (pid: %d)", cmd.Process.Pid)
	return cmd, nil
}

// stopChild sends SIGTERM to the child process and schedules a SIGKILL
// after the grace period if the process doesn't exit.
// It does NOT call cmd.Wait() — the caller is responsible for draining childDone.
func (s *sentinel) stopChild(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	pid := cmd.Process.Pid
	log.Printf("sending SIGTERM to child (pid: %d)", pid)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("failed to send SIGTERM (process may have already exited): %v", err)
		return
	}

	// Schedule a SIGKILL after the grace period.
	go func() {
		time.Sleep(sentinelGracePeriod)
		// Check if process is still alive by trying to signal it.
		if err := cmd.Process.Signal(syscall.Signal(0)); err == nil {
			log.Printf("grace period expired, sending SIGKILL to child (pid: %d)", pid)
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("failed to send SIGKILL: %v", err)
			}
		}
	}()
}

// watchBinary watches the parent directory of the binary for filesystem events
// using fsnotify. When a relevant event is detected and the SHA256 hash has
// changed, it sends a notification on updateCh.
func (s *sentinel) watchBinary(updateCh chan<- struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("failed to create fsnotify watcher: %v", err)
		return
	}
	defer watcher.Close()

	// Watch the parent directory, not the file itself.
	// Many deployment tools do atomic replace (write temp file, rename),
	// which changes the inode. Watching the directory catches these renames.
	watchDir := filepath.Dir(s.binaryPath)
	binaryName := filepath.Base(s.binaryPath)

	if err := watcher.Add(watchDir); err != nil {
		log.Printf("failed to watch directory %s: %v", watchDir, err)
		return
	}
	log.Printf("watching directory %s for changes to %s", watchDir, binaryName)

	// Debounce timer: after a relevant event, wait before computing the checksum
	// to let multiple rapid events settle (e.g., atomic deploy: write + rename).
	var debounceTimer *time.Timer

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only care about events for our binary filename.
			if filepath.Base(event.Name) != binaryName {
				continue
			}
			// Interesting operations: Create (atomic rename lands here), Write, Rename.
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}

			log.Printf("detected filesystem event: %s %s", event.Op, event.Name)

			// Reset debounce timer.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(sentinelDebounceInterval, func() {
				newHash, err := hashFile(s.binaryPath)
				if err != nil {
					log.Printf("failed to hash binary after event: %v", err)
					return
				}
				if newHash != s.lastHash {
					log.Printf("binary checksum changed (old: %x, new: %x)",
						s.lastHash[:8], newHash[:8])
					// Non-blocking send.
					select {
					case updateCh <- struct{}{}:
					default:
					}
				} else {
					log.Printf("filesystem event but checksum unchanged, ignoring")
				}
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("fsnotify error: %v", err)

		case <-s.stopCh:
			return
		}
	}
}

// hashFile computes the SHA256 hash of the file at the given path.
func hashFile(path string) ([sha256.Size]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("hash %s: %w", path, err)
	}

	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result, nil
}

// sleepBackoff waits for the current backoff duration.
// It can be interrupted by closing stopCh.
func (s *sentinel) sleepBackoff() {
	log.Printf("waiting %v before restart...", s.backoff)
	select {
	case <-time.After(s.backoff):
	case <-s.stopCh:
	}
}

// increaseBackoff multiplies the backoff by the factor, capping at the maximum.
func (s *sentinel) increaseBackoff() {
	s.backoff = time.Duration(float64(s.backoff) * sentinelBackoffFactor)
	if s.backoff > sentinelMaxBackoff {
		s.backoff = sentinelMaxBackoff
	}
}
