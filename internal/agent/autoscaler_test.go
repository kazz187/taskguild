package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoScaler_countAgentsByStatus(t *testing.T) {
	scaler := &AutoScaler{}

	agents := []*Agent{
		{ID: "agent-1", Status: StatusBusy},
		{ID: "agent-2", Status: StatusBusy},
		{ID: "agent-3", Status: StatusIdle},
		{ID: "agent-4", Status: StatusError},
		{ID: "agent-5", Status: StatusIdle},
	}

	busy, idle := scaler.countAgentsByStatus(agents)
	assert.Equal(t, 2, busy)
	assert.Equal(t, 2, idle)
}

func TestAutoScaler_StartStop(t *testing.T) {
	config := &Config{
		Agents: []*AgentConfig{
			{
				Name: "test-agent",
				Type: "claude-code",
				Scaling: &ScalingConfig{
					Min:  1,
					Max:  3,
					Auto: true,
				},
			},
		},
	}

	registry := &MockAgentRegistry{
		agents: make(map[string]*Agent),
	}

	scaler := NewAutoScaler(config, &MockExecutorFactory{}, registry)
	scaler.SetMonitorInterval(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := scaler.Start(ctx)
	require.NoError(t, err)

	// Wait for a few monitor cycles
	time.Sleep(300 * time.Millisecond)

	err = scaler.Stop()
	require.NoError(t, err)
}

func TestAutoScaler_SetMonitorInterval(t *testing.T) {
	scaler := &AutoScaler{
		monitorInterval: 10 * time.Second,
	}

	scaler.SetMonitorInterval(5 * time.Second)
	assert.Equal(t, 5*time.Second, scaler.monitorInterval)
}

// MockAgentRegistry implements AgentRegistry for testing
type MockAgentRegistry struct {
	agents map[string]*Agent
	mutex  sync.RWMutex
}

func (r *MockAgentRegistry) GetAgentsByName(name string) []*Agent {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var result []*Agent
	for _, agent := range r.agents {
		if agent.Name == name {
			result = append(result, agent)
		}
	}
	return result
}

func (r *MockAgentRegistry) CreateAgent(config *AgentConfig) (*Agent, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	id := config.Name + "-001"
	agent := &Agent{
		ID:     id,
		Name:   config.Name,
		Type:   config.Type,
		Status: StatusIdle,
	}
	r.agents[id] = agent
	return agent, nil
}

func (r *MockAgentRegistry) RemoveAgent(agentID string) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	delete(r.agents, agentID)
	return nil
}

func (r *MockAgentRegistry) ListAgents() []*Agent {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	var result []*Agent
	for _, agent := range r.agents {
		result = append(result, agent)
	}
	return result
}
