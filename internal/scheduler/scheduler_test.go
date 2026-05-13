package scheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kazz187/taskguild/internal/schedule"
	"github.com/kazz187/taskguild/internal/task"
)

// fakeRepo is an in-memory schedule.Repository for testing.
type fakeRepo struct {
	mu        sync.Mutex
	schedules map[string]*schedule.Schedule
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{schedules: make(map[string]*schedule.Schedule)}
}

func (r *fakeRepo) Create(_ context.Context, s *schedule.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := *s
	r.schedules[s.ID] = &c

	return nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (*schedule.Schedule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.schedules[id]
	if !ok {
		return nil, errNotFound
	}

	c := *s

	return &c, nil
}

func (r *fakeRepo) List(_ context.Context, projectID string, _, _ int) ([]*schedule.Schedule, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]*schedule.Schedule, 0, len(r.schedules))

	for _, s := range r.schedules {
		if projectID != "" && s.ProjectID != projectID {
			continue
		}

		c := *s
		out = append(out, &c)
	}

	return out, len(out), nil
}

func (r *fakeRepo) ListAll(ctx context.Context) ([]*schedule.Schedule, error) {
	out, _, err := r.List(ctx, "", 0, 0)
	return out, err
}

func (r *fakeRepo) Update(_ context.Context, s *schedule.Schedule) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := *s
	r.schedules[s.ID] = &c

	return nil
}

func (r *fakeRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.schedules, id)

	return nil
}

type sentinelErr string

func (e sentinelErr) Error() string { return string(e) }

var errNotFound sentinelErr = "schedule not found"

// fakeTaskCreator records each invocation. The schedule pointer is shallow-copied
// so callers can assert on the value seen at fire-time without races.
type fakeTaskCreator struct {
	mu    sync.Mutex
	calls []firedCall
}

type firedCall struct {
	scheduleID string
	title      string
	firedAt    time.Time
}

func (f *fakeTaskCreator) CreateTaskFromSchedule(_ context.Context, s *schedule.Schedule, firedAt time.Time) (*task.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, firedCall{
		scheduleID: s.ID,
		title:      schedule.ExpandTemplate(s.TaskTitle, firedAt),
		firedAt:    firedAt,
	})

	return &task.Task{ID: "task-" + s.ID}, nil
}

func (f *fakeTaskCreator) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

func TestSchedulerAddInvalidCron(t *testing.T) {
	repo := newFakeRepo()
	tc := &fakeTaskCreator{}
	s := New(repo, tc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start in a goroutine then wait briefly so cron runner spins up.
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()

	// Allow Start to initialize internal cron.
	for range 50 {
		s.mu.Lock()
		ready := s.cron != nil
		s.mu.Unlock()

		if ready {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	err := s.Add(&schedule.Schedule{ID: "bad", CronExpression: "not-a-cron"})
	if err == nil {
		t.Fatal("expected error for invalid cron expression, got nil")
	}

	cancel()
	<-done
}

func TestSchedulerFiresEnabledSchedule(t *testing.T) {
	repo := newFakeRepo()

	// Seed an enabled schedule that fires every second.
	sched := &schedule.Schedule{
		ID:             "s1",
		ProjectID:      "p1",
		WorkflowID:     "w1",
		Name:           "Every second",
		CronExpression: "@every 1s",
		Enabled:        true,
		TaskTitle:      "scheduled-{{date}}",
		StatusID:       "todo",
	}
	if err := repo.Create(context.Background(), sched); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	tc := &fakeTaskCreator{}
	s := New(repo, tc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()

	// Wait up to ~3s for at least one fire.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if tc.callCount() > 0 {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	if tc.callCount() == 0 {
		t.Fatal("expected at least one fire within 3 seconds")
	}

	cancel()
	<-done

	// Verify LastRunAt was persisted.
	got, _ := repo.Get(context.Background(), "s1")
	if got.LastRunAt.IsZero() {
		t.Error("expected LastRunAt to be set after fire")
	}
}

func TestSchedulerSkipsDisabledOnFire(t *testing.T) {
	repo := newFakeRepo()

	sched := &schedule.Schedule{
		ID:             "disabled-after-add",
		CronExpression: "@every 1s",
		Enabled:        true,
		TaskTitle:      "x",
	}
	if err := repo.Create(context.Background(), sched); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	tc := &fakeTaskCreator{}
	s := New(repo, tc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()

	// Wait until cron is up.
	for range 50 {
		s.mu.Lock()
		ready := s.cron != nil
		s.mu.Unlock()

		if ready {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	// Disable in repo (without removing entry) so fire() detects and skips.
	sched.Enabled = false
	if err := repo.Update(context.Background(), sched); err != nil {
		t.Fatalf("disable update: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tc.callCount() > 0 {
			t.Fatal("expected no fire while disabled, but got one")
		}

		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	<-done
}

func TestSchedulerRemove(t *testing.T) {
	repo := newFakeRepo()
	tc := &fakeTaskCreator{}
	s := New(repo, tc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Start(ctx)
	}()

	for range 50 {
		s.mu.Lock()
		ready := s.cron != nil
		s.mu.Unlock()

		if ready {
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	sched := &schedule.Schedule{
		ID:             "s-remove",
		CronExpression: "@every 1s",
		Enabled:        true,
		TaskTitle:      "x",
	}
	if err := repo.Create(context.Background(), sched); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.Add(sched); err != nil {
		t.Fatalf("add: %v", err)
	}

	s.Remove(sched.ID)

	deadline := time.Now().Add(2 * time.Second)

	var fired int32

	go func() {
		for time.Now().Before(deadline) {
			atomic.StoreInt32(&fired, int32(tc.callCount()))
			time.Sleep(50 * time.Millisecond)
		}
	}()

	time.Sleep(2 * time.Second)

	if atomic.LoadInt32(&fired) > 0 {
		t.Fatal("expected no fire after remove, but got at least one")
	}

	cancel()
	<-done
}

func TestNextRun(t *testing.T) {
	s := New(newFakeRepo(), &fakeTaskCreator{})

	got := s.NextRun("0 9 * * *", time.Date(2026, 5, 13, 8, 0, 0, 0, time.UTC))
	if got.IsZero() {
		t.Fatal("expected non-zero next-run for valid cron")
	}

	got = s.NextRun("bogus", time.Now())
	if !got.IsZero() {
		t.Errorf("expected zero next-run for invalid cron, got %v", got)
	}
}
