package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.AgentServiceHandler = (*Server)(nil)

// ChangeNotifier is called after agent CRUD operations to notify connected
// agents that they should re-sync their local agent definitions.
type ChangeNotifier interface {
	NotifyAgentChange(projectID string)
}

type Server struct {
	repo     Repository
	notifier ChangeNotifier
}

func NewServer(repo Repository, notifier ChangeNotifier) *Server {
	return &Server{repo: repo, notifier: notifier}
}

func (s *Server) notifyChange(projectID string) {
	if s.notifier != nil {
		s.notifier.NotifyAgentChange(projectID)
	}
}

func (s *Server) CreateAgent(ctx context.Context, req *connect.Request[taskguildv1.CreateAgentRequest]) (*connect.Response[taskguildv1.CreateAgentResponse], error) {
	now := time.Now()
	a := &Agent{
		ID:             ulid.Make().String(),
		ProjectID:      req.Msg.ProjectId,
		Name:           req.Msg.Name,
		Description:    req.Msg.Description,
		Prompt:         req.Msg.Prompt,
		Tools:          req.Msg.Tools,
		Model:          req.Msg.Model,
		MaxTurns:       req.Msg.MaxTurns,
		PermissionMode: req.Msg.PermissionMode,
		Isolation:      req.Msg.Isolation,
		IsSynced:       false,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, err
	}
	s.notifyChange(a.ProjectID)
	return connect.NewResponse(&taskguildv1.CreateAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) GetAgent(ctx context.Context, req *connect.Request[taskguildv1.GetAgentRequest]) (*connect.Response[taskguildv1.GetAgentResponse], error) {
	a, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) ListAgents(ctx context.Context, req *connect.Request[taskguildv1.ListAgentsRequest]) (*connect.Response[taskguildv1.ListAgentsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	agents, total, err := s.repo.List(ctx, req.Msg.ProjectId, int(limit), int(offset))
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
	a, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Name != "" {
		a.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		a.Description = req.Msg.Description
	}
	if req.Msg.Prompt != "" {
		a.Prompt = req.Msg.Prompt
	}
	if req.Msg.Tools != nil {
		a.Tools = req.Msg.Tools
	}
	if req.Msg.Model != "" {
		a.Model = req.Msg.Model
	}
	if req.Msg.MaxTurns != 0 {
		a.MaxTurns = req.Msg.MaxTurns
	}
	if req.Msg.PermissionMode != "" {
		a.PermissionMode = req.Msg.PermissionMode
	}
	if req.Msg.Isolation != "" {
		a.Isolation = req.Msg.Isolation
	}
	a.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, a); err != nil {
		return nil, err
	}
	s.notifyChange(a.ProjectID)
	return connect.NewResponse(&taskguildv1.UpdateAgentResponse{
		Agent: toProto(a),
	}), nil
}

func (s *Server) DeleteAgent(ctx context.Context, req *connect.Request[taskguildv1.DeleteAgentRequest]) (*connect.Response[taskguildv1.DeleteAgentResponse], error) {
	// Fetch the agent before deleting to capture the project ID for notification.
	a, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	s.notifyChange(a.ProjectID)
	return connect.NewResponse(&taskguildv1.DeleteAgentResponse{}), nil
}

// SyncAgentsFromDir scans a directory for .claude/agents/*.md files and syncs them.
func (s *Server) SyncAgentsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncAgentsFromDirRequest]) (*connect.Response[taskguildv1.SyncAgentsFromDirResponse], error) {
	dir := req.Msg.Directory
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
		existing, err := s.repo.FindByName(ctx, req.Msg.ProjectId, parsed.Name)
		if err == nil && existing != nil {
			// Update existing agent.
			existing.Description = parsed.Description
			existing.Prompt = parsed.Prompt
			existing.Tools = parsed.Tools
			existing.Model = parsed.Model
			existing.MaxTurns = parsed.MaxTurns
			existing.PermissionMode = parsed.PermissionMode
			existing.Isolation = parsed.Isolation
			existing.IsSynced = true
			existing.UpdatedAt = time.Now()
			if err := s.repo.Update(ctx, existing); err != nil {
				continue
			}
			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new agent.
			now := time.Now()
			a := &Agent{
				ID:             ulid.Make().String(),
				ProjectID:      req.Msg.ProjectId,
				Name:           parsed.Name,
				Description:    parsed.Description,
				Prompt:         parsed.Prompt,
				Tools:          parsed.Tools,
				Model:          parsed.Model,
				MaxTurns:       parsed.MaxTurns,
				PermissionMode: parsed.PermissionMode,
				Isolation:      parsed.Isolation,
				IsSynced:       true,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if err := s.repo.Create(ctx, a); err != nil {
				continue
			}
			synced = append(synced, toProto(a))
			created++
		}
	}

	if created > 0 || updated > 0 {
		s.notifyChange(req.Msg.ProjectId)
	}

	return connect.NewResponse(&taskguildv1.SyncAgentsFromDirResponse{
		Agents:  synced,
		Created: created,
		Updated: updated,
	}), nil
}

