package eventbus

// Event metadata keys carried in taskguildv1.Event.Metadata.
//
// These values are part of the wire contract shared with the frontend
// (frontend/src/lib/event-stream.ts, components/organisms/WorktreeList.tsx) and
// with server-side readers (internal/event, internal/orchestrator,
// internal/interaction, internal/chatnotifier). Do not rename the values.
const (
	MetaProjectID      = "project_id"
	MetaTaskID         = "task_id"
	MetaAgentID        = "agent_id"
	MetaWorkflowID     = "workflow_id"
	MetaRequestID      = "request_id"
	MetaScriptID       = "script_id"
	MetaAgentManagerID = "agent_manager_id"
	MetaAgentConfigID  = "agent_config_id"
	MetaAgentStatus    = "agent_status"
	MetaWorktreeName   = "worktree_name"
	MetaNewStatusID    = "new_status_id"
	MetaReason         = "reason"
	MetaRetryCount     = "retry_count"
	MetaSuccess        = "success"
	MetaOutput         = "output"
	MetaErrorMessage   = "error_message"
	MetaExitCode       = "exit_code"
	MetaDiffCount      = "diff_count"
	MetaMessage        = "message"
)
