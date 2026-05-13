package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
)

// fire is invoked by the cron runner when a schedule's expression matches the
// current wall time. It re-reads the schedule from the repository (so edits
// applied between the previous tick and now are honoured), then dispatches
// task creation through the TaskCreator and persists state updates.
//
// Errors from task creation are logged and recorded on the schedule's
// LastError field; the cron entry itself is left in place so the schedule
// continues firing on its normal cadence.
func (s *Scheduler) fire(scheduleID string) {
	// Bound work by a generous deadline so a hung task creation cannot pile up
	// goroutines indefinitely. Schedule firings should be quick (just a YAML
	// write); 30s is more than enough on local storage and reasonable on S3.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sched, err := s.repo.Get(ctx, scheduleID)
	if err != nil {
		slog.Error("scheduler: failed to load schedule on fire",
			"schedule_id", scheduleID, "error", err)
		return
	}

	if !sched.Enabled {
		slog.Debug("scheduler: skipping disabled schedule", "schedule_id", scheduleID)
		return
	}

	firedAt := time.Now()

	_, createErr := s.taskCreator.CreateTaskFromSchedule(ctx, sched, firedAt)

	sched.LastRunAt = firedAt
	if createErr != nil {
		sched.LastError = createErr.Error()

		slog.Error("scheduler: failed to create task from schedule",
			"schedule_id", scheduleID, "error", createErr)
	} else {
		sched.LastError = ""
	}

	// Recompute next-run from the runner's view of "now" so it stays accurate
	// even if the create took a noticeable amount of wall time.
	if parsed, perr := cron.ParseStandard(sched.CronExpression); perr == nil {
		sched.NextRunAt = parsed.Next(time.Now())
	}

	sched.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, sched); err != nil {
		slog.Error("scheduler: failed to persist schedule state after fire",
			"schedule_id", scheduleID, "error", err)
	}
}
