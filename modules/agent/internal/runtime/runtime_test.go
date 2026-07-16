package runtime

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"redpanda/agent/internal/worker"
	"strings"
	"testing"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

func TestCancelInterruptsPendingPermissionRun(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 16)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	sendRequest(t, ctx, rt, "run_1", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_cancel_test",
		Session: methods.ReplySession{
			ID: "session_cancel_test",
		},
		Input: methods.ReplyInput{
			Text: "please wait for permission",
		},
		Options: methods.RunExecuteOptions{
			RequirePermission: true,
		},
	})

	waitForResponse(t, lines, "run_1")
	waitForEventType(t, lines, events.EventPermissionRequest)
	var waiting bool
	for _, assignment := range rt.workerPool.Snapshot().Assignments {
		if assignment.RunID == "run_cancel_test" && assignment.Status == worker.AssignmentWaitingPermission {
			waiting = true
			break
		}
	}
	if !waiting {
		t.Fatalf("run assignment did not enter waiting_permission: %#v", rt.workerPool.Snapshot())
	}

	sendRequest(t, ctx, rt, "cancel_1", methods.RunCancel, methods.RunCancelParams{
		RunID:  "run_cancel_test",
		Reason: "test",
	})
	finish := waitForResponseAndEvent(t, lines, "cancel_1", events.EventFinish)
	if got := finish.Payload["status"]; got != "cancelled" {
		t.Fatalf("expected cancelled finish status, got %#v", got)
	}
	for _, assignment := range rt.workerPool.Snapshot().Assignments {
		if assignment.RunID == "run_cancel_test" && assignment.Status != worker.AssignmentCancelled {
			t.Fatalf("cancelled run left assignment in %s", assignment.Status)
		}
	}
}

func TestProviderToolLoopExecutesRequestedTool(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "note.txt"), []byte("hello from tool loop"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	sendRequest(t, ctx, rt, "run_tool_loop", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_tool_loop_test",
		Session: methods.ReplySession{
			ID:         "session_tool_loop_test",
			WorkingDir: tempDir,
		},
		Input: methods.ReplyInput{
			Text: "read file note.txt",
		},
		Options: methods.RunExecuteOptions{},
	})

	waitForResponse(t, lines, "run_tool_loop")
	runEvents := waitForEventsUntilFinish(t, lines)
	// v0.2 worker model emits injected events early; filter them for sequencing.
	filtered := []events.EnvelopeV2{}
	for _, e := range runEvents {
		if e.Type != events.EventSkillsInjected && e.Type != events.EventMemoryInjected {
			filtered = append(filtered, e)
		}
	}
	// v0.2: tolerate injected events and extra message deltas before finish.
	got := []events.EventType{}
	for _, e := range filtered {
		got = append(got, e.Type)
	}
	// Require the critical progression to be present (order relaxed for injected noise).
	required := []events.EventType{events.EventToolStarted, events.EventToolFinished, events.EventFinish}
	idx := 0
	for _, g := range got {
		if idx < len(required) && g == required[idx] {
			idx++
		}
	}
	if idx != len(required) {
		t.Fatalf("missing required progression %v in %v", required, got)
	}
	var sawToolOutput bool
	for _, event := range runEvents {
		delta, _ := event.Payload["delta"].(string)
		if event.Type == events.EventToolOutput && strings.Contains(delta, "hello from tool loop") {
			sawToolOutput = true
		}
	}
	if !sawToolOutput {
		t.Fatal("expected tool output to include file contents")
	}
}

// ---- minimal test helper stubs for v0.2 transition ----
func assertEventSequence(t *testing.T, events []events.EnvelopeV2, want []events.EventType) {
	t.Helper()
	if len(events) < len(want) {
		t.Fatalf("not enough events: got %d want >= %d", len(events), len(want))
	}
	for i, w := range want {
		if events[i].Type != w {
			t.Fatalf("event[%d] = %s want %s", i, events[i].Type, w)
		}
	}
}

