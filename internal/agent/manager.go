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
	factory         ExecutorFactory
	eventBus        *event.EventBus
	taskService     task.Service
	worktreeManager *worktree.Manager
	ctx             context.Context
	mutex           sync.RWMutex
	approvals       chan *ApprovalRequest
	waitGroup       *conc.WaitGroup
	agentSeqNum     map[string]map[int]bool // role -> {used numbers}

	// Auto scaling
	autoScaler *AutoScaler
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
	factory := NewDefaultExecutorFactory(taskService, eventBus, worktreeManager)

	m := &Manager{
		agents:          make(map[string]*Agent),
		config:          config,
		factory:         factory,
		eventBus:        eventBus,
		taskService:     taskService,
		worktreeManager: worktreeManager,
		approvals:       make(chan *ApprovalRequest, 100),
		waitGroup:       conc.NewWaitGroup(),
		agentSeqNum:     make(map[string]map[int]bool),
	}

	// Create auto scaler with manager as registry
	m.autoScaler = NewAutoScaler(config, factory, m)

	return m
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
			agent, err := m.CreateAgent(agentConfig)
			if err != nil {
				m.mutex.Unlock()
				return fmt.Errorf("failed to create agent: %w", err)
			}
			color.ColoredPrintf(agent.ID, "Started (type: %s)\n", agent.Type)
		}
	}
	m.mutex.Unlock()

	// Start auto scaler
	if err := m.autoScaler.Start(ctx); err != nil {
		return fmt.Errorf("failed to start auto scaler: %w", err)
	}

	// Handle approvals
	m.waitGroup.Go(m.handleApprovals)

	m.waitGroup.Go(func() {
		<-ctx.Done()
		m.cleanup()
	})
	m.waitGroup.Wait()
	return nil
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

// CreateAgent creates a new agent and starts it (implements AgentRegistry)
func (m *Manager) CreateAgent(config *AgentConfig) (*Agent, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agentID := m.generateSequentialAgentID(config.Name)
	agent, err := NewAgent(agentID, config, m.factory)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	// Initialize with dependencies
	if err := agent.InitializeWithDependencies(m.taskService, m.eventBus, m.worktreeManager); err != nil {
		return nil, fmt.Errorf("failed to initialize agent: %w", err)
	}

	// Add to registry
	m.agents[agent.ID] = agent

	// Start the agent
	if err := agent.Start(m.ctx); err != nil {
		delete(m.agents, agent.ID)
		return nil, fmt.Errorf("failed to start agent %s: %w", agent.ID, err)
	}

	return agent, nil
}

// RemoveAgent removes an agent and stops it (implements AgentRegistry)
func (m *Manager) RemoveAgent(agentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	// Stop the agent
	if err := agent.Stop(); err != nil {
		color.ColoredPrintf(agent.ID, "Error stopping: %v\n", err)
	}

	// Remove from registry
	delete(m.agents, agentID)
	m.freeAgentSequenceNumber(agentID, agent.Name)

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
	defer m.mutex.Unlock()

	// Stop auto scaler
	if m.autoScaler != nil {
		m.autoScaler.Stop()
	}

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
}
