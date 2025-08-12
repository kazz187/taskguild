package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sourcegraph/conc"

	"github.com/kazz187/taskguild/internal/event"
	"github.com/kazz187/taskguild/pkg/worktree"
)

type Manager struct {
	agents          map[string]*Agent
	config          *Config
	eventBus        *event.EventBus
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

func NewManager(config *Config, eventBus *event.EventBus, worktreeManager *worktree.Manager) *Manager {
	return &Manager{
		agents:          make(map[string]*Agent),
		config:          config,
		eventBus:        eventBus,
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

	if err := m.subscribeToEvents(); err != nil {
		m.mutex.Unlock()
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}

	for _, agentConfig := range m.config.Agents {
		agent := m.createAgentFromConfig(agentConfig)
		m.agents[agent.ID] = agent
	}
	m.mutex.Unlock()

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
				m.freeAgentSequenceNumber(agent.ID, agent.Role)
				delete(m.agents, agent.ID)
			}
		}
	}

	return nil
}

func (m *Manager) createAgentFromConfig(config AgentConfig) *Agent {
	// Use Name if available, otherwise fall back to Role
	agentIdentifier := config.Name
	if agentIdentifier == "" {
		agentIdentifier = config.Role
	}
	agentID := m.generateSequentialAgentID(agentIdentifier)
	agent := NewAgentWithID(agentID, agentIdentifier, config.Type)
	agent.Triggers = config.Triggers
	agent.Scaling = config.Scaling
	return agent
}

func (m *Manager) subscribeToEvents() error {
	// Subscribe to task.created events
	err := event.SubscribeTyped(m.eventBus, event.TaskCreated, "agent-manager-task-created",
		func(ctx context.Context, event *event.Event[event.TaskCreatedData]) error {
			return m.handleTaskCreated(&event.Data)
		})
	if err != nil {
		return fmt.Errorf("failed to subscribe to task.created: %w", err)
	}

	// Subscribe to task.status_changed events
	err = event.SubscribeTyped(m.eventBus, event.TaskStatusChanged, "agent-manager-status-changed",
		func(ctx context.Context, event *event.Event[event.TaskStatusChangedData]) error {
			return m.handleTaskStatusChanged(&event.Data)
		})
	if err != nil {
		return fmt.Errorf("failed to subscribe to task.status_changed: %w", err)
	}

	return nil
}

func (m *Manager) handleTaskCreated(data *event.TaskCreatedData) error {
	// Create worktree for the task
	branchName := fmt.Sprintf("task-%s", data.TaskID)
	worktreePath, err := m.worktreeManager.CreateWorktree(data.TaskID, branchName)
	if err != nil {
		return fmt.Errorf("failed to create worktree for task %s: %w", data.TaskID, err)
	}

	// Find agents that should respond to task creation
	contextData := map[string]interface{}{
		"task_id":    data.TaskID,
		"task_type":  data.Type,
		"task_title": data.Title,
		"worktree":   worktreePath,
	}

	m.mutex.RLock()
	var matchingAgents []*Agent
	for _, agent := range m.agents {
		if agent.MatchesTrigger("task.created", contextData) {
			matchingAgents = append(matchingAgents, agent)
		}
	}
	m.mutex.RUnlock()

	// Assign matching agents to the task
	for _, agent := range matchingAgents {
		if agent.IsAvailable() {
			if err := m.AssignAgentToTask(agent.ID, data.TaskID, worktreePath); err != nil {
				fmt.Printf("Failed to assign agent %s to task %s: %v\n", agent.ID, data.TaskID, err)
				continue
			}
			fmt.Printf("Assigned agent %s to task %s\n", agent.ID, data.TaskID)
		}
	}

	return nil
}

func (m *Manager) handleTaskStatusChanged(data *event.TaskStatusChangedData) error {

	// Find agents that should respond to status change
	contextData := map[string]interface{}{
		"task_id":     data.TaskID,
		"from_status": data.OldStatus,
		"to_status":   data.NewStatus,
	}

	m.mutex.RLock()
	var matchingAgents []*Agent
	for _, agent := range m.agents {
		if agent.MatchesTrigger("task.status_changed", contextData) {
			matchingAgents = append(matchingAgents, agent)
		}
	}
	m.mutex.RUnlock()

	// Handle scaling based on status change
	if data.NewStatus == "IN_PROGRESS" {
		// Scale up developers if needed
		if err := m.ScaleAgents("developer", 2); err != nil {
			fmt.Printf("Failed to scale developer agents: %v\n", err)
		}
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
			fmt.Printf("Error stopping agent %s: %v\n", agent.ID, err)
		}
		m.freeAgentSequenceNumber(agent.ID, agent.Role)
	}
	m.mutex.Unlock()
}

func (m *Manager) StartAgent(agentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	if agent.ctx != nil {
		return fmt.Errorf("agent %s is already running", agentID)
	}

	return agent.Start(m.ctx)
}

func (m *Manager) StopAgent(agentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	agent, exists := m.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	err := agent.Stop()
	if err == nil {
		m.freeAgentSequenceNumber(agent.ID, agent.Role)
		delete(m.agents, agent.ID)
	}
	return err
}
