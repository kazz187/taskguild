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
	HookTriggerUnspecified          HookTrigger = ""
	HookTriggerBeforeTaskExecution  HookTrigger = "before_task_execution"
	HookTriggerAfterTaskExecution   HookTrigger = "after_task_execution"
	HookTriggerAfterWorktreeCreation HookTrigger = "after_worktree_creation"
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
	ID            string       `yaml:"id"`
	Name          string       `yaml:"name"`
	Order         int32        `yaml:"order"`
	IsInitial     bool         `yaml:"is_initial"`
	IsTerminal    bool         `yaml:"is_terminal"`
	TransitionsTo []string     `yaml:"transitions_to"`
	AgentID       string       `yaml:"agent_id,omitempty"`
	Hooks         []StatusHook `yaml:"hooks,omitempty"`
}

// FindAgentIDForStatus returns the agent ID configured for the given status.
// It first checks the status-level AgentID field, then falls back to the
// legacy AgentConfig list on the workflow. Returns "" if no agent is configured.
func (w *Workflow) FindAgentIDForStatus(statusID string) string {
	for _, s := range w.Statuses {
		if s.ID == statusID && s.AgentID != "" {
			return s.AgentID
		}
	}
	for _, cfg := range w.AgentConfigs {
		if cfg.WorkflowStatusID == statusID {
			return cfg.ID
		}
	}
	return ""
}

type AgentConfig struct {
	ID               string   `yaml:"id"`
	WorkflowStatusID string   `yaml:"workflow_status_id"`
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	Instructions     string   `yaml:"instructions"`
	AllowedTools     []string `yaml:"allowed_tools"`
}
