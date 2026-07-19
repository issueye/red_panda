package service

import (
	"path/filepath"
	"testing"
	"time"

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

func createGoalV2(t *testing.T, svc GoalService, sessionID, runID string, extra map[string]any) methods.GoalDTO {
	t.Helper()
	args := map[string]any{
		"title": "Ship outcome", "objective": "Implement and prove the requested behavior",
		"criteria": []any{
			map[string]any{"id": "behavior", "description": "The behavior works"},
			map[string]any{"id": "quality", "description": "Relevant tests pass"},
		},
	}
	for key, value := range extra {
		args[key] = value
	}
	result, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: runID, SessionID: sessionID, ToolCallID: "create_" + runID,
		ToolName: "goal.create", Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Goal == nil {
		t.Fatal("goal.create returned no goal")
	}
	return *result.Goal
}

func satisfiedAssessmentArgs(goalID string) map[string]any {
	return map[string]any{
		"goal_id": goalID, "verdict": "satisfied", "summary": "All outcomes verified",
		"evidence": "behavior test and quality checks passed",
		"criteria": []any{
			map[string]any{"id": "behavior", "status": "met", "evidence": "integration test passed"},
			map[string]any{"id": "quality", "status": "met", "evidence": "test suite passed"},
		},
	}
}

func prepareGoalForCompletion(t *testing.T, svc GoalService, sessionID, goalID string) {
	t.Helper()
	_, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "assessment_" + goalID,
		ToolName: "goal.assess", Arguments: satisfiedAssessmentArgs(goalID),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func goalCompletionArgs(goalID, status, summary string) map[string]any {
	return map[string]any{
		"goal_id": goalID, "status": status, "summary": summary,
		"report_markdown": "## Outcome\n" + summary,
	}
}

func TestGoalV2FeedbackLoopCompletesWithoutSessionTodos(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_root", map[string]any{
		"constraints": []any{"Keep public API stable"}, "strategy": "Start with the highest-risk behavior",
	})
	if goal.Status != "active" || len(goal.Criteria) != 2 || goal.MaxIterations != 20 {
		t.Fatalf("created goal = %#v", goal)
	}

	planned, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "plan", ToolName: "goal.plan",
		Arguments: map[string]any{
			"goal_id": goal.ID, "strategy": "Implement then run focused tests", "decision": "This closes both criteria",
			"actions": []any{
				map[string]any{"key": "implement", "title": "Implement behavior", "acceptance": "Focused behavior works", "status": "active"},
				map[string]any{"key": "test", "title": "Run quality checks", "acceptance": "Relevant suite passes", "status": "queued"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Goal.Actions) != 2 || planned.Goal.CurrentAction != "Implement behavior" {
		t.Fatalf("planned goal = %#v", planned.Goal)
	}

	_, err = svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "observe", ToolName: "goal.observe",
		Arguments: map[string]any{
			"goal_id": goal.ID, "action_id": "implement", "action_status": "done",
			"observation": "Behavior implemented", "evidence": "focused integration test passes",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	afterObserve, err := svc.Get(sessionID, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterObserve.CurrentActionID != "" || afterObserve.Actions[0].Status != "done" {
		t.Fatalf("completed action should release focus: %#v", afterObserve)
	}

	progress, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "assess_progress", ToolName: "goal.assess",
		Arguments: map[string]any{
			"goal_id": goal.ID, "verdict": "progress", "summary": "Behavior works but quality suite remains",
			"gap": "Run the relevant suite", "decision": "Activate the quality action", "evidence": "focused test passed",
			"criteria": []any{
				map[string]any{"id": "behavior", "status": "met", "evidence": "integration test passed"},
				map[string]any{"id": "quality", "status": "not_met", "evidence": "suite has not run yet"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.Goal.Iteration != 1 || progress.Goal.LastAssessment.Verdict != "progress" {
		t.Fatalf("progress assessment = %#v", progress.Goal)
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "finish_early", ToolName: "goal.finish",
		Arguments: goalCompletionArgs(goal.ID, "succeeded", "too early"),
	}); err == nil {
		t.Fatal("finish before satisfied assessment should fail")
	}

	assessed, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "assess_done", ToolName: "goal.assess",
		Arguments: satisfiedAssessmentArgs(goal.ID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if assessed.Goal.LastAssessment.Verdict != "satisfied" {
		t.Fatalf("assessment = %#v", assessed.Goal.LastAssessment)
	}
	finished, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "finish", ToolName: "goal.finish",
		Arguments: goalCompletionArgs(goal.ID, "succeeded", "Outcome delivered"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Goal.Status != "succeeded" || finished.Goal.OutcomeSummary != "Outcome delivered" {
		t.Fatalf("finished goal = %#v", finished.Goal)
	}
	events, err := svc.repos.Goals.ListEvents(goal.ID, 20)
	if err != nil || len(events) < 5 {
		t.Fatalf("events = %d, err=%v", len(events), err)
	}
	journal, err := svc.ListJournal(sessionID, goal.ID)
	if err != nil || len(journal) != len(events) || journal[0].Kind != "finished" {
		t.Fatalf("journal = %#v, err=%v", journal, err)
	}
}

func TestGoalV2RejectsLegacyPipelineTools(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	for _, tool := range []string{"goal.write", "goal.update", "goal.checkpoint", "goal.complete"} {
		if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
			RunID: "run_root", SessionID: sessionID, ToolCallID: tool, ToolName: tool,
			Arguments: map[string]any{"objective": "legacy"},
		}); err == nil {
			t.Fatalf("legacy tool %s should be rejected", tool)
		}
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "legacy_criteria", ToolName: "goal.create",
		Arguments: map[string]any{"objective": "legacy", "success_criteria": "old alias"},
	}); err == nil {
		t.Fatal("goal.create should reject the legacy success_criteria alias")
	}
}

func TestGoalV2ControllerLimitsAndActionStatesAreValidated(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_root", map[string]any{
		"max_iterations": 1000, "max_stagnation": 100,
	})
	if goal.MaxIterations != 100 || goal.MaxStagnation != 10 {
		t.Fatalf("controller limits were not clamped: %#v", goal)
	}
	_, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "bad_plan", ToolName: "goal.plan",
		Arguments: map[string]any{
			"goal_id": goal.ID,
			"actions": []any{map[string]any{"key": "bad", "title": "Bad action", "acceptance": "none", "status": "running"}},
		},
	})
	if err == nil {
		t.Fatal("invalid action status should be rejected")
	}
}

func TestGoalV2AssessmentRequiresEveryCriterion(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_root", nil)
	_, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "partial", ToolName: "goal.assess",
		Arguments: map[string]any{
			"goal_id": goal.ID, "verdict": "satisfied", "summary": "claimed", "evidence": "some evidence",
			"criteria": []any{map[string]any{"id": "behavior", "status": "met", "evidence": "passed"}},
		},
	})
	if err == nil {
		t.Fatal("partial criterion assessment should fail")
	}

	args := satisfiedAssessmentArgs(goal.ID)
	args["criteria"] = []any{
		map[string]any{"id": "behavior", "status": "met", "evidence": "passed"},
		map[string]any{"id": "quality", "status": "not_met", "evidence": "still failing"},
	}
	if _, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "unmet", ToolName: "goal.assess", Arguments: args,
	}); err == nil {
		t.Fatal("satisfied with unmet criterion should fail")
	}
}

