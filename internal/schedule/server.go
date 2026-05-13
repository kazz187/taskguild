package schedule

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/robfig/cron/v3"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kazz187/taskguild/internal/workflow"
	"github.com/kazz187/taskguild/pkg/cerr"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
	"github.com/kazz187/taskguild/proto/gen/go/taskguild/v1/taskguildv1connect"
)

var _ taskguildv1connect.ScheduleServiceHandler = (*Server)(nil)

// Scheduler defines the behavior that schedule.Server requires from the
// in-process scheduler. The concrete implementation lives in internal/scheduler.
type Scheduler interface {
	// Add registers (or replaces) the schedule with the underlying cron runner.
	// Must be safe to call concurrently. Implementations should be a no-op when
	// the schedule is disabled, but it is the caller's responsibility to not
	// register disabled schedules.
	Add(s *Schedule) error
	// Update is equivalent to Add for an already-registered schedule. It removes
	// the existing entry then re-registers using the new cron expression.
	Update(s *Schedule) error
	// Remove unregisters the schedule. No-op if it was not registered.
	Remove(id string)
	// NextRun returns the next fire time for the given cron expression, or zero
	// time if the expression is invalid.
	NextRun(expr string, from time.Time) time.Time
}

type Server struct {
	repo         Repository
	workflowRepo workflow.Repository
	scheduler    Scheduler
}

func NewServer(repo Repository, workflowRepo workflow.Repository, sched Scheduler) *Server {
	return &Server{
		repo:         repo,
		workflowRepo: workflowRepo,
		scheduler:    sched,
	}
}

// validateCronExpression parses the expression and returns the parsed schedule
// for next-run computation. Uses the standard 5-field parser.
func validateCronExpression(expr string) (cron.Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "cron_expression is required", nil).ConnectError()
	}

	parsed, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, cerr.NewError(cerr.InvalidArgument,
			fmt.Sprintf("invalid cron expression %q: %v", expr, err), nil).ConnectError()
	}

	return parsed, nil
}

// validateWorkflowAndStatus loads the workflow and verifies the requested
// status exists. If statusID is empty, it returns the workflow's initial
// status name.
func (s *Server) validateWorkflowAndStatus(ctx context.Context, workflowID, statusID string) (string, error) {
	if workflowID == "" {
		return "", cerr.NewError(cerr.InvalidArgument, "workflow_id is required", nil).ConnectError()
	}

	wf, err := s.workflowRepo.Get(ctx, workflowID)
	if err != nil {
		return "", err
	}

	if statusID != "" {
		for _, st := range wf.Statuses {
			if st.Name == statusID {
				return statusID, nil
			}
		}

		return "", cerr.NewError(cerr.InvalidArgument,
			fmt.Sprintf("status %q not found in workflow", statusID), nil).ConnectError()
	}

	for _, st := range wf.Statuses {
		if st.IsInitial {
			return st.Name, nil
		}
	}

	return "", cerr.NewError(cerr.FailedPrecondition, "workflow has no initial status", nil).ConnectError()
}

