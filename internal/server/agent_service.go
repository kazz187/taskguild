package server

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/agent"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

// AgentServiceHandler implements the AgentService Connect handler
type AgentServiceHandler struct {
	manager *agent.Manager
}

// NewAgentServiceHandler creates a new AgentService handler
func NewAgentServiceHandler(manager *agent.Manager) *AgentServiceHandler {
	return &AgentServiceHandler{
		manager: manager,
	}
}

// PathAndHandler returns the Connect path and handler
func (h *AgentServiceHandler) PathAndHandler() (string, http.Handler) {
	return taskguildv1connect.NewAgentServiceHandler(h)
}

// ListAgents lists all agents
func (h *AgentServiceHandler) ListAgents(
	ctx context.Context,
	req *connect.Request[taskguildv1.ListAgentsRequest],
) (*connect.Response[taskguildv1.ListAgentsResponse], error) {
	agents := h.manager.ListAgents()

	protoAgents := make([]*taskguildv1.Agent, 0, len(agents))
	for _, a := range agents {
		protoAgent, err := h.agentToProto(a)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
		}
		protoAgents = append(protoAgents, protoAgent)
	}

	return connect.NewResponse(&taskguildv1.ListAgentsResponse{
		Agents: protoAgents,
	}), nil
}

// GetAgent gets a specific agent
func (h *AgentServiceHandler) GetAgent(
	ctx context.Context,
	req *connect.Request[taskguildv1.GetAgentRequest],
) (*connect.Response[taskguildv1.GetAgentResponse], error) {
	agentObj, exists := h.manager.GetAgent(req.Msg.Id)
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent not found: %s", req.Msg.Id))
	}

	protoAgent, err := h.agentToProto(agentObj)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
	}

	return connect.NewResponse(&taskguildv1.GetAgentResponse{
		Agent: protoAgent,
	}), nil
}

// StartAgent starts an agent
func (h *AgentServiceHandler) StartAgent(
	ctx context.Context,
	req *connect.Request[taskguildv1.StartAgentRequest],
) (*connect.Response[taskguildv1.StartAgentResponse], error) {
	err := h.manager.StartAgent(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start agent: %w", err))
	}

	agentObj, exists := h.manager.GetAgent(req.Msg.Id)
	if !exists {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get started agent: %s", req.Msg.Id))
	}

	protoAgent, err := h.agentToProto(agentObj)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
	}

	return connect.NewResponse(&taskguildv1.StartAgentResponse{
		Agent: protoAgent,
	}), nil
}

// StopAgent stops an agent
func (h *AgentServiceHandler) StopAgent(
	ctx context.Context,
	req *connect.Request[taskguildv1.StopAgentRequest],
) (*connect.Response[taskguildv1.StopAgentResponse], error) {
	err := h.manager.StopAgent(req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stop agent: %w", err))
	}

	agentObj, exists := h.manager.GetAgent(req.Msg.Id)
	if !exists {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get stopped agent: %s", req.Msg.Id))
	}

	protoAgent, err := h.agentToProto(agentObj)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
	}

	return connect.NewResponse(&taskguildv1.StopAgentResponse{
		Agent: protoAgent,
	}), nil
}

// GetAgentStatus gets agent status
func (h *AgentServiceHandler) GetAgentStatus(
	ctx context.Context,
	req *connect.Request[taskguildv1.GetAgentStatusRequest],
) (*connect.Response[taskguildv1.GetAgentStatusResponse], error) {
	agentObj, exists := h.manager.GetAgent(req.Msg.Id)
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("agent not found: %s", req.Msg.Id))
	}

	protoAgent, err := h.agentToProto(agentObj)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
	}

	return connect.NewResponse(&taskguildv1.GetAgentStatusResponse{
		Agent: protoAgent,
	}), nil
}

// ScaleAgent scales agents
func (h *AgentServiceHandler) ScaleAgent(
	ctx context.Context,
	req *connect.Request[taskguildv1.ScaleAgentRequest],
) (*connect.Response[taskguildv1.ScaleAgentResponse], error) {
	err := h.manager.ScaleAgents(req.Msg.Role, int(req.Msg.Count))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to scale agents: %w", err))
	}

	// Get updated agents for this role
	agents := h.manager.GetAgentsByRole(req.Msg.Role)

	protoAgents := make([]*taskguildv1.Agent, 0, len(agents))
	for _, a := range agents {
		protoAgent, err := h.agentToProto(a)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to convert agent: %w", err))
		}
		protoAgents = append(protoAgents, protoAgent)
	}

	return connect.NewResponse(&taskguildv1.ScaleAgentResponse{
		Agents: protoAgents,
	}), nil
}

// Helper methods for conversion

func (h *AgentServiceHandler) agentToProto(a *agent.Agent) (*taskguildv1.Agent, error) {
	status, err := h.agentStatusToProto(a.GetStatus())
	if err != nil {
		return nil, err
	}

	// Convert triggers
	protoTriggers := make([]*taskguildv1.EventTrigger, 0, len(a.Triggers))
	for _, trigger := range a.Triggers {
		protoTriggers = append(protoTriggers, &taskguildv1.EventTrigger{
			Event:     trigger.Event,
			Condition: trigger.Condition,
		})
	}

	// Convert approval rules
	protoApprovalRules := make([]*taskguildv1.ApprovalRule, 0, len(a.ApprovalRequired))
	for _, rule := range a.ApprovalRequired {
		protoApprovalRules = append(protoApprovalRules, &taskguildv1.ApprovalRule{
			Action:    string(rule.Action),
			Pattern:   rule.Pattern,
			Condition: rule.Condition,
		})
	}

	// Convert scaling config
	var protoScaling *taskguildv1.ScalingConfig
	if a.Scaling != nil {
		protoScaling = &taskguildv1.ScalingConfig{
			Min:  int32(a.Scaling.Min),
			Max:  int32(a.Scaling.Max),
			Auto: a.Scaling.Auto,
		}
	}

	return &taskguildv1.Agent{
		Id:               a.ID,
		Role:             a.Role,
		Type:             a.Type,
		MemoryPath:       a.MemoryPath,
		Status:           status,
		TaskId:           a.TaskID,
		WorktreePath:     a.WorktreePath,
		Triggers:         protoTriggers,
		ApprovalRequired: protoApprovalRules,
		Scaling:          protoScaling,
		CreatedAt:        timestamppb.New(a.CreatedAt),
		UpdatedAt:        timestamppb.New(a.UpdatedAt),
	}, nil
}

func (h *AgentServiceHandler) agentStatusToProto(status agent.Status) (taskguildv1.AgentStatus, error) {
	switch status {
	case agent.StatusIdle:
		return taskguildv1.AgentStatus_AGENT_STATUS_IDLE, nil
	case agent.StatusBusy:
		return taskguildv1.AgentStatus_AGENT_STATUS_BUSY, nil
	case agent.StatusWaiting:
		return taskguildv1.AgentStatus_AGENT_STATUS_WAITING, nil
	case agent.StatusError:
		return taskguildv1.AgentStatus_AGENT_STATUS_ERROR, nil
	case agent.StatusStopped:
		return taskguildv1.AgentStatus_AGENT_STATUS_STOPPED, nil
	default:
		return taskguildv1.AgentStatus_AGENT_STATUS_UNSPECIFIED, fmt.Errorf("unknown agent status: %s", status)
	}
}
