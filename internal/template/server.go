package template

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/agent"
	"github.com/kazz187/taskguild/internal/script"
	"github.com/kazz187/taskguild/internal/skill"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.TemplateServiceHandler = (*Server)(nil)

type Server struct {
	repo       Repository
	agentRepo  agent.Repository
	skillRepo  skill.Repository
	scriptRepo script.Repository
}

func NewServer(repo Repository, agentRepo agent.Repository, skillRepo skill.Repository, scriptRepo script.Repository) *Server {
	return &Server{
		repo:       repo,
		agentRepo:  agentRepo,
		skillRepo:  skillRepo,
		scriptRepo: scriptRepo,
	}
}

// CreateTemplate creates a new template with direct config input.
func (s *Server) CreateTemplate(ctx context.Context, req *taskguildv1.CreateTemplateRequest) (*taskguildv1.CreateTemplateResponse, error) {
	now := time.Now()
	t := &Template{
		ID:          ulid.Make().String(),
		Name:        req.GetName(),
		Description: req.GetDescription(),
		EntityType:  req.GetEntityType(),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	switch req.GetEntityType() {
	case EntityTypeAgent:
		if req.GetAgentConfig() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "agent_config is required for entity_type=agent")
		}

		t.AgentConfig = agentConfigFromProto(req.GetAgentConfig())
	case EntityTypeSkill:
		if req.GetSkillConfig() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "skill_config is required for entity_type=skill")
		}

		t.SkillConfig = skillConfigFromProto(req.GetSkillConfig())
	case EntityTypeScript:
		if req.GetScriptConfig() == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, "script_config is required for entity_type=script")
		}

		t.ScriptConfig = scriptConfigFromProto(req.GetScriptConfig())
	default:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "invalid entity_type: %s", req.GetEntityType())
	}

	err := s.repo.Create(ctx, t)
	if err != nil {
		return nil, err
	}

	return &taskguildv1.CreateTemplateResponse{
		Template: toProto(t),
	}, nil
}

// GetTemplate retrieves a single template by ID.
func (s *Server) GetTemplate(ctx context.Context, req *taskguildv1.GetTemplateRequest) (*taskguildv1.GetTemplateResponse, error) {
	t, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetTemplateResponse{
		Template: toProto(t),
	}, nil
}

// ListTemplates lists templates, optionally filtered by entity type.
func (s *Server) ListTemplates(ctx context.Context, req *taskguildv1.ListTemplatesRequest) (*taskguildv1.ListTemplatesResponse, error) {
	limit, offset := int32(50), int32(0)

	if req.GetPagination() != nil {
		if req.GetPagination().GetLimit() > 0 {
			limit = req.GetPagination().GetLimit()
		}

		offset = req.GetPagination().GetOffset()
	}

	templates, total, err := s.repo.List(ctx, req.GetEntityType(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.Template, len(templates))
	for i, t := range templates {
		protos[i] = toProto(t)
	}

	return &taskguildv1.ListTemplatesResponse{
		Templates: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}, nil
}

// UpdateTemplate updates an existing template.
func (s *Server) UpdateTemplate(ctx context.Context, req *taskguildv1.UpdateTemplateRequest) (*taskguildv1.UpdateTemplateResponse, error) {
	t, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if req.GetName() != "" {
		t.Name = req.GetName()
	}

	if req.GetDescription() != "" {
		t.Description = req.GetDescription()
	}

	switch t.EntityType {
	case EntityTypeAgent:
		if req.GetAgentConfig() != nil {
			t.AgentConfig = agentConfigFromProto(req.GetAgentConfig())
		}
	case EntityTypeSkill:
		if req.GetSkillConfig() != nil {
			t.SkillConfig = skillConfigFromProto(req.GetSkillConfig())
		}
	case EntityTypeScript:
		if req.GetScriptConfig() != nil {
			t.ScriptConfig = scriptConfigFromProto(req.GetScriptConfig())
		}
	}

	t.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}

	return &taskguildv1.UpdateTemplateResponse{
		Template: toProto(t),
	}, nil
}