func (s *Server) CreateSchedule(ctx context.Context, req *connect.Request[taskguildv1.CreateScheduleRequest]) (*connect.Response[taskguildv1.CreateScheduleResponse], error) {
	if req.Msg.GetProjectId() == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "project_id is required", nil).ConnectError()
	}

	if strings.TrimSpace(req.Msg.GetName()) == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "name is required", nil).ConnectError()
	}

	if strings.TrimSpace(req.Msg.GetTaskTitle()) == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_title is required", nil).ConnectError()
	}

	parsedCron, err := validateCronExpression(req.Msg.GetCronExpression())
	if err != nil {
		return nil, err
	}

	statusID, err := s.validateWorkflowAndStatus(ctx, req.Msg.GetWorkflowId(), req.Msg.GetStatusId())
	if err != nil {
		return nil, err
	}

	now := time.Now()
	sched := &Schedule{
		ID:              ulid.Make().String(),
		ProjectID:       req.Msg.GetProjectId(),
		WorkflowID:      req.Msg.GetWorkflowId(),
		Name:            req.Msg.GetName(),
		Description:     req.Msg.GetDescription(),
		CronExpression:  strings.TrimSpace(req.Msg.GetCronExpression()),
		Enabled:         req.Msg.GetEnabled(),
		TaskTitle:       req.Msg.GetTaskTitle(),
		TaskDescription: req.Msg.GetTaskDescription(),
		StatusID:        statusID,
		UseWorktree:     req.Msg.GetUseWorktree(),
		Effort:          req.Msg.GetEffort(),
		TaskMetadata:    req.Msg.GetTaskMetadata(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if sched.Enabled {
		sched.NextRunAt = parsedCron.Next(now)
	}

	if err := s.repo.Create(ctx, sched); err != nil {
		return nil, err
	}

	if sched.Enabled {
		if err := s.scheduler.Add(sched); err != nil {
			// scheduler failure should not orphan the persisted record; surface as Internal
			return nil, cerr.NewError(cerr.Internal, "failed to register schedule with scheduler", err).ConnectError()
		}
	}

	return connect.NewResponse(&taskguildv1.CreateScheduleResponse{
		Schedule: toProto(sched),
	}), nil
}

func (s *Server) GetSchedule(ctx context.Context, req *connect.Request[taskguildv1.GetScheduleRequest]) (*connect.Response[taskguildv1.GetScheduleResponse], error) {
	sched, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	return connect.NewResponse(&taskguildv1.GetScheduleResponse{
		Schedule: toProto(sched),
	}), nil
}

func (s *Server) ListSchedules(ctx context.Context, req *connect.Request[taskguildv1.ListSchedulesRequest]) (*connect.Response[taskguildv1.ListSchedulesResponse], error) {
	limit, offset := int32(50), int32(0)

	if req.Msg.GetPagination() != nil {
		if req.Msg.GetPagination().GetLimit() > 0 {
			limit = req.Msg.GetPagination().GetLimit()
		}

		offset = req.Msg.GetPagination().GetOffset()
	}

	schedules, total, err := s.repo.List(ctx, req.Msg.GetProjectId(), int(limit), int(offset))
	if err != nil {
		return nil, err
	}

	protos := make([]*taskguildv1.Schedule, len(schedules))
	for i, sched := range schedules {
		protos[i] = toProto(sched)
	}

	return connect.NewResponse(&taskguildv1.ListSchedulesResponse{
		Schedules: protos,
		Pagination: &taskguildv1.PaginationResponse{
			Total:  int32(total),
			Limit:  limit,
			Offset: offset,
		},
	}), nil
}

func (s *Server) UpdateSchedule(ctx context.Context, req *connect.Request[taskguildv1.UpdateScheduleRequest]) (*connect.Response[taskguildv1.UpdateScheduleResponse], error) {
	sched, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(req.Msg.GetName()) == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "name is required", nil).ConnectError()
	}

	if strings.TrimSpace(req.Msg.GetTaskTitle()) == "" {
		return nil, cerr.NewError(cerr.InvalidArgument, "task_title is required", nil).ConnectError()
	}

	parsedCron, err := validateCronExpression(req.Msg.GetCronExpression())
	if err != nil {
		return nil, err
	}

	wfID := req.Msg.GetWorkflowId()
	if wfID == "" {
		wfID = sched.WorkflowID
	}

	statusID, err := s.validateWorkflowAndStatus(ctx, wfID, req.Msg.GetStatusId())
	if err != nil {
		return nil, err
	}

	sched.WorkflowID = wfID
	sched.Name = req.Msg.GetName()
	sched.Description = req.Msg.GetDescription()
	sched.CronExpression = strings.TrimSpace(req.Msg.GetCronExpression())
	sched.TaskTitle = req.Msg.GetTaskTitle()
	sched.TaskDescription = req.Msg.GetTaskDescription()
	sched.StatusID = statusID
	sched.Effort = req.Msg.GetEffort()
	sched.TaskMetadata = req.Msg.GetTaskMetadata()

	if req.Msg.UseWorktree != nil {
		sched.UseWorktree = req.Msg.GetUseWorktree()
	}

	sched.UpdatedAt = time.Now()

	if sched.Enabled {
		sched.NextRunAt = parsedCron.Next(sched.UpdatedAt)
	} else {
		sched.NextRunAt = time.Time{}
	}

	if err := s.repo.Update(ctx, sched); err != nil {
		return nil, err
	}

	if sched.Enabled {
		if err := s.scheduler.Update(sched); err != nil {
			return nil, cerr.NewError(cerr.Internal, "failed to update schedule in scheduler", err).ConnectError()
		}
	} else {
		s.scheduler.Remove(sched.ID)
	}

	return connect.NewResponse(&taskguildv1.UpdateScheduleResponse{
		Schedule: toProto(sched),
	}), nil
}

