package workflow

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

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
		ProjectID:             req.Msg.ProjectId,
		Name:                  req.Msg.Name,
		Description:           req.Msg.Description,
		DefaultPermissionMode: req.Msg.DefaultPermissionMode,
		DefaultUseWorktree:    req.Msg.DefaultUseWorktree,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	for _, ps := range req.Msg.Statuses {
		w.Statuses = append(w.Statuses, statusFromProto(ps))
	}
	for _, pa := range req.Msg.AgentConfigs {
		w.AgentConfigs = append(w.AgentConfigs, agentConfigFromProto(pa))
	}
	if err := s.repo.Create(ctx, w); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.CreateWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) GetWorkflow(ctx context.Context, req *connect.Request[taskguildv1.GetWorkflowRequest]) (*connect.Response[taskguildv1.GetWorkflowResponse], error) {
	w, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) ListWorkflows(ctx context.Context, req *connect.Request[taskguildv1.ListWorkflowsRequest]) (*connect.Response[taskguildv1.ListWorkflowsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	workflows, total, err := s.repo.List(ctx, req.Msg.ProjectId, int(limit), int(offset))
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
	w, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Name != "" {
		w.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		w.Description = req.Msg.Description
	}
	if req.Msg.Statuses != nil {
		w.Statuses = nil
		for _, ps := range req.Msg.Statuses {
			w.Statuses = append(w.Statuses, statusFromProto(ps))
		}
	}
	if req.Msg.AgentConfigs != nil {
		w.AgentConfigs = nil
		for _, pa := range req.Msg.AgentConfigs {
			w.AgentConfigs = append(w.AgentConfigs, agentConfigFromProto(pa))
		}
	}
	// Task defaults: always overwrite (empty string is a valid value for permission mode)
	w.DefaultPermissionMode = req.Msg.DefaultPermissionMode
	w.DefaultUseWorktree = req.Msg.DefaultUseWorktree
	w.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, w); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.UpdateWorkflowResponse{
		Workflow: toProto(w),
	}), nil
}

func (s *Server) DeleteWorkflow(ctx context.Context, req *connect.Request[taskguildv1.DeleteWorkflowRequest]) (*connect.Response[taskguildv1.DeleteWorkflowResponse], error) {
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
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
		Id:            s.ID,
		Name:          s.Name,
		Order:         s.Order,
		IsInitial:     s.IsInitial,
		IsTerminal:    s.IsTerminal,
		TransitionsTo: s.TransitionsTo,
		AgentId:       s.AgentID,
	}
	for _, h := range s.Hooks {
		pb.Hooks = append(pb.Hooks, hookToProto(h))
	}
	return pb
}

func hookToProto(h StatusHook) *taskguildv1.StatusHook {
	return &taskguildv1.StatusHook{
		Id:      h.ID,
		SkillId: h.SkillID,
		Trigger: hookTriggerToProto(h.Trigger),
		Order:   h.Order,
		Name:    h.Name,
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

func statusFromProto(ps *taskguildv1.WorkflowStatus) Status {
	id := ps.Id
	if id == "" {
		id = ulid.Make().String()
	}
	s := Status{
		ID:            id,
		Name:          ps.Name,
		Order:         ps.Order,
		IsInitial:     ps.IsInitial,
		IsTerminal:    ps.IsTerminal,
		TransitionsTo: ps.TransitionsTo,
		AgentID:       ps.AgentId,
	}
	for _, ph := range ps.Hooks {
		s.Hooks = append(s.Hooks, hookFromProto(ph))
	}
	return s
}

func hookFromProto(ph *taskguildv1.StatusHook) StatusHook {
	id := ph.Id
	if id == "" {
		id = ulid.Make().String()
	}
	return StatusHook{
		ID:      id,
		SkillID: ph.SkillId,
		Trigger: hookTriggerFromProto(ph.Trigger),
		Order:   ph.Order,
		Name:    ph.Name,
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
	default:
		return HookTriggerUnspecified
	}
}

func agentConfigFromProto(pa *taskguildv1.AgentConfig) AgentConfig {
	id := pa.Id
	if id == "" {
		id = ulid.Make().String()
	}
	return AgentConfig{
		ID:               id,
		WorkflowStatusID: pa.WorkflowStatusId,
		Name:             pa.Name,
		Description:      pa.Description,
		Instructions:     pa.Instructions,
		AllowedTools:     pa.AllowedTools,
	}
}