// DeleteTemplate deletes a template by ID.
func (s *Server) DeleteTemplate(ctx context.Context, req *taskguildv1.DeleteTemplateRequest) (*taskguildv1.DeleteTemplateResponse, error) {
	err := s.repo.Delete(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.DeleteTemplateResponse{}, nil
}

// SaveAsTemplate saves an existing entity as a reusable template.
// For agents, optionally includes referenced skills as dependent templates.
func (s *Server) SaveAsTemplate(ctx context.Context, req *taskguildv1.SaveAsTemplateRequest) (*taskguildv1.SaveAsTemplateResponse, error) {
	now := time.Now()

	var (
		mainTemplate       *Template
		dependentTemplates []*Template
	)

	switch req.GetEntityType() {
	case EntityTypeAgent:
		a, err := s.agentRepo.Get(ctx, req.GetEntityId())
		if err != nil {
			return nil, err
		}

		templateName := req.GetTemplateName()
		if templateName == "" {
			templateName = a.Name
		}

		templateDesc := req.GetTemplateDescription()
		if templateDesc == "" {
			templateDesc = a.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  EntityTypeAgent,
			AgentConfig: &AgentConfig{
				Name:            a.Name,
				Description:     a.Description,
				Prompt:          a.Prompt,
				Tools:           a.Tools,
				DisallowedTools: a.DisallowedTools,
				Model:           a.Model,
				PermissionMode:  a.PermissionMode,
				Skills:          a.Skills,
				Memory:          a.Memory,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		// Save dependent skills as templates if requested.
		if req.GetIncludeDependentSkills() && len(a.Skills) > 0 {
			for _, skillName := range a.Skills {
				sk, err := s.skillRepo.FindByName(ctx, a.ProjectID, skillName)
				if err != nil {
					continue // Skip skills that are not found.
				}

				skillTmpl := &Template{
					ID:          ulid.Make().String(),
					Name:        sk.Name,
					Description: sk.Description,
					EntityType:  EntityTypeSkill,
					SkillConfig: &SkillConfig{
						Name:                   sk.Name,
						Description:            sk.Description,
						Content:                sk.Content,
						DisableModelInvocation: sk.DisableModelInvocation,
						UserInvocable:          sk.UserInvocable,
						AllowedTools:           sk.AllowedTools,
						Model:                  sk.Model,
						Context:                sk.Context,
						Agent:                  sk.Agent,
						ArgumentHint:           sk.ArgumentHint,
					},
					CreatedAt: now,
					UpdatedAt: now,
				}
				if err := s.repo.Create(ctx, skillTmpl); err != nil {
					continue
				}

				dependentTemplates = append(dependentTemplates, skillTmpl)
			}
		}

	case EntityTypeSkill:
		sk, err := s.skillRepo.Get(ctx, req.GetEntityId())
		if err != nil {
			return nil, err
		}

		templateName := req.GetTemplateName()
		if templateName == "" {
			templateName = sk.Name
		}

		templateDesc := req.GetTemplateDescription()
		if templateDesc == "" {
			templateDesc = sk.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  EntityTypeSkill,
			SkillConfig: &SkillConfig{
				Name:                   sk.Name,
				Description:            sk.Description,
				Content:                sk.Content,
				DisableModelInvocation: sk.DisableModelInvocation,
				UserInvocable:          sk.UserInvocable,
				AllowedTools:           sk.AllowedTools,
				Model:                  sk.Model,
				Context:                sk.Context,
				Agent:                  sk.Agent,
				ArgumentHint:           sk.ArgumentHint,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

	case EntityTypeScript:
		sc, err := s.scriptRepo.Get(ctx, req.GetEntityId())
		if err != nil {
			return nil, err
		}

		templateName := req.GetTemplateName()
		if templateName == "" {
			templateName = sc.Name
		}

		templateDesc := req.GetTemplateDescription()
		if templateDesc == "" {
			templateDesc = sc.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  EntityTypeScript,
			ScriptConfig: &ScriptConfig{
				Name:        sc.Name,
				Description: sc.Description,
				Filename:    sc.Filename,
				Content:     sc.Content,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

	default:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "invalid entity_type: %s", req.GetEntityType())
	}

	err := s.repo.Create(ctx, mainTemplate)
	if err != nil {
		return nil, err
	}

	// Build response.
	depProtos := make([]*taskguildv1.Template, len(dependentTemplates))
	for i, dt := range dependentTemplates {
		depProtos[i] = toProto(dt)
	}

	return &taskguildv1.SaveAsTemplateResponse{
		Template:           toProto(mainTemplate),
		DependentTemplates: depProtos,
	}, nil
}

// CreateFromTemplate instantiates a new entity in a project from a template.
// For agent templates, optionally creates dependent skills from their templates.
func (s *Server) CreateFromTemplate(ctx context.Context, req *taskguildv1.CreateFromTemplateRequest) (*taskguildv1.CreateFromTemplateResponse, error) {
	tmpl, err := s.repo.Get(ctx, req.GetTemplateId())
	if err != nil {
		return nil, err
	}

	now := time.Now()

	var (
		createdEntityID   string
		dependentSkillIDs []string
		warnings          []string
	)

	switch tmpl.EntityType {
	case EntityTypeAgent:
		cfg := tmpl.AgentConfig
		if req.GetAgentConfig() != nil {
			cfg = agentConfigFromProto(req.GetAgentConfig())
		}

		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, "agent template has no config")
		}

		a := &agent.Agent{
			ID:              ulid.Make().String(),
			ProjectID:       req.GetProjectId(),
			Name:            cfg.Name,
			Description:     cfg.Description,
			Prompt:          cfg.Prompt,
			Tools:           cfg.Tools,
			DisallowedTools: cfg.DisallowedTools,
			Model:           cfg.Model,
			PermissionMode:  cfg.PermissionMode,
			Skills:          cfg.Skills,
			Memory:          cfg.Memory,
			IsSynced:        false,
			CreatedAt:       now,
			UpdatedAt:       now,
		}

		err := s.agentRepo.Create(ctx, a)
		if err != nil {
			return nil, err
		}

		createdEntityID = a.ID

		// Create dependent skills from templates.
		if req.GetCreateDependentSkills() && len(cfg.Skills) > 0 {
			for _, skillName := range cfg.Skills {
				// Check if the skill already exists in the target project.
				_, err := s.skillRepo.FindByName(ctx, req.GetProjectId(), skillName)
				if err == nil {
					// Skill already exists; skip.
					continue
				}

				// Find the skill template by config name.
				skillTmpl, err := s.repo.FindByConfigName(ctx, EntityTypeSkill, skillName)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("Skill '%s' template not found", skillName))
					continue
				}

				if skillTmpl.SkillConfig == nil {
					warnings = append(warnings, fmt.Sprintf("Skill '%s' template has no config", skillName))
					continue
				}

				sc := skillTmpl.SkillConfig

				sk := &skill.Skill{
					ID:                     ulid.Make().String(),
					ProjectID:              req.GetProjectId(),
					Name:                   sc.Name,
					Description:            sc.Description,
					Content:                sc.Content,
					DisableModelInvocation: sc.DisableModelInvocation,
					UserInvocable:          sc.UserInvocable,
					AllowedTools:           sc.AllowedTools,
					Model:                  sc.Model,
					Context:                sc.Context,
					Agent:                  sc.Agent,
					ArgumentHint:           sc.ArgumentHint,
					IsSynced:               false,
					CreatedAt:              now,
					UpdatedAt:              now,
				}
				if err := s.skillRepo.Create(ctx, sk); err != nil {
					warnings = append(warnings, fmt.Sprintf("Failed to create skill '%s': %v", skillName, err))
					continue
				}

				dependentSkillIDs = append(dependentSkillIDs, sk.ID)
			}
		}

	case EntityTypeSkill:
		cfg := tmpl.SkillConfig
		if req.GetSkillConfig() != nil {
			cfg = skillConfigFromProto(req.GetSkillConfig())
		}

		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, "skill template has no config")
		}

		sk := &skill.Skill{
			ID:                     ulid.Make().String(),
			ProjectID:              req.GetProjectId(),
			Name:                   cfg.Name,
			Description:            cfg.Description,
			Content:                cfg.Content,
			DisableModelInvocation: cfg.DisableModelInvocation,
			UserInvocable:          cfg.UserInvocable,
			AllowedTools:           cfg.AllowedTools,
			Model:                  cfg.Model,
			Context:                cfg.Context,
			Agent:                  cfg.Agent,
			ArgumentHint:           cfg.ArgumentHint,
			IsSynced:               false,
			CreatedAt:              now,
			UpdatedAt:              now,
		}

		err := s.skillRepo.Create(ctx, sk)
		if err != nil {
			return nil, err
		}

		createdEntityID = sk.ID

	case EntityTypeScript:
		cfg := tmpl.ScriptConfig
		if req.GetScriptConfig() != nil {
			cfg = scriptConfigFromProto(req.GetScriptConfig())
		}

		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, "script template has no config")
		}

		sc := &script.Script{
			ID:          ulid.Make().String(),
			ProjectID:   req.GetProjectId(),
			Name:        cfg.Name,
			Description: cfg.Description,
			Filename:    cfg.Filename,
			Content:     cfg.Content,
			IsSynced:    false,
			CreatedAt:   now,
			UpdatedAt:   now,
		}

		err := s.scriptRepo.Create(ctx, sc)
		if err != nil {
			return nil, err
		}

		createdEntityID = sc.ID

	default:
		return nil, connect.Errorf(connect.CodeInternal, "unknown entity_type in template: %s", tmpl.EntityType)
	}

	return &taskguildv1.CreateFromTemplateResponse{
		CreatedEntityId:   createdEntityID,
		EntityType:        tmpl.EntityType,
		DependentSkillIds: dependentSkillIDs,
		Warnings:          warnings,
	}, nil
}

