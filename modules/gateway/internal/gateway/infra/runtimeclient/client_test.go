package runtimeclient

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func TestClientHandlesRuntimeOriginatedRequest(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	client := New("", nil, "test", nil, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != methods.MemoryToolExecute {
			t.Fatalf("method = %s, want %s", method, methods.MemoryToolExecute)
		}
		var req methods.MemoryToolExecuteParams
		if err := json.Unmarshal(params, &req); err != nil {
			t.Fatal(err)
		}
		if req.ToolName != "memory.list" {
			t.Fatalf("tool name = %s, want memory.list", req.ToolName)
		}
		return methods.MemoryToolExecuteResult{Status: "completed", Output: "[]"}, nil
	})
	client.running = true
	client.stdin = writer

	request, err := jsonrpc.NewRequest("rt_1", methods.MemoryToolExecute, methods.MemoryToolExecuteParams{
		RunID:      "run_1",
		ToolCallID: "tool_1",
		ToolName:   "memory.list",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		client.handleRequest(raw)
		close(done)
	}()

	var resp jsonrpc.Response
	if err := json.NewDecoder(reader).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	<-done
	if resp.ID != "rt_1" || resp.Error != nil {
		t.Fatalf("unexpected response: %#v", resp)
	}
	var result methods.MemoryToolExecuteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Output != "[]" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
