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
		ID:          ulid.Make().String(),
		ProjectID:   req.Msg.ProjectId,
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
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
		Id:          w.ID,
		ProjectId:   w.ProjectID,
		Name:        w.Name,
		Description: w.Description,
		CreatedAt:   timestamppb.New(w.CreatedAt),
		UpdatedAt:   timestamppb.New(w.UpdatedAt),
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
	return &taskguildv1.WorkflowStatus{
		Id:            s.ID,
		Name:          s.Name,
		Order:         s.Order,
		IsInitial:     s.IsInitial,
		IsTerminal:    s.IsTerminal,
		TransitionsTo: s.TransitionsTo,
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
		UseWorktree:      a.UseWorktree,
		PermissionMode:   a.PermissionMode,
	}
}

func statusFromProto(ps *taskguildv1.WorkflowStatus) Status {
	id := ps.Id
	if id == "" {
		id = ulid.Make().String()
	}
	return Status{
		ID:            id,
		Name:          ps.Name,
		Order:         ps.Order,
		IsInitial:     ps.IsInitial,
		IsTerminal:    ps.IsTerminal,
		TransitionsTo: ps.TransitionsTo,
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
		UseWorktree:      pa.UseWorktree,
		PermissionMode:   pa.PermissionMode,
	}
}
