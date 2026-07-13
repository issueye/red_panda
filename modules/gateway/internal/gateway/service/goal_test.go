package service

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

func newGoalTestService(t *testing.T) (GoalService, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "goals.db")), &gorm.Config{})
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
	session, err := repos.Sessions.Create("goal-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewGoalService(repos), session.ID
}

func TestGoalWriteActivateCheckpointComplete(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID:      "run_1",
		SessionID:  sessionID,
		ToolCallID: "tc_1",
		ToolName:   "goal.write",
		Arguments: map[string]any{
			"objective":        "实现登录",
			"success_criteria": "测试通过",
			"analysis_summary": "用户要登录",
			"activate":         true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if write.Goal == nil || write.Goal.Status != "active" {
		t.Fatalf("write: %#v", write.Goal)
	}
	goalID := write.Goal.ID

	// Second active rejected
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_2", SessionID: sessionID, ToolCallID: "tc_2", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	}); err == nil {
		t.Fatal("expected second active rejected")
	}

	cp, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_1", SessionID: sessionID, ToolCallID: "tc_3", ToolName: "goal.checkpoint",
		Arguments: map[string]any{"goal_id": goalID, "summary": "完成一半", "pipeline_phase": "execute"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cp.Goal.CheckpointSummary != "完成一半" || cp.Goal.PipelinePhase != "execute" {
		t.Fatalf("checkpoint: %#v", cp.Goal)
	}

	done, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_1", SessionID: sessionID, ToolCallID: "tc_4", ToolName: "goal.complete",
		Arguments: map[string]any{
			"goal_id": goalID, "status": "succeeded", "summary": "全部完成",
			"report_markdown": "## 报告\nok",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Goal.Status != "succeeded" {
		t.Fatalf("complete: %#v", done.Goal)
	}

	// Cancel is no-op on terminal
	cancelled, err := svc.CancelGoal(sessionID, goalID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != "succeeded" {
		t.Fatalf("cancel terminal should keep status: %#v", cancelled)
	}
}

func TestGoalPauseByRunAndBindContinue(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_a", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{
			"objective": "任务", "success_criteria": "完成", "activate": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.PauseByRun("run_a", "awaiting_continue"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paused" || got.PauseReason != "awaiting_continue" {
		t.Fatalf("pause: %#v", got)
	}
	bound, err := svc.BindToRun(sessionID, "run_b", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Status != "active" || bound.ID != write.Goal.ID {
		t.Fatalf("bind continue: %#v", bound)
	}
}

func TestGoalContinueRejectsActiveAndTerminal(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_c", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Continue via RunService without runtime should fail on client not configured —
	// here we only assert Get + status gates for Cancel path after complete.
	if err := svc.PauseByRun("run_c", "awaiting_continue"); err != nil {
		t.Fatal(err)
	}
	paused, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil || paused.Status != "paused" {
		t.Fatalf("paused: %#v %v", paused, err)
	}
	// Active goal exists path for second write already covered; cancel then complete.
	if _, err := svc.CancelGoal(sessionID, write.Goal.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, _ := svc.Get(sessionID, write.Goal.ID)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancel status = %s", cancelled.Status)
	}
}

func TestGoalSegmentEndBudgetFail(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_s", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Force tiny budget
	row, _ := svc.repos.Goals.Get(write.Goal.ID)
	row.MaxTotalToolTurns = 5
	row.UsedToolTurns = 0
	_, _ = svc.repos.Goals.Update(row)

	res, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_s", SessionID: sessionID, ToolCallID: "t2", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 0, "delta_tool_turns": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Goal.Status != "failed" || res.Goal.FailReason != "budget_exhausted" {
		t.Fatalf("budget: %#v", res.Goal)
	}
}

func TestGoalMutationRequiresCurrentBoundRun(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"goal.checkpoint", "goal.complete"} {
		args := map[string]any{"goal_id": write.Goal.ID, "summary": "not allowed"}
		if tool == "goal.complete" {
			args["status"] = "succeeded"
		}
		if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
			RunID: "run_other", SessionID: sessionID, ToolCallID: "other", ToolName: tool, Arguments: args,
		}); err == nil {
			t.Fatalf("%s from unbound run should fail", tool)
		}
	}
}