func TestGoalV2StagnationPausesForIntervention(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_root", map[string]any{"max_stagnation": 2})
	for i := 0; i < 2; i++ {
		result, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
			RunID: "run_root", SessionID: sessionID, ToolCallID: "stagnant", ToolName: "goal.assess",
			Arguments: map[string]any{
				"goal_id": goal.ID, "verdict": "no_progress", "summary": "Approach did not reduce the gap", "evidence": "same failure remains",
				"criteria": []any{
					map[string]any{"id": "behavior", "status": "not_met", "evidence": "failure remains"},
					map[string]any{"id": "quality", "status": "not_met", "evidence": "tests fail"},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 && (result.Goal.Status != "paused" || result.Goal.PauseReason != "stagnated") {
			t.Fatalf("stagnated goal = %#v", result.Goal)
		}
	}
}

func TestGoalV2MutationsRequireBoundRun(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_owner", nil)
	_, err := svc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_other", SessionID: sessionID, ToolCallID: "wrong", ToolName: "goal.plan",
		Arguments: map[string]any{"goal_id": goal.ID, "strategy": "steal lease"},
	})
	if err == nil {
		t.Fatal("unbound run should not mutate goal")
	}
}

func TestGoalV2SegmentLedgerRemainsIdempotent(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	goal := createGoalV2(t, svc, sessionID, "run_root", nil)
	params := methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: sessionID, ToolCallID: "segment", ToolName: methods.InternalGoalSegmentEnd,
		Arguments: map[string]any{"goal_id": goal.ID, "segment_index": 0, "delta_tool_turns": 7},
	}
	if _, err := svc.ExecuteRuntimeTool(params); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExecuteRuntimeTool(params); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(sessionID, goal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedToolTurns != 7 || got.UsedSegments != 1 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestGoalV2DatabaseRejectsSecondActiveGoal(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	createGoalV2(t, svc, sessionID, "run_root", nil)
	now := time.Now().UTC()
	_, err := svc.repos.Goals.Create(model.Goal{SessionID: sessionID, Objective: "second", Status: "active", CreatedAt: now, UpdatedAt: now})
	if err == nil {
		t.Fatal("database should reject a second active goal")
	}
}

func TestGoalV2UserInitiatedBindAndCancel(t *testing.T) {
	svc, sessionID := newGoalTestService(t)
	created, err := svc.CreateUserInitiated(sessionID, "Deliver result", "", "Result is verified")
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != "pending" || len(created.Criteria) != 1 {
		t.Fatalf("created = %#v", created)
	}
	if err := svc.repos.Runs.Start(model.RunRecord{ID: "run_bind", SessionID: sessionID, Status: "running"}); err != nil {
		t.Fatal(err)
	}
	bound, err := svc.BindToRun(sessionID, "run_bind", created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Status != "active" || bound.ActiveRunID != "run_bind" {
		t.Fatalf("bound = %#v", bound)
	}
	cancelled, err := svc.CancelGoal(sessionID, created.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancelled=%#v err=%v", cancelled, err)
	}
}
