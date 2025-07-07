package claudecode

import (
	"fmt"
)

// ClaudeSDKError is the base error type for all Claude SDK errors
type ClaudeSDKError struct {
	Message string
}

func (e *ClaudeSDKError) Error() string {
	return e.Message
}

// CLIConnectionError represents an error connecting to the Claude CLI
type CLIConnectionError struct {
	ClaudeSDKError
}

// NewCLIConnectionError creates a new CLIConnectionError
func NewCLIConnectionError(message string) *CLIConnectionError {
	return &CLIConnectionError{
		ClaudeSDKError: ClaudeSDKError{Message: message},
	}
}

// CLINotFoundError represents an error when the Claude CLI is not found
type CLINotFoundError struct {
	CLIConnectionError
	CLIPath string
}

// NewCLINotFoundError creates a new CLINotFoundError
func NewCLINotFoundError(cliPath string) *CLINotFoundError {
	message := fmt.Sprintf("Claude Code CLI not found at '%s'. Please install it with: npm install -g @anthropic-ai/claude-code", cliPath)
	if cliPath == "" {
		message = "Claude Code CLI not found. Please install it with: npm install -g @anthropic-ai/claude-code"
	}
	return &CLINotFoundError{
		CLIConnectionError: CLIConnectionError{
			ClaudeSDKError: ClaudeSDKError{Message: message},
		},
		CLIPath: cliPath,
	}
}

// ProcessError represents an error during process execution
type ProcessError struct {
	ClaudeSDKError
	ExitCode *int
	Stderr   *string
}

// NewProcessError creates a new ProcessError
func NewProcessError(message string, exitCode *int, stderr *string) *ProcessError {
	return &ProcessError{
		ClaudeSDKError: ClaudeSDKError{Message: message},
		ExitCode:       exitCode,
		Stderr:         stderr,
	}
}

// CLIJSONDecodeError represents an error decoding JSON from the CLI
type CLIJSONDecodeError struct {
	ClaudeSDKError
	Line          string
	OriginalError error
}

// NewCLIJSONDecodeError creates a new CLIJSONDecodeError
func NewCLIJSONDecodeError(line string, originalError error) *CLIJSONDecodeError {
	message := fmt.Sprintf("Failed to decode JSON from CLI: %v\nLine: %s", originalError, line)
	return &CLIJSONDecodeError{
		ClaudeSDKError: ClaudeSDKError{Message: message},
		Line:           line,
		OriginalError:  originalError,
	}
}

// Unwrap returns the underlying error for CLIJSONDecodeError
func (e *CLIJSONDecodeError) Unwrap() error {
	return e.OriginalError
}