func TestGoalSegmentEndIsIdempotent(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_seg", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	params := methods.GoalToolExecuteParams{
		RunID: "run_seg", SessionID: sessionID, ToolCallID: "segment-0", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 0, "delta_tool_turns": 3},
	}
	first, err := svc.ExecuteRuntimeTool(params)
	if err != nil {
		t.Fatal(err)
	}
	if first.Goal == nil || first.Goal.UsedToolTurns != 3 {
		t.Fatalf("first segment: %#v", first.Goal)
	}
	// Same tool call id / same segment key replay must not double-count.
	params.ToolCallID = "segment-0-retry"
	if _, err := svc.ExecuteRuntimeTool(params); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedToolTurns != 3 || got.UsedSegments != 1 {
		t.Fatalf("duplicate segment counted: %#v", got)
	}
	n, err := svc.repos.Goals.CountSegmentsForRun(write.Goal.ID, "run_seg")
	if err != nil || n != 1 {
		t.Fatalf("ledger rows = %d err=%v", n, err)
	}
}

func TestSegmentEndRejectsUnboundRunAndTerminalNewSegment(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner_seg", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_other_seg", SessionID: sessionID, ToolCallID: "bad", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 0, "delta_tool_turns": 2},
	}); err == nil {
		t.Fatal("unbound run should not record segment")
	}
	// Record once then complete; new segment index must fail, replay of old is OK.
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner_seg", SessionID: sessionID, ToolCallID: "s0", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 0, "delta_tool_turns": 2},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner_seg", SessionID: sessionID, ToolCallID: "done", ToolName: "goal.complete",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "status": "succeeded", "summary": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner_seg", SessionID: sessionID, ToolCallID: "late-new", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 1, "delta_tool_turns": 4},
	}); err == nil {
		t.Fatal("new segment on terminal goal should fail")
	}
	// Idempotent replay of segment 0 after terminal is allowed and must not change counters.
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_owner_seg", SessionID: sessionID, ToolCallID: "late-replay", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "segment_index": 0, "delta_tool_turns": 2},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "succeeded" || got.UsedToolTurns != 2 || got.UsedSegments != 1 {
		t.Fatalf("terminal counters mutated: %#v", got)
	}
}

func TestSegmentEndRequiresSegmentIndex(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_idx", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_idx", SessionID: sessionID, ToolCallID: "no-idx", ToolName: "segment_end",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "delta_tool_turns": 1},
	}); err == nil {
		t.Fatal("missing segment_index should fail")
	}
}

func TestBindRepairsStaleActiveBeforeSelectingGoal(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "missing_old_run", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := svc.BindToRun(sessionID, "new_run", write.Goal.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Status != "active" || bound.ActiveRunID != "new_run" {
		t.Fatalf("stale goal was not rebound: %#v", bound)
	}
}

func TestTerminalGoalCannotBeCompletedAgain(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_terminal", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	params := methods.GoalToolExecuteParams{
		RunID: "run_terminal", SessionID: sessionID, ToolCallID: "t2", ToolName: "goal.complete",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "status": "succeeded", "summary": "done"},
	}
	if _, err := svc.ExecuteRuntimeTool(params); err != nil {
		t.Fatal(err)
	}
	params.ToolCallID = "t3"
	params.Arguments["status"] = "failed"
	if _, err := svc.ExecuteRuntimeTool(params); err == nil {
		t.Fatal("terminal goal should reject a second completion")
	}
	got, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil || got.Status != "succeeded" {
		t.Fatalf("terminal status was overwritten: %#v %v", got, err)
	}
}

func TestDatabaseRejectsSecondActiveGoal(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	if _, err := svc.repos.Goals.Create(model.Goal{
		SessionID: sessionID, Objective: "first", SuccessCriteria: "done", Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repos.Goals.Create(model.Goal{
		SessionID: sessionID, Objective: "second", SuccessCriteria: "done", Status: "active",
	}); err == nil {
		t.Fatal("database should enforce one active goal per session")
	}
}

func TestPendingAndPausedCannotCheckpointOrComplete(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	// Pending (no activate)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_pending", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "later", "success_criteria": "done", "activate": false},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"goal.checkpoint", "goal.complete"} {
		args := map[string]any{"goal_id": write.Goal.ID, "summary": "nope"}
		if tool == "goal.complete" {
			args["status"] = "succeeded"
		}
		if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
			RunID: "run_pending", SessionID: sessionID, ToolCallID: tool, ToolName: tool, Arguments: args,
		}); err == nil {
			t.Fatalf("%s on pending goal should fail", tool)
		}
	}

	// Activate then pause
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_active", SessionID: sessionID, ToolCallID: "act", ToolName: "goal.update",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "activate": true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PauseByRun("run_active", "awaiting_continue"); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"goal.checkpoint", "goal.complete"} {
		args := map[string]any{"goal_id": write.Goal.ID, "summary": "nope"}
		if tool == "goal.complete" {
			args["status"] = "succeeded"
		}
		if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
			RunID: "run_active", SessionID: sessionID, ToolCallID: "p-" + tool, ToolName: tool, Arguments: args,
		}); err == nil {
			t.Fatalf("%s on paused goal should fail", tool)
		}
	}
}

