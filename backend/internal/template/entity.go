package template

import "time"

// Template represents a reusable snapshot of an Agent, Skill, or Script configuration.
// Templates are global (not project-scoped) and can be used across any project.
type Template struct {
	ID           string        `yaml:"id"`
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	EntityType   string        `yaml:"entity_type"` // "agent", "skill", "script"
	AgentConfig  *AgentConfig  `yaml:"agent_config,omitempty"`
	SkillConfig  *SkillConfig  `yaml:"skill_config,omitempty"`
	ScriptConfig *ScriptConfig `yaml:"script_config,omitempty"`
	CreatedAt    time.Time     `yaml:"created_at"`
	UpdatedAt    time.Time     `yaml:"updated_at"`
}

// AgentConfig holds the agent-specific configuration for a template.
type AgentConfig struct {
	Name            string   `yaml:"name"`
	Description     string   `yaml:"description"`
	Prompt          string   `yaml:"prompt"`
	Tools           []string `yaml:"tools"`
	DisallowedTools []string `yaml:"disallowed_tools"`
	Model           string   `yaml:"model"`
	PermissionMode  string   `yaml:"permission_mode"`
	Skills          []string `yaml:"skills"`
	Memory          string   `yaml:"memory"`
}

// SkillConfig holds the skill-specific configuration for a template.
type SkillConfig struct {
	Name                   string   `yaml:"name"`
	Description            string   `yaml:"description"`
	Content                string   `yaml:"content"`
	DisableModelInvocation bool     `yaml:"disable_model_invocation"`
	UserInvocable          bool     `yaml:"user_invocable"`
	AllowedTools           []string `yaml:"allowed_tools"`
	Model                  string   `yaml:"model"`
	Context                string   `yaml:"context"`
	Agent                  string   `yaml:"agent"`
	ArgumentHint           string   `yaml:"argument_hint"`
}

// ScriptConfig holds the script-specific configuration for a template.
type ScriptConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Filename    string `yaml:"filename"`
	Content     string `yaml:"content"`
}
