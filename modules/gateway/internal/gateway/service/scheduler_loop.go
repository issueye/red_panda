package service

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"
)

// RunLoop polls for due schedules until ctx is cancelled.
func (s ScheduleService) RunLoop(ctx context.Context) {
	if os.Getenv("RED_PANDA_SCHEDULER_DISABLED") == "1" {
		log.Printf("scheduler loop disabled (RED_PANDA_SCHEDULER_DISABLED=1)")
		return
	}
	tickMS := 1000
	if raw := stringsTrim(os.Getenv("RED_PANDA_SCHEDULER_TICK_MS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 200 {
			tickMS = n
		}
	}
	if raw := stringsTrim(os.Getenv("RED_PANDA_SCHEDULE_MAX_INFLIGHT")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			s.maxInflight = n
		}
	}

	// Startup catch-up (at most one fire per due task, globally capped).
	s.CatchUpOnStartup(ctx)

	ticker := time.NewTicker(time.Duration(tickMS) * time.Millisecond)
	defer ticker.Stop()
	log.Printf("scheduler loop started (tick=%dms max_inflight=%d)", tickMS, s.maxInflight)
	for {
		select {
		case <-ctx.Done():
			log.Printf("scheduler loop stopped")
			return
		case <-ticker.C:
			s.TickDue(ctx)
		}
	}
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
