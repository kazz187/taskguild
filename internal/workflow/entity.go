package workflow

import "time"

type Workflow struct {
	ID           string        `yaml:"id"`
	ProjectID    string        `yaml:"project_id"`
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	Statuses     []Status      `yaml:"statuses"`
	AgentConfigs []AgentConfig `yaml:"agent_configs"`
	CreatedAt    time.Time     `yaml:"created_at"`
	UpdatedAt    time.Time     `yaml:"updated_at"`

	// Task defaults
	DefaultPermissionMode string `yaml:"default_permission_mode"`
	DefaultUseWorktree    bool   `yaml:"default_use_worktree"`

	// Custom prompt prepended to agent instructions for tasks in this workflow
	CustomPrompt string `yaml:"custom_prompt"`
}

type HookTrigger string

const (
	HookTriggerUnspecified            HookTrigger = ""
	HookTriggerBeforeTaskExecution    HookTrigger = "before_task_execution"
	HookTriggerAfterTaskExecution     HookTrigger = "after_task_execution"
	HookTriggerAfterWorktreeCreation  HookTrigger = "after_worktree_creation"
	HookTriggerBeforeWorktreeCreation HookTrigger = "before_worktree_creation"
)

type HookActionType string

const (
	HookActionTypeUnspecified HookActionType = ""
	HookActionTypeSkill       HookActionType = "skill"
	HookActionTypeScript      HookActionType = "script"
)

type StatusHook struct {
	ID         string         `yaml:"id"`
	SkillID    string         `yaml:"skill_id"`
	Trigger    HookTrigger    `yaml:"trigger"`
	Order      int32          `yaml:"order"`
	Name       string         `yaml:"name"`
	ActionType HookActionType `yaml:"action_type,omitempty"`
	ActionID   string         `yaml:"action_id,omitempty"`
}

type Status struct {
	Name          string       `yaml:"name"`
	Order         int32        `yaml:"order"`
	IsInitial     bool         `yaml:"is_initial"`
	IsTerminal    bool         `yaml:"is_terminal"`
	TransitionsTo []string     `yaml:"transitions_to"`
	AgentID       string       `yaml:"agent_id,omitempty"`
	Hooks         []StatusHook `yaml:"hooks,omitempty"`

	// PermissionMode for agents executing tasks in this status.
	PermissionMode string `yaml:"permission_mode,omitempty"`

	// InheritSessionFrom is the name of a previous status whose session
	// should be forked when entering this status. Empty means fresh session.
	InheritSessionFrom string `yaml:"inherit_session_from,omitempty"`

	// Execution configuration (replaces agent MD frontmatter)
	Model           string   `yaml:"model,omitempty"`
	Tools           []string `yaml:"tools,omitempty"`
	DisallowedTools []string `yaml:"disallowed_tools,omitempty"`
	SkillIDs        []string `yaml:"skill_ids,omitempty"`
	Effort          string   `yaml:"effort,omitempty"` // "low" / "medium" / "high" / "max"

	// Skill-based harness: appends failure patterns to Skill files.
	EnableSkillHarness             bool `yaml:"enable_skill_harness"`
	SkillHarnessExplicitlyDisabled bool `yaml:"skill_harness_explicitly_disabled,omitempty"`
}

// FindAgentIDForStatus returns the agent ID configured for the given status.
// It first checks the status-level AgentID field, then falls back to the
// legacy AgentConfig list on the workflow. Returns "" if no agent is configured.
func (w *Workflow) FindAgentIDForStatus(statusName string) string {
	for _, s := range w.Statuses {
		if s.Name == statusName && s.AgentID != "" {
			return s.AgentID
		}
	}
	for _, cfg := range w.AgentConfigs {
		if cfg.WorkflowStatusID == statusName {
			return cfg.ID
		}
	}
	return ""
}

// FindSkillIDsForStatus returns the skill IDs configured for the given status.
// Returns nil if no skills are configured.
func (w *Workflow) FindSkillIDsForStatus(statusName string) []string {
	for _, s := range w.Statuses {
		if s.Name == statusName && len(s.SkillIDs) > 0 {
			return s.SkillIDs
		}
	}
	return nil
}

type AgentConfig struct {
	ID               string   `yaml:"id"`
	WorkflowStatusID string   `yaml:"workflow_status_id"`
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	Instructions     string   `yaml:"instructions"`
	AllowedTools     []string `yaml:"allowed_tools"`
}
