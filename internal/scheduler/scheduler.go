// Package scheduler implements a cron-based scheduler that fires registered
// schedules (see internal/schedule) and creates Tasks at their scheduled times.
//
// The scheduler runs as a singleton goroutine started from
// cmd/taskguild-server/run.go. It uses github.com/robfig/cron/v3 as the
// underlying cron runner with the standard 5-field parser and the server's
// local timezone. Missed firings during downtime are skipped (no catch-up).
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/kazz187/taskguild/internal/schedule"
	"github.com/kazz187/taskguild/internal/task"
)

// TaskCreator abstracts the cross-package call into internal/task so the
// scheduler can be tested without spinning up a full task.Server.
type TaskCreator interface {
	CreateTaskFromSchedule(ctx context.Context, s *schedule.Schedule, firedAt time.Time) (*task.Task, error)
}

// Scheduler manages the cron entries for all enabled schedules and dispatches
// task creation when entries fire.
type Scheduler struct {
	repo         schedule.Repository
	taskCreator  TaskCreator
	cronLocation *time.Location

	mu      sync.Mutex
	cron    *cron.Cron
	entries map[string]cron.EntryID // schedule.ID -> entry
}

// New creates a Scheduler. Start must be called to begin firing entries.
func New(repo schedule.Repository, tc TaskCreator) *Scheduler {
	return &Scheduler{
		repo:         repo,
		taskCreator:  tc,
		cronLocation: time.Local,
		entries:      make(map[string]cron.EntryID),
	}
}

// SetLocation overrides the cron timezone (default: time.Local). Must be
// called before Start.
func (s *Scheduler) SetLocation(loc *time.Location) {
	if loc != nil {
		s.cronLocation = loc
	}
}

// Start initializes the cron runner, registers every enabled schedule from the
// repository, and runs until ctx is canceled. Safe to call once per Scheduler.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.cron = cron.New(cron.WithLocation(s.cronLocation))
	s.mu.Unlock()

	schedules, err := s.repo.ListAll(ctx)
	if err != nil {
		slog.Error("scheduler: failed to load schedules", "error", err)
	}

	for _, sched := range schedules {
		if !sched.Enabled {
			continue
		}

		if err := s.Add(sched); err != nil {
			slog.Warn("scheduler: failed to register schedule on startup",
				"schedule_id", sched.ID, "cron", sched.CronExpression, "error", err)
		}
	}

	s.cron.Start()
	slog.Info("scheduler started", "registered", len(s.entries), "location", s.cronLocation.String())

	<-ctx.Done()

	stopCtx := s.cron.Stop()
	<-stopCtx.Done()
	slog.Info("scheduler stopped")
}

// Add registers a schedule. If the schedule is already registered, the
// previous entry is removed first.
func (s *Scheduler) Add(sched *schedule.Schedule) error {
	if sched == nil {
		return fmt.Errorf("schedule is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron == nil {
		// Start has not yet been called. Defer registration: the Start path
		// loads schedules from the repository, so anything created before
		// Start runs will be picked up there.
		return nil
	}

	if oldID, ok := s.entries[sched.ID]; ok {
		s.cron.Remove(oldID)
		delete(s.entries, sched.ID)
	}

	scheduleID := sched.ID
	entryID, err := s.cron.AddFunc(sched.CronExpression, func() {
		s.fire(scheduleID)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", sched.CronExpression, err)
	}

	s.entries[sched.ID] = entryID

	return nil
}

// Update re-registers a schedule with new parameters. Equivalent to Add but
// communicates intent.
func (s *Scheduler) Update(sched *schedule.Schedule) error {
	return s.Add(sched)
}

// Remove unregisters a schedule. No-op if it was not registered.
func (s *Scheduler) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cron == nil {
		delete(s.entries, id)
		return
	}

	if entryID, ok := s.entries[id]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}
}

// NextRun computes the next firing time of expr (5-field cron) after from.
// Returns the zero Time if the expression is invalid.
func (s *Scheduler) NextRun(expr string, from time.Time) time.Time {
	parsed, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}
	}

	return parsed.Next(from)
}