func TestToolCancelSetsCancelRunID(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cancel_tool", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wrong run cannot cancel active goal
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "other_run", SessionID: sessionID, ToolCallID: "bad", ToolName: "goal.update",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "action": "cancel"},
	}); err == nil {
		t.Fatal("expected cancel from unbound run to fail")
	}
	res, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cancel_tool", SessionID: sessionID, ToolCallID: "ok", ToolName: "goal.update",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "action": "cancel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Goal == nil || res.Goal.Status != "cancelled" {
		t.Fatalf("cancel goal: %#v", res.Goal)
	}
	if res.CancelRunID != "run_cancel_tool" {
		t.Fatalf("CancelRunID = %q, want run_cancel_tool", res.CancelRunID)
	}
	// Second cancel is idempotent and does not re-request run cancel
	again, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cancel_tool", SessionID: sessionID, ToolCallID: "again", ToolName: "goal.update",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "action": "cancel"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.CancelRunID != "" {
		t.Fatalf("idempotent cancel should not set CancelRunID, got %q", again.CancelRunID)
	}
}

func TestCheckpointCASRejectsAfterComplete(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cas", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cas", SessionID: sessionID, ToolCallID: "t2", ToolName: "goal.complete",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "status": "succeeded", "summary": "done"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_cas", SessionID: sessionID, ToolCallID: "t3", ToolName: "goal.checkpoint",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "summary": "late"},
	}); err == nil {
		t.Fatal("checkpoint after complete should fail")
	}
	got, err := svc.Get(sessionID, write.Goal.ID)
	if err != nil || got.Status != "succeeded" {
		t.Fatalf("status overwritten: %#v %v", got, err)
	}
	if got.CheckpointSummary == "late" {
		t.Fatal("late checkpoint mutated terminal goal")
	}
}

func TestCancelGoalIdempotentOnTerminal(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	write, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_idem", SessionID: sessionID, ToolCallID: "t1", ToolName: "goal.write",
		Arguments: map[string]any{"objective": "x", "success_criteria": "y", "activate": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_idem", SessionID: sessionID, ToolCallID: "t2", ToolName: "goal.complete",
		Arguments: map[string]any{"goal_id": write.Goal.ID, "status": "failed", "summary": "boom"},
	}); err != nil {
		t.Fatal(err)
	}
	dto, err := svc.CancelGoal(sessionID, write.Goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if dto.Status != "failed" {
		t.Fatalf("cancel must not overwrite terminal status, got %s", dto.Status)
	}
}

func TestCreateUserInitiatedAndBind(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	created, err := svc.CreateUserInitiated(sessionID, "实现登录", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" {
		t.Fatalf("expected pending, got %s", created.Status)
	}
	if created.Objective != "实现登录" {
		t.Fatalf("objective = %q", created.Objective)
	}
	if created.SuccessCriteria == "" {
		t.Fatal("default success criteria expected")
	}
	if created.PipelinePhase != "analyze" {
		t.Fatalf("phase = %q", created.PipelinePhase)
	}
	// Keep a live run so RepairStaleActive does not pause the bound goal.
	if err := svc.repos.Runs.Start(model.RunRecord{
		ID: "run_user_goal", SessionID: sessionID, Status: "running",
	}); err != nil {
		t.Fatal(err)
	}
	bound, err := svc.BindToRun(sessionID, "run_user_goal", created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Status != "active" || bound.ActiveRunID != "run_user_goal" {
		t.Fatalf("bind failed: %#v", bound)
	}
	// Second user create while active should fail.
	if _, err := svc.CreateUserInitiated(sessionID, "另一个目标", "", ""); err == nil {
		t.Fatal("expected error when active goal exists")
	}
}
