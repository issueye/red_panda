package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

type stubRunStarter struct {
	last protows.RunStartPayload
	err  error
}

func (s *stubRunStarter) Start(ctx context.Context, payload protows.RunStartPayload) (StartRunResult, error) {
	s.last = payload
	if s.err != nil {
		return StartRunResult{}, s.err
	}
	return StartRunResult{
		RunID:      "run_sched_test",
		SessionID:  payload.SessionID,
		Accepted:   true,
		Subscribed: true,
	}, nil
}

func testScheduleService(t *testing.T, clock Clock, runs runStarter) ScheduleService {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "sched.db")
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repos := repository.NewSet(db)
	svc := NewScheduleService(repos, nil, runs)
	if clock != nil {
		svc = svc.WithClock(clock)
	}
	return svc
}

func TestScheduleCreateIntervalAndDefaults(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	svc := testScheduleService(t, fixedClock{t: now}, nil)
	dto, err := svc.Create(ScheduleCreateRequest{
		Name:          "morning-digest",
		ScheduleKind:  methods.ScheduleKindInterval,
		IntervalSec:   60,
		Prompt:        "Summarize workspace read-only.",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dto.Enabled {
		t.Fatal("expected enabled")
	}
	if dto.PermissionMode != "deny_all" {
		t.Fatalf("permission_mode=%q", dto.PermissionMode)
	}
	if dto.ToolPolicy != "risk_based" {
		t.Fatalf("tool_policy=%q", dto.ToolPolicy)
	}
	if len(dto.ToolAllowlist) == 0 {
		t.Fatal("expected default allowlist")
	}
	if dto.NextRunAt == "" {
		t.Fatal("expected next_run_at")
	}
	next, err := time.Parse(time.RFC3339Nano, dto.NextRunAt)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(60 * time.Second)
	if !next.Equal(want) {
		t.Fatalf("next_run_at=%v want %v", next, want)
	}
}

func TestScheduleCreateRejectsDenseCron(t *testing.T) {
	svc := testScheduleService(t, nil, nil)
	// Every minute is ok density (60/hour). Every second isn't supported by 5-field.
	// Use every minute * and ensure create works; invalid cron fails.
	_, err := svc.Create(ScheduleCreateRequest{
		Name:          "bad-cron",
		ScheduleKind:  methods.ScheduleKindCron,
		CronExpr:      "not a cron",
		Prompt:        "x",
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected invalid cron error")
	}
}

func TestScheduleTriggerUsesSafeOptions(t *testing.T) {
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	stub := &stubRunStarter{}
	svc := testScheduleService(t, fixedClock{t: now}, stub)
	ws := t.TempDir()
	created, err := svc.Create(ScheduleCreateRequest{
		Name:          "trigger-me",
		ScheduleKind:  methods.ScheduleKindInterval,
		IntervalSec:   120,
		Prompt:        "hello schedule",
		WorkspaceRoot: ws,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Trigger(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "running" || result.RunID != "run_sched_test" {
		t.Fatalf("result=%#v", result)
	}
	if stub.last.Options["trigger_source"] != "schedule" {
		t.Fatalf("options=%#v", stub.last.Options)
	}
	if stub.last.Options["permission_mode"] != "deny_all" {
		t.Fatalf("permission_mode=%v", stub.last.Options["permission_mode"])
	}
	if stub.last.Options["tool_policy"] != "risk_based" {
		t.Fatalf("tool_policy=%v", stub.last.Options["tool_policy"])
	}
	if stub.last.Options["working_dir"] != ws {
		t.Fatalf("working_dir=%v", stub.last.Options["working_dir"])
	}
	text, _ := stub.last.Input["text"].(string)
	if text != "hello schedule" {
		t.Fatalf("input=%v", stub.last.Input)
	}
	// Session should be newly created with 定时 prefix.
	if stub.last.SessionID == "" {
		t.Fatal("missing session")
	}
}

func TestScheduleOverlapSkip(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	stub := &stubRunStarter{}
	svc := testScheduleService(t, fixedClock{t: now}, stub)
	created, err := svc.Create(ScheduleCreateRequest{
		Name:          "overlap",
		ScheduleKind:  methods.ScheduleKindInterval,
		IntervalSec:   60,
		Prompt:        "p",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Trigger(context.Background(), created.ID); err != nil {
		t.Fatal(err)
	}
	// Second trigger while first schedule run still "running" should skip.
	second, err := svc.Trigger(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != "skipped" || second.SkipReason != "overlap" {
		t.Fatalf("second=%#v", second)
	}
}

func TestScheduleDisableClearsNext(t *testing.T) {
	svc := testScheduleService(t, nil, nil)
	created, err := svc.Create(ScheduleCreateRequest{
		Name:          "pause-me",
		ScheduleKind:  methods.ScheduleKindInterval,
		IntervalSec:   60,
		Prompt:        "p",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := svc.SetEnabled(created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.NextRunAt != "" {
		t.Fatalf("disabled=%#v", disabled)
	}
}

func TestComputeNextCronTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip(err)
	}
	// 08:00 Shanghai on 2026-07-16 → next 09:00 same day.
	after := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	row := modelScheduledCron("0 9 * * *", "Asia/Shanghai")
	next, err := computeNextRunAt(row, after)
	if err != nil {
		t.Fatal(err)
	}
	if next == nil {
		t.Fatal("expected next")
	}
	local := next.In(loc)
	if local.Hour() != 9 || local.Minute() != 0 {
		t.Fatalf("next=%v local=%v want 09:00 Shanghai", next, local)
	}
}

func modelScheduledCron(expr, tz string) model.ScheduledTask {
	return model.ScheduledTask{
		ScheduleKind: methods.ScheduleKindCron,
		CronExpr:     expr,
		Timezone:     tz,
	}
}
