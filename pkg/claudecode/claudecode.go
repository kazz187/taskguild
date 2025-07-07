// Package claudecode provides a Go SDK for interacting with Claude Code.
//
// This package allows Go developers to integrate Claude Code capabilities into their applications.
// It provides a simple API to send prompts to Claude and handle responses including text, tool usage, and results.
//
// Basic usage:
//
//	import (
//	    "context"
//	    "fmt"
//	    "github.com/kazz187/claude-code-sdk-go/pkg/claudecode"
//	)
//
//	func main() {
//	    ctx := context.Background()
//
//	    // Create a new client
//	    client := claudecode.NewClient()
//
//	    // Query Claude
//	    messages, err := client.Query(ctx, "What is 2 + 2?", nil)
//	    if err != nil {
//	        panic(err)
//	    }
//
//	    // Process messages
//	    for msg := range messages {
//	        switch m := msg.(type) {
//	        case claudecode.AssistantMessage:
//	            for _, block := range m.Content {
//	                if textBlock, ok := block.(claudecode.TextBlock); ok {
//	                    fmt.Println(textBlock.Text)
//	                }
//	            }
//	        }
//	    }
//	}
package claudecode

import (
	"context"
)

// Query is a convenience function that creates a client and sends a query to Claude.
// This is the main entry point for most use cases.
//
// Example:
//
//	messages, err := claudecode.Query(ctx, "Hello Claude", nil)
//	if err != nil {
//	    return err
//	}
//
//	for msg := range messages {
//	    // Process messages
//	}
func Query(ctx context.Context, prompt string, options *ClaudeCodeOptions) (<-chan Message, error) {
	client := NewClient()
	return client.Query(ctx, prompt, options)
}

// Version returns the current version of the SDK
func Version() string {
	return "0.1.0"
}
