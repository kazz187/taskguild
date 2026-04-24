package agentmanager

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	"github.com/kazz187/taskguild/internal/skill"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// --- Skill comparison & conflict resolution RPCs ---

// RequestSkillComparison sends a CompareSkillsCommand to connected agent-managers
// so they compare local skills with server versions.
func (s *Server) RequestSkillComparison(ctx context.Context, req *connect.Request[taskguildv1.RequestSkillComparisonRequest]) (*connect.Response[taskguildv1.RequestSkillComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Fetch all skills for this project so the agent can compare.
	skills, _, err := s.skillRepo.List(ctx, proj.ID, 1000, 0)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
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
		"project_id", req.Msg.ProjectId,
		"project_name", proj.Name,
		"request_id", requestID,
		"skill_count", len(skills),
	)

	return connect.NewResponse(&taskguildv1.RequestSkillComparisonResponse{
		RequestId: requestID,
	}), nil
}

// ReportSkillComparison receives comparison results from the agent and caches them.
func (s *Server) ReportSkillComparison(ctx context.Context, req *connect.Request[taskguildv1.ReportSkillComparisonRequest]) (*connect.Response[taskguildv1.ReportSkillComparisonResponse], error) {
	projectName := req.Msg.ProjectName
	if projectName == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_name is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.FindByName(ctx, projectName)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	// Cache the diffs for this project.
	s.skillDiffMu.Lock()
	s.skillDiffCache[proj.ID] = req.Msg.Diffs
	s.skillDiffMu.Unlock()

	// Publish event so frontend can pick up the comparison results.
	s.eventBus.PublishNew(
		taskguildv1.EventType_EVENT_TYPE_SKILL_COMPARISON,
		req.Msg.RequestId,
		"",
		map[string]string{
			"project_id": proj.ID,
			"request_id": req.Msg.RequestId,
			"diff_count": strconv.Itoa(len(req.Msg.Diffs)),
		},
	)

	slog.Info("skill comparison reported",
		"project_id", proj.ID,
		"project_name", projectName,
		"request_id", req.Msg.RequestId,
		"diff_count", len(req.Msg.Diffs),
	)

	return connect.NewResponse(&taskguildv1.ReportSkillComparisonResponse{}), nil
}

