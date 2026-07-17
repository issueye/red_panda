package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
)

// Clock abstracts time for deterministic schedule tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func loadLocation(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return loc, nil
}

// computeNextRunAt returns the next fire time in UTC after `after` (exclusive of exact equality when needed).
// after should be UTC. For first schedule, pass now.
func computeNextRunAt(task model.ScheduledTask, after time.Time) (*time.Time, error) {
	after = after.UTC()
	loc, err := loadLocation(task.Timezone)
	if err != nil {
		return nil, err
	}
	switch strings.TrimSpace(task.ScheduleKind) {
	case methods.ScheduleKindOneShot:
		if task.RunAt == nil {
			return nil, fmt.Errorf("run_at is required for one_shot")
		}
		runAt := task.RunAt.UTC()
		if !runAt.After(after) {
			return nil, nil
		}
		return &runAt, nil
	case methods.ScheduleKindInterval:
		if task.IntervalSec < 60 {
			return nil, fmt.Errorf("interval_sec must be >= 60")
		}
		// First fire: if never fired, schedule at after+interval unless RunAt/start used.
		// Use next = after + interval for predictability when enabling.
		next := after.Add(time.Duration(task.IntervalSec) * time.Second)
		if task.LastFiredAt != nil {
			next = task.LastFiredAt.UTC().Add(time.Duration(task.IntervalSec) * time.Second)
			for !next.After(after) {
				next = next.Add(time.Duration(task.IntervalSec) * time.Second)
			}
		}
		return &next, nil
	case methods.ScheduleKindCron:
		expr := strings.TrimSpace(task.CronExpr)
		if expr == "" {
			return nil, fmt.Errorf("cron_expr is required for cron")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		sched, err := parser.Parse(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid cron_expr: %w", err)
		}
		// Evaluate in local timezone of the task.
		localAfter := after.In(loc)
		nextLocal := sched.Next(localAfter)
		if nextLocal.IsZero() {
			return nil, nil
		}
		// Density guard: count fires in the next hour from after.
		if err := guardCronDensity(sched, localAfter, 60); err != nil {
			return nil, err
		}
		next := nextLocal.UTC()
		return &next, nil
	default:
		return nil, fmt.Errorf("unsupported schedule_kind %q", task.ScheduleKind)
	}
}

func guardCronDensity(sched cron.Schedule, from time.Time, maxPerHour int) error {
	count := 0
	t := from
	end := from.Add(time.Hour)
	for count <= maxPerHour {
		n := sched.Next(t)
		if n.IsZero() || n.After(end) {
			return nil
		}
		count++
		t = n
	}
	return fmt.Errorf("cron fires more than %d times per hour; choose a sparser expression", maxPerHour)
}

func validateScheduleKindFields(kind string, intervalSec int, cronExpr string, runAt *time.Time) error {
	switch strings.TrimSpace(kind) {
	case methods.ScheduleKindOneShot:
		if runAt == nil {
			return fmt.Errorf("run_at is required for one_shot")
		}
	case methods.ScheduleKindInterval:
		if intervalSec < 60 {
			return fmt.Errorf("interval_sec must be >= 60")
		}
	case methods.ScheduleKindCron:
		if strings.TrimSpace(cronExpr) == "" {
			return fmt.Errorf("cron_expr is required for cron")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
		if _, err := parser.Parse(strings.TrimSpace(cronExpr)); err != nil {
			return fmt.Errorf("invalid cron_expr: %w", err)
		}
	default:
		return fmt.Errorf("schedule_kind must be one_shot, interval, or cron")
	}
	return nil
}
