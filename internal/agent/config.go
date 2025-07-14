package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Role             string         `yaml:"role"`
	Type             string         `yaml:"type"`
	Memory           string         `yaml:"memory"`
	Triggers         []EventTrigger `yaml:"triggers"`
	ApprovalRequired []ApprovalRule `yaml:"approval_required"`
	Scaling          *ScalingConfig `yaml:"scaling,omitempty"`
}

// IndividualAgentConfig represents a single agent's configuration file
type IndividualAgentConfig struct {
	Role             string         `yaml:"role"`
	Type             string         `yaml:"type"`
	Memory           string         `yaml:"memory"`
	Triggers         []EventTrigger `yaml:"triggers"`
	ApprovalRequired []ApprovalRule `yaml:"approval_required"`
	Scaling          *ScalingConfig `yaml:"scaling,omitempty"`
	Description      string         `yaml:"description,omitempty"`
	Version          string         `yaml:"version,omitempty"`
}

type Config struct {
	Agents []AgentConfig `yaml:"agents"`
}

func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = ".taskguild/agent.yaml"
	}

	// First try to load individual agent configs from .taskguild/agents/ directory
	if config, err := loadIndividualConfigs(); err == nil && len(config.Agents) > 0 {
		return config, nil
	}

	// Fallback to unified config file
	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return createDefaultConfig(configPath)
	}

	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

func loadIndividualConfigs() (*Config, error) {
	agentsDir := ".taskguild/agents"

	// Check if agents directory exists
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("agents directory does not exist")
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	var config Config
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		roleName := entry.Name()
		configFilePath := filepath.Join(agentsDir, roleName, "agent.yaml")

		// Check if individual config file exists
		if _, err := os.Stat(configFilePath); os.IsNotExist(err) {
			continue
		}

		agentConfig, err := loadIndividualAgentConfig(configFilePath)
		if err != nil {
			fmt.Printf("Warning: failed to load agent config %s: %v\n", configFilePath, err)
			continue
		}

		// Convert IndividualAgentConfig to AgentConfig
		memoryPath := agentConfig.Memory
		if !filepath.IsAbs(memoryPath) {
			memoryPath = filepath.Join(agentsDir, roleName, memoryPath)
		}

		config.Agents = append(config.Agents, AgentConfig{
			Role:             agentConfig.Role,
			Type:             agentConfig.Type,
			Memory:           memoryPath,
			Triggers:         agentConfig.Triggers,
			ApprovalRequired: agentConfig.ApprovalRequired,
			Scaling:          agentConfig.Scaling,
		})
	}

	if len(config.Agents) == 0 {
		return nil, fmt.Errorf("no valid agent configs found")
	}

	// Validate config
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid individual configs: %w", err)
	}

	return &config, nil
}

