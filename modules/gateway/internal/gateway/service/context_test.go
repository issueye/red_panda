package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

func newContextTestService(t *testing.T) (ContextService, GoalService, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ctx.db")), &gorm.Config{})
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
	session, err := repos.Sessions.Create("ctx-test", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewContextService(repos), NewGoalService(repos), session.ID
}

// createActiveGoal creates a V2 contract bound to runID.
func createActiveGoal(t *testing.T, goalSvc GoalService, sessionID, runID string) string {
	t.Helper()
	write, err := goalSvc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: runID, SessionID: sessionID, ToolCallID: "g_" + runID, ToolName: "goal.create",
		Arguments: map[string]any{
			"objective": "test goal", "criteria": []any{
				map[string]any{"id": "behavior", "description": "The behavior works"},
				map[string]any{"id": "quality", "description": "Relevant tests pass"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return write.Goal.ID
}

func TestContextWriteAndRead(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	// Root writes a finding.
	w, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "n1", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "finding", "title": "Found bug", "body": "nil deref in parser"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "completed" || len(w.Notes) != 1 || w.Notes[0].Title != "Found bug" {
		t.Fatalf("write result: %+v", w)
	}
	if w.Notes[0].Source != "root" {
		t.Fatalf("source = %s, want root", w.Notes[0].Source)
	}

	// A specialist child (different run id) reads the shared note.
	r, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_child", SessionID: session, ToolCallID: "n2", ToolName: "context.read",
		Arguments: map[string]any{"goal_id": goalID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) != 1 || r.Notes[0].Title != "Found bug" {
		t.Fatalf("child should see root's note: %+v", r.Notes)
	}
}

func TestContextSpecialistCanWriteButRootOwnsGoalState(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	// A specialist child writes a handoff note — allowed (scratchpad is shared).
	w, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_child", SessionID: session, ToolCallID: "h1", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "handoff", "title": "Implementation done", "body": "see diff"},
	})
	if err != nil {
		t.Fatalf("specialist write should succeed: %v", err)
	}
	if w.Notes[0].Source != "specialist:run_child" {
		t.Fatalf("source = %s, want specialist:run_child", w.Notes[0].Source)
	}
}

func TestContextWriteRejectsTerminalGoal(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	// Complete the goal → terminal.
	prepareGoalForCompletion(t, goalSvc, session, goalID)
	_, err := goalSvc.ExecuteRuntimeTool(methods.GoalToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "gc", ToolName: "goal.finish",
		Arguments: goalCompletionArgs(goalID, "succeeded", "Goal accomplished"),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Writing to a terminal goal must be rejected.
	_, err = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "n3", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "note", "title": "late", "body": "x"},
	})
	if err == nil {
		t.Fatal("write to terminal goal should fail")
	}
}

func TestContextReplaceUpserts(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	args := map[string]any{"goal_id": goalID, "kind": "decision", "title": "Use SQLite", "body": "v1"}
	first, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "r1", ToolName: "context.replace", Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	args["body"] = "v2-updated"
	second, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "r2", ToolName: "context.replace", Arguments: args,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Same seq (updated in place), body changed.
	if second.Notes[0].Seq != first.Notes[0].Seq {
		t.Fatalf("replace changed seq: %d vs %d", first.Notes[0].Seq, second.Notes[0].Seq)
	}
	if second.Notes[0].Body != "v2-updated" {
		t.Fatalf("body not updated: %s", second.Notes[0].Body)
	}
}

func TestContextSearchAndDelete(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	_, _ = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "s1", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "risk", "title": "API rate limit", "body": "may hit 429"},
	})
	s, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "s2", ToolName: "context.search",
		Arguments: map[string]any{"goal_id": goalID, "query": "rate"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Notes) != 1 {
		t.Fatalf("search should find 1 note, got %d", len(s.Notes))
	}
	noteID := s.Notes[0].ID

	_, err = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "s3", ToolName: "context.delete",
		Arguments: map[string]any{"goal_id": goalID, "note_id": noteID},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Reading back should now be empty.
	r, _ := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "s4", ToolName: "context.read",
		Arguments: map[string]any{"goal_id": goalID},
	})
	if len(r.Notes) != 0 {
		t.Fatalf("note should be deleted, got %d", len(r.Notes))
	}
}

func TestContextReadPinnedFirstOrder(t *testing.T) {
	// Single inject path is Runtime fetchGoalNotes → context.read (not a parallel
	// AutoInjectNotes service API). List order must keep pinned notes first.
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	_, _ = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "p1", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "decision", "title": "Pinned", "body": "important", "pinned": true},
	})
	_, _ = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "p2", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "finding", "title": "Regular", "body": "ok"},
	})

	r, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "p3", ToolName: "context.read",
		Arguments: map[string]any{"goal_id": goalID, "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) != 2 {
		t.Fatalf("read should return 2 notes, got %d", len(r.Notes))
	}
	if r.Notes[0].Pinned == 0 || r.Notes[0].Title != "Pinned" {
		t.Fatalf("pinned note should be first: %+v", r.Notes[0])
	}
}

// Desktop C4 projection: ListNotes is the HTTP-facing read of the same scratchpad.
func TestContextListNotesForDesktop(t *testing.T) {
	ctxSvc, goalSvc, session := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, session, "run_root")

	_, err := ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_root", SessionID: session, ToolCallID: "desk1", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "finding", "title": "UI visible", "body": "scratchpad body"},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := ctxSvc.ListNotes(session, goalID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("ListNotes count = %d, want 1", len(items))
	}
	if items[0].Title != "UI visible" || items[0].Body != "scratchpad body" {
		t.Fatalf("unexpected note: %+v", items[0])
	}

	if _, err := ctxSvc.ListNotes(session, "missing", 0); err == nil {
		t.Fatal("expected missing goal error")
	}
	other, err := ctxSvc.repos.Sessions.Create("other", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ctxSvc.ListNotes(other.ID, goalID, 0); err == nil {
		t.Fatal("expected cross-session rejection")
	}
}

func TestContextReadRejectsCrossSessionGoal(t *testing.T) {
	ctxSvc, goalSvc, sessionA := newContextTestService(t)
	goalID := createActiveGoal(t, goalSvc, sessionA, "run_a")

	_, _ = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_a", SessionID: sessionA, ToolCallID: "n_cross", ToolName: "context.write",
		Arguments: map[string]any{"goal_id": goalID, "kind": "finding", "title": "secret", "body": "do not leak"},
	})

	// Second session in the same DB.
	sessionB, err := ctxSvc.repos.Sessions.Create("other-session", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_b", SessionID: sessionB.ID, ToolCallID: "n_read", ToolName: "context.read",
		Arguments: map[string]any{"goal_id": goalID},
	})
	if err == nil {
		t.Fatal("cross-session context.read should fail")
	}
	if !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = ctxSvc.ExecuteRuntimeTool(methods.ContextToolExecuteParams{
		RunID: "run_b", SessionID: sessionB.ID, ToolCallID: "n_search", ToolName: "context.search",
		Arguments: map[string]any{"goal_id": goalID, "query": "secret"},
	})
	if err == nil {
		t.Fatal("cross-session context.search should fail")
	}
}