// --- Proto conversion helpers ---

func toProto(t *Template) *taskguildv1.Template {
	p := &taskguildv1.Template{
		Id:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		EntityType:  t.EntityType,
		CreatedAt:   timestamppb.New(t.CreatedAt),
		UpdatedAt:   timestamppb.New(t.UpdatedAt),
	}
	if t.AgentConfig != nil {
		p.AgentConfig = agentConfigToProto(t.AgentConfig)
	}

	if t.SkillConfig != nil {
		p.SkillConfig = skillConfigToProto(t.SkillConfig)
	}

	if t.ScriptConfig != nil {
		p.ScriptConfig = scriptConfigToProto(t.ScriptConfig)
	}

	return p
}

func agentConfigFromProto(p *taskguildv1.AgentTemplateConfig) *AgentConfig {
	if p == nil {
		return nil
	}

	return &AgentConfig{
		Name:            p.GetName(),
		Description:     p.GetDescription(),
		Prompt:          p.GetPrompt(),
		Tools:           p.GetTools(),
		DisallowedTools: p.GetDisallowedTools(),
		Model:           p.GetModel(),
		PermissionMode:  p.GetPermissionMode(),
		Skills:          p.GetSkills(),
		Memory:          p.GetMemory(),
	}
}

func agentConfigToProto(c *AgentConfig) *taskguildv1.AgentTemplateConfig {
	if c == nil {
		return nil
	}

	return &taskguildv1.AgentTemplateConfig{
		Name:            c.Name,
		Description:     c.Description,
		Prompt:          c.Prompt,
		Tools:           c.Tools,
		DisallowedTools: c.DisallowedTools,
		Model:           c.Model,
		PermissionMode:  c.PermissionMode,
		Skills:          c.Skills,
		Memory:          c.Memory,
	}
}

