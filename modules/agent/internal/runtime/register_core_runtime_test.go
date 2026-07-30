package runtime

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func TestRuntimeRegistryBindsTodoWriteToGateway(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	defer rt.Close(context.Background())

	type executeResult struct {
		output string
		err    error
	}
	done := make(chan executeResult, 1)
	go func() {
		result, err := rt.registry.Execute(context.Background(), "todo.write", &registry.ToolContext{
			ToolCallID: "tool_todo_write",
			RunID:      "run_todo_write",
			SessionID:  "session_todo_write",
			WorkingDir: t.TempDir(),
		}, map[string]any{
			"todos": []any{map[string]any{"content": "verify binding", "status": "in_progress"}},
		})
		if result != nil {
			done <- executeResult{output: result.Output, err: err}
			return
		}
		done <- executeResult{err: err}
	}()

	var outbound jsonrpc.Request
	if err := json.NewDecoder(reader).Decode(&outbound); err != nil {
		t.Fatal(err)
	}
	if outbound.Method != methods.StateToolExecute {
		t.Fatalf("method = %q, want %q", outbound.Method, methods.StateToolExecute)
	}
	var params methods.StateToolExecuteParams
	if err := json.Unmarshal(outbound.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Domain != methods.StateToolDomainTodo || params.ToolCallID != "tool_todo_write" || params.ToolName != "todo.write" {
		t.Fatalf("unexpected state tool params: %#v", params)
	}

	response, err := jsonrpc.NewResult(outbound.ID, methods.TodoToolExecuteResult{
		Status: "completed",
		Output: `{"items":[{"id":"todo_1","content":"verify binding","status":"in_progress"}]}`,
		Items:  []methods.TodoItemDTO{{ID: "todo_1", Content: "verify binding", Status: "in_progress"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(context.Background(), rawResponse); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if !strings.Contains(result.output, "todo_1") {
			t.Fatalf("output = %q", result.output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for todo registry result")
	}
	if items := rt.getRunTodos("run_todo_write"); len(items) != 1 || items[0].ID != "todo_1" {
		t.Fatalf("runtime todos = %#v", items)
	}
}

func TestRuntimeRegistryOverridesWorkerPlaceholders(t *testing.T) {
	rt := New(strings.NewReader(""), io.Discard, io.Discard, "test")
	defer rt.Close(context.Background())

	result, err := rt.registry.Execute(context.Background(), "worker.pool_status", &registry.ToolContext{
		RunID: "run_worker_status",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Status != "completed" || !strings.Contains(result.Output, `"pool"`) {
		t.Fatalf("unexpected worker.pool_status result: %#v", result)
	}
	if strings.Contains(result.Output, "not yet migrated") {
		t.Fatalf("placeholder result leaked: %q", result.Output)
	}
}
