package skill

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

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.SkillServiceHandler = (*Server)(nil)

type Server struct {
	repo Repository
}

func NewServer(repo Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) CreateSkill(ctx context.Context, req *connect.Request[taskguildv1.CreateSkillRequest]) (*connect.Response[taskguildv1.CreateSkillResponse], error) {
	now := time.Now()
	sk := &Skill{
		ID:                     ulid.Make().String(),
		ProjectID:              req.Msg.ProjectId,
		Name:                   req.Msg.Name,
		Description:            req.Msg.Description,
		Content:                req.Msg.Content,
		DisableModelInvocation: req.Msg.DisableModelInvocation,
		UserInvocable:          req.Msg.UserInvocable,
		AllowedTools:           req.Msg.AllowedTools,
		Model:                  req.Msg.Model,
		Context:                req.Msg.Context,
		Agent:                  req.Msg.Agent,
		ArgumentHint:           req.Msg.ArgumentHint,
		IsSynced:               false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	if err := s.repo.Create(ctx, sk); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.CreateSkillResponse{
		Skill: toProto(sk),
	}), nil
}

func (s *Server) GetSkill(ctx context.Context, req *connect.Request[taskguildv1.GetSkillRequest]) (*connect.Response[taskguildv1.GetSkillResponse], error) {
	sk, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetSkillResponse{
		Skill: toProto(sk),
	}), nil
}

func (s *Server) ListSkills(ctx context.Context, req *connect.Request[taskguildv1.ListSkillsRequest]) (*connect.Response[taskguildv1.ListSkillsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	skills, total, err := s.repo.List(ctx, req.Msg.ProjectId, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.SkillDefinition, len(skills))
	for i, sk := range skills {
		protos[i] = toProto(sk)
	}
	return connect.NewResponse(&taskguildv1.ListSkillsResponse{
		Skills: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateSkill(ctx context.Context, req *connect.Request[taskguildv1.UpdateSkillRequest]) (*connect.Response[taskguildv1.UpdateSkillResponse], error) {
	sk, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Name != "" {
		sk.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		sk.Description = req.Msg.Description
	}
	if req.Msg.Content != "" {
		sk.Content = req.Msg.Content
	}
	// Boolean fields are always applied (proto3 default is false).
	sk.DisableModelInvocation = req.Msg.DisableModelInvocation
	sk.UserInvocable = req.Msg.UserInvocable
	if req.Msg.AllowedTools != nil {
		sk.AllowedTools = req.Msg.AllowedTools
	}
	if req.Msg.Model != "" {
		sk.Model = req.Msg.Model
	}
	if req.Msg.Context != "" {
		sk.Context = req.Msg.Context
	}
	if req.Msg.Agent != "" {
		sk.Agent = req.Msg.Agent
	}
	if req.Msg.ArgumentHint != "" {
		sk.ArgumentHint = req.Msg.ArgumentHint
	}
	sk.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, sk); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.UpdateSkillResponse{
		Skill: toProto(sk),
	}), nil
}

func (s *Server) DeleteSkill(ctx context.Context, req *connect.Request[taskguildv1.DeleteSkillRequest]) (*connect.Response[taskguildv1.DeleteSkillResponse], error) {
	if _, err := s.repo.Get(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.DeleteSkillResponse{}), nil
}

// SyncSkillsFromDir scans a directory for .claude/skills/*/SKILL.md files and syncs them.
func (s *Server) SyncSkillsFromDir(ctx context.Context, req *connect.Request[taskguildv1.SyncSkillsFromDirRequest]) (*connect.Response[taskguildv1.SyncSkillsFromDirResponse], error) {
	dir := req.Msg.Directory
	if dir == "" {
		dir = "."
	}
	skillsDir := filepath.Join(dir, ".claude", "skills")

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return connect.NewResponse(&taskguildv1.SyncSkillsFromDirResponse{}), nil
		}
		return nil, fmt.Errorf("failed to read skills directory: %w", err)
	}

	var (
		synced  []*taskguildv1.SkillDefinition
		created int32
		updated int32
	)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillMDPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		parsed, err := parseSkillMDFile(skillMDPath, entry.Name())
		if err != nil {
			continue
		}

		// Try to find existing skill with same name in this project.
		existing, err := s.repo.FindByName(ctx, req.Msg.ProjectId, parsed.Name)
		if err == nil && existing != nil {
			// Update existing skill.
			existing.Description = parsed.Description
			existing.Content = parsed.Content
			existing.DisableModelInvocation = parsed.DisableModelInvocation
			existing.UserInvocable = parsed.UserInvocable
			existing.AllowedTools = parsed.AllowedTools
			existing.Model = parsed.Model
			existing.Context = parsed.Context
			existing.Agent = parsed.Agent
			existing.ArgumentHint = parsed.ArgumentHint
			existing.IsSynced = true
			existing.UpdatedAt = time.Now()
			if err := s.repo.Update(ctx, existing); err != nil {
				continue
			}
			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new skill.
			now := time.Now()
			sk := &Skill{
				ID:                     ulid.Make().String(),
				ProjectID:              req.Msg.ProjectId,
				Name:                   parsed.Name,
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
			if err := s.repo.Create(ctx, sk); err != nil {
				continue
			}
			synced = append(synced, toProto(sk))
			created++
		}
	}

	return connect.NewResponse(&taskguildv1.SyncSkillsFromDirResponse{
		Skills:  synced,
		Created: created,
		Updated: updated,
	}), nil
}

// parsedSkill holds data extracted from a .claude/skills/*/SKILL.md file.
type parsedSkill struct {
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

// parseSkillMDFile parses a Claude Code skill definition markdown file.
// Format: YAML frontmatter between --- delimiters, followed by the content body.
func parseSkillMDFile(filePath string, dirName string) (*parsedSkill, error) {
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

	result := &parsedSkill{
		Name:          dirName,
		UserInvocable: true, // Default is true per skill spec.
	}

	// Parse frontmatter as simple key: value pairs.
	// Also supports YAML list format (  - item) for list fields like allowed-tools.
	var currentListKey string
	for _, line := range frontmatterLines {
		// Check for YAML list item (e.g. "  - Read").
		trimmed := strings.TrimSpace(line)
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
			currentListKey = "" // Reset list context.

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
					parts := strings.Split(value, ",")
					for _, p := range parts {
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

	// The body is the skill content.
	body := strings.Join(bodyLines, "\n")
	body = strings.TrimSpace(body)
	result.Content = body

	return result, nil
}

func toProto(s *Skill) *taskguildv1.SkillDefinition {
	return &taskguildv1.SkillDefinition{
		Id:                     s.ID,
		ProjectId:              s.ProjectID,
		Name:                   s.Name,
		Description:            s.Description,
		Content:                s.Content,
		DisableModelInvocation: s.DisableModelInvocation,
		UserInvocable:          s.UserInvocable,
		AllowedTools:           s.AllowedTools,
		Model:                  s.Model,
		Context:                s.Context,
		Agent:                  s.Agent,
		ArgumentHint:           s.ArgumentHint,
		IsSynced:               s.IsSynced,
		CreatedAt:              timestamppb.New(s.CreatedAt),
		UpdatedAt:              timestamppb.New(s.UpdatedAt),
	}
}
