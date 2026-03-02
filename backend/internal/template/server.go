package template

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/backend/internal/agent"
	"github.com/kazz187/taskguild/backend/internal/script"
	"github.com/kazz187/taskguild/backend/internal/skill"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
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
func (s *Server) CreateTemplate(ctx context.Context, req *connect.Request[taskguildv1.CreateTemplateRequest]) (*connect.Response[taskguildv1.CreateTemplateResponse], error) {
	now := time.Now()
	t := &Template{
		ID:          ulid.Make().String(),
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
		EntityType:  req.Msg.EntityType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	switch req.Msg.EntityType {
	case "agent":
		if req.Msg.AgentConfig == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("agent_config is required for entity_type=agent"))
		}
		t.AgentConfig = agentConfigFromProto(req.Msg.AgentConfig)
	case "skill":
		if req.Msg.SkillConfig == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("skill_config is required for entity_type=skill"))
		}
		t.SkillConfig = skillConfigFromProto(req.Msg.SkillConfig)
	case "script":
		if req.Msg.ScriptConfig == nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("script_config is required for entity_type=script"))
		}
		t.ScriptConfig = scriptConfigFromProto(req.Msg.ScriptConfig)
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid entity_type: %s", req.Msg.EntityType))
	}

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.CreateTemplateResponse{
		Template: toProto(t),
	}), nil
}

// GetTemplate retrieves a single template by ID.
func (s *Server) GetTemplate(ctx context.Context, req *connect.Request[taskguildv1.GetTemplateRequest]) (*connect.Response[taskguildv1.GetTemplateResponse], error) {
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetTemplateResponse{
		Template: toProto(t),
	}), nil
}

// ListTemplates lists templates, optionally filtered by entity type.
func (s *Server) ListTemplates(ctx context.Context, req *connect.Request[taskguildv1.ListTemplatesRequest]) (*connect.Response[taskguildv1.ListTemplatesResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	templates, total, err := s.repo.List(ctx, req.Msg.EntityType, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.Template, len(templates))
	for i, t := range templates {
		protos[i] = toProto(t)
	}
	return connect.NewResponse(&taskguildv1.ListTemplatesResponse{
		Templates: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

// UpdateTemplate updates an existing template.
func (s *Server) UpdateTemplate(ctx context.Context, req *connect.Request[taskguildv1.UpdateTemplateRequest]) (*connect.Response[taskguildv1.UpdateTemplateResponse], error) {
	t, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}

	if req.Msg.Name != "" {
		t.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		t.Description = req.Msg.Description
	}

	switch t.EntityType {
	case "agent":
		if req.Msg.AgentConfig != nil {
			t.AgentConfig = agentConfigFromProto(req.Msg.AgentConfig)
		}
	case "skill":
		if req.Msg.SkillConfig != nil {
			t.SkillConfig = skillConfigFromProto(req.Msg.SkillConfig)
		}
	case "script":
		if req.Msg.ScriptConfig != nil {
			t.ScriptConfig = scriptConfigFromProto(req.Msg.ScriptConfig)
		}
	}

	t.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, t); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.UpdateTemplateResponse{
		Template: toProto(t),
	}), nil
}

// DeleteTemplate deletes a template by ID.
func (s *Server) DeleteTemplate(ctx context.Context, req *connect.Request[taskguildv1.DeleteTemplateRequest]) (*connect.Response[taskguildv1.DeleteTemplateResponse], error) {
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.DeleteTemplateResponse{}), nil
}

