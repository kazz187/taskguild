package agent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type AgentConfig struct {
	Name         string         `yaml:"name"`
	Type         string         `yaml:"type"`
	Instructions string         `yaml:"instructions,omitempty"`
	Triggers     []EventTrigger `yaml:"triggers"`
	Scaling      *ScalingConfig `yaml:"scaling,omitempty"`
}

// IndividualAgentConfig represents a single agent's configuration file
type IndividualAgentConfig struct {
	Name         string         `yaml:"name"`
	Type         string         `yaml:"type"`
	Triggers     []EventTrigger `yaml:"triggers"`
	Scaling      *ScalingConfig `yaml:"scaling,omitempty"`
	Description  string         `yaml:"description,omitempty"`
	Version      string         `yaml:"version,omitempty"`
	Instructions string         `yaml:"instructions,omitempty"`
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
		// Skip directories and non-yaml files
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		configFilePath := filepath.Join(agentsDir, entry.Name())

		agentConfig, err := loadIndividualAgentConfig(configFilePath)
		if err != nil {
			fmt.Printf("Warning: failed to load agent config %s: %v\n", configFilePath, err)
			continue
		}

		// Use Name if available, otherwise use filename without extension
		agentName := agentConfig.Name
		if agentName == "" {
			filenameWithoutExt := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			agentName = filenameWithoutExt
		}

		config.Agents = append(config.Agents, AgentConfig{
			Name:     agentName,
			Type:     agentConfig.Type,
			Triggers: agentConfig.Triggers,
			Scaling:  agentConfig.Scaling,
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

	agentName := config.Name

	// Create agents directory if it doesn't exist
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	configPath := filepath.Join(agentsDir, agentName+".yaml")
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
				Name: "developer",
				Type: "claude-code",
				Scaling: &ScalingConfig{
					Min:  1,
					Max:  3,
					Auto: true,
				},
				Triggers: []EventTrigger{
					{
						Event:     "task.created",
						Condition: `task.type == "feature"`,
					},
				},
			},
			{
				Name: "reviewer",
				Type: "claude-code",
				Triggers: []EventTrigger{
					{
						Event:     "task.status_changed",
						Condition: `task.status == "REVIEW_READY"`,
					},
				},
			},
			{
				Name: "qa-validator",
				Type: "claude-code",
				Triggers: []EventTrigger{
					{
						Event:     "task.status_changed",
						Condition: `task.status == "QA_READY"`,
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
			Name:        "developer",
			Type:        "claude-code",
			Description: "Developer for implementing features",
			Version:     "1.0",
			Scaling: &ScalingConfig{
				Min:  1,
				Max:  3,
				Auto: true,
			},
			Triggers: []EventTrigger{
				{
					Event:     "task.created",
					Condition: `task.type == "feature"`,
				},
			},
		},
		{
			Name:        "reviewer",
			Type:        "claude-code",
			Description: "Senior engineer for conducting thorough code reviews",
			Version:     "1.0",
			Triggers: []EventTrigger{
				{
					Event:     "task.status_changed",
					Condition: `task.status == "REVIEW_READY"`,
				},
			},
		},
		{
			Name:        "qa-validator",
			Type:        "claude-code",
			Description: "QA engineer for comprehensive testing",
			Version:     "1.0",
			Triggers: []EventTrigger{
				{
					Event:     "task.status_changed",
					Condition: `task.status == "QA_READY"`,
				},
			},
		},
	}

	for _, config := range defaultConfigs {
		if err := SaveIndividualAgentConfig(config, agentsDir); err != nil {
			return fmt.Errorf("failed to save %s config: %w", config.Name, err)
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
		agentIdentifier := agent.Name
		if agentIdentifier == "" {
			return fmt.Errorf("agent name cannot be empty")
		}
		if agent.Type == "" {
			return fmt.Errorf("agent type cannot be empty")
		}

		// Check for duplicate agent names
		if roleMap[agentIdentifier] {
			return fmt.Errorf("duplicate agent name: %s", agentIdentifier)
		}
		roleMap[agentIdentifier] = true

		// Validate scaling config
		if agent.Scaling != nil {
			if agent.Scaling.Min < 0 {
				return fmt.Errorf("agent %s: min scaling cannot be negative", agentIdentifier)
			}
			if agent.Scaling.Max < agent.Scaling.Min {
				return fmt.Errorf("agent %s: max scaling cannot be less than min", agentIdentifier)
			}
		}
	}

	return nil
}

func (c *Config) GetAgentConfig(name string) (*AgentConfig, bool) {
	for _, agent := range c.Agents {
		if agent.Name == name {
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
