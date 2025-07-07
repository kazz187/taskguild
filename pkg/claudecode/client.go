package claudecode

import (
	"context"
	"fmt"
)

// Client is the interface for interacting with Claude Code
type Client interface {
	// Query sends a prompt to Claude and returns a channel of messages
	Query(ctx context.Context, prompt string, options *ClaudeCodeOptions) (<-chan Message, error)
}

// internalClient is the internal implementation of the Client interface
type internalClient struct{}

// NewClient creates a new Claude Code client
func NewClient() Client {
	return &internalClient{}
}

// Query sends a prompt to Claude and returns a channel of messages
func (c *internalClient) Query(ctx context.Context, prompt string, options *ClaudeCodeOptions) (<-chan Message, error) {
	if options == nil {
		options = NewClaudeCodeOptions()
	}

	// Create transport
	transport := NewSubprocessCLITransport(prompt, options)

	// Connect to CLI
	if err := transport.Connect(ctx); err != nil {
		return nil, err
	}

	// Create message channel
	messages := make(chan Message)

	// Start goroutine to receive messages
	go func() {
		defer close(messages)
		defer transport.Disconnect()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-transport.ReceiveMessages():
				if !ok {
					return
				}

				parsedMsg, err := parseMessage(msg)
				if err != nil {
					// If we can't parse a message, we should probably log it
					// For now, we'll continue to the next message
					continue
				}

				select {
				case messages <- parsedMsg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return messages, nil
}

// parseMessage converts a raw JSON message from the CLI into a Message type
func parseMessage(raw map[string]interface{}) (Message, error) {
	msgType, ok := raw["type"].(string)
	if !ok {
		return nil, fmt.Errorf("message missing type field")
	}

	switch msgType {
	case "user":
		content, ok := raw["content"].(string)
		if !ok {
			return nil, fmt.Errorf("user message missing content field")
		}
		return UserMessage{Content: content}, nil

	case "assistant":
		contentRaw, ok := raw["content"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("assistant message missing content field")
		}

		var content []ContentBlock
		for _, blockRaw := range contentRaw {
			block, err := parseContentBlock(blockRaw)
			if err != nil {
				return nil, err
			}
			content = append(content, block)
		}

		return AssistantMessage{Content: content}, nil

	case "system":
		subtype, ok := raw["subtype"].(string)
		if !ok {
			return nil, fmt.Errorf("system message missing subtype field")
		}

		data, ok := raw["data"].(map[string]interface{})
		if !ok {
			data = make(map[string]interface{})
		}

		return SystemMessage{
			Subtype: subtype,
			Data:    data,
		}, nil

	case "result":
		// Parse result message fields
		var result ResultMessage

		if subtype, ok := raw["subtype"].(string); ok {
			result.Subtype = subtype
		}

		if durationMs, ok := raw["duration_ms"].(float64); ok {
			result.DurationMs = int(durationMs)
		}

		if durationApiMs, ok := raw["duration_api_ms"].(float64); ok {
			result.DurationApiMs = int(durationApiMs)
		}

		if isError, ok := raw["is_error"].(bool); ok {
			result.IsError = isError
		}

		if numTurns, ok := raw["num_turns"].(float64); ok {
			result.NumTurns = int(numTurns)
		}

		if sessionID, ok := raw["session_id"].(string); ok {
			result.SessionID = sessionID
		}

		if totalCostUSD, ok := raw["total_cost_usd"].(float64); ok {
			result.TotalCostUSD = &totalCostUSD
		}

		if usage, ok := raw["usage"].(map[string]interface{}); ok {
			result.Usage = usage
		}

		if resultStr, ok := raw["result"].(string); ok {
			result.Result = &resultStr
		}

		return result, nil

	default:
		return nil, fmt.Errorf("unknown message type: %s", msgType)
	}
}

// parseContentBlock converts a raw content block into a ContentBlock type
func parseContentBlock(raw interface{}) (ContentBlock, error) {
	blockMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("content block is not a map")
	}

	blockType, ok := blockMap["type"].(string)
	if !ok {
		return nil, fmt.Errorf("content block missing type field")
	}

	switch blockType {
	case "text":
		text, ok := blockMap["text"].(string)
		if !ok {
			return nil, fmt.Errorf("text block missing text field")
		}
		return TextBlock{Text: text}, nil

	case "tool_use":
		id, ok := blockMap["id"].(string)
		if !ok {
			return nil, fmt.Errorf("tool_use block missing id field")
		}

		name, ok := blockMap["name"].(string)
		if !ok {
			return nil, fmt.Errorf("tool_use block missing name field")
		}

		input, ok := blockMap["input"].(map[string]interface{})
		if !ok {
			input = make(map[string]interface{})
		}

		return ToolUseBlock{
			ID:    id,
			Name:  name,
			Input: input,
		}, nil

	case "tool_result":
		toolUseID, ok := blockMap["tool_use_id"].(string)
		if !ok {
			return nil, fmt.Errorf("tool_result block missing tool_use_id field")
		}

		block := ToolResultBlock{
			ToolUseID: toolUseID,
		}

		if content, ok := blockMap["content"]; ok {
			block.Content = content
		}

		if isError, ok := blockMap["is_error"].(bool); ok {
			block.IsError = &isError
		}

		return block, nil

	default:
		return nil, fmt.Errorf("unknown content block type: %s", blockType)
	}
}

// marshalMessage converts a Message to a map for JSON encoding
func marshalMessage(msg Message) (map[string]interface{}, error) {
	switch m := msg.(type) {
	case UserMessage:
		return map[string]interface{}{
			"type":    "user",
			"content": m.Content,
		}, nil

	case AssistantMessage:
		var content []interface{}
		for _, block := range m.Content {
			blockMap, err := marshalContentBlock(block)
			if err != nil {
				return nil, err
			}
			content = append(content, blockMap)
		}
		return map[string]interface{}{
			"type":    "assistant",
			"content": content,
		}, nil

	case SystemMessage:
		return map[string]interface{}{
			"type":    "system",
			"subtype": m.Subtype,
			"data":    m.Data,
		}, nil

	case ResultMessage:
		result := map[string]interface{}{
			"type":            "result",
			"subtype":         m.Subtype,
			"duration_ms":     m.DurationMs,
			"duration_api_ms": m.DurationApiMs,
			"is_error":        m.IsError,
			"num_turns":       m.NumTurns,
			"session_id":      m.SessionID,
		}

		if m.TotalCostUSD != nil {
			result["total_cost_usd"] = *m.TotalCostUSD
		}

		if m.Usage != nil {
			result["usage"] = m.Usage
		}

		if m.Result != nil {
			result["result"] = *m.Result
		}

		return result, nil

	default:
		return nil, fmt.Errorf("unknown message type: %T", msg)
	}
}

// marshalContentBlock converts a ContentBlock to a map for JSON encoding
func marshalContentBlock(block ContentBlock) (map[string]interface{}, error) {
	switch b := block.(type) {
	case TextBlock:
		return map[string]interface{}{
			"type": "text",
			"text": b.Text,
		}, nil

	case ToolUseBlock:
		return map[string]interface{}{
			"type":  "tool_use",
			"id":    b.ID,
			"name":  b.Name,
			"input": b.Input,
		}, nil

	case ToolResultBlock:
		result := map[string]interface{}{
			"type":        "tool_result",
			"tool_use_id": b.ToolUseID,
		}

		if b.Content != nil {
			result["content"] = b.Content
		}

		if b.IsError != nil {
			result["is_error"] = *b.IsError
		}

		return result, nil

	default:
		return nil, fmt.Errorf("unknown content block type: %T", block)
	}
}
