package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

// Trigger fires a schedule immediately (manual). Does not advance cron phase beyond normal recompute after fire.
func (s ScheduleService) Trigger(ctx context.Context, id string) (ScheduleTriggerResult, error) {
	row, err := s.repos.Schedules.Get(id)
	if err != nil {
		return ScheduleTriggerResult{}, fmt.Errorf("schedule not found")
	}
	return s.fire(ctx, row, s.now(), true)
}

// TickDue claims and fires due schedules (scheduler loop).
func (s ScheduleService) TickDue(ctx context.Context) int {
	now := s.now()
	due, err := s.repos.Schedules.ListDue(now, 10)
	if err != nil {
		log.Printf("schedule tick list due: %v", err)
		return 0
	}
	fired := 0
	for _, row := range due {
		if !s.tryAcquireInflight() {
			break
		}
		result, err := s.fire(ctx, row, now, false)
		s.releaseInflight()
		if err != nil {
			log.Printf("schedule fire %s: %v", row.ID, err)
			continue
		}
		if result.Status == "running" || result.Status == "starting" || result.Status == "skipped" {
			fired++
		}
	}
	return fired
}

// CatchUpOnStartup fires at most one due task per schedule with global limit.
func (s ScheduleService) CatchUpOnStartup(ctx context.Context) {
	now := s.now()
	due, err := s.repos.Schedules.ListEnabledWithPastNext(now)
	if err != nil {
		log.Printf("schedule catch-up list: %v", err)
		return
	}
	limit := 2
	count := 0
	for _, row := range due {
		if count >= limit {
			// Push next into the future without firing remaining.
			if next, err := s.recomputeNext(row, now); err == nil {
				row.NextRunAt = next
				_, _ = s.repos.Schedules.Update(row)
			}
			continue
		}
		if !s.tryAcquireInflight() {
			break
		}
		_, err := s.fire(ctx, row, now, false)
		s.releaseInflight()
		if err != nil {
			log.Printf("schedule catch-up fire %s: %v", row.ID, err)
			continue
		}
		count++
	}
}

// OnRunTerminal updates schedule run audit when a bound agent run finishes.
func (s ScheduleService) OnRunTerminal(runID, status, errText string) {
	if runID == "" {
		return
	}
	srun, err := s.repos.Schedules.GetRunByRunID(runID)
	if err != nil {
		return
	}
	now := s.now()
	srun.FinishedAt = &now
	switch status {
	case "completed", "succeeded":
		srun.Status = "succeeded"
	case "cancelled":
		srun.Status = "cancelled"
	default:
		srun.Status = "failed"
		if errText != "" {
			srun.Error = errText
		}
	}
	_ = s.repos.Schedules.UpdateRun(srun)

	task, err := s.repos.Schedules.Get(srun.ScheduleID)
	if err != nil {
		return
	}
	task.LastStatus = srun.Status
	if srun.Error != "" {
		task.LastError = srun.Error
	}
	_, _ = s.repos.Schedules.Update(task)
}

