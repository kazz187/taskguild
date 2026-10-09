package agentmanager

import (
	"context"
	"log/slog"
	"strconv"
	"time"

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
func (s *Server) RequestAgentComparison(ctx context.Context, req *taskguildv1.RequestAgentComparisonRequest) (*taskguildv1.RequestAgentComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}

	// Fetch all agents for this project so the agent can compare.
	agents, _, err := s.agentRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, err
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
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
		"agent_count", len(agents),
	)

	return &taskguildv1.RequestAgentComparisonResponse{
		RequestId: requestID,
	}, nil
}

// ReportAgentComparison receives comparison results from the agent and caches them.
func (s *Server) ReportAgentComparison(ctx context.Context, req *taskguildv1.ReportAgentComparisonRequest) (*taskguildv1.ReportAgentComparisonResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, err
	}

	// Cache the diffs for this project.
	s.agentDiffMu.Lock()
	s.agentDiffCache[proj.ID] = req.GetDiffs()
	s.agentDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_AGENT_COMPARISON,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID: proj.ID,
			eventbus.MetaRequestID: req.GetRequestId(),
			eventbus.MetaDiffCount: strconv.Itoa(len(req.GetDiffs())),
		},
	)

	slog.Info("agent comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.GetRequestId(),
		"diff_count", len(req.GetDiffs()),
	)

	return &taskguildv1.ReportAgentComparisonResponse{}, nil
}

// GetAgentComparison returns the cached agent diffs for a project.
func (s *Server) GetAgentComparison(ctx context.Context, req *taskguildv1.GetAgentComparisonRequest) (*taskguildv1.GetAgentComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	s.agentDiffMu.RLock()
	diffs := s.agentDiffCache[req.GetProjectId()]
	s.agentDiffMu.RUnlock()

	return &taskguildv1.GetAgentComparisonResponse{
		Diffs: diffs,
	}, nil
}

// ResolveAgentConflict resolves a single agent conflict between server and agent versions.
func (s *Server) ResolveAgentConflict(ctx context.Context, req *taskguildv1.ResolveAgentConflictRequest) (*taskguildv1.ResolveAgentConflictResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}

	var resultAgent *agent.Agent

	switch req.GetChoice() {
	case taskguildv1.AgentResolutionChoice_AGENT_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncAgentsCommand.
		if req.GetAgentName() != "" {
			if req.GetAgentId() != "" {
				resultAgent, err = s.agentRepo.Get(ctx, req.GetAgentId())
				if err != nil {
					return nil, err
				}
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncAgents{
					SyncAgents: &taskguildv1.SyncAgentsCommand{
						ForceOverwriteAgentNames: []string{req.GetAgentName()},
					},
				},
			})
		}

	case taskguildv1.AgentResolutionChoice_AGENT_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		// Parse the agent MD content from the agent side.
		if req.GetAgentId() != "" {
			// Update existing agent.
			resultAgent, err = s.agentRepo.Get(ctx, req.GetAgentId())
			if err != nil {
				return nil, err
			}
			// Parse the agent content to extract fields.
			parsed := claudemd.ParseAgent(req.GetAgentContent())

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
				return nil, err
			}
		} else {
			// Agent-only agent — create new in DB.
			parsed := claudemd.ParseAgent(req.GetAgentContent())

			now := time.Now()

			resultAgent = &agent.Agent{
				ID:              ulid.Make().String(),
				ProjectID:       req.GetProjectId(),
				Name:            req.GetAgentName(),
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
				return nil, err
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil)
	}

	// Remove the resolved diff from cache.
	s.removeAgentDiff(req.GetProjectId(), req.GetAgentId(), req.GetFilename())

	var proto *taskguildv1.AgentDefinition
	if resultAgent != nil {
		proto = agentToProto(resultAgent)
	}

	slog.Info("agent conflict resolved",
		"project_id", req.GetProjectId(),
		"agent_id", req.GetAgentId(),
		"agent_name", req.GetAgentName(),
		"choice", req.GetChoice().String(),
	)

	return &taskguildv1.ResolveAgentConflictResponse{
		Agent: proto,
	}, nil
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