func skillConfigFromProto(p *taskguildv1.SkillTemplateConfig) *SkillConfig {
	if p == nil {
		return nil
	}

	return &SkillConfig{
		Name:                   p.GetName(),
		Description:            p.GetDescription(),
		Content:                p.GetContent(),
		DisableModelInvocation: p.GetDisableModelInvocation(),
		UserInvocable:          p.GetUserInvocable(),
		AllowedTools:           p.GetAllowedTools(),
		Model:                  p.GetModel(),
		Context:                p.GetContext(),
		Agent:                  p.GetAgent(),
		ArgumentHint:           p.GetArgumentHint(),
	}
}

func skillConfigToProto(c *SkillConfig) *taskguildv1.SkillTemplateConfig {
	if c == nil {
		return nil
	}

	return &taskguildv1.SkillTemplateConfig{
		Name:                   c.Name,
		Description:            c.Description,
		Content:                c.Content,
		DisableModelInvocation: c.DisableModelInvocation,
		UserInvocable:          c.UserInvocable,
		AllowedTools:           c.AllowedTools,
		Model:                  c.Model,
		Context:                c.Context,
		Agent:                  c.Agent,
		ArgumentHint:           c.ArgumentHint,
	}
}

func scriptConfigFromProto(p *taskguildv1.ScriptTemplateConfig) *ScriptConfig {
	if p == nil {
		return nil
	}

	return &ScriptConfig{
		Name:        p.GetName(),
		Description: p.GetDescription(),
		Filename:    p.GetFilename(),
		Content:     p.GetContent(),
	}
}

func scriptConfigToProto(c *ScriptConfig) *taskguildv1.ScriptTemplateConfig {
	if c == nil {
		return nil
	}

	return &taskguildv1.ScriptTemplateConfig{
		Name:        c.Name,
		Description: c.Description,
		Filename:    c.Filename,
		Content:     c.Content,
	}
}