func (s ScheduleService) fire(ctx context.Context, row model.ScheduledTask, now time.Time, manual bool) (ScheduleTriggerResult, error) {
	if s.runs == nil {
		return ScheduleTriggerResult{}, fmt.Errorf("run starter not configured")
	}
	// Reload for consistency.
	fresh, err := s.repos.Schedules.Get(row.ID)
	if err != nil {
		return ScheduleTriggerResult{}, fmt.Errorf("schedule not found")
	}
	row = fresh
	if !manual && !row.Enabled {
		return ScheduleTriggerResult{Status: "skipped", SkipReason: "disabled"}, nil
	}
	if !manual && row.MaxRuns > 0 && row.RunCount >= row.MaxRuns {
		row.Enabled = false
		row.NextRunAt = nil
		row.LastStatus = "skipped"
		row.LastError = "max_runs reached"
		_, _ = s.repos.Schedules.Update(row)
		return ScheduleTriggerResult{Status: "skipped", SkipReason: "max_runs"}, nil
	}

	if row.OverlapPolicy == "" || row.OverlapPolicy == methods.ScheduleOverlapSkip {
		open, err := s.repos.Schedules.HasOpenRun(row.ID)
		if err != nil {
			return ScheduleTriggerResult{}, err
		}
		if open {
			// Still advance next for automatic fires so we do not spin.
			if !manual {
				s.advanceAfterFire(&row, now, false)
				_, _ = s.repos.Schedules.Update(row)
			}
			srun, _ := s.repos.Schedules.CreateRun(model.ScheduledTaskRun{
				ScheduleID:   row.ID,
				Status:       "skipped",
				SkipReason:   "overlap",
				ScheduledFor: now,
				CreatedAt:    now,
			})
			return ScheduleTriggerResult{
				ScheduleRunID: srun.ID,
				Status:        "skipped",
				SkipReason:    "overlap",
			}, nil
		}
	}

	sessionID, err := s.resolveSession(row, now)
	if err != nil {
		return ScheduleTriggerResult{}, err
	}

	started := now
	srun, err := s.repos.Schedules.CreateRun(model.ScheduledTaskRun{
		ScheduleID:   row.ID,
		SessionID:    sessionID,
		Status:       "starting",
		ScheduledFor: scheduledFor(row, now),
		StartedAt:    &started,
		CreatedAt:    now,
	})
	if err != nil {
		return ScheduleTriggerResult{}, err
	}

	payload := s.buildRunPayload(row, sessionID)
	result, startErr := s.runs.Start(ctx, payload)
	if startErr != nil {
		finished := s.now()
		srun.Status = "failed"
		srun.Error = startErr.Error()
		srun.FinishedAt = &finished
		_ = s.repos.Schedules.UpdateRun(srun)
		row.LastStatus = "failed"
		row.LastError = startErr.Error()
		row.LastFiredAt = &now
		if !manual {
			s.advanceAfterFire(&row, now, true)
		}
		_, _ = s.repos.Schedules.Update(row)
		return ScheduleTriggerResult{
			ScheduleRunID: srun.ID,
			SessionID:     sessionID,
			Status:        "failed",
			Error:         startErr.Error(),
		}, nil
	}

	srun.RunID = result.RunID
	srun.SessionID = result.SessionID
	srun.Status = "running"
	_ = s.repos.Schedules.UpdateRun(srun)

	row.LastRunID = result.RunID
	row.LastStatus = "running"
	row.LastError = ""
	row.LastFiredAt = &now
	row.RunCount++
	if row.ScheduleKind == methods.ScheduleKindOneShot || (row.MaxRuns > 0 && row.RunCount >= row.MaxRuns) {
		row.Enabled = false
		row.NextRunAt = nil
	} else if !manual {
		s.advanceAfterFire(&row, now, true)
	} else {
		// Manual trigger: keep next as-is if still enabled.
		if row.Enabled && row.NextRunAt == nil {
			s.advanceAfterFire(&row, now, true)
		}
	}
	_, _ = s.repos.Schedules.Update(row)

	s.publishSessionUpserted(result.SessionID, result.RunID, row.ID, manual)

	return ScheduleTriggerResult{
		ScheduleRunID: srun.ID,
		RunID:         result.RunID,
		SessionID:     result.SessionID,
		Status:        "running",
	}, nil
}

// publishSessionUpserted notifies connected Desktops so the session tree shows schedule-created sessions.
func (s ScheduleService) publishSessionUpserted(sessionID, runID, scheduleID string, manual bool) {
	if s.hub == nil || sessionID == "" {
		return
	}
	row, err := s.repos.Sessions.Get(sessionID)
	if err != nil {
		return
	}
	broadcastSessionUpserted(s.hub, row, map[string]any{
		"reason":      "schedule",
		"run_id":      runID,
		"schedule_id": scheduleID,
		"manual":      manual,
		// source kept for Desktop auto-select (legacy key used by App.jsx).
		"source": "schedule",
	})
}

