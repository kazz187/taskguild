package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/claudemd"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentServiceHandler = (*Server)(nil)

// ChangeNotifier is called after agent CRUD operations to notify connected
// agents that they should re-sync their local agent definitions.
// changedAgentNames lists agents that were created or updated and should be
// force-overwritten on the agent side even if a local file already exists.
type ChangeNotifier interface {
	NotifyAgentChange(projectID string, changedAgentNames []string)
}

// WorkDirResolver resolves the absolute working directory for a project
// by looking up the connected agent's work_dir.
type WorkDirResolver interface {
	ResolveWorkDir(projectID string) (string, error)
}

type Server struct {
	repo     Repository
	notifier ChangeNotifier
	resolver WorkDirResolver
}

func NewServer(repo Repository, notifier ChangeNotifier, resolver WorkDirResolver) *Server {
	return &Server{repo: repo, notifier: notifier, resolver: resolver}
}

func (s *Server) notifyChange(projectID string, changedAgentNames []string) {
	if s.notifier != nil {
		s.notifier.NotifyAgentChange(projectID, changedAgentNames)
	}
}

func (s *Server) CreateAgent(ctx context.Context, req *taskguildv1.CreateAgentRequest) (*taskguildv1.CreateAgentResponse, error) {
	now := time.Now()

	a := &Agent{
		ID:              ulid.Make().String(),
		ProjectID:       req.GetProjectId(),
		Name:            req.GetName(),
		Description:     req.GetDescription(),
		Prompt:          req.GetPrompt(),
		Tools:           req.GetTools(),
		DisallowedTools: req.GetDisallowedTools(),
		Model:           req.GetModel(),
		PermissionMode:  req.GetPermissionMode(),
		Skills:          req.GetSkills(),
		Memory:          req.GetMemory(),
		IsSynced:        false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err := s.repo.Create(ctx, a)
	if err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, []string{a.Name})

	return &taskguildv1.CreateAgentResponse{
		Agent: toProto(a),
	}, nil
}

func (s *Server) GetAgent(ctx context.Context, req *taskguildv1.GetAgentRequest) (*taskguildv1.GetAgentResponse, error) {
	a, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetAgentResponse{
		Agent: toProto(a),
	}, nil
}

func (s *Server) ListAgents(ctx context.Context, req *taskguildv1.ListAgentsRequest) (*taskguildv1.ListAgentsResponse, error) {
	limit, offset := int32(50), int32(0)

	if req.GetPagination() != nil {
		if req.GetPagination().GetLimit() > 0 {
			limit = req.GetPagination().GetLimit()
		}

		offset = req.GetPagination().GetOffset()
	}

	agents, total, err := s.repo.List(ctx, req.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.AgentDefinition, len(agents))
	for i, a := range agents {
		protos[i] = toProto(a)
	}

	return &taskguildv1.ListAgentsResponse{
		Agents: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}, nil
}

func (s *Server) UpdateAgent(ctx context.Context, req *taskguildv1.UpdateAgentRequest) (*taskguildv1.UpdateAgentResponse, error) {
	a, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if req.GetName() != "" {
		a.Name = req.GetName()
	}

	if req.GetDescription() != "" {
		a.Description = req.GetDescription()
	}

	if req.GetPrompt() != "" {
		a.Prompt = req.GetPrompt()
	}

	if req.Tools != nil {
		a.Tools = req.GetTools()
	}

	if req.DisallowedTools != nil {
		a.DisallowedTools = req.GetDisallowedTools()
	}

	if req.GetModel() != "" {
		a.Model = req.GetModel()
	}

	if req.GetPermissionMode() != "" {
		a.PermissionMode = req.GetPermissionMode()
	}

	if req.Skills != nil {
		a.Skills = req.GetSkills()
	}

	if req.GetMemory() != "" {
		a.Memory = req.GetMemory()
	}

	a.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, []string{a.Name})

	return &taskguildv1.UpdateAgentResponse{
		Agent: toProto(a),
	}, nil
}

