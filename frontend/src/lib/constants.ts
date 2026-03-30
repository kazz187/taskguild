export const AVAILABLE_TOOLS = [
  'Read', 'Write', 'Edit', 'Glob', 'Grep', 'Bash',
  'WebSearch', 'WebFetch', 'Task', 'NotebookEdit',
]

export const MODEL_OPTIONS = [
  { value: '', label: 'Inherit (default)' },
  { value: 'sonnet', label: 'Sonnet' },
  { value: 'opus', label: 'Opus' },
  { value: 'haiku', label: 'Haiku' },
]

export const CONTEXT_OPTIONS = [
  { value: '', label: 'Inline (default)' },
  { value: 'fork', label: 'Fork (run in sub-agent)' },
]

export const AGENT_OPTIONS = [
  { value: '', label: 'general-purpose (default)' },
  { value: 'Explore', label: 'Explore' },
  { value: 'Plan', label: 'Plan' },
  { value: 'general-purpose', label: 'General Purpose' },
]

export const PERMISSION_MODE_OPTIONS = [
  { value: '', label: 'None (inherit)' },
  { value: 'default', label: 'Default (ask for permission)' },
  { value: 'acceptEdits', label: 'Accept Edits' },
  { value: 'dontAsk', label: "Don't Ask (auto-deny unpermitted)" },
  { value: 'bypassPermissions', label: 'Bypass Permissions' },
  { value: 'plan', label: 'Plan (read-only exploration)' },
]

export const MEMORY_OPTIONS = [
  { value: '', label: 'None' },
  { value: 'user', label: 'User (~/.claude/agent-memory/)' },
  { value: 'project', label: 'Project (.claude/agent-memory/)' },
  { value: 'local', label: 'Local (.claude/agent-memory-local/)' },
]
