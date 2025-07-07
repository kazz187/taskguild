package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		return err
	}

	// Build command
	args := t.buildCommand()
	t.cmd = exec.CommandContext(ctx, cliPath, args...)

	// Set environment
	t.cmd.Env = append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go")

	// Set working directory if specified
	if t.options.Cwd != nil {
		t.cmd.Dir = *t.options.Cwd
	}

	// Setup pipes
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := t.cmd.Start(); err != nil {
		return NewCLIConnectionError(fmt.Sprintf("failed to start CLI process: %v", err))
	}

	// Start reading from stdout and stderr
	t.wg.Add(2)
	go t.readStdout()
	go t.readStderr()

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
		"--output-format", "stream-json",
		"--verbose",
	}

	// Add prompt
	args = append(args, "--prompt", t.prompt)

	// Add options
	if t.options.Model != nil {
		args = append(args, "--model", *t.options.Model)
	}

	if t.options.MaxTurns != nil {
		args = append(args, "--max-turns", fmt.Sprintf("%d", *t.options.MaxTurns))
	}

	if t.options.Resume != nil {
		args = append(args, "--resume", *t.options.Resume)
	}

	if t.options.ContinueConversation {
		args = append(args, "--continue")
	}

	if t.options.SystemPrompt != nil {
		args = append(args, "--system-prompt", *t.options.SystemPrompt)
	}

	if t.options.AppendSystemPrompt != nil {
		args = append(args, "--append-system-prompt", *t.options.AppendSystemPrompt)
	}

	// Add allowed tools
	for _, tool := range t.options.AllowedTools {
		args = append(args, "--allow-tool", tool)
	}

	// Add disallowed tools
	for _, tool := range t.options.DisallowedTools {
		args = append(args, "--disallow-tool", tool)
	}

	// Add MCP tools
	for _, tool := range t.options.McpTools {
		args = append(args, "--mcp-tool", tool)
	}

	// Add permission mode
	if t.options.PermissionMode != nil {
		switch *t.options.PermissionMode {
		case PermissionModeAcceptEdits:
			args = append(args, "--accept-edits")
		case PermissionModeBypassPermissions:
			args = append(args, "--bypass-permissions")
		}
	}

	if t.options.PermissionPromptToolName != nil {
		args = append(args, "--permission-prompt-tool-name", *t.options.PermissionPromptToolName)
	}

	args = append(args, "--max-thinking-tokens", fmt.Sprintf("%d", t.options.MaxThinkingTokens))

	// Add MCP servers
	for name, config := range t.options.McpServers {
		configJSON, _ := json.Marshal(config)
		args = append(args, "--mcp-server", fmt.Sprintf("%s=%s", name, string(configJSON)))
	}

	return args
}

// readStdout reads JSON messages from stdout
func (t *SubprocessCLITransport) readStdout() {
	defer t.wg.Done()

	scanner := bufio.NewScanner(t.stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line size

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		if line == "" {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Log error but continue processing
			continue
		}

		select {
		case t.messages <- msg:
		case <-time.After(time.Second):
			// Timeout sending message, likely receiver is not reading
			return
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		// Handle scanner error
		// For now, we'll just return
		return
	}
}

// readStderr reads error messages from stderr
func (t *SubprocessCLITransport) readStderr() {
	defer t.wg.Done()

	// For now, we'll just consume stderr to prevent blocking
	// In a production implementation, you might want to log these
	buf := make([]byte, 4096)
	for {
		_, err := t.stderr.Read(buf)
		if err != nil {
			break
		}
	}
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