func (s *Server) DeleteAgent(ctx context.Context, req *taskguildv1.DeleteAgentRequest) (*taskguildv1.DeleteAgentResponse, error) {
	// Fetch the agent before deleting to capture the project ID for notification.
	a, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if err := s.repo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, nil)

	return &taskguildv1.DeleteAgentResponse{}, nil
}

// SyncAgentsFromDir scans a directory for .claude/agents/*.md files and syncs them.
func (s *Server) SyncAgentsFromDir(ctx context.Context, req *taskguildv1.SyncAgentsFromDirRequest) (*taskguildv1.SyncAgentsFromDirResponse, error) {
	dir := req.GetDirectory()
	if (dir == "" || dir == ".") && s.resolver != nil {
		resolved, err := s.resolver.ResolveWorkDir(req.GetProjectId())
		if err != nil {
			return nil, connect.Errorf(connect.CodeFailedPrecondition, "failed to resolve work directory: %v", err).WithCause(err)
		}

		dir = resolved
	}

	if dir == "" {
		dir = "."
	}

	agentsDir := filepath.Join(dir, ".claude", "agents")

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &taskguildv1.SyncAgentsFromDirResponse{}, nil
		}

		return nil, fmt.Errorf("failed to read agents directory: %w", err)
	}

	var (
		synced  []*taskguildv1.AgentDefinition
		created int32
		updated int32
	)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		filePath := filepath.Join(agentsDir, entry.Name())

		parsed, err := parseAgentMDFile(filePath)
		if err != nil {
			continue
		}

		// Try to find existing agent with same name in this project.
		existing, err := s.repo.FindByName(ctx, req.GetProjectId(), parsed.Name)
		if err == nil && existing != nil {
			// Update existing agent.
			existing.Description = parsed.Description
			existing.Prompt = parsed.Prompt
			existing.Tools = parsed.Tools
			existing.DisallowedTools = parsed.DisallowedTools
			existing.Model = parsed.Model
			existing.PermissionMode = parsed.PermissionMode
			existing.Skills = parsed.Skills
			existing.Memory = parsed.Memory
			existing.IsSynced = true

			existing.UpdatedAt = time.Now()

			err := s.repo.Update(ctx, existing)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new agent.
			now := time.Now()

			a := &Agent{
				ID:              ulid.Make().String(),
				ProjectID:       req.GetProjectId(),
				Name:            parsed.Name,
				Description:     parsed.Description,
				Prompt:          parsed.Prompt,
				Tools:           parsed.Tools,
				Model:           parsed.Model,
				PermissionMode:  parsed.PermissionMode,
				DisallowedTools: parsed.DisallowedTools,
				Skills:          parsed.Skills,
				Memory:          parsed.Memory,
				IsSynced:        true,
				CreatedAt:       now,
				UpdatedAt:       now,
			}

			err := s.repo.Create(ctx, a)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(a))
			created++
		}
	}

	if created > 0 || updated > 0 {
		s.notifyChange(req.GetProjectId(), nil)
	}

	return &taskguildv1.SyncAgentsFromDirResponse{
		Agents:  synced,
		Created: created,
		Updated: updated,
	}, nil
}

// parseAgentMDFile reads a Claude Code agent definition markdown file and
// parses its YAML frontmatter. The file name (without extension) is used as the
// agent name when the frontmatter carries no name.
func parseAgentMDFile(filePath string) (*claudemd.Agent, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	parsed := claudemd.ParseAgent(string(data))
	if parsed.Name == "" {
		parsed.Name = strings.TrimSuffix(filepath.Base(filePath), ".md")
	}

	return parsed, nil
}

func toProto(a *Agent) *taskguildv1.AgentDefinition {
	return &taskguildv1.AgentDefinition{
		Id:              a.ID,
		ProjectId:       a.ProjectID,
		Name:            a.Name,
		Description:     a.Description,
		Prompt:          a.Prompt,
		Tools:           a.Tools,
		DisallowedTools: a.DisallowedTools,
		Model:           a.Model,
		PermissionMode:  a.PermissionMode,
		Skills:          a.Skills,
		Memory:          a.Memory,
		IsSynced:        a.IsSynced,
		CreatedAt:       timestamppb.New(a.CreatedAt),
		UpdatedAt:       timestamppb.New(a.UpdatedAt),
	}
}
