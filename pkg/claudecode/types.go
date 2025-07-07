package claudecode

// PermissionMode represents the permission handling mode for tools
type PermissionMode string

const (
	PermissionModeDefault           PermissionMode = "default"
	PermissionModeAcceptEdits       PermissionMode = "acceptEdits"
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// McpServerConfig represents MCP server configuration
type McpServerConfig struct {
	Transport []string               `json:"transport"`
	Env       map[string]interface{} `json:"env,omitempty"`
}

// ContentBlock is an interface for different types of content blocks
type ContentBlock interface {
	isContentBlock()
}

// TextBlock represents a text content block
type TextBlock struct {
	Text string `json:"text"`
}

func (TextBlock) isContentBlock() {}

// ToolUseBlock represents a tool use content block
type ToolUseBlock struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

func (ToolUseBlock) isContentBlock() {}

// ToolResultBlock represents a tool result content block
type ToolResultBlock struct {
	ToolUseID string      `json:"tool_use_id"`
	Content   interface{} `json:"content,omitempty"` // string or []map[string]interface{} or nil
	IsError   *bool       `json:"is_error,omitempty"`
}

func (ToolResultBlock) isContentBlock() {}

// Message is an interface for different types of messages
type Message interface {
	isMessage()
}

// UserMessage represents a message from the user
type UserMessage struct {
	Content string `json:"content"`
}

func (UserMessage) isMessage() {}

// AssistantMessage represents a message from the assistant
type AssistantMessage struct {
	Content []ContentBlock `json:"content"`
}

func (AssistantMessage) isMessage() {}

// SystemMessage represents a system message
type SystemMessage struct {
	Subtype string                 `json:"subtype"`
	Data    map[string]interface{} `json:"data"`
}

func (SystemMessage) isMessage() {}

// ResultMessage represents a result message with metadata
type ResultMessage struct {
	Subtype       string                 `json:"subtype"`
	DurationMs    int                    `json:"duration_ms"`
	DurationApiMs int                    `json:"duration_api_ms"`
	IsError       bool                   `json:"is_error"`
	NumTurns      int                    `json:"num_turns"`
	SessionID     string                 `json:"session_id"`
	TotalCostUSD  *float64               `json:"total_cost_usd,omitempty"`
	Usage         map[string]interface{} `json:"usage,omitempty"`
	Result        *string                `json:"result,omitempty"`
}

func (ResultMessage) isMessage() {}

// ClaudeCodeOptions represents configuration options for Claude Code queries
type ClaudeCodeOptions struct {
	// AllowedTools is a list of tool names that Claude can use
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// MaxThinkingTokens is the maximum number of thinking tokens Claude can use
	MaxThinkingTokens int `json:"max_thinking_tokens,omitempty"`

	// SystemPrompt is the system prompt to use (overrides default)
	SystemPrompt *string `json:"system_prompt,omitempty"`

	// AppendSystemPrompt is additional text to append to the system prompt
	AppendSystemPrompt *string `json:"append_system_prompt,omitempty"`

	// McpTools is a list of MCP tool names to allow
	McpTools []string `json:"mcp_tools,omitempty"`

	// McpServers is a map of MCP server configurations
	McpServers map[string]McpServerConfig `json:"mcp_servers,omitempty"`

	// PermissionMode controls how permissions are handled
	PermissionMode *PermissionMode `json:"permission_mode,omitempty"`

	// ContinueConversation continues a previous conversation
	ContinueConversation bool `json:"continue_conversation,omitempty"`

	// Resume continues from a specific session ID
	Resume *string `json:"resume,omitempty"`

	// MaxTurns limits the number of conversation turns
	MaxTurns *int `json:"max_turns,omitempty"`

	// DisallowedTools is a list of tool names that Claude cannot use
	DisallowedTools []string `json:"disallowed_tools,omitempty"`

	// Model specifies which Claude model to use
	Model *string `json:"model,omitempty"`

	// PermissionPromptToolName customizes the permission prompt tool name
	PermissionPromptToolName *string `json:"permission_prompt_tool_name,omitempty"`

	// Cwd is the working directory for file operations
	Cwd *string `json:"cwd,omitempty"`
}

// NewClaudeCodeOptions creates a new ClaudeCodeOptions with default values
func NewClaudeCodeOptions() *ClaudeCodeOptions {
	return &ClaudeCodeOptions{
		MaxThinkingTokens: 8000,
	}
}
