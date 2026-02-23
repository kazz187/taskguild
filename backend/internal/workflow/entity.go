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
}

type Status struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Order         int32    `yaml:"order"`
	IsInitial     bool     `yaml:"is_initial"`
	IsTerminal    bool     `yaml:"is_terminal"`
	TransitionsTo []string `yaml:"transitions_to"`
}

type AgentConfig struct {
	ID               string   `yaml:"id"`
	WorkflowStatusID string   `yaml:"workflow_status_id"`
	Name             string   `yaml:"name"`
	Description      string   `yaml:"description"`
	Instructions     string   `yaml:"instructions"`
	AllowedTools     []string `yaml:"allowed_tools"`
}