func (s *Server) DeleteSchedule(ctx context.Context, req *connect.Request[taskguildv1.DeleteScheduleRequest]) (*connect.Response[taskguildv1.DeleteScheduleResponse], error) {
	if err := s.repo.Delete(ctx, req.Msg.GetId()); err != nil {
		return nil, err
	}

	s.scheduler.Remove(req.Msg.GetId())

	return connect.NewResponse(&taskguildv1.DeleteScheduleResponse{}), nil
}

func (s *Server) SetScheduleEnabled(ctx context.Context, req *connect.Request[taskguildv1.SetScheduleEnabledRequest]) (*connect.Response[taskguildv1.SetScheduleEnabledResponse], error) {
	sched, err := s.repo.Get(ctx, req.Msg.GetId())
	if err != nil {
		return nil, err
	}

	sched.Enabled = req.Msg.GetEnabled()
	sched.UpdatedAt = time.Now()

	if sched.Enabled {
		sched.NextRunAt = s.scheduler.NextRun(sched.CronExpression, sched.UpdatedAt)
	} else {
		sched.NextRunAt = time.Time{}
	}

	if err := s.repo.Update(ctx, sched); err != nil {
		return nil, err
	}

	if sched.Enabled {
		if err := s.scheduler.Add(sched); err != nil {
			return nil, cerr.NewError(cerr.Internal, "failed to register schedule with scheduler", err).ConnectError()
		}
	} else {
		s.scheduler.Remove(sched.ID)
	}

	return connect.NewResponse(&taskguildv1.SetScheduleEnabledResponse{
		Schedule: toProto(sched),
	}), nil
}

func toProto(s *Schedule) *taskguildv1.Schedule {
	pb := &taskguildv1.Schedule{
		Id:              s.ID,
		ProjectId:       s.ProjectID,
		WorkflowId:      s.WorkflowID,
		Name:            s.Name,
		Description:     s.Description,
		CronExpression:  s.CronExpression,
		Enabled:         s.Enabled,
		TaskTitle:       s.TaskTitle,
		TaskDescription: s.TaskDescription,
		StatusId:        s.StatusID,
		UseWorktree:     s.UseWorktree,
		Effort:          s.Effort,
		TaskMetadata:    s.TaskMetadata,
		LastError:       s.LastError,
		CreatedAt:       timestamppb.New(s.CreatedAt),
		UpdatedAt:       timestamppb.New(s.UpdatedAt),
	}
	if !s.LastRunAt.IsZero() {
		pb.LastRunAt = timestamppb.New(s.LastRunAt)
	}

	if !s.NextRunAt.IsZero() {
		pb.NextRunAt = timestamppb.New(s.NextRunAt)
	}

	return pb
}
