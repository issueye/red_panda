package service

import (
	"path/filepath"
	"testing"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

func newTodoTestService(t *testing.T) (TodoService, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "todo.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repos := repository.NewSet(db)
	session, err := repos.Sessions.Create("test", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return NewTodoService(repos), session.ID
}

func TestTodoWriteMergeAndGatewayPKEcho(t *testing.T) {
	svc, sessionID := newTodoTestService(t)

	first, err := svc.ExecuteRuntimeTool(methods.TodoToolExecuteParams{
		RunID:      "run_1",
		SessionID:  sessionID,
		ToolCallID: "tool_1",
		ToolName:   "todo.write",
		Arguments: map[string]any{
			"merge": true,
			"todos": []any{
				map[string]any{"id": "1", "content": "read design", "status": "completed"},
				map[string]any{"id": "2", "content": "implement gateway", "status": "in_progress"},
			},
		},
	})
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if len(first.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(first.Items))
	}
	if first.Items[0].ClientKey != "1" || first.Items[1].ClientKey != "2" {
		t.Fatalf("client keys: %#v", first.Items)
	}
	pk2 := first.Items[1].ID
	if pk2 == "" || pk2 == "2" {
		t.Fatalf("expected gateway pk, got %q", pk2)
	}

	// Second write echoes Gateway PK as id — must update same row, not insert.
	second, err := svc.ExecuteRuntimeTool(methods.TodoToolExecuteParams{
		RunID:      "run_2",
		SessionID:  sessionID,
		ToolCallID: "tool_2",
		ToolName:   "todo.write",
		Arguments: map[string]any{
			"merge": true,
			"todos": []any{
				map[string]any{"id": first.Items[0].ID, "content": "read design", "status": "completed"},
				map[string]any{"id": pk2, "content": "implement gateway done", "status": "completed"},
				map[string]any{"id": "3", "content": "desktop strip", "status": "pending"},
			},
		},
	})
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if len(second.Items) != 3 {
		t.Fatalf("expected 3 items after echo write, got %d: %#v", len(second.Items), second.Items)
	}
	found := false
	for _, item := range second.Items {
		if item.ID == pk2 {
			found = true
			if item.Content != "implement gateway done" {
				t.Fatalf("pk row content not updated: %#v", item)
			}
			if item.ClientKey != "2" {
				t.Fatalf("client_key should be preserved, got %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("gateway pk row missing after echo write: %#v", second.Items)
	}

	// Short client key still updates.
	third, err := svc.ExecuteRuntimeTool(methods.TodoToolExecuteParams{
		RunID:      "run_3",
		SessionID:  sessionID,
		ToolCallID: "tool_3",
		ToolName:   "todo.write",
		Arguments: map[string]any{
			"merge": true,
			"todos": []any{
				map[string]any{"id": "3", "content": "desktop strip updated", "status": "in_progress"},
			},
		},
	})
	if err != nil {
		t.Fatalf("third write: %v", err)
	}
	if len(third.Items) != 3 {
		t.Fatalf("merge should keep unlisted items, got %d", len(third.Items))
	}
	var item3 methods.TodoItemDTO
	for _, item := range third.Items {
		if item.ClientKey == "3" {
			item3 = item
		}
	}
	if item3.Content != "desktop strip updated" || item3.Status != "in_progress" {
		t.Fatalf("client key merge failed: %#v", third.Items)
	}
}

func TestTodoWriteReplaceAndMaxInProgress(t *testing.T) {
	svc, sessionID := newTodoTestService(t)

	res, err := svc.ExecuteRuntimeTool(methods.TodoToolExecuteParams{
		RunID:      "run_1",
		SessionID:  sessionID,
		ToolCallID: "tool_1",
		ToolName:   "todo.write",
		Arguments: map[string]any{
			"merge": false,
			"todos": []any{
				map[string]any{"id": "a", "content": "one", "status": "in_progress"},
				map[string]any{"id": "b", "content": "two", "status": "in_progress"},
			},
		},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	inProgress := 0
	for _, item := range res.Items {
		if item.Status == "in_progress" {
			inProgress++
		}
	}
	if inProgress != 1 {
		t.Fatalf("expected single in_progress, got %#v", res.Items)
	}
}

