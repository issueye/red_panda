package runtime

import (
	"context"
	"encoding/json"
	"errors"
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
	sendRequest(t, ctx, rt, "reply_1", methods.AgentReply, methods.ReplyParams{
		RunID: "run_cancel_test",
		Session: methods.ReplySession{
			ID: "session_cancel_test",
		},
		Input: methods.ReplyInput{
			Text: "please wait for permission",
		},
		Options: methods.ReplyOptions{
			RequirePermission: true,
		},
	})

	waitForResponse(t, lines, "reply_1")
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

	sendRequest(t, ctx, rt, "cancel_1", methods.AgentCancel, methods.CancelParams{
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
	sendRequest(t, ctx, rt, "reply_tool_loop", methods.AgentReply, methods.ReplyParams{
		RunID: "run_tool_loop_test",
		Session: methods.ReplySession{
			ID:         "session_tool_loop_test",
			WorkingDir: tempDir,
		},
		Input: methods.ReplyInput{
			Text: "read file note.txt",
		},
		Options: methods.ReplyOptions{},
	})

	waitForResponse(t, lines, "reply_tool_loop")
	runEvents := waitForEventsUntilFinish(t, lines)
	assertEventSequence(t, runEvents, []events.EventType{
		events.EventToolStarted,
		events.EventToolOutput,
		events.EventToolFinished,
		events.EventMessageDelta,
		events.EventFinish,
	})
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

func TestRuntimeEmitsMemoryInjectedEvent(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	sendRequest(t, ctx, rt, "reply_memory", methods.AgentReply, methods.ReplyParams{
		RunID: "run_memory_test",
		Session: methods.ReplySession{
			ID: "session_memory_test",
		},
		Input: methods.ReplyInput{
			Text: "hello",
		},
		Options: methods.ReplyOptions{
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

	waitForResponse(t, lines, "reply_memory")
	runEvents := waitForEventsUntilFinish(t, lines)
	assertEventSequence(t, runEvents, []events.EventType{
		events.EventSkillsInjected,
		events.EventMemoryInjected,
		events.EventMessageDelta,
		events.EventFinish,
	})
	memoryEvent := runEvents[1]
	if memoryEvent.Type != events.EventMemoryInjected {
		t.Fatalf("second event = %s, want memory_injected: %#v", memoryEvent.Type, runEvents)
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