func (s ScheduleService) advanceAfterFire(row *model.ScheduledTask, now time.Time, countAsFire bool) {
	if !row.Enabled {
		row.NextRunAt = nil
		return
	}
	// One-shot handled by caller.
	if row.ScheduleKind == methods.ScheduleKindOneShot {
		row.Enabled = false
		row.NextRunAt = nil
		return
	}
	base := now
	if countAsFire && row.LastFiredAt != nil {
		base = *row.LastFiredAt
	}
	next, err := computeNextRunAt(*row, base)
	if err != nil || next == nil {
		row.NextRunAt = nil
		if err != nil {
			row.LastError = err.Error()
		}
		return
	}
	row.NextRunAt = next
}

func (s ScheduleService) resolveSession(row model.ScheduledTask, now time.Time) (string, error) {
	if row.SessionMode == methods.ScheduleSessionFixed {
		session, err := s.repos.Sessions.Get(row.SessionID)
		if err != nil {
			return "", fmt.Errorf("fixed session not found")
		}
		return session.ID, nil
	}
	name := fmt.Sprintf("定时 · %s", row.Name)
	if len(name) > 80 {
		name = name[:80]
	}
	session, err := s.repos.Sessions.Create(name, row.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	return session.ID, nil
}

func (s ScheduleService) buildRunPayload(row model.ScheduledTask, sessionID string) protows.RunStartPayload {
	// Always normalize: legacy rows may store tool_policy="allowlist" / permission_mode="deny"
	// which Runtime rejects or mis-handles (see tools.EvaluateToolPolicy).
	toolPolicy := scheduleToolPolicy(row.ToolPolicy)
	permissionMode := schedulePermissionMode(row.PermissionMode)
	options := map[string]any{
		"working_dir":     row.WorkspaceRoot,
		"trigger_source":  "schedule",
		"trigger_ref":     row.ID,
		"permission_mode": permissionMode,
		"tool_policy":     toolPolicy,
		"runtime_mode":    "per_run_process",
	}
	if row.ProviderProfileID != "" {
		options["provider_profile_id"] = row.ProviderProfileID
	}
	allow := decodeStringSlice(row.ToolAllowlistJSON)
	if len(allow) == 0 {
		allow = append([]string(nil), defaultScheduleAllowlist...)
	}
	options["tool_allowlist"] = allow
	if deny := decodeStringSlice(row.ToolDenylistJSON); len(deny) > 0 {
		options["tool_denylist"] = deny
	}
	if strings.TrimSpace(row.RunOptionsJSON) != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(row.RunOptionsJSON), &extra); err == nil {
			for k, v := range extra {
				if isAllowedScheduleOption(k) {
					options[k] = v
				}
			}
		}
	}
	// Safety options from the schedule row win over free-form extras for critical keys.
	options["permission_mode"] = permissionMode
	options["tool_policy"] = toolPolicy
	options["tool_allowlist"] = allow
	options["trigger_source"] = "schedule"
	options["trigger_ref"] = row.ID
	options["working_dir"] = row.WorkspaceRoot

	return protows.RunStartPayload{
		SessionID: sessionID,
		Input:     map[string]any{"text": row.Prompt},
		Options:   options,
		Subscribe: true,
	}
}

func isAllowedScheduleOption(key string) bool {
	switch key {
	case "model", "provider_profile_id", "runtime_mode",
		"tool_policy", "tool_allowlist", "tool_denylist", "permission_mode",
		"worker_pool_size", "web_search_max_results", "web_fetch_max_bytes",
		"max_concurrent_runs":
		return true
	default:
		return false
	}
}

func scheduledFor(row model.ScheduledTask, now time.Time) time.Time {
	if row.NextRunAt != nil {
		return *row.NextRunAt
	}
	return now
}

func (s *ScheduleService) tryAcquireInflight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	max := s.maxInflight
	if max <= 0 {
		max = defaultScheduleMaxInflight
	}
	if s.inflight >= max {
		return false
	}
	s.inflight++
	return true
}

func (s *ScheduleService) releaseInflight() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight > 0 {
		s.inflight--
	}
}

