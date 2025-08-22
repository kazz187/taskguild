package client

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// AgentClient provides client operations for agents
type AgentClient struct {
	client taskguildv1connect.AgentServiceClient
}

// NewAgentClient creates a new agent client
func NewAgentClient(baseURL string) *AgentClient {
	client := taskguildv1connect.NewAgentServiceClient(
		http.DefaultClient,
		baseURL,
	)

	return &AgentClient{
		client: client,
	}
}

// ListAgents lists all agents
func (c *AgentClient) ListAgents(ctx context.Context) ([]*taskguildv1.Agent, error) {
	req := connect.NewRequest(&taskguildv1.ListAgentsRequest{})

	resp, err := c.client.ListAgents(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return resp.Msg.Agents, nil
}

// GetAgent gets a specific agent
func (c *AgentClient) GetAgent(ctx context.Context, agentID string) (*taskguildv1.Agent, error) {
	req := connect.NewRequest(&taskguildv1.GetAgentRequest{
		Id: agentID,
	})

	resp, err := c.client.GetAgent(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	return resp.Msg.Agent, nil
}

// GetAgentStatus gets agent status
func (c *AgentClient) GetAgentStatus(ctx context.Context, agentID string) (*taskguildv1.Agent, error) {
	req := connect.NewRequest(&taskguildv1.GetAgentStatusRequest{
		Id: agentID,
	})

	resp, err := c.client.GetAgentStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent status: %w", err)
	}

	return resp.Msg.Agent, nil
}
