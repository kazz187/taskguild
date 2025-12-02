package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kazz187/taskguild/pkg/color"
)

// AutoScaler manages automatic scaling of agents
type AutoScaler struct {
	config          *Config
	factory         ExecutorFactory
	agentRegistry   AgentRegistry
	ctx             context.Context
	cancel          context.CancelFunc
	mutex           sync.RWMutex
	monitorInterval time.Duration
}

// AgentRegistry defines the interface for agent registry operations
type AgentRegistry interface {
	GetAgentsByName(name string) []*Agent
	CreateAgent(config *AgentConfig) (*Agent, error)
	RemoveAgent(agentID string) error
	ListAgents() []*Agent
}

// NewAutoScaler creates a new auto scaler
func NewAutoScaler(config *Config, factory ExecutorFactory, registry AgentRegistry) *AutoScaler {
	return &AutoScaler{
		config:          config,
		factory:         factory,
		agentRegistry:   registry,
		monitorInterval: 10 * time.Second,
	}
}

// Start starts the auto scaling monitor
func (s *AutoScaler) Start(ctx context.Context) error {
	s.mutex.Lock()
	if s.ctx != nil {
		s.mutex.Unlock()
		return fmt.Errorf("auto scaler already running")
	}

	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mutex.Unlock()

	go s.monitorLoop()
	return nil
}

// Stop stops the auto scaling monitor
func (s *AutoScaler) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
		s.ctx = nil
	}

	return nil
}

// monitorLoop runs the scaling monitoring loop
func (s *AutoScaler) monitorLoop() {
	ticker := time.NewTicker(s.monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.performScaling()
		}
	}
}

// performScaling checks agent status and scales appropriately
func (s *AutoScaler) performScaling() {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for _, config := range s.config.Agents {
		if config.Scaling == nil || !config.Scaling.Auto {
			continue
		}

		agents := s.agentRegistry.GetAgentsByName(config.Name)
		if len(agents) == 0 {
			continue
		}

		// Count busy and idle agents
		busyCount, idleCount := s.countAgentsByStatus(agents)
		totalCount := len(agents)

		// Scale up if all agents are busy and we haven't reached max
		if busyCount == totalCount && totalCount < config.Scaling.Max {
			s.scaleUp(config)
		}

		// Scale down if we have too many idle agents
		if idleCount >= 2 && totalCount > config.Scaling.Min {
			s.scaleDown(config, agents)
		}
	}
}

// countAgentsByStatus counts agents by their status
func (s *AutoScaler) countAgentsByStatus(agents []*Agent) (busy, idle int) {
	for _, agent := range agents {
		switch agent.GetStatus() {
		case StatusBusy:
			busy++
		case StatusIdle:
			idle++
		}
	}
	return busy, idle
}

// scaleUp creates a new agent instance
func (s *AutoScaler) scaleUp(config *AgentConfig) {
	go func() {
		newAgent, err := s.agentRegistry.CreateAgent(config)
		if err != nil {
			color.ColoredPrintf("AutoScaler", "Failed to create new %s agent: %v\n", config.Name, err)
			return
		}

		color.ColoredPrintf("AutoScaler", "Scaled up: created %s agent %s\n", config.Name, newAgent.ID)
	}()
}

// scaleDown removes an idle agent instance
func (s *AutoScaler) scaleDown(config *AgentConfig, agents []*Agent) {
	// Find an idle agent to remove
	for _, agent := range agents {
		if agent.GetStatus() == StatusIdle {
			go func(a *Agent) {
				if err := s.agentRegistry.RemoveAgent(a.ID); err != nil {
					color.ColoredPrintf("AutoScaler", "Failed to remove %s agent %s: %v\n", config.Name, a.ID, err)
					return
				}

				color.ColoredPrintf("AutoScaler", "Scaled down: removed %s agent %s\n", config.Name, a.ID)
			}(agent)
			break
		}
	}
}

// SetMonitorInterval sets the monitoring interval
func (s *AutoScaler) SetMonitorInterval(interval time.Duration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.monitorInterval = interval
}
