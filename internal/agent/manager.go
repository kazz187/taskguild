package agent

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/internal/task"
	"github.com/kazz187/taskguild/pkg/color"
	"github.com/kazz187/taskguild/pkg/worktree"
)

type Manager struct {
	agents          map[string]*Agent
	config          *Config
	eventBus        *event.EventBus
	taskService     task.Service
	worktreeManager *worktree.Manager
	ctx             context.Context
	mutex           sync.RWMutex
	approvals       chan *ApprovalRequest
	waitGroup       *conc.WaitGroup
	agentSeqNum     map[string]map[int]bool // role -> {used numbers}
}

type ApprovalRequest struct {
	AgentID   string
	Action    Action
	Target    string
	Details   map[string]interface{}
	Response  chan bool
	Timestamp time.Time
}

func NewManager(config *Config, eventBus *event.EventBus, taskService task.Service, worktreeManager *worktree.Manager) *Manager {
	return &Manager{
		agents:          make(map[string]*Agent),
		config:          config,
		eventBus:        eventBus,
		taskService:     taskService,
		worktreeManager: worktreeManager,
		approvals:       make(chan *ApprovalRequest, 100),
		waitGroup:       conc.NewWaitGroup(),
		agentSeqNum:     make(map[string]map[int]bool),
	}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mutex.Lock()
	if m.ctx != nil {
		m.mutex.Unlock()
		return fmt.Errorf("agent manager is already running")
	}

	m.ctx = ctx

	// Create initial agents based on configuration
	for _, agentConfig := range m.config.Agents {
		// Create minimum number of agents
		minAgents := 1
		if agentConfig.Scaling != nil && agentConfig.Scaling.Min > 0 {
			minAgents = agentConfig.Scaling.Min
		}

		for i := 0; i < minAgents; i++ {
			agent, err := m.createAgentFromConfig(agentConfig)
			if err != nil {
				m.mutex.Unlock()
				return fmt.Errorf("failed to create agent: %w", err)
			}
			m.agents[agent.ID] = agent

			// Start the agent
			if err := agent.Start(ctx); err != nil {
				m.mutex.Unlock()
				return fmt.Errorf("failed to start agent %s: %w", agent.ID, err)
			}
			color.ColoredPrintf(agent.ID, "Started (type: %s)\n", agent.Type)
		}
	}
	m.mutex.Unlock()

	// Start scaling monitor
	m.waitGroup.Go(m.monitorAndScale)

	// Handle approvals
	m.waitGroup.Go(m.handleApprovals)

	m.waitGroup.Go(func() {
		<-ctx.Done()
		m.cleanup()
	})
	m.waitGroup.Wait()
	return nil
}

// monitorAndScale monitors agent status and scales up/down as needed
func (m *Manager) monitorAndScale() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.performScaling()
		}
	}
}

// performScaling checks agent status and scales appropriately
func (m *Manager) performScaling() {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Group agents by name
	agentsByName := make(map[string][]*Agent)
	for _, agent := range m.agents {
		agentsByName[agent.Name] = append(agentsByName[agent.Name], agent)
	}

	// Check each agent type
	for _, config := range m.config.Agents {
		agents := agentsByName[config.Name]
		if len(agents) == 0 {
			continue
		}

		// Count busy and idle agents
		busyCount := 0
		idleCount := 0
		for _, agent := range agents {
			switch agent.GetStatus() {
			case StatusBusy:
				busyCount++
			case StatusIdle:
				idleCount++
			}
		}

		totalCount := len(agents)

		// Scale up if all agents are busy and we haven't reached max
		if config.Scaling != nil && config.Scaling.Auto {
			if busyCount == totalCount && totalCount < config.Scaling.Max {
				// Create a new agent
				go func(cfg *AgentConfig) {
					m.mutex.Lock()
					defer m.mutex.Unlock()

					newAgent, err := m.createAgentFromConfig(cfg)
					if err != nil {
						fmt.Printf("Failed to create new agent: %v\n", err)
						return
					}

					m.agents[newAgent.ID] = newAgent
					if err := newAgent.Start(m.ctx); err != nil {
						color.ColoredPrintf(newAgent.ID, "Failed to start: %v\n", err)
						delete(m.agents, newAgent.ID)
						return
					}
					color.ColoredPrintf(newAgent.ID, "Scaled up: created (type: %s)\n", newAgent.Type)
				}(config)
			}

			// Scale down if we have too many idle agents
			if idleCount >= 2 && totalCount > config.Scaling.Min {
				// Stop an idle agent
				for _, agent := range agents {
					if agent.GetStatus() == StatusIdle {
						go func(a *Agent) {
							m.mutex.Lock()
							defer m.mutex.Unlock()

							if err := a.Stop(); err != nil {
								color.ColoredPrintf(a.ID, "Failed to stop: %v\n", err)
								return
							}
							delete(m.agents, a.ID)
							m.freeAgentSequenceNumber(a.Name, a.ID)
							color.ColoredPrintln(a.ID, "Scaled down: stopped")
						}(agent)
						break
					}
				}
			}
		}
	}
}

func (m *Manager) ListAgents() []*Agent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	agents := make([]*Agent, 0, len(m.agents))
	for _, agent := range m.agents {
		agents = append(agents, agent)
	}

	// Sort agents by ID for consistent ordering
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].ID < agents[j].ID
	})

	return agents
}

func (m *Manager) GetAgent(agentID string) (*Agent, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	agent, exists := m.agents[agentID]
	return agent, exists
}

func (m *Manager) GetAgentsByName(name string) []*Agent {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var agents []*Agent
	for _, agent := range m.agents {
		if agent.Name == name {
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

func (m *Manager) createAgentFromConfig(config *AgentConfig) (*Agent, error) {
	agentID := m.generateSequentialAgentID(config.Name)
	agent, err := NewAgent(agentID, config, m.taskService, m.eventBus, m.worktreeManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}
	agent.Triggers = config.Triggers
	agent.Scaling = config.Scaling
	return agent, nil
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

func (m *Manager) generateSequentialAgentID(role string) string {
	// Initialize role map if not exists
	if m.agentSeqNum[role] == nil {
		m.agentSeqNum[role] = make(map[int]bool)
	}

	// Find the lowest available sequence number
	seqNum := 1
	for m.agentSeqNum[role][seqNum] {
		seqNum++
	}

	// Mark the number as used
	m.agentSeqNum[role][seqNum] = true

	return fmt.Sprintf("%s-%04d", role, seqNum)
}

func (m *Manager) freeAgentSequenceNumber(agentID, role string) {
	// Extract sequence number from agent ID
	var seqNum int
	if n, err := fmt.Sscanf(agentID, role+"-%04d", &seqNum); n == 1 && err == nil {
		if m.agentSeqNum[role] != nil {
			delete(m.agentSeqNum[role], seqNum)
		}
	}
}

func (m *Manager) cleanup() {
	m.mutex.Lock()
	m.ctx = nil
	close(m.approvals)

	// Stop all agents
	for _, agent := range m.agents {
		if err := agent.Stop(); err != nil {
			// Log error but continue stopping other agents
			color.ColoredPrintf(agent.ID, "Error stopping: %v\n", err)
		}
		m.freeAgentSequenceNumber(agent.ID, agent.Name)
	}
	m.mutex.Unlock()
}
