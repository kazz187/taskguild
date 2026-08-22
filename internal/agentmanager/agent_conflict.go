package agentmanager

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/claudemd"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Agent comparison & conflict resolution RPCs ---

// RequestAgentComparison sends a CompareAgentsCommand to connected agent-managers
// so they compare local agents with server versions.
func (s *Server) RequestAgentComparison(ctx context.Context, req *connect.Request[taskguildv1.RequestAgentComparisonRequest]) (*connect.Response[taskguildv1.RequestAgentComparisonResponse], error) {
	if req.Msg.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Fetch all agents for this project so the agent can compare.
	agents, _, err := s.agentRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	protos := make([]*taskguildv1.AgentDefinition, len(agents))
	for i, a := range agents {
		protos[i] = agentToProto(a)
	}

	requestID := ulid.Make().String()

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_CompareAgents{
			CompareAgents: &taskguildv1.CompareAgentsCommand{
				RequestId: requestID,
				Agents:    protos,
			},
		},
	})

	slog.Info("agent comparison requested",
		"project_id", req.Msg.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
		"agent_count", len(agents),
	)

	return connect.NewResponse(&taskguildv1.RequestAgentComparisonResponse{
		RequestId: requestID,
	}), nil
}

// ReportAgentComparison receives comparison results from the agent and caches them.
func (s *Server) ReportAgentComparison(ctx context.Context, req *connect.Request[taskguildv1.ReportAgentComparisonRequest]) (*connect.Response[taskguildv1.ReportAgentComparisonResponse], error) {
	projectName := req.Msg.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the diffs for this project.
	s.agentDiffMu.Lock()
	s.agentDiffCache[proj.ID] = req.Msg.GetDiffs()
	s.agentDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_COMPARISON,
		req.Msg.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID: proj.ID,
			eventbus.MetaRequestID: req.Msg.GetRequestId(),
			eventbus.MetaDiffCount: strconv.Itoa(len(req.Msg.GetDiffs())),
		},
	)

	slog.Info("agent comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.GetRequestId(),
		"diff_count", len(req.Msg.GetDiffs()),
	)

	return connect.NewResponse(&taskguildv1.ReportAgentComparisonResponse{}), nil
}

// GetAgentComparison returns the cached agent diffs for a project.
func (s *Server) GetAgentComparison(ctx context.Context, req *connect.Request[taskguildv1.GetAgentComparisonRequest]) (*connect.Response[taskguildv1.GetAgentComparisonResponse], error) {
	if req.Msg.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.agentDiffMu.RLock()
	diffs := s.agentDiffCache[req.Msg.GetProjectId()]
	s.agentDiffMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetAgentComparisonResponse{
		Diffs: diffs,
	}), nil
}

// ResolveAgentConflict resolves a single agent conflict between server and agent versions.
func (s *Server) ResolveAgentConflict(ctx context.Context, req *connect.Request[taskguildv1.ResolveAgentConflictRequest]) (*connect.Response[taskguildv1.ResolveAgentConflictResponse], error) {
	if req.Msg.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.GetProjectId())
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var resultAgent *agent.Agent

	switch req.Msg.GetChoice() {
	case taskguildv1.AgentResolutionChoice_AGENT_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncAgentsCommand.
		if req.Msg.GetAgentName() != "" {
			if req.Msg.GetAgentId() != "" {
				resultAgent, err = s.agentRepo.Get(ctx, req.Msg.GetAgentId())
				if err != nil {
					return nil, cerr.ExtractConnectError(ctx, err)
				}
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncAgents{
					SyncAgents: &taskguildv1.SyncAgentsCommand{
						ForceOverwriteAgentNames: []string{req.Msg.GetAgentName()},
					},
				},
			})
		}

	case taskguildv1.AgentResolutionChoice_AGENT_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		// Parse the agent MD content from the agent side.
		if req.Msg.GetAgentId() != "" {
			// Update existing agent.
			resultAgent, err = s.agentRepo.Get(ctx, req.Msg.GetAgentId())
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
			// Parse the agent content to extract fields.
			parsed := claudemd.ParseAgent(req.Msg.GetAgentContent())

			resultAgent.Description = parsed.Description
			resultAgent.Prompt = parsed.Prompt
			resultAgent.Tools = parsed.Tools
			resultAgent.DisallowedTools = parsed.DisallowedTools
			resultAgent.Model = parsed.Model
			resultAgent.PermissionMode = parsed.PermissionMode
			resultAgent.Skills = parsed.Skills
			resultAgent.Memory = parsed.Memory
			resultAgent.IsSynced = true

			resultAgent.UpdatedAt = time.Now()

			err := s.agentRepo.Update(ctx, resultAgent)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		} else {
			// Agent-only agent — create new in DB.
			parsed := claudemd.ParseAgent(req.Msg.GetAgentContent())

			now := time.Now()

			resultAgent = &agent.Agent{
				ID:              ulid.Make().String(),
				ProjectID:       req.Msg.GetProjectId(),
				Name:            req.Msg.GetAgentName(),
				Description:     parsed.Description,
				Prompt:          parsed.Prompt,
				Tools:           parsed.Tools,
				DisallowedTools: parsed.DisallowedTools,
				Model:           parsed.Model,
				PermissionMode:  parsed.PermissionMode,
				Skills:          parsed.Skills,
				Memory:          parsed.Memory,
				IsSynced:        true,
				CreatedAt:       now,
				UpdatedAt:       now,
			}

			err := s.agentRepo.Create(ctx, resultAgent)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil).ConnectError()
	}

	// Remove the resolved diff from cache.
	s.removeAgentDiff(req.Msg.GetProjectId(), req.Msg.GetAgentId(), req.Msg.GetFilename())

	var proto *taskguildv1.AgentDefinition
	if resultAgent != nil {
		proto = agentToProto(resultAgent)
	}

	slog.Info("agent conflict resolved",
		"project_id", req.Msg.GetProjectId(),
		"agent_id", req.Msg.GetAgentId(),
		"agent_name", req.Msg.GetAgentName(),
		"choice", req.Msg.GetChoice().String(),
	)

	return connect.NewResponse(&taskguildv1.ResolveAgentConflictResponse{
		Agent: proto,
	}), nil
}

// removeAgentDiff removes a specific diff entry from the cache.
// It matches by agent_id if non-empty, otherwise by filename.
func (s *Server) removeAgentDiff(projectID, agentID, filename string) {
	s.agentDiffMu.Lock()
	defer s.agentDiffMu.Unlock()

	diffs := s.agentDiffCache[projectID]
	if len(diffs) == 0 {
		return
	}

	filtered := make([]*taskguildv1.AgentDiff, 0, len(diffs))
	for _, d := range diffs {
		if agentID != "" && d.GetAgentId() == agentID {
			continue // remove this diff
		}

		if agentID == "" && filename != "" && d.GetFilename() == filename {
			continue // remove agent-only diff by filename
		}

		filtered = append(filtered, d)
	}

	s.agentDiffCache[projectID] = filtered
}
