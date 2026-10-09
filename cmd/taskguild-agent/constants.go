package main

// Claude Code tool names.
const (
	toolBash            = "Bash"
	toolRead            = "Read"
	toolWrite           = "Write"
	toolEdit            = "Edit"
	toolGlob            = "Glob"
	toolGrep            = "Grep"
	toolWebSearch       = "WebSearch"
	toolWebFetch        = "WebFetch"
	toolNotebookEdit    = "NotebookEdit"
	toolTodoWrite       = "TodoWrite"
	toolAgent           = "Agent"
	toolSkill           = "Skill"
	toolAskUserQuestion = "AskUserQuestion"
	toolExitPlanMode    = "ExitPlanMode"
	toolEnterPlanMode   = "EnterPlanMode"
)

// Interaction option values exchanged with the frontend.
const (
	optionAllow              = "allow"
	optionDeny               = "deny"
	optionAlwaysAllowCommand = "always_allow_command"
	optionApprove            = "approve"
	optionReject             = "reject"
)

// hookDecisionBlock is the Claude Code hook decision that stops a tool call
// (claudeagent.HookOutput.Decision).
const hookDecisionBlock = "block"

// .claude/settings.json permissions section keys.
const (
	settingsKeyPermissions = "permissions"
	settingsKeyAllow       = "allow"
	settingsKeyAsk         = "ask"
	settingsKeyDeny        = "deny"
)

// Task / task-log metadata keys.
const (
	metaClaudeMode    = "claude_mode"
	metaTurn          = "turn"
	metaDirectiveType = "directive_type"
	metaFullText      = "full_text"
	metaResultType    = "result_type"
)

// Anthropic content block field names and type values.
const (
	blockFieldType = "type"
	blockFieldText = "text"
	blockTypeText  = "text"
	blockTypeImage = "image"
)

// msgContextCanceled is the PermissionResultDeny message used when the context
// is canceled while waiting for a user decision.
const msgContextCanceled = "context canceled"
