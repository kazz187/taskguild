package project

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ProjectServiceHandler = (*Server)(nil)

type Server struct {
	repo   Repository
	seeder *Seeder
}

func NewServer(repo Repository, seeder *Seeder) *Server {
	return &Server{repo: repo, seeder: seeder}
}

func (s *Server) CreateProject(ctx context.Context, req *connect.Request[taskguildv1.CreateProjectRequest]) (*connect.Response[taskguildv1.CreateProjectResponse], error) {
	// Determine order for the new project (append to end).
	allProjects, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	maxOrder := int32(0)
	for _, ep := range allProjects {
		if ep.Order > maxOrder {
			maxOrder = ep.Order
		}
	}

	now := time.Now()
	p := &Project{
		ID:            ulid.Make().String(),
		Name:          req.Msg.GetName(),
		Description:   req.Msg.GetDescription(),
		RepositoryURL: req.Msg.GetRepositoryUrl(),
		DefaultBranch: req.Msg.GetDefaultBranch(),
		Order:         maxOrder + 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	// Seed default workflow, agents, and skills for the new project.
	if s.seeder != nil {
		if err := s.seeder.Seed(ctx, p.ID); err != nil {
			// Clean up the project if seeding fails.
			_ = s.repo.Delete(ctx, p.ID)
			return nil, err
		}
	}

	return connect.NewResponse(&taskguildv1.CreateProjectResponse{
		Project: toProto(p),
	}), nil
}

func (s *Server) GetProject(ctx context.Context, req *connect.Request[taskguildv1.GetProjectRequest]) (*connect.Response[taskguildv1.GetProjectResponse], error) {
	p, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.GetProjectResponse{
		Project: toProto(p),
	}), nil
}

func (s *Server) ListProjects(ctx context.Context, req *connect.Request[taskguildv1.ListProjectsRequest]) (*connect.Response[taskguildv1.ListProjectsResponse], error) {
	limit, offset := int32(50), int32(0)
	if req.Msg.GetPagination() != nil {
		if req.Msg.GetPagination().GetLimit() > 0 {
			limit = req.Msg.GetPagination().GetLimit()
		}
		offset = req.Msg.GetPagination().GetOffset()
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
	p, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}
	if req.Msg.GetName() != "" {
		p.Name = req.Msg.GetName()
	}
	if req.Msg.GetDescription() != "" {
		p.Description = req.Msg.GetDescription()
	}
	if req.Msg.GetRepositoryUrl() != "" {
		p.RepositoryURL = req.Msg.GetRepositoryUrl()
	}
	if req.Msg.GetDefaultBranch() != "" {
		p.DefaultBranch = req.Msg.GetDefaultBranch()
	}
	if req.Msg.HiddenFromSidebar != nil {
		p.HiddenFromSidebar = req.Msg.GetHiddenFromSidebar()
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
	if err := s.repo.Delete(ctx, req.Msg.GetId()); err != nil {
		return nil, err
	}
	return connect.NewResponse(&taskguildv1.DeleteProjectResponse{}), nil
}

func (s *Server) ReorderProjects(ctx context.Context, req *connect.Request[taskguildv1.ReorderProjectsRequest]) (*connect.Response[taskguildv1.ReorderProjectsResponse], error) {
	now := time.Now()
	for i, id := range req.Msg.GetProjectIds() {
		p, err := s.repo.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		p.Order = int32(i + 1)
		p.UpdatedAt = now
		if err := s.repo.Update(ctx, p); err != nil {
			return nil, err
		}
	}

	// Return updated project list in order.
	allProjects, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	protos := make([]*taskguildv1.Project, len(allProjects))
	for i, p := range allProjects {
		protos[i] = toProto(p)
	}
	return connect.NewResponse(&taskguildv1.ReorderProjectsResponse{
		Projects: protos,
	}), nil
}

func toProto(p *Project) *taskguildv1.Project {
	return &taskguildv1.Project{
		Id:                p.ID,
		Name:              p.Name,
		Description:       p.Description,
		RepositoryUrl:     p.RepositoryURL,
		DefaultBranch:     p.DefaultBranch,
		Order:             p.Order,
		HiddenFromSidebar: p.HiddenFromSidebar,
		CreatedAt:         timestamppb.New(p.CreatedAt),
		UpdatedAt:         timestamppb.New(p.UpdatedAt),
	}
}
