package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Transport interface defines methods for communicating with the CLI
type Transport interface {
	Connect(ctx context.Context) error
	ReceiveMessages() <-chan map[string]interface{}
	Disconnect() error
}

// SubprocessCLITransport implements Transport using subprocess communication
type SubprocessCLITransport struct {
	prompt   string
	options  *ClaudeCodeOptions
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	messages chan map[string]interface{}
	wg       sync.WaitGroup
	mu       sync.Mutex
}

// NewSubprocessCLITransport creates a new subprocess transport
func NewSubprocessCLITransport(prompt string, options *ClaudeCodeOptions) *SubprocessCLITransport {
	return &SubprocessCLITransport{
		prompt:   prompt,
		options:  options,
		messages: make(chan map[string]interface{}),
	}
}

// Connect establishes connection to the CLI
func (t *SubprocessCLITransport) Connect(ctx context.Context) error {
	// Find CLI binary
	cliPath, err := findCLI()
	if err != nil {
		log.Printf("[Claude Code Transport] Failed to find CLI: %v", err)
		return err
	}
	log.Printf("[Claude Code Transport] Found CLI at: %s", cliPath)

	// Build command
	args := t.buildCommand()
	t.cmd = exec.CommandContext(ctx, cliPath, args...)
	log.Printf("[Claude Code Transport] Command: %s", cliPath)
	for i, arg := range args {
		log.Printf("[Claude Code Transport] Arg[%d]: %s", i, arg)
	}

	// Set environment
	t.cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go")

	// Set working directory if specified
	if t.options.Cwd != nil {
		t.cmd.Dir = *t.options.Cwd
		log.Printf("[Claude Code Transport] Working directory: %s", *t.options.Cwd)
	}

	// Setup pipes
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		log.Printf("[Claude Code Transport] Failed to create stdout pipe: %v", err)
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		log.Printf("[Claude Code Transport] Failed to create stderr pipe: %v", err)
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	log.Printf("[Claude Code Transport] Starting CLI process...")
	if err := t.cmd.Start(); err != nil {
		log.Printf("[Claude Code Transport] Failed to start process: %v", err)
		return NewCLIConnectionError(fmt.Sprintf("failed to start CLI process: %v", err))
	}
	log.Printf("[Claude Code Transport] CLI process started with PID: %d", t.cmd.Process.Pid)

	// Start reading from stdout and stderr
	t.wg.Add(2)
	go t.readStdout()
	go t.readStderr()
	log.Printf("[Claude Code Transport] Started stdout/stderr readers")

	return nil
}

// ReceiveMessages returns a channel for receiving messages
func (t *SubprocessCLITransport) ReceiveMessages() <-chan map[string]interface{} {
	return t.messages
}

// Disconnect terminates the CLI process
func (t *SubprocessCLITransport) Disconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	// Close the message channel
	close(t.messages)

	// Try graceful termination first
	if err := t.cmd.Process.Signal(os.Interrupt); err != nil {
		// If interrupt fails, kill the process
		_ = t.cmd.Process.Kill()
	}

	// Wait for a short time for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(5 * time.Second):
		// Timeout, force kill
		_ = t.cmd.Process.Kill()
		<-done
	}

	// Wait for goroutines to finish
	t.wg.Wait()

	return nil
}

// buildCommand constructs the CLI command arguments
func (t *SubprocessCLITransport) buildCommand() []string {
	args := []string{
		"--print", // Use print mode for non-interactive
		"--output-format", "stream-json",
		"--verbose",
	}

	// Add MCP config early in the arguments (BEFORE other options)
	if len(t.options.McpServers) > 0 {
		mcpConfig := map[string]interface{}{
			"mcpServers": t.options.McpServers,
		}
		configJSON, err := json.Marshal(mcpConfig)
		if err == nil {
			jsonStr := string(configJSON)
			args = append(args, "--mcp-config", jsonStr)
			log.Printf("[Claude Code Transport] Added MCP config early: %s", jsonStr)
		}
	}

	// Add options
	if t.options.Model != nil {
		args = append(args, "--model", *t.options.Model)
	}

	if t.options.Resume != nil {
		args = append(args, "--resume", *t.options.Resume)
	}

	if t.options.ContinueConversation {
		args = append(args, "--continue")
	}

	if t.options.AppendSystemPrompt != nil {
		args = append(args, "--append-system-prompt", *t.options.AppendSystemPrompt)
	}

	// Add allowed tools
	if len(t.options.AllowedTools) > 0 {
		args = append(args, "--allowed-tools")
		args = append(args, t.options.AllowedTools...)
	}

	// Add disallowed tools
	if len(t.options.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools")
		args = append(args, t.options.DisallowedTools...)
	}

	// Add permission mode
	if t.options.PermissionMode != nil {
		switch *t.options.PermissionMode {
		case PermissionModeAcceptEdits:
			args = append(args, "--permission-mode", "acceptEdits")
		case PermissionModeBypassPermissions:
			args = append(args, "--permission-mode", "bypassPermissions")
		}
	}

	// MCP config is now added early in the arguments

	// Add prompt as the last argument
	args = append(args, t.prompt)

	return args
}

// readStdout reads JSON messages from stdout
func (t *SubprocessCLITransport) readStdout() {
	defer t.wg.Done()
	log.Printf("[Claude Code Transport] Started reading stdout")

	scanner := bufio.NewScanner(t.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line size

	lineCount := 0
	for scanner.Scan() {
		lineCount++
		line := scanner.Text()
		line = strings.TrimSpace(line)

		log.Printf("[Claude Code Transport] Stdout line #%d: %s", lineCount, line)

		if line == "" {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Printf("[Claude Code Transport] Failed to parse JSON: %v, line: %s", err, line)
			continue
		}

		log.Printf("[Claude Code Transport] Parsed message: %+v", msg)

		select {
		case t.messages <- msg:
			log.Printf("[Claude Code Transport] Sent message to channel")
		case <-time.After(time.Second):
			log.Printf("[Claude Code Transport] Timeout sending message, likely receiver is not reading")
			return
		}
	}

	log.Printf("[Claude Code Transport] Finished reading stdout, processed %d lines", lineCount)

	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("[Claude Code Transport] Scanner error: %v", err)
		return
	}
}

// readStderr reads error messages from stderr
func (t *SubprocessCLITransport) readStderr() {
	defer t.wg.Done()
	log.Printf("[Claude Code Transport] Started reading stderr")

	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[Claude Code Transport] Stderr: %s", line)
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("[Claude Code Transport] Stderr scanner error: %v", err)
	}
	log.Printf("[Claude Code Transport] Finished reading stderr")
}

// findCLI attempts to locate the Claude CLI binary
func findCLI() (string, error) {
	// First, try to find 'claude' in PATH
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}

	// Try common installation locations
	commonPaths := []string{
		// npm global installations
		filepath.Join(os.Getenv("HOME"), ".npm", "bin", "claude"),
		filepath.Join(os.Getenv("HOME"), "node_modules", ".bin", "claude"),
		"/usr/local/bin/claude",
		"/usr/bin/claude",
		// Windows paths
		filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
		filepath.Join(os.Getenv("APPDATA"), "npm", "claude"),
		// macOS/Linux npm prefix
		filepath.Join(os.Getenv("HOME"), ".nvm", "versions", "node", "*", "bin", "claude"),
	}

	for _, path := range commonPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Check if Node.js is installed
	if _, err := exec.LookPath("node"); err != nil {
		return "", NewCLINotFoundError("")
	}

	return "", NewCLINotFoundError("")
}
