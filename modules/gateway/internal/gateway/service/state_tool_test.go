package service

import (
	"path/filepath"
	"testing"

	"redpanda/gateway/internal/gateway/infra/database"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
)

func newStateToolTestSet(t *testing.T) (Set, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state-tool.db")
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
	session, err := repos.Sessions.Create("state-tool", t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return NewSet(Options{Repos: repos}), session.ID
}

func TestDispatchStateToolTodoList(t *testing.T) {
	services, sessionID := newStateToolTestSet(t)

	// Seed via domain method, read via unified dispatcher.
	if _, err := services.Todo.ExecuteRuntimeTool(methods.TodoToolExecuteParams{
		RunID: "run_seed", SessionID: sessionID, ToolCallID: "seed", ToolName: "todo.write",
		Arguments: map[string]any{
			"merge": false,
			"todos": []any{
				map[string]any{"id": "1", "content": "via state.tool", "status": "pending"},
			},
		},
	}); err != nil {
		t.Fatalf("seed todo: %v", err)
	}

	raw, err := services.DispatchStateTool(methods.StateToolExecuteParams{
		// Domain omitted — inferred from tool_name (docs/41 W2-3).
		RunID: "run_list", SessionID: sessionID, ToolCallID: "list", ToolName: "todo.list",
	})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	result, ok := raw.(methods.TodoToolExecuteResult)
	if !ok {
		t.Fatalf("result type = %T, want TodoToolExecuteResult", raw)
	}
	if result.Status != RuntimeToolStatusCompleted || len(result.Items) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Items[0].Content != "via state.tool" {
		t.Fatalf("content = %q", result.Items[0].Content)
	}
}

func TestDispatchStateToolRejectsUnknownDomain(t *testing.T) {
	services, sessionID := newStateToolTestSet(t)
	_, err := services.DispatchStateTool(methods.StateToolExecuteParams{
		Domain: "filesystem", RunID: "run_1", SessionID: sessionID, ToolCallID: "tc", ToolName: "fs.read",
	})
	if err == nil {
		t.Fatal("expected domain error")
	}
}

func TestValidateStateToolMetaMemorySkipsSession(t *testing.T) {
	services, _ := newStateToolTestSet(t)
	domain, err := validateStateToolMeta(services.Memory.repos, methods.StateToolExecuteParams{
		Domain: methods.StateToolDomainMemory, RunID: "run_1", ToolCallID: "tc", ToolName: "memory.list",
	})
	if err != nil {
		t.Fatalf("memory without session should validate: %v", err)
	}
	if domain != methods.StateToolDomainMemory {
		t.Fatalf("domain = %q", domain)
	}
}
