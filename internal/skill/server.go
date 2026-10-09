package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/claudemd"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.SkillServiceHandler = (*Server)(nil)

// ChangeNotifier is called after skill CRUD operations to notify connected
// agents that they should re-sync their local skill definitions.
// changedSkillIDs lists skills that were created or updated and should be
// force-overwritten on the agent side even if a local file already exists.
type ChangeNotifier interface {
	NotifySkillChange(projectID string, changedSkillIDs []string)
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

func (s *Server) notifyChange(projectID string, changedSkillIDs []string) {
	if s.notifier != nil {
		s.notifier.NotifySkillChange(projectID, changedSkillIDs)
	}
}

func (s *Server) CreateSkill(ctx context.Context, req *taskguildv1.CreateSkillRequest) (*taskguildv1.CreateSkillResponse, error) {
	now := time.Now()

	sk := &Skill{
		ID:                     ulid.Make().String(),
		ProjectID:              req.GetProjectId(),
		Name:                   req.GetName(),
		Description:            req.GetDescription(),
		Content:                req.GetContent(),
		DisableModelInvocation: req.GetDisableModelInvocation(),
		UserInvocable:          req.GetUserInvocable(),
		AllowedTools:           req.GetAllowedTools(),
		Model:                  req.GetModel(),
		Context:                req.GetContext(),
		Agent:                  req.GetAgent(),
		ArgumentHint:           req.GetArgumentHint(),
		IsSynced:               false,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	err := s.repo.Create(ctx, sk)
	if err != nil {
		return nil, err
	}

	s.notifyChange(sk.ProjectID, []string{sk.ID})

	return &taskguildv1.CreateSkillResponse{
		Skill: toProto(sk),
	}, nil
}

func (s *Server) GetSkill(ctx context.Context, req *taskguildv1.GetSkillRequest) (*taskguildv1.GetSkillResponse, error) {
	sk, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetSkillResponse{
		Skill: toProto(sk),
	}, nil
}

func (s *Server) ListSkills(ctx context.Context, req *taskguildv1.ListSkillsRequest) (*taskguildv1.ListSkillsResponse, error) {
	limit, offset := int32(50), int32(0)

	if req.GetPagination() != nil {
		if req.GetPagination().GetLimit() > 0 {
			limit = req.GetPagination().GetLimit()
		}

		offset = req.GetPagination().GetOffset()
	}

	skills, total, err := s.repo.List(ctx, req.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.SkillDefinition, len(skills))
	for i, sk := range skills {
		protos[i] = toProto(sk)
	}

	return &taskguildv1.ListSkillsResponse{
		Skills: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}, nil
}

func (s *Server) UpdateSkill(ctx context.Context, req *taskguildv1.UpdateSkillRequest) (*taskguildv1.UpdateSkillResponse, error) {
	sk, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if req.GetName() != "" {
		sk.Name = req.GetName()
	}

	if req.GetDescription() != "" {
		sk.Description = req.GetDescription()
	}

	if req.GetContent() != "" {
		sk.Content = req.GetContent()
	}
	// Boolean fields are always applied (proto3 default is false).
	sk.DisableModelInvocation = req.GetDisableModelInvocation()

	sk.UserInvocable = req.GetUserInvocable()
	if req.AllowedTools != nil {
		sk.AllowedTools = req.GetAllowedTools()
	}

	if req.GetModel() != "" {
		sk.Model = req.GetModel()
	}

	if req.GetContext() != "" {
		sk.Context = req.GetContext()
	}

	if req.GetAgent() != "" {
		sk.Agent = req.GetAgent()
	}

	if req.GetArgumentHint() != "" {
		sk.ArgumentHint = req.GetArgumentHint()
	}

	sk.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, sk); err != nil {
		return nil, err
	}

	s.notifyChange(sk.ProjectID, []string{sk.ID})

	return &taskguildv1.UpdateSkillResponse{
		Skill: toProto(sk),
	}, nil
}

func (s *Server) DeleteSkill(ctx context.Context, req *taskguildv1.DeleteSkillRequest) (*taskguildv1.DeleteSkillResponse, error) {
	sk, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if err := s.repo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}

	s.notifyChange(sk.ProjectID, nil)

	return &taskguildv1.DeleteSkillResponse{}, nil
}

// SyncSkillsFromDir scans a directory for .claude/skills/*/SKILL.md files and syncs them.
func (s *Server) SyncSkillsFromDir(ctx context.Context, req *taskguildv1.SyncSkillsFromDirRequest) (*taskguildv1.SyncSkillsFromDirResponse, error) {
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

	skillsDir := filepath.Join(dir, ".claude", "skills")

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &taskguildv1.SyncSkillsFromDirResponse{}, nil
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
		existing, err := s.repo.FindByName(ctx, req.GetProjectId(), parsed.Name)
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

			err := s.repo.Update(ctx, existing)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(existing))
			updated++
		} else {
			// Create new skill.
			now := time.Now()

			sk := &Skill{
				ID:                     ulid.Make().String(),
				ProjectID:              req.GetProjectId(),
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

			err := s.repo.Create(ctx, sk)
			if err != nil {
				continue
			}

			synced = append(synced, toProto(sk))
			created++
		}
	}

	return &taskguildv1.SyncSkillsFromDirResponse{
		Skills:  synced,
		Created: created,
		Updated: updated,
	}, nil
}

// parseSkillMDFile reads a Claude Code skill definition markdown file and
// parses its YAML frontmatter. dirName is used as the skill name when the
// frontmatter carries no name.
func parseSkillMDFile(filePath string, dirName string) (*claudemd.Skill, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	parsed := claudemd.ParseSkill(string(data))
	if parsed.Name == "" {
		parsed.Name = dirName
	}

	return parsed, nil
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
