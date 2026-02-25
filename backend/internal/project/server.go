package project

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1"
	"github.com/kazz187/taskguild/backend/gen/proto/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ProjectServiceHandler = (*Server)(nil)

type Server struct {
	repo Repository
}

func NewServer(repo Repository) *Server {
	return &Server{repo: repo}
}

func (s *Server) CreateProject(ctx context.Context, req *connect.Request[taskguildv1.CreateProjectRequest]) (*connect.Response[taskguildv1.CreateProjectResponse], error) {
	now := time.Now()
	p := &Project{
		ID:            ulid.Make().String(),
		Name:          req.Msg.Name,
		Description:   req.Msg.Description,
		RepositoryURL: req.Msg.RepositoryUrl,
		DefaultBranch: req.Msg.DefaultBranch,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.CreateProjectResponse{
		Project: toProto(p),
	}), nil
}

func (s *Server) GetProject(ctx context.Context, req *connect.Request[taskguildv1.GetProjectRequest]) (*connect.Response[taskguildv1.GetProjectResponse], error) {
	p, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetProjectResponse{
		Project: toProto(p),
	}), nil
}

func (s *Server) ListProjects(ctx context.Context, req *connect.Request[taskguildv1.ListProjectsRequest]) (*connect.Response[taskguildv1.ListProjectsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.Pagination != nil {
		if req.Msg.Pagination.Limit > 0 {
			limit = req.Msg.Pagination.Limit
		}
		offset = req.Msg.Pagination.Offset
	}
	projects, total, err := s.repo.List(ctx, int(limit), int(offset))
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.Project, len(projects))
	for i, p := range projects {
		protos[i] = toProto(p)
	}
	return connect.NewResponse(&taskguildv1.ListProjectsResponse{
		Projects: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateProject(ctx context.Context, req *connect.Request[taskguildv1.UpdateProjectRequest]) (*connect.Response[taskguildv1.UpdateProjectResponse], error) {
	p, err := s.repo.Get(ctx, req.Msg.Id)
	if err != nil {
		return nil, err
	}
	if req.Msg.Name != "" {
		p.Name = req.Msg.Name
	}
	if req.Msg.Description != "" {
		p.Description = req.Msg.Description
	}
	if req.Msg.RepositoryUrl != "" {
		p.RepositoryURL = req.Msg.RepositoryUrl
	}
	if req.Msg.DefaultBranch != "" {
		p.DefaultBranch = req.Msg.DefaultBranch
	}
	p.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.UpdateProjectResponse{
		Project: toProto(p),
	}), nil
}

func (s *Server) DeleteProject(ctx context.Context, req *connect.Request[taskguildv1.DeleteProjectRequest]) (*connect.Response[taskguildv1.DeleteProjectResponse], error) {
	if err := s.repo.Delete(ctx, req.Msg.Id); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.DeleteProjectResponse{}), nil
}

func toProto(p *Project) *taskguildv1.Project {
	return &taskguildv1.Project{
		Id:            p.ID,
		Name:          p.Name,
		Description:   p.Description,
		RepositoryUrl: p.RepositoryURL,
		DefaultBranch: p.DefaultBranch,
		CreatedAt:     timestamppb.New(p.CreatedAt),
		UpdatedAt:     timestamppb.New(p.UpdatedAt),
	}
}
