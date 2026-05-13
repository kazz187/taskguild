package schedule_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/kazz187/taskguild/internal/schedule"
	"github.com/kazz187/taskguild/internal/workflow"
	taskguildv1 "github.com/kazz187/taskguild/proto/gen/go/taskguild/v1"
)

// memRepo is an in-memory schedule.Repository for tests.
type memRepo struct {
	mu sync.Mutex
	m  map[string]*schedule.Schedule
}

func newMemRepo() *memRepo { return &memRepo{m: make(map[string]*schedule.Schedule)} }

func (r *memRepo) Create(_ context.Context, s *schedule.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.m[s.ID]; ok {
		return errors.New("already exists")
	}

	c := *s
	r.m[s.ID] = &c

	return nil
}

func (r *memRepo) Get(_ context.Context, id string) (*schedule.Schedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.m[id]
	if !ok {
		return nil, errors.New("not found")
	}

	c := *s

	return &c, nil
}

func (r *memRepo) List(_ context.Context, projectID string, _, _ int) ([]*schedule.Schedule, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []*schedule.Schedule

	for _, s := range r.m {
		if projectID != "" && s.ProjectID != projectID {
			continue
		}

		c := *s
		out = append(out, &c)
	}

	return out, len(out), nil
}

func (r *memRepo) ListAll(ctx context.Context) ([]*schedule.Schedule, error) {
	out, _, err := r.List(ctx, "", 0, 0)
	return out, err
}

func (r *memRepo) Update(_ context.Context, s *schedule.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := *s
	r.m[s.ID] = &c

	return nil
}

func (r *memRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)

	return nil
}

// memWorkflowRepo is a minimal workflow.Repository sufficient for the schedule
// server flows under test.
type memWorkflowRepo struct {
	wf *workflow.Workflow
}

func (r *memWorkflowRepo) Create(_ context.Context, _ *workflow.Workflow) error { return nil }
func (r *memWorkflowRepo) Get(_ context.Context, id string) (*workflow.Workflow, error) {
	if r.wf == nil || r.wf.ID != id {
		return nil, errors.New("not found")
	}

	c := *r.wf

	return &c, nil
}
func (r *memWorkflowRepo) List(_ context.Context, _ string, _, _ int) ([]*workflow.Workflow, int, error) {
	if r.wf == nil {
		return nil, 0, nil
	}

	return []*workflow.Workflow{r.wf}, 1, nil
}
func (r *memWorkflowRepo) Update(_ context.Context, _ *workflow.Workflow) error { return nil }
func (r *memWorkflowRepo) Delete(_ context.Context, _ string) error             { return nil }

// stubScheduler records calls.
type stubScheduler struct {
	added   []string
	updated []string
	removed []string
}

func (s *stubScheduler) Add(sc *schedule.Schedule) error {
	s.added = append(s.added, sc.ID)
	return nil
}
func (s *stubScheduler) Update(sc *schedule.Schedule) error {
	s.updated = append(s.updated, sc.ID)
	return nil
}
func (s *stubScheduler) Remove(id string) {
	s.removed = append(s.removed, id)
}
func (s *stubScheduler) NextRun(_ string, _ time.Time) time.Time {
	return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
}

func sampleWorkflow() *workflow.Workflow {
	return &workflow.Workflow{
		ID:        "w1",
		ProjectID: "p1",
		Name:      "default",
		Statuses: []workflow.Status{
			{Name: "todo", IsInitial: true},
			{Name: "done", IsTerminal: true},
		},
	}
}

func TestCreateScheduleSuccess(t *testing.T) {
	repo := newMemRepo()
	wfr := &memWorkflowRepo{wf: sampleWorkflow()}
	sched := &stubScheduler{}
	srv := schedule.NewServer(repo, wfr, sched)

	resp, err := srv.CreateSchedule(context.Background(), connect.NewRequest(&taskguildv1.CreateScheduleRequest{
		ProjectId:      "p1",
		WorkflowId:     "w1",
		Name:           "every minute",
		CronExpression: "* * * * *",
		Enabled:        true,
		TaskTitle:      "[scheduled] {{datetime}}",
	}))
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	got := resp.Msg.GetSchedule()
	if got.GetId() == "" {
		t.Error("expected non-empty ID")
	}

	if got.GetStatusId() != "todo" {
		t.Errorf("expected default status 'todo', got %q", got.GetStatusId())
	}

	if got.GetEnabled() != true {
		t.Error("expected enabled=true")
	}

	if len(sched.added) != 1 {
		t.Errorf("expected scheduler.Add to be called once, got %d", len(sched.added))
	}
}

