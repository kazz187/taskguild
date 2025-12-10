package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgent_Status(t *testing.T) {
	agent := &Agent{
		ID:     "test-agent",
		Status: StatusIdle,
	}

	// Test GetStatus
	assert.Equal(t, StatusIdle, agent.GetStatus())

	// Test UpdateStatus
	agent.UpdateStatus(StatusBusy)
	assert.Equal(t, StatusBusy, agent.GetStatus())

	// Test IsAvailable
	assert.False(t, agent.IsAvailable())

	agent.UpdateStatus(StatusIdle)
	assert.True(t, agent.IsAvailable())
}

func TestNewAgent(t *testing.T) {
	config := &AgentConfig{
		Name:         "test-developer",
		Type:         "claude-code",
		Process:      "implement",
		Instructions: "Test instructions",
		Scaling: &ScalingConfig{
			Min:  1,
			Max:  3,
			Auto: true,
		},
	}

	factory := &MockExecutorFactory{}

	agent, err := NewAgent("dev-0001", config, factory)
	assert.NoError(t, err)
	assert.NotNil(t, agent)
	assert.Equal(t, "dev-0001", agent.ID)
	assert.Equal(t, "test-developer", agent.Name)
	assert.Equal(t, "claude-code", agent.Type)
	assert.Equal(t, "implement", agent.Process)
	assert.Equal(t, StatusIdle, agent.Status)
}

func TestNewAgent_WithDifferentProcesses(t *testing.T) {
	tests := []struct {
		name        string
		processName string
	}{
		{"implement agent", "implement"},
		{"review agent", "review"},
		{"qa agent", "qa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AgentConfig{
				Name:    "test-agent",
				Type:    "claude-code",
				Process: tt.processName,
			}

			factory := &MockExecutorFactory{}
			agent, err := NewAgent("agent-001", config, factory)

			assert.NoError(t, err)
			assert.NotNil(t, agent)
			assert.Equal(t, tt.processName, agent.Process)
		})
	}
}

// MockExecutorFactory is a mock implementation of ExecutorFactory
type MockExecutorFactory struct{}

func (f *MockExecutorFactory) CreateExecutor(executorType string) (Executor, error) {
	return &MockExecutor{}, nil
}

// MockExecutor is a mock implementation of Executor
type MockExecutor struct {
	BaseExecutor
}

func (e *MockExecutor) Execute(ctx context.Context, work *WorkItem) (*ExecutionResult, error) {
	return &ExecutionResult{Success: true}, nil
}

func (e *MockExecutor) CanExecute(work *WorkItem) bool {
	return true
}

func (e *MockExecutor) Cleanup() error {
	return nil
}