// parsedAgent holds data extracted from a .claude/agents/*.md file.
type parsedAgent struct {
	Name           string
	Description    string
	Prompt         string
	Tools          []string
	Model          string
	MaxTurns       int32
	PermissionMode string
	Isolation      string
}

// parseAgentMDFile parses a Claude Code agent definition markdown file.
// Format: YAML frontmatter between --- delimiters, followed by the prompt body.
func parseAgentMDFile(filePath string) (*parsedAgent, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	// Detect frontmatter start.
	hasFrontmatter := false
	var frontmatterLines []string
	var bodyLines []string
	inFrontmatter := false
	frontmatterDone := false

	for scanner.Scan() {
		line := scanner.Text()
		if !hasFrontmatter && !frontmatterDone {
			if strings.TrimSpace(line) == "---" {
				hasFrontmatter = true
				inFrontmatter = true
				continue
			}
			// No frontmatter, everything is body.
			frontmatterDone = true
			bodyLines = append(bodyLines, line)
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				frontmatterDone = true
				continue
			}
			frontmatterLines = append(frontmatterLines, line)
		} else {
			bodyLines = append(bodyLines, line)
		}
	}

	// Extract name from filename.
	base := filepath.Base(filePath)
	name := strings.TrimSuffix(base, ".md")

	result := &parsedAgent{
		Name: name,
	}

	// Parse frontmatter as simple key: value pairs.
	for _, line := range frontmatterLines {
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			switch key {
			case "name":
				result.Name = value
			case "description":
				result.Description = value
			case "tools":
				// Tools can be comma-separated.
				parts := strings.Split(value, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						result.Tools = append(result.Tools, p)
					}
				}
			case "model":
				result.Model = value
			case "maxTurns":
				var n int32
				fmt.Sscanf(value, "%d", &n)
				result.MaxTurns = n
			case "permissionMode":
				result.PermissionMode = value
			case "isolation":
				result.Isolation = value
			}
		}
	}

	// The body is the system prompt.
	body := strings.Join(bodyLines, "\n")
	body = strings.TrimSpace(body)
	result.Prompt = body

	return result, nil
}

func toProto(a *Agent) *taskguildv1.AgentDefinition {
	return &taskguildv1.AgentDefinition{
		Id:             a.ID,
		ProjectId:      a.ProjectID,
		Name:           a.Name,
		Description:    a.Description,
		Prompt:         a.Prompt,
		Tools:          a.Tools,
		Model:          a.Model,
		MaxTurns:       a.MaxTurns,
		PermissionMode: a.PermissionMode,
		Isolation:      a.Isolation,
		IsSynced:       a.IsSynced,
		CreatedAt:      timestamppb.New(a.CreatedAt),
		UpdatedAt:      timestamppb.New(a.UpdatedAt),
	}
}