func TestCreateScheduleInvalidCron(t *testing.T) {
	repo := newMemRepo()
	wfr := &memWorkflowRepo{wf: sampleWorkflow()}
	sched := &stubScheduler{}
	srv := schedule.NewServer(repo, wfr, sched)

	_, err := srv.CreateSchedule(context.Background(), connect.NewRequest(&taskguildv1.CreateScheduleRequest{
		ProjectId:      "p1",
		WorkflowId:     "w1",
		Name:           "bad",
		CronExpression: "this is not a cron",
		TaskTitle:      "x",
	}))
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}

	var ce *connect.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected connect.Error, got %T: %v", err, err)
	}

	if ce.Code() != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", ce.Code())
	}
}

func TestCreateScheduleStatusValidation(t *testing.T) {
	repo := newMemRepo()
	wfr := &memWorkflowRepo{wf: sampleWorkflow()}
	sched := &stubScheduler{}
	srv := schedule.NewServer(repo, wfr, sched)

	bogus := "nonexistent"

	_, err := srv.CreateSchedule(context.Background(), connect.NewRequest(&taskguildv1.CreateScheduleRequest{
		ProjectId:      "p1",
		WorkflowId:     "w1",
		Name:           "bad status",
		CronExpression: "* * * * *",
		TaskTitle:      "x",
		StatusId:       &bogus,
	}))
	if err == nil {
		t.Fatal("expected error for nonexistent status")
	}
}

func TestSetScheduleEnabledTogglesScheduler(t *testing.T) {
	repo := newMemRepo()
	wfr := &memWorkflowRepo{wf: sampleWorkflow()}
	sched := &stubScheduler{}
	srv := schedule.NewServer(repo, wfr, sched)

	// Create disabled.
	createResp, err := srv.CreateSchedule(context.Background(), connect.NewRequest(&taskguildv1.CreateScheduleRequest{
		ProjectId:      "p1",
		WorkflowId:     "w1",
		Name:           "s",
		CronExpression: "* * * * *",
		Enabled:        false,
		TaskTitle:      "x",
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	id := createResp.Msg.GetSchedule().GetId()
	if len(sched.added) != 0 {
		t.Errorf("expected scheduler.Add to NOT be called for disabled create, got %d", len(sched.added))
	}

	// Enable.
	_, err = srv.SetScheduleEnabled(context.Background(), connect.NewRequest(&taskguildv1.SetScheduleEnabledRequest{
		Id:      id,
		Enabled: true,
	}))
	if err != nil {
		t.Fatalf("enable: %v", err)
	}

	if len(sched.added) != 1 {
		t.Errorf("expected scheduler.Add to be called once after enable, got %d", len(sched.added))
	}

	// Disable.
	_, err = srv.SetScheduleEnabled(context.Background(), connect.NewRequest(&taskguildv1.SetScheduleEnabledRequest{
		Id:      id,
		Enabled: false,
	}))
	if err != nil {
		t.Fatalf("disable: %v", err)
	}

	if len(sched.removed) == 0 {
		t.Errorf("expected scheduler.Remove to be called after disable, got %d", len(sched.removed))
	}
}

func TestDeleteScheduleCallsRemove(t *testing.T) {
	repo := newMemRepo()
	wfr := &memWorkflowRepo{wf: sampleWorkflow()}
	sched := &stubScheduler{}
	srv := schedule.NewServer(repo, wfr, sched)

	createResp, err := srv.CreateSchedule(context.Background(), connect.NewRequest(&taskguildv1.CreateScheduleRequest{
		ProjectId:      "p1",
		WorkflowId:     "w1",
		Name:           "s",
		CronExpression: "* * * * *",
		Enabled:        true,
		TaskTitle:      "x",
	}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	id := createResp.Msg.GetSchedule().GetId()

	_, err = srv.DeleteSchedule(context.Background(), connect.NewRequest(&taskguildv1.DeleteScheduleRequest{Id: id}))
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(sched.removed) != 1 || sched.removed[0] != id {
		t.Errorf("expected scheduler.Remove(%q), got %v", id, sched.removed)
	}
}
