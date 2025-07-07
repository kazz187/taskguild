package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kazz187/taskguild/internal/event"
)

type Manager struct {
	agents    map[string]*Agent
	config    *Config
	eventBus  *event.EventBus
	ctx       context.Context
	cancel    context.CancelFunc
	mutex     sync.RWMutex
	approvals chan *ApprovalRequest
}

type ApprovalRequest struct {
	AgentID   string
	Action    Action
	Target    string
	Details   map[string]interface{}
	Response  chan bool
	Timestamp time.Time
}

func NewManager(config *Config, eventBus *event.EventBus) *Manager {
	return &Manager{
		agents:    make(map[string]*Agent),
		config:    config,
		eventBus:  eventBus,
		approvals: make(chan *ApprovalRequest, 100),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.ctx != nil {
		return fmt.Errorf("agent manager is already running")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)

	// Subscribe to events
	if err := m.subscribeToEvents(); err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	// Initialize agents from config
	for _, agentConfig := range m.config.Agents {
		agent := m.createAgentFromConfig(agentConfig)
		m.agents[agent.ID] = agent
	}

	// Start approval handler
	go m.handleApprovals()

	return nil
}

func (m *Manager) Stop() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.ctx = nil
	}

	// Stop all agents
	for _, agent := range m.agents {
		if err := agent.Stop(); err != nil {
			// Log error but continue stopping other agents
			fmt.Printf("Error stopping agent %s: %v\n", agent.ID, err)
		}
	}

	close(m.approvals)

	return nil
}

func (m *Manager) ListAgents() []*Agent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	agents := make([]*Agent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}
	return agents
}

func (m *Manager) GetAgent(agentID string) (*Agent, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	agent, exists := m.agents[agentID]
	return agent, exists
}

func (m *Manager) GetAgentsByRole(role string) []*Agent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var agents []*Agent
	for _, agent := range m.agents {
		if agent.Role == role {
			agents = append(agents, agent)
		}
	}
	return agents
}

func (m *Manager) GetAvailableAgents() []*Agent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var available []*Agent
	for _, agent := range m.agents {
		if agent.IsAvailable() {
			available = append(available, agent)
		}
	}
	return available
}

func (m *Manager) AssignAgentToTask(agentID, taskID, worktreePath string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if !agent.IsAvailable() {
		return fmt.Errorf("agent %s is not available", agentID)
	}

	agent.AssignTask(taskID, worktreePath)
	agent.UpdateStatus(StatusBusy)

	// Start the agent if not already running
	if agent.ctx == nil {
		if err := agent.Start(m.ctx); err != nil {
			return fmt.Errorf("failed to start agent %s: %w", agentID, err)
		}
	}

	return nil
}

func (m *Manager) UnassignAgent(agentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	agent.ClearTask()
	agent.UpdateStatus(StatusIdle)

	return nil
}

func (m *Manager) RequestApproval(agentID string, action Action, target string, details map[string]interface{}) (bool, error) {
	request := &ApprovalRequest{
		AgentID:   agentID,
		Action:    action,
		Target:    target,
		Details:   details,
		Response:  make(chan bool, 1),
		Timestamp: time.Now(),
	}

	select {
	case m.approvals <- request:
		// Wait for approval response
		select {
		case approved := <-request.Response:
			return approved, nil
		case <-time.After(5 * time.Minute):
			return false, fmt.Errorf("approval timeout for agent %s", agentID)
		}
	case <-m.ctx.Done():
		return false, fmt.Errorf("manager is shutting down")
	}
}

func (m *Manager) ScaleAgents(role string, targetCount int) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agentConfig, exists := m.config.GetAgentConfig(role)
	if !exists {
		return fmt.Errorf("agent role %s not found in config", role)
	}

	if agentConfig.Scaling == nil {
		return fmt.Errorf("agent role %s is not scalable", role)
	}

	if targetCount < agentConfig.Scaling.Min {
		targetCount = agentConfig.Scaling.Min
	}
	if targetCount > agentConfig.Scaling.Max {
		targetCount = agentConfig.Scaling.Max
	}

	currentAgents := m.GetAgentsByRole(role)
	currentCount := len(currentAgents)

	if targetCount > currentCount {
		// Scale up
		for i := currentCount; i < targetCount; i++ {
			agent := m.createAgentFromConfig(*agentConfig)
			m.agents[agent.ID] = agent
		}
	} else if targetCount < currentCount {
		// Scale down
		for i := currentCount - 1; i >= targetCount; i-- {
			agent := currentAgents[i]
			if !agent.IsAssigned() {
				if err := agent.Stop(); err != nil {
					fmt.Printf("Error stopping agent %s: %v\n", agent.ID, err)
				}
				delete(m.agents, agent.ID)
			}
		}
	}

	return nil
}

func (m *Manager) createAgentFromConfig(config AgentConfig) *Agent {
	agent := NewAgent(config.Role, config.Type, config.Memory)
	agent.Triggers = config.Triggers
	agent.ApprovalRequired = config.ApprovalRequired
	agent.Scaling = config.Scaling
	return agent
}

func (m *Manager) subscribeToEvents() error {
	// TODO: Implement proper event subscription using watermill
	// For now, this is a placeholder
	return nil
}

func (m *Manager) handleEvent(eventType string, data map[string]interface{}) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Find agents that should respond to this event
	var matchingAgents []*Agent
	for _, agent := range m.agents {
		if agent.MatchesTrigger(eventType, data) {
			matchingAgents = append(matchingAgents, agent)
		}
	}

	// TODO: Implement event handling logic
	// For now, just log the matching agents
	if len(matchingAgents) > 0 {
		fmt.Printf("Event %s matched %d agents\n", eventType, len(matchingAgents))
	}

	return nil
}

func (m *Manager) handleApprovals() {
	for {
		select {
		case request := <-m.approvals:
			if request == nil {
				return
			}
			// TODO: Implement approval UI/logic
			// For now, auto-approve everything
			request.Response <- true
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *Manager) GetApprovalRequests() <-chan *ApprovalRequest {
	return m.approvals
}
