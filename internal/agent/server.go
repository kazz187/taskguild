package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
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

func (s *Server) CreateAgent(ctx context.Context, req *connect.Request[taskguildv1.CreateAgentRequest]) (*connect.Response[taskguildv1.CreateAgentResponse], error) {
	now := time.Now()

	a := &Agent{
		ID:              ulid.Make().String(),
		ProjectID:       req.Msg.GetProjectId(),
		Name:            req.Msg.GetName(),
		Description:     req.Msg.GetDescription(),
		Prompt:          req.Msg.GetPrompt(),
		Tools:           req.Msg.GetTools(),
		DisallowedTools: req.Msg.GetDisallowedTools(),
		Model:           req.Msg.GetModel(),
		PermissionMode:  req.Msg.GetPermissionMode(),
		Skills:          req.Msg.GetSkills(),
		Memory:          req.Msg.GetMemory(),
		IsSynced:        false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	err := s.repo.Create(ctx, a)
	if err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, []string{a.Name})

	return connect.NewResponse(&taskguildv1.CreateAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) GetAgent(ctx context.Context, req *connect.Request[taskguildv1.GetAgentRequest]) (*connect.Response[taskguildv1.GetAgentResponse], error) {
	a, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.GetAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) ListAgents(ctx context.Context, req *connect.Request[taskguildv1.ListAgentsRequest]) (*connect.Response[taskguildv1.ListAgentsResponse], error) {
	limit, offset := int32(50), int32(0)

	if req.Msg.GetPagination() != nil {
		if req.Msg.GetPagination().GetLimit() > 0 {
			limit = req.Msg.GetPagination().GetLimit()
		}

		offset = req.Msg.GetPagination().GetOffset()
	}

	agents, total, err := s.repo.List(ctx, req.Msg.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.AgentDefinition, len(agents))
	for i, a := range agents {
		protos[i] = toProto(a)
	}

	return connect.NewResponse(&taskguildv1.ListAgentsResponse{
		Agents: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateAgent(ctx context.Context, req *connect.Request[taskguildv1.UpdateAgentRequest]) (*connect.Response[taskguildv1.UpdateAgentResponse], error) {
	a, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	if req.Msg.GetName() != "" {
		a.Name = req.Msg.GetName()
	}

	if req.Msg.GetDescription() != "" {
		a.Description = req.Msg.GetDescription()
	}

	if req.Msg.GetPrompt() != "" {
		a.Prompt = req.Msg.GetPrompt()
	}

	if req.Msg.Tools != nil {
		a.Tools = req.Msg.GetTools()
	}

	if req.Msg.DisallowedTools != nil {
		a.DisallowedTools = req.Msg.GetDisallowedTools()
	}

	if req.Msg.GetModel() != "" {
		a.Model = req.Msg.GetModel()
	}

	if req.Msg.GetPermissionMode() != "" {
		a.PermissionMode = req.Msg.GetPermissionMode()
	}

	if req.Msg.Skills != nil {
		a.Skills = req.Msg.GetSkills()
	}

	if req.Msg.GetMemory() != "" {
		a.Memory = req.Msg.GetMemory()
	}

	a.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, []string{a.Name})

	return connect.NewResponse(&taskguildv1.UpdateAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) DeleteAgent(ctx context.Context, req *connect.Request[taskguildv1.DeleteAgentRequest]) (*connect.Response[taskguildv1.DeleteAgentResponse], error) {
	// Fetch the agent before deleting to capture the project ID for notification.
	a, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	if err := s.repo.Delete(ctx, req.Msg.GetId()); err != nil {
		return nil, err
	}

	s.notifyChange(a.ProjectID, nil)

	return connect.NewResponse(&taskguildv1.DeleteAgentResponse{}), nil
}

// SyncAgentsFromDir scans a directory for .claude/agents/*.md files and syncs them.
func (s *Server) SyncAgentsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncAgentsFromDirRequest]) (*connect.Response[taskguildv1.SyncAgentsFromDirResponse], error) {
	dir := req.Msg.GetDirectory()
	if (dir == "" || dir == ".") && s.resolver != nil {
		resolved, err := s.resolver.ResolveWorkDir(req.Msg.GetProjectId())
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("failed to resolve work directory: %w", err))
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
			return connect.NewResponse(&taskguildv1.SyncAgentsFromDirResponse{}), nil
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
		existing, err := s.repo.FindByName(ctx, req.Msg.GetProjectId(), parsed.Name)
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
				ProjectID:       req.Msg.GetProjectId(),
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
		s.notifyChange(req.Msg.GetProjectId(), nil)
	}

	return connect.NewResponse(&taskguildv1.SyncAgentsFromDirResponse{
		Agents:  synced,
		Created: created,
		Updated: updated,
	}), nil
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