// SaveAsTemplate saves an existing entity as a reusable template.
// For agents, optionally includes referenced skills as dependent templates.
func (s *Server) SaveAsTemplate(ctx context.Context, req *connect.Request[taskguildv1.SaveAsTemplateRequest]) (*connect.Response[taskguildv1.SaveAsTemplateResponse], error) {
	now := time.Now()
	var mainTemplate *Template
	var dependentTemplates []*Template

	switch req.Msg.EntityType {
	case "agent":
		a, err := s.agentRepo.Get(ctx, req.Msg.EntityId)
		if err != nil {
			return nil, err
		}

		templateName := req.Msg.TemplateName
		if templateName == "" {
			templateName = a.Name
		}
		templateDesc := req.Msg.TemplateDescription
		if templateDesc == "" {
			templateDesc = a.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  "agent",
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
		if req.Msg.IncludeDependentSkills && len(a.Skills) > 0 {
			for _, skillName := range a.Skills {
				sk, err := s.skillRepo.FindByName(ctx, a.ProjectID, skillName)
				if err != nil {
					continue // Skip skills that are not found.
				}
				skillTmpl := &Template{
					ID:          ulid.Make().String(),
					Name:        sk.Name,
					Description: sk.Description,
					EntityType:  "skill",
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

	case "skill":
		sk, err := s.skillRepo.Get(ctx, req.Msg.EntityId)
		if err != nil {
			return nil, err
		}

		templateName := req.Msg.TemplateName
		if templateName == "" {
			templateName = sk.Name
		}
		templateDesc := req.Msg.TemplateDescription
		if templateDesc == "" {
			templateDesc = sk.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  "skill",
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

	case "script":
		sc, err := s.scriptRepo.Get(ctx, req.Msg.EntityId)
		if err != nil {
			return nil, err
		}

		templateName := req.Msg.TemplateName
		if templateName == "" {
			templateName = sc.Name
		}
		templateDesc := req.Msg.TemplateDescription
		if templateDesc == "" {
			templateDesc = sc.Description
		}

		mainTemplate = &Template{
			ID:          ulid.Make().String(),
			Name:        templateName,
			Description: templateDesc,
			EntityType:  "script",
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
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid entity_type: %s", req.Msg.EntityType))
	}

	if err := s.repo.Create(ctx, mainTemplate); err != nil {
		return nil, err
	}

	// Build response.
	depProtos := make([]*taskguildv1.Template, len(dependentTemplates))
	for i, dt := range dependentTemplates {
		depProtos[i] = toProto(dt)
	}

	return connect.NewResponse(&taskguildv1.SaveAsTemplateResponse{
		Template:           toProto(mainTemplate),
		DependentTemplates: depProtos,
	}), nil
}

// CreateFromTemplate instantiates a new entity in a project from a template.
// For agent templates, optionally creates dependent skills from their templates.
func (s *Server) CreateFromTemplate(ctx context.Context, req *connect.Request[taskguildv1.CreateFromTemplateRequest]) (*connect.Response[taskguildv1.CreateFromTemplateResponse], error) {
	tmpl, err := s.repo.Get(ctx, req.Msg.TemplateId)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var createdEntityID string
	var dependentSkillIDs []string
	var warnings []string

	switch tmpl.EntityType {
	case "agent":
		cfg := tmpl.AgentConfig
		if req.Msg.AgentConfig != nil {
			cfg = agentConfigFromProto(req.Msg.AgentConfig)
		}
		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("agent template has no config"))
		}

		a := &agent.Agent{
			ID:              ulid.Make().String(),
			ProjectID:       req.Msg.ProjectId,
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
		if err := s.agentRepo.Create(ctx, a); err != nil {
			return nil, err
		}
		createdEntityID = a.ID

		// Create dependent skills from templates.
		if req.Msg.CreateDependentSkills && len(cfg.Skills) > 0 {
			for _, skillName := range cfg.Skills {
				// Check if the skill already exists in the target project.
				_, err := s.skillRepo.FindByName(ctx, req.Msg.ProjectId, skillName)
				if err == nil {
					// Skill already exists; skip.
					continue
				}

				// Find the skill template by config name.
				skillTmpl, err := s.repo.FindByConfigName(ctx, "skill", skillName)
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
					ProjectID:              req.Msg.ProjectId,
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

	case "skill":
		cfg := tmpl.SkillConfig
		if req.Msg.SkillConfig != nil {
			cfg = skillConfigFromProto(req.Msg.SkillConfig)
		}
		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("skill template has no config"))
		}

		sk := &skill.Skill{
			ID:                     ulid.Make().String(),
			ProjectID:              req.Msg.ProjectId,
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
		if err := s.skillRepo.Create(ctx, sk); err != nil {
			return nil, err
		}
		createdEntityID = sk.ID

	case "script":
		cfg := tmpl.ScriptConfig
		if req.Msg.ScriptConfig != nil {
			cfg = scriptConfigFromProto(req.Msg.ScriptConfig)
		}
		if cfg == nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("script template has no config"))
		}

		sc := &script.Script{
			ID:          ulid.Make().String(),
			ProjectID:   req.Msg.ProjectId,
			Name:        cfg.Name,
			Description: cfg.Description,
			Filename:    cfg.Filename,
			Content:     cfg.Content,
			IsSynced:    false,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.scriptRepo.Create(ctx, sc); err != nil {
			return nil, err
		}
		createdEntityID = sc.ID

	default:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("unknown entity_type in template: %s", tmpl.EntityType))
	}

	return connect.NewResponse(&taskguildv1.CreateFromTemplateResponse{
		CreatedEntityId:    createdEntityID,
		EntityType:         tmpl.EntityType,
		DependentSkillIds:  dependentSkillIDs,
		Warnings:           warnings,
	}), nil
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
		Name:            p.Name,
		Description:     p.Description,
		Prompt:          p.Prompt,
		Tools:           p.Tools,
		DisallowedTools: p.DisallowedTools,
		Model:           p.Model,
		PermissionMode:  p.PermissionMode,
		Skills:          p.Skills,
		Memory:          p.Memory,
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
		Name:                   p.Name,
		Description:            p.Description,
		Content:                p.Content,
		DisableModelInvocation: p.DisableModelInvocation,
		UserInvocable:          p.UserInvocable,
		AllowedTools:           p.AllowedTools,
		Model:                  p.Model,
		Context:                p.Context,
		Agent:                  p.Agent,
		ArgumentHint:           p.ArgumentHint,
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
		Name:        p.Name,
		Description: p.Description,
		Filename:    p.Filename,
		Content:     p.Content,
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
