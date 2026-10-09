package agentmanager

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/claudemd"
	"github.com/kazz187/taskguild/internal/eventbus"
	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Skill comparison & conflict resolution RPCs ---

// RequestSkillComparison sends a CompareSkillsCommand to connected agent-managers
// so they compare local skills with server versions.
func (s *Server) RequestSkillComparison(ctx context.Context, req *taskguildv1.RequestSkillComparisonRequest) (*taskguildv1.RequestSkillComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}

	// Fetch all skills for this project so the agent can compare.
	skills, _, err := s.skillRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.SkillDefinition, len(skills))
	for i, sk := range skills {
		protos[i] = skillToProto(sk)
	}

	requestID := ulid.Make().String()

	s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
		Command: &taskguildv1.AgentCommand_CompareSkills{
			CompareSkills: &taskguildv1.CompareSkillsCommand{
				RequestId: requestID,
				Skills:    protos,
			},
		},
	})

	slog.Info("skill comparison requested",
		"project_id", req.GetProjectId(),
		"project_name", proj.Name,
		"request_id", requestID,
		"skill_count", len(skills),
	)

	return &taskguildv1.RequestSkillComparisonResponse{
		RequestId: requestID,
	}, nil
}

// ReportSkillComparison receives comparison results from the agent and caches them.
func (s *Server) ReportSkillComparison(ctx context.Context, req *taskguildv1.ReportSkillComparisonRequest) (*taskguildv1.ReportSkillComparisonResponse, error) {
	projectName := req.GetProjectName()
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil)
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, err
	}

	// Cache the diffs for this project.
	s.skillDiffMu.Lock()
	s.skillDiffCache[proj.ID] = req.GetDiffs()
	s.skillDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SKILL_COMPARISON,
		req.GetRequestId(),
		"",
		map[string]string{
			eventbus.MetaProjectID: proj.ID,
			eventbus.MetaRequestID: req.GetRequestId(),
			eventbus.MetaDiffCount: strconv.Itoa(len(req.GetDiffs())),
		},
	)

	slog.Info("skill comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.GetRequestId(),
		"diff_count", len(req.GetDiffs()),
	)

	return &taskguildv1.ReportSkillComparisonResponse{}, nil
}

// GetSkillComparison returns the cached skill diffs for a project.
func (s *Server) GetSkillComparison(ctx context.Context, req *taskguildv1.GetSkillComparisonRequest) (*taskguildv1.GetSkillComparisonResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	s.skillDiffMu.RLock()
	diffs := s.skillDiffCache[req.GetProjectId()]
	s.skillDiffMu.RUnlock()

	return &taskguildv1.GetSkillComparisonResponse{
		Diffs: diffs,
	}, nil
}

// ResolveSkillConflict resolves a single skill conflict between server and agent versions.
func (s *Server) ResolveSkillConflict(ctx context.Context, req *taskguildv1.ResolveSkillConflictRequest) (*taskguildv1.ResolveSkillConflictResponse, error) {
	if req.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil)
	}

	proj, err := s.projectRepo.Get(ctx, req.GetProjectId())
	if err != nil {
		return nil, err
	}

	var resultSkill *skill.Skill

	switch req.GetChoice() {
	case taskguildv1.SkillResolutionChoice_SKILL_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncSkillsCommand.
		if req.GetSkillId() != "" {
			resultSkill, err = s.skillRepo.Get(ctx, req.GetSkillId())
			if err != nil {
				return nil, err
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncSkills{
					SyncSkills: &taskguildv1.SyncSkillsCommand{
						ForceOverwriteSkillIds: []string{req.GetSkillId()},
					},
				},
			})
		}

	case taskguildv1.SkillResolutionChoice_SKILL_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		parsed := claudemd.ParseSkill(req.GetAgentContent())

		if req.GetSkillId() != "" {
			// Update existing skill.
			resultSkill, err = s.skillRepo.Get(ctx, req.GetSkillId())
			if err != nil {
				return nil, err
			}

			resultSkill.Description = parsed.Description
			resultSkill.Content = parsed.Content
			resultSkill.DisableModelInvocation = parsed.DisableModelInvocation
			resultSkill.UserInvocable = parsed.UserInvocable
			resultSkill.AllowedTools = parsed.AllowedTools
			resultSkill.Model = parsed.Model
			resultSkill.Context = parsed.Context
			resultSkill.Agent = parsed.Agent
			resultSkill.ArgumentHint = parsed.ArgumentHint
			resultSkill.IsSynced = true

			resultSkill.UpdatedAt = time.Now()

			err := s.skillRepo.Update(ctx, resultSkill)
			if err != nil {
				return nil, err
			}
		} else {
			// Agent-only skill — create new in DB.
			now := time.Now()

			resultSkill = &skill.Skill{
				ID:                     ulid.Make().String(),
				ProjectID:              req.GetProjectId(),
				Name:                   req.GetSkillName(),
				Description:            parsed.Description,
				Content:                parsed.Content,
				DisableModelInvocation: parsed.DisableModelInvocation,
				UserInvocable:          parsed.UserInvocable,
				AllowedTools:           parsed.AllowedTools,
				Model:                  parsed.Model,
				Context:                parsed.Context,
				Agent:                  parsed.Agent,
				ArgumentHint:           parsed.ArgumentHint,
				IsSynced:               true,
				CreatedAt:              now,
				UpdatedAt:              now,
			}

			err := s.skillRepo.Create(ctx, resultSkill)
			if err != nil {
				return nil, err
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil)
	}

	// Remove the resolved diff from cache.
	s.removeSkillDiff(req.GetProjectId(), req.GetSkillId(), req.GetFilename())

	var proto *taskguildv1.SkillDefinition
	if resultSkill != nil {
		proto = skillToProto(resultSkill)
	}

	slog.Info("skill conflict resolved",
		"project_id", req.GetProjectId(),
		"skill_id", req.GetSkillId(),
		"skill_name", req.GetSkillName(),
		"choice", req.GetChoice().String(),
	)

	return &taskguildv1.ResolveSkillConflictResponse{
		Skill: proto,
	}, nil
}

// removeSkillDiff removes a specific diff entry from the cache.
// It matches by skill_id if non-empty, otherwise by filename.
func (s *Server) removeSkillDiff(projectID, skillID, filename string) {
	s.skillDiffMu.Lock()
	defer s.skillDiffMu.Unlock()

	diffs := s.skillDiffCache[projectID]
	if len(diffs) == 0 {
		return
	}

	filtered := make([]*taskguildv1.SkillDiff, 0, len(diffs))
	for _, d := range diffs {
		if skillID != "" && d.GetSkillId() == skillID {
			continue // remove this diff
		}

		if skillID == "" && filename != "" && d.GetFilename() == filename {
			continue // remove agent-only diff by filename
		}

		filtered = append(filtered, d)
	}

	s.skillDiffCache[projectID] = filtered
}
