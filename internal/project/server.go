package project

import (
	"context"
	"time"

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

func (s *Server) CreateProject(ctx context.Context, req *taskguildv1.CreateProjectRequest) (*taskguildv1.CreateProjectResponse, error) {
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
		Name:          req.GetName(),
		Description:   req.GetDescription(),
		RepositoryURL: req.GetRepositoryUrl(),
		DefaultBranch: req.GetDefaultBranch(),
		Order:         maxOrder + 1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	// Seed default workflow, agents, and skills for the new project.
	if s.seeder != nil {
		err := s.seeder.Seed(ctx, p.ID)
		if err != nil {
			// Clean up the project if seeding fails.
			_ = s.repo.Delete(ctx, p.ID)
			return nil, err
		}
	}

	return &taskguildv1.CreateProjectResponse{
		Project: toProto(p),
	}, nil
}

func (s *Server) GetProject(ctx context.Context, req *taskguildv1.GetProjectRequest) (*taskguildv1.GetProjectResponse, error) {
	p, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.GetProjectResponse{
		Project: toProto(p),
	}, nil
}

func (s *Server) ListProjects(ctx context.Context, req *taskguildv1.ListProjectsRequest) (*taskguildv1.ListProjectsResponse, error) {
	limit, offset := int32(50), int32(0)

	if req.GetPagination() != nil {
		if req.GetPagination().GetLimit() > 0 {
			limit = req.GetPagination().GetLimit()
		}

		offset = req.GetPagination().GetOffset()
	}

	projects, total, err := s.repo.List(ctx, int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.Project, len(projects))
	for i, p := range projects {
		protos[i] = toProto(p)
	}

	return &taskguildv1.ListProjectsResponse{
		Projects: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}, nil
}

func (s *Server) UpdateProject(ctx context.Context, req *taskguildv1.UpdateProjectRequest) (*taskguildv1.UpdateProjectResponse, error) {
	p, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	if req.GetName() != "" {
		p.Name = req.GetName()
	}

	if req.GetDescription() != "" {
		p.Description = req.GetDescription()
	}

	if req.GetRepositoryUrl() != "" {
		p.RepositoryURL = req.GetRepositoryUrl()
	}

	if req.GetDefaultBranch() != "" {
		p.DefaultBranch = req.GetDefaultBranch()
	}

	if req.HiddenFromSidebar != nil {
		p.HiddenFromSidebar = req.GetHiddenFromSidebar()
	}

	p.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}

	return &taskguildv1.UpdateProjectResponse{
		Project: toProto(p),
	}, nil
}

func (s *Server) DeleteProject(ctx context.Context, req *taskguildv1.DeleteProjectRequest) (*taskguildv1.DeleteProjectResponse, error) {
	err := s.repo.Delete(ctx, req.GetId())
	if err != nil {
		return nil, err
	}

	return &taskguildv1.DeleteProjectResponse{}, nil
}

func (s *Server) ReorderProjects(ctx context.Context, req *taskguildv1.ReorderProjectsRequest) (*taskguildv1.ReorderProjectsResponse, error) {
	now := time.Now()

	for i, id := range req.GetProjectIds() {
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

	return &taskguildv1.ReorderProjectsResponse{
		Projects: protos,
	}, nil
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
