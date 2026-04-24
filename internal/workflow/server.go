package workflow

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var alphanumericRe = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

var _ taskguildv1connect.WorkflowServiceHandler = (*Server)(nil)

type Server struct {
	repo Repository
}

func NewServer(repo Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) CreateWorkflow(ctx context.Context, req *connect.Request[taskguildv1.CreateWorkflowRequest]) (*connect.Response[taskguildv1.CreateWorkflowResponse], error) {
	now := time.Now()

	w := &Workflow{
		ID:                    ulid.Make().String(),
		ProjectID:             req.Msg.GetProjectId(),
		Name:                  req.Msg.GetName(),
		Description:           req.Msg.GetDescription(),
		DefaultPermissionMode: req.Msg.GetDefaultPermissionMode(),
		DefaultUseWorktree:    req.Msg.GetDefaultUseWorktree(),
		CustomPrompt:          req.Msg.GetCustomPrompt(),
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	err := validateStatuses(req.Msg.GetStatuses())
	if err != nil {
		return nil, err
	}

	for _, ps := range req.Msg.GetStatuses() {
		w.Statuses = append(w.Statuses, statusFromProto(ps))
	}

	for _, pa := range req.Msg.GetAgentConfigs() {
		w.AgentConfigs = append(w.AgentConfigs, agentConfigFromProto(pa))
	}

	if err = s.repo.Create(ctx, w); err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.CreateWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) GetWorkflow(ctx context.Context, req *connect.Request[taskguildv1.GetWorkflowRequest]) (*connect.Response[taskguildv1.GetWorkflowResponse], error) {
	w, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.GetWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) ListWorkflows(ctx context.Context, req *connect.Request[taskguildv1.ListWorkflowsRequest]) (*connect.Response[taskguildv1.ListWorkflowsResponse], error) {
	limit, offset := int32(50), int32(0)

	if req.Msg.GetPagination() != nil {
		if req.Msg.GetPagination().GetLimit() > 0 {
			limit = req.Msg.GetPagination().GetLimit()
		}

		offset = req.Msg.GetPagination().GetOffset()
	}

	workflows, total, err := s.repo.List(ctx, req.Msg.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.Workflow, len(workflows))
	for i, w := range workflows {
		protos[i] = toProto(w)
	}

	return connect.NewResponse(&taskguildv1.ListWorkflowsResponse{
		Workflows: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateWorkflow(ctx context.Context, req *connect.Request[taskguildv1.UpdateWorkflowRequest]) (*connect.Response[taskguildv1.UpdateWorkflowResponse], error) {
	w, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	if req.Msg.GetName() != "" {
		w.Name = req.Msg.GetName()
	}

	if req.Msg.GetDescription() != "" {
		w.Description = req.Msg.GetDescription()
	}

	if req.Msg.Statuses != nil {
		err := validateStatuses(req.Msg.GetStatuses())
		if err != nil {
			return nil, err
		}

		w.Statuses = nil
		for _, ps := range req.Msg.GetStatuses() {
			w.Statuses = append(w.Statuses, statusFromProto(ps))
		}
	}

	if req.Msg.AgentConfigs != nil {
		w.AgentConfigs = nil
		for _, pa := range req.Msg.GetAgentConfigs() {
			w.AgentConfigs = append(w.AgentConfigs, agentConfigFromProto(pa))
		}
	}
	// Task defaults: always overwrite (empty string is a valid value for permission mode)
	w.DefaultPermissionMode = req.Msg.GetDefaultPermissionMode()
	w.DefaultUseWorktree = req.Msg.GetDefaultUseWorktree()
	w.CustomPrompt = req.Msg.GetCustomPrompt()

	w.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, w); err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.UpdateWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) DeleteWorkflow(ctx context.Context, req *connect.Request[taskguildv1.DeleteWorkflowRequest]) (*connect.Response[taskguildv1.DeleteWorkflowResponse], error) {
	err := s.repo.Delete(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.DeleteWorkflowResponse{}), nil
}

func toProto(w *Workflow) *taskguildv1.Workflow {
	pb := &taskguildv1.Workflow{
		Id:                    w.ID,
		ProjectId:             w.ProjectID,
		Name:                  w.Name,
		Description:           w.Description,
		DefaultPermissionMode: w.DefaultPermissionMode,
		DefaultUseWorktree:    w.DefaultUseWorktree,
		CustomPrompt:          w.CustomPrompt,
		CreatedAt:             timestamppb.New(w.CreatedAt),
		UpdatedAt:             timestamppb.New(w.UpdatedAt),
	}
	for _, st := range w.Statuses {
		pb.Statuses = append(pb.Statuses, statusToProto(st))
	}

	for _, ac := range w.AgentConfigs {
		pb.AgentConfigs = append(pb.AgentConfigs, agentConfigToProto(ac))
	}

	return pb
}

func statusToProto(s Status) *taskguildv1.WorkflowStatus {
	pb := &taskguildv1.WorkflowStatus{
		Id:                             s.Name, // Deprecated: populated with Name for backward compat
		Name:                           s.Name,
		Order:                          s.Order,
		IsInitial:                      s.IsInitial,
		IsTerminal:                     s.IsTerminal,
		TransitionsTo:                  s.TransitionsTo,
		AgentId:                        s.AgentID,
		PermissionMode:                 s.PermissionMode,
		InheritSessionFrom:             s.InheritSessionFrom,
		Model:                          s.Model,
		Tools:                          s.Tools,
		DisallowedTools:                s.DisallowedTools,
		SkillIds:                       s.SkillIDs,
		EnableSkillHarness:             s.EnableSkillHarness,
		SkillHarnessExplicitlyDisabled: s.SkillHarnessExplicitlyDisabled,
		Effort:                         s.Effort,
	}
	for _, h := range s.Hooks {
		pb.Hooks = append(pb.Hooks, hookToProto(h))
	}

	return pb
}

func hookToProto(h StatusHook) *taskguildv1.StatusHook {
	return &taskguildv1.StatusHook{
		Id:         h.ID,
		SkillId:    h.SkillID,
		Trigger:    hookTriggerToProto(h.Trigger),
		Order:      h.Order,
		Name:       h.Name,
		ActionType: hookActionTypeToProto(h.ActionType),
		ActionId:   h.ActionID,
	}
}

func hookTriggerToProto(t HookTrigger) taskguildv1.HookTrigger {
	switch t {
	case HookTriggerBeforeTaskExecution:
		return taskguildv1.HookTrigger_HOOK_TRIGGER_BEFORE_TASK_EXECUTION
	case HookTriggerAfterTaskExecution:
		return taskguildv1.HookTrigger_HOOK_TRIGGER_AFTER_TASK_EXECUTION
	case HookTriggerAfterWorktreeCreation:
		return taskguildv1.HookTrigger_HOOK_TRIGGER_AFTER_WORKTREE_CREATION
	case HookTriggerBeforeWorktreeCreation:
		return taskguildv1.HookTrigger_HOOK_TRIGGER_BEFORE_WORKTREE_CREATION
	default:
		return taskguildv1.HookTrigger_HOOK_TRIGGER_UNSPECIFIED
	}
}

func agentConfigToProto(a AgentConfig) *taskguildv1.AgentConfig {
	return &taskguildv1.AgentConfig{
		Id:               a.ID,
		WorkflowStatusId: a.WorkflowStatusID,
		Name:             a.Name,
		Description:      a.Description,
		Instructions:     a.Instructions,
		AllowedTools:     a.AllowedTools,
	}
}

func validateStatuses(statuses []*taskguildv1.WorkflowStatus) error {
	seen := make(map[string]bool)

	for _, s := range statuses {
		name := s.GetName()
		if !alphanumericRe.MatchString(name) {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("status name %q must be alphanumeric", name))
		}

		if seen[name] {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("duplicate status name %q", name))
		}

		seen[name] = true
	}

	return nil
}

func statusFromProto(ps *taskguildv1.WorkflowStatus) Status {
	s := Status{
		Name:                           ps.GetName(),
		Order:                          ps.GetOrder(),
		IsInitial:                      ps.GetIsInitial(),
		IsTerminal:                     ps.GetIsTerminal(),
		TransitionsTo:                  ps.GetTransitionsTo(),
		AgentID:                        ps.GetAgentId(),
		PermissionMode:                 ps.GetPermissionMode(),
		InheritSessionFrom:             ps.GetInheritSessionFrom(),
		Model:                          ps.GetModel(),
		Tools:                          ps.GetTools(),
		DisallowedTools:                ps.GetDisallowedTools(),
		SkillIDs:                       ps.GetSkillIds(),
		EnableSkillHarness:             ps.GetEnableSkillHarness(),
		SkillHarnessExplicitlyDisabled: ps.GetSkillHarnessExplicitlyDisabled(),
		Effort:                         ps.GetEffort(),
	}
	for _, ph := range ps.GetHooks() {
		s.Hooks = append(s.Hooks, hookFromProto(ph))
	}

	return s
}

func hookFromProto(ph *taskguildv1.StatusHook) StatusHook {
	id := ph.GetId()
	if id == "" {
		id = ulid.Make().String()
	}

	return StatusHook{
		ID:         id,
		SkillID:    ph.GetSkillId(),
		Trigger:    hookTriggerFromProto(ph.GetTrigger()),
		Order:      ph.GetOrder(),
		Name:       ph.GetName(),
		ActionType: hookActionTypeFromProto(ph.GetActionType()),
		ActionID:   ph.GetActionId(),
	}
}

func hookActionTypeToProto(t HookActionType) taskguildv1.HookActionType {
	switch t {
	case HookActionTypeSkill:
		return taskguildv1.HookActionType_HOOK_ACTION_TYPE_SKILL
	case HookActionTypeScript:
		return taskguildv1.HookActionType_HOOK_ACTION_TYPE_SCRIPT
	default:
		return taskguildv1.HookActionType_HOOK_ACTION_TYPE_UNSPECIFIED
	}
}

func hookActionTypeFromProto(t taskguildv1.HookActionType) HookActionType {
	switch t {
	case taskguildv1.HookActionType_HOOK_ACTION_TYPE_SKILL:
		return HookActionTypeSkill
	case taskguildv1.HookActionType_HOOK_ACTION_TYPE_SCRIPT:
		return HookActionTypeScript
	default:
		return HookActionTypeUnspecified
	}
}

func hookTriggerFromProto(t taskguildv1.HookTrigger) HookTrigger {
	switch t {
	case taskguildv1.HookTrigger_HOOK_TRIGGER_BEFORE_TASK_EXECUTION:
		return HookTriggerBeforeTaskExecution
	case taskguildv1.HookTrigger_HOOK_TRIGGER_AFTER_TASK_EXECUTION:
		return HookTriggerAfterTaskExecution
	case taskguildv1.HookTrigger_HOOK_TRIGGER_AFTER_WORKTREE_CREATION:
		return HookTriggerAfterWorktreeCreation
	case taskguildv1.HookTrigger_HOOK_TRIGGER_BEFORE_WORKTREE_CREATION:
		return HookTriggerBeforeWorktreeCreation
	default:
		return HookTriggerUnspecified
	}
}

func agentConfigFromProto(pa *taskguildv1.AgentConfig) AgentConfig {
	id := pa.GetId()
	if id == "" {
		id = ulid.Make().String()
	}

	return AgentConfig{
		ID:               id,
		WorkflowStatusID: pa.GetWorkflowStatusId(),
		Name:             pa.GetName(),
		Description:      pa.GetDescription(),
		Instructions:     pa.GetInstructions(),
		AllowedTools:     pa.GetAllowedTools(),
	}
}