// GetSkillComparison returns the cached skill diffs for a project.
func (s *Server) GetSkillComparison(ctx context.Context, req *connect.Request[taskguildv1.GetSkillComparisonRequest]) (*connect.Response[taskguildv1.GetSkillComparisonResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	s.skillDiffMu.RLock()
	diffs := s.skillDiffCache[req.Msg.ProjectId]
	s.skillDiffMu.RUnlock()

	return connect.NewResponse(&taskguildv1.GetSkillComparisonResponse{
		Diffs: diffs,
	}), nil
}

// ResolveSkillConflict resolves a single skill conflict between server and agent versions.
func (s *Server) ResolveSkillConflict(ctx context.Context, req *connect.Request[taskguildv1.ResolveSkillConflictRequest]) (*connect.Response[taskguildv1.ResolveSkillConflictResponse], error) {
	if req.Msg.ProjectId == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	proj, err := s.projectRepo.Get(ctx, req.Msg.ProjectId)
	if err != nil {
		return nil, cerr.ExtractConnectError(ctx, err)
	}

	var resultSkill *skill.Skill

	switch req.Msg.Choice {
	case taskguildv1.SkillResolutionChoice_SKILL_RESOLUTION_CHOICE_SERVER:
		// Server version wins. DB is already correct.
		// Force-overwrite the agent's local file by sending SyncSkillsCommand.
		if req.Msg.SkillId != "" {
			resultSkill, err = s.skillRepo.Get(ctx, req.Msg.SkillId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}

			s.registry.BroadcastCommandToProject(proj.Name, &taskguildv1.AgentCommand{
				Command: &taskguildv1.AgentCommand_SyncSkills{
					SyncSkills: &taskguildv1.SyncSkillsCommand{
						ForceOverwriteSkillIds: []string{req.Msg.SkillId},
					},
				},
			})
		}

	case taskguildv1.SkillResolutionChoice_SKILL_RESOLUTION_CHOICE_AGENT:
		// Agent version wins. Update the DB with agent's content.
		parsed, parseErr := parseSkillMDContent(req.Msg.AgentContent)
		if parseErr != nil {
			return nil, cerr.NewError(cerr.InvalidArgument, fmt.Sprintf("failed to parse skill content: %v", parseErr), nil).ConnectError()
		}

		if req.Msg.SkillId != "" {
			// Update existing skill.
			resultSkill, err = s.skillRepo.Get(ctx, req.Msg.SkillId)
			if err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
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
			if err := s.skillRepo.Update(ctx, resultSkill); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		} else {
			// Agent-only skill — create new in DB.
			now := time.Now()
			resultSkill = &skill.Skill{
				ID:                     ulid.Make().String(),
				ProjectID:              req.Msg.ProjectId,
				Name:                   req.Msg.SkillName,
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
			if err := s.skillRepo.Create(ctx, resultSkill); err != nil {
				return nil, cerr.ExtractConnectError(ctx, err)
			}
		}

	default:
		return nil, cerr.NewError(cerr.InvalidArgument, "invalid resolution choice", nil).ConnectError()
	}

	// Remove the resolved diff from cache.
	s.removeSkillDiff(req.Msg.ProjectId, req.Msg.SkillId, req.Msg.Filename)

	var proto *taskguildv1.SkillDefinition
	if resultSkill != nil {
		proto = skillToProto(resultSkill)
	}

	slog.Info("skill conflict resolved",
		"project_id", req.Msg.ProjectId,
		"skill_id", req.Msg.SkillId,
		"skill_name", req.Msg.SkillName,
		"choice", req.Msg.Choice.String(),
	)

	return connect.NewResponse(&taskguildv1.ResolveSkillConflictResponse{
		Skill: proto,
	}), nil
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
		if skillID != "" && d.SkillId == skillID {
			continue // remove this diff
		}
		if skillID == "" && filename != "" && d.Filename == filename {
			continue // remove agent-only diff by filename
		}
		filtered = append(filtered, d)
	}
	s.skillDiffCache[projectID] = filtered
}

// parseSkillMDContent parses a SKILL.md content string (YAML frontmatter + body)
// and returns the extracted fields. Used when resolving conflicts with AGENT choice.
func parseSkillMDContent(content string) (*parsedSkillMD, error) {
	result := &parsedSkillMD{
		UserInvocable: true, // Default per skill spec.
	}

	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		// No frontmatter, treat entire content as body.
		result.Content = content
		return result, nil
	}

	// Find closing ---.
	closingIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closingIdx = i
			break
		}
	}
	if closingIdx == -1 {
		result.Content = content
		return result, nil
	}

	// Parse YAML frontmatter.
	var currentListKey string
	for i := 1; i < closingIdx; i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Check for YAML list item.
		if strings.HasPrefix(trimmed, "- ") && currentListKey != "" {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if item != "" {
				switch currentListKey {
				case "allowed-tools":
					result.AllowedTools = append(result.AllowedTools, item)
				}
			}
			continue
		}

		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			currentListKey = ""

			switch key {
			case "name":
				result.Name = value
			case "description":
				result.Description = value
			case "disable-model-invocation":
				result.DisableModelInvocation = strings.EqualFold(value, "true")
			case "user-invocable":
				result.UserInvocable = strings.EqualFold(value, "true")
			case "allowed-tools":
				if value == "" {
					currentListKey = "allowed-tools"
				} else {
					for p := range strings.SplitSeq(value, ",") {
						p = strings.TrimSpace(p)
						if p != "" {
							result.AllowedTools = append(result.AllowedTools, p)
						}
					}
				}
			case "model":
				result.Model = value
			case "context":
				result.Context = value
			case "agent":
				result.Agent = value
			case "argument-hint":
				result.ArgumentHint = value
			}
		}
	}

	// Extract body (everything after closing ---).
	if closingIdx+1 < len(lines) {
		bodyLines := lines[closingIdx+1:]
		body := strings.Join(bodyLines, "\n")
		result.Content = strings.TrimSpace(body)
	}

	return result, nil
}

// parsedSkillMD holds data extracted from a SKILL.md content string.
type parsedSkillMD struct {
	Name                   string
	Description            string
	Content                string
	DisableModelInvocation bool
	UserInvocable          bool
	AllowedTools           []string
	Model                  string
	Context                string
	Agent                  string
	ArgumentHint           string
}