func loadIndividualAgentConfig(configPath string) (*IndividualAgentConfig, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config IndividualAgentConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func SaveIndividualAgentConfig(config *IndividualAgentConfig, agentsDir string) error {
	if agentsDir == "" {
		agentsDir = ".taskguild/agents"
	}

	roleDir := filepath.Join(agentsDir, config.Role)
	if err := os.MkdirAll(roleDir, 0755); err != nil {
		return fmt.Errorf("failed to create role directory: %w", err)
	}

	configPath := filepath.Join(roleDir, "agent.yaml")
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := ioutil.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func SaveConfig(config *Config, configPath string) error {
	if configPath == "" {
		configPath = ".taskguild/agent.yaml"
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := ioutil.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func createDefaultConfig(configPath string) (*Config, error) {
	// Create individual agent configs first
	if err := createDefaultIndividualConfigs(); err != nil {
		return nil, fmt.Errorf("failed to create individual configs: %w", err)
	}

	// Try to load individual configs
	if config, err := loadIndividualConfigs(); err == nil {
		return config, nil
	}

	// Fallback to unified config
	config := &Config{
		Agents: []AgentConfig{
			{
				Role:   "architect",
				Type:   "claude-code",
				Memory: ".taskguild/agents/architect/CLAUDE.md",
				Triggers: []EventTrigger{
					{
						Event:     "task.created",
						Condition: `task.type == "feature"`,
					},
				},
			},
			{
				Role:   "developer",
				Type:   "claude-code",
				Memory: ".taskguild/agents/developer/CLAUDE.md",
				Scaling: &ScalingConfig{
					Min:  1,
					Max:  3,
					Auto: true,
				},
				Triggers: []EventTrigger{
					{
						Event:     "task.status_changed",
						Condition: `task.status == "DESIGNED"`,
					},
				},
				ApprovalRequired: []ApprovalRule{
					{
						Action:  ActionFileWrite,
						Pattern: "*.go",
					},
					{
						Action: ActionGitCommit,
					},
					{
						Action: ActionGitPush,
					},
				},
			},
			{
				Role:   "reviewer",
				Type:   "claude-code",
				Memory: ".taskguild/agents/reviewer/CLAUDE.md",
				Triggers: []EventTrigger{
					{
						Event:     "task.status_changed",
						Condition: `task.status == "REVIEW_READY"`,
					},
				},
				ApprovalRequired: []ApprovalRule{
					{
						Action:    ActionStatusChange,
						Condition: `to_status == "QA_READY"`,
					},
				},
			},
			{
				Role:   "qa",
				Type:   "claude-code",
				Memory: ".taskguild/agents/qa/CLAUDE.md",
				Triggers: []EventTrigger{
					{
						Event:     "task.status_changed",
						Condition: `task.status == "QA_READY"`,
					},
				},
				ApprovalRequired: []ApprovalRule{
					{
						Action:    ActionStatusChange,
						Condition: `to_status == "CLOSED"`,
					},
				},
			},
		},
	}

	// Save default config
	if err := SaveConfig(config, configPath); err != nil {
		return nil, fmt.Errorf("failed to save default config: %w", err)
	}

	return config, nil
}

func createDefaultIndividualConfigs() error {
	agentsDir := ".taskguild/agents"

	defaultConfigs := []*IndividualAgentConfig{
		{
			Role:        "architect",
			Type:        "claude-code",
			Memory:      "CLAUDE.md",
			Description: "System architect for analyzing tasks and proposing optimal designs",
			Version:     "1.0",
			Triggers: []EventTrigger{
				{
					Event:     "task.created",
					Condition: `task.type == "feature"`,
				},
			},
		},
		{
			Role:        "developer",
			Type:        "claude-code",
			Memory:      "CLAUDE.md",
			Description: "Developer for implementing features based on architectural designs",
			Version:     "1.0",
			Scaling: &ScalingConfig{
				Min:  1,
				Max:  3,
				Auto: true,
			},
			Triggers: []EventTrigger{
				{
					Event:     "task.status_changed",
					Condition: `task.status == "DESIGNED"`,
				},
			},
			ApprovalRequired: []ApprovalRule{
				{
					Action:  ActionFileWrite,
					Pattern: "*.go",
				},
				{
					Action: ActionGitCommit,
				},
				{
					Action: ActionGitPush,
				},
			},
		},
		{
			Role:        "reviewer",
			Type:        "claude-code",
			Memory:      "CLAUDE.md",
			Description: "Senior engineer for conducting thorough code reviews",
			Version:     "1.0",
			Triggers: []EventTrigger{
				{
					Event:     "task.status_changed",
					Condition: `task.status == "REVIEW_READY"`,
				},
			},
			ApprovalRequired: []ApprovalRule{
				{
					Action:    ActionStatusChange,
					Condition: `to_status == "QA_READY"`,
				},
			},
		},
		{
			Role:        "qa",
			Type:        "claude-code",
			Memory:      "CLAUDE.md",
			Description: "QA engineer for comprehensive testing of implemented features",
			Version:     "1.0",
			Triggers: []EventTrigger{
				{
					Event:     "task.status_changed",
					Condition: `task.status == "QA_READY"`,
				},
			},
			ApprovalRequired: []ApprovalRule{
				{
					Action:    ActionStatusChange,
					Condition: `to_status == "CLOSED"`,
				},
			},
		},
	}

	for _, config := range defaultConfigs {
		if err := SaveIndividualAgentConfig(config, agentsDir); err != nil {
			return fmt.Errorf("failed to save %s config: %w", config.Role, err)
		}
	}

	return nil
}

func validateConfig(config *Config) error {
	if len(config.Agents) == 0 {
		return fmt.Errorf("no agents defined")
	}

	roleMap := make(map[string]bool)
	for _, agent := range config.Agents {
		if agent.Role == "" {
			return fmt.Errorf("agent role cannot be empty")
		}
		if agent.Type == "" {
			return fmt.Errorf("agent type cannot be empty")
		}
		if agent.Memory == "" {
			return fmt.Errorf("agent memory path cannot be empty")
		}

		// Check for duplicate roles
		if roleMap[agent.Role] {
			return fmt.Errorf("duplicate agent role: %s", agent.Role)
		}
		roleMap[agent.Role] = true

		// Validate scaling config
		if agent.Scaling != nil {
			if agent.Scaling.Min < 0 {
				return fmt.Errorf("agent %s: min scaling cannot be negative", agent.Role)
			}
			if agent.Scaling.Max < agent.Scaling.Min {
				return fmt.Errorf("agent %s: max scaling cannot be less than min", agent.Role)
			}
		}
	}

	return nil
}

func (c *Config) GetAgentConfig(role string) (*AgentConfig, bool) {
	for _, agent := range c.Agents {
		if agent.Role == role {
			return &agent, true
		}
	}
	return nil, false
}

func (c *Config) GetScalableAgents() []AgentConfig {
	var scalable []AgentConfig
	for _, agent := range c.Agents {
		if agent.Scaling != nil && agent.Scaling.Auto {
			scalable = append(scalable, agent)
		}
	}
	return scalable
}