func TestRuntimeEmitsMemoryInjectedEvent(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	sendRequest(t, ctx, rt, "run_memory", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_memory_test",
		Session: methods.ReplySession{
			ID: "session_memory_test",
		},
		Input: methods.ReplyInput{
			Text: "hello",
		},
		Options: methods.RunExecuteOptions{
			MemoryContext: &methods.MemoryContext{
				Context: "Memory:\n- [project/fact] Build: Use memory.",
				Items: []methods.MemoryItem{{
					ID:      "mem_1",
					Scope:   "project",
					Kind:    "fact",
					Title:   "Build",
					Content: "Use memory.",
				}},
			},
		},
	})

	waitForResponse(t, lines, "run_memory")
	runEvents := waitForEventsUntilFinish(t, lines)

	// v0.2 worker model: injected events appear early, but multiple message_deltas
	// may be emitted before finish. Use tolerant progression check.
	filtered := []events.EnvelopeV2{}
	for _, e := range runEvents {
		if e.Type != events.EventSkillsInjected && e.Type != events.EventMemoryInjected {
			filtered = append(filtered, e)
		}
	}
	got := []events.EventType{}
	for _, e := range runEvents {
		got = append(got, e.Type)
	}
	required := []events.EventType{events.EventSkillsInjected, events.EventMemoryInjected, events.EventMessageDelta, events.EventFinish}
	idx := 0
	for _, g := range got {
		if idx < len(required) && g == required[idx] {
			idx++
		}
	}
	if idx != len(required) {
		t.Fatalf("missing required progression %v in %v", required, got)
	}

	// Locate the memory event by type for payload assertions (order-tolerant).
	var memoryEvent events.EnvelopeV2
	for _, e := range runEvents {
		if e.Type == events.EventMemoryInjected {
			memoryEvent = e
			break
		}
	}
	if memoryEvent.Type != events.EventMemoryInjected {
		t.Fatalf("no memory_injected event found: %#v", runEvents)
	}
	if memoryEvent.Payload["count"] != float64(1) && memoryEvent.Payload["count"] != 1 {
		t.Fatalf("memory count mismatch: %#v", memoryEvent.Payload)
	}
	ids, ok := memoryEvent.Payload["memory_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "mem_1" {
		t.Fatalf("memory ids mismatch: %#v", memoryEvent.Payload)
	}
}

func TestRuntimeCallGatewayRoundTrip(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	type callResult struct {
		raw json.RawMessage
		err error
	}
	done := make(chan callResult, 1)
	go func() {
		raw, err := rt.callGateway(ctx, methods.MemoryToolExecute, methods.MemoryToolExecuteParams{
			RunID:      "run_gateway_call",
			ToolCallID: "tool_gateway_call",
			ToolName:   "memory.list",
		})
		done <- callResult{raw: raw, err: err}
	}()

	var outbound jsonrpc.Request
	if err := json.NewDecoder(reader).Decode(&outbound); err != nil {
		t.Fatal(err)
	}
	if outbound.Method != methods.MemoryToolExecute || outbound.ID == "" {
		t.Fatalf("unexpected outbound request: %#v", outbound)
	}
	response, err := jsonrpc.NewResult(outbound.ID, methods.MemoryToolExecuteResult{
		Status: "completed",
		Output: "[]",
	})
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(ctx, rawResponse); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		var decoded methods.MemoryToolExecuteResult
		if err := json.Unmarshal(result.raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Status != "completed" || decoded.Output != "[]" {
			t.Fatalf("decoded result mismatch: %#v", decoded)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gateway call result")
	}
}

func TestRuntimeCallStateToolRoundTrip(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	type callResult struct {
		result methods.TodoToolExecuteResult
		err    error
	}
	done := make(chan callResult, 1)
	go func() {
		got, err := rt.executeTodoTool(ctx, methods.TodoToolExecuteParams{
			RunID: "run_state", SessionID: "sess_state", ToolCallID: "tc_state", ToolName: "todo.list",
		})
		done <- callResult{result: got, err: err}
	}()

	var outbound jsonrpc.Request
	if err := json.NewDecoder(reader).Decode(&outbound); err != nil {
		t.Fatal(err)
	}
	if outbound.Method != methods.StateToolExecute {
		t.Fatalf("method = %s, want %s", outbound.Method, methods.StateToolExecute)
	}
	var params methods.StateToolExecuteParams
	if err := json.Unmarshal(outbound.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.Domain != methods.StateToolDomainTodo || params.ToolName != "todo.list" {
		t.Fatalf("params mismatch: %#v", params)
	}
	response, err := jsonrpc.NewResult(outbound.ID, methods.TodoToolExecuteResult{
		Status: "completed", Output: "[]", OpenCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	rawResponse, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(ctx, rawResponse); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.result.Status != "completed" {
			t.Fatalf("result = %#v", result.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for state tool result")
	}
}
