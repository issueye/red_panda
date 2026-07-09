package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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

	sendRequest(t, ctx, rt, "cancel_1", methods.AgentCancel, methods.CancelParams{
		RunID:  "run_cancel_test",
		Reason: "test",
	})
	finish := waitForResponseAndEvent(t, lines, "cancel_1", events.EventFinish)
	if got := finish.Payload["status"]; got != "cancelled" {
		t.Fatalf("expected cancelled finish status, got %#v", got)
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
		events.EventMemoryInjected,
		events.EventMessageDelta,
		events.EventFinish,
	})
	memoryEvent := runEvents[0]
	if memoryEvent.Type != events.EventMemoryInjected {
		t.Fatalf("first event = %s, want memory_injected: %#v", memoryEvent.Type, runEvents)
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

func TestSubAgentQueryAndCancel(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	ctx := context.Background()
	sendRequest(t, ctx, rt, "reply_subagent", methods.AgentReply, methods.ReplyParams{
		RunID: "run_subagent_cancel_test",
		Session: methods.ReplySession{
			ID: "session_subagent_cancel_test",
		},
		Input: methods.ReplyInput{
			Text: "spawn planner",
		},
		Options: methods.ReplyOptions{
			SpawnSubAgents: true,
		},
	})
	waitForResponse(t, lines, "reply_subagent")
	running := waitForEventType(t, lines, events.EventSubAgentUpdate)
	subAgentID, _ := running.Payload["subagent_id"].(string)
	if subAgentID == "" {
		t.Fatalf("expected subagent_id in running event, got %#v", running.Payload)
	}
	if running.RootRunID != "run_subagent_cancel_test" || running.ParentRunID != "run_subagent_cancel_test" || running.RunID == running.RootRunID {
		t.Fatalf("expected subagent event to keep root run and expose child run, got %#v", running)
	}

	sendRequest(t, ctx, rt, "subagents_1", methods.AgentSubAgents, methods.SubAgentsParams{
		RunID: "run_subagent_cancel_test",
	})
	query := waitForResponse(t, lines, "subagents_1")
	var result methods.SubAgentsResult
	if err := json.Unmarshal(query.Result, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0].SubAgentID != subAgentID || result.Items[0].Status != "running" {
		t.Fatalf("expected running subagent record, got %#v", result.Items)
	}

	sendRequest(t, ctx, rt, "cancel_subagent_1", methods.AgentSubAgentCancel, methods.SubAgentCancelParams{
		RunID:      "run_subagent_cancel_test",
		SubAgentID: subAgentID,
		Reason:     "test",
	})
	cancelResp := waitForResponse(t, lines, "cancel_subagent_1")
	var cancelResult methods.SubAgentCancelResult
	if err := json.Unmarshal(cancelResp.Result, &cancelResult); err != nil {
		t.Fatal(err)
	}
	if !cancelResult.Cancelled {
		t.Fatalf("expected subagent cancellation to be accepted, got %#v", cancelResult)
	}
	cancelled := waitForEventType(t, lines, events.EventSubAgentUpdate)
	if got := cancelled.Payload["status"]; got != "cancelled" {
		t.Fatalf("expected cancelled subagent event, got %#v", cancelled.Payload)
	}
}

func TestRuntimeProcessSubAgentBridgesChildEvents(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.newProcessSubAgent = func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
		return fakeProcessSubAgent{}, nil
	}
	ctx := context.Background()
	sendRequest(t, ctx, rt, "reply_process_subagent", methods.AgentReply, methods.ReplyParams{
		RunID: "run_process_subagent_test",
		Session: methods.ReplySession{
			ID: "session_process_subagent_test",
		},
		Input: methods.ReplyInput{
			Text: "spawn runtime process planner",
		},
		Options: methods.ReplyOptions{
			SpawnSubAgents:  true,
			SubAgentBackend: "runtime_process",
		},
	})
	waitForResponse(t, lines, "reply_process_subagent")
	runEvents := waitForEventsUntilFinish(t, lines)
	assertEventSequence(t, runEvents, []events.EventType{
		events.EventSubAgentUpdate,
		events.EventMessageDelta,
		events.EventSubAgentUpdate,
		events.EventFinish,
	})
	var sawChildMessage bool
	for _, event := range runEvents {
		if event.Type != events.EventMessageDelta {
			continue
		}
		delta, _ := event.Payload["delta"].(string)
		if !strings.Contains(delta, "child process output") {
			continue
		}
		if event.RootRunID != "run_process_subagent_test" || event.ParentRunID != "run_process_subagent_test" {
			t.Fatalf("expected bridged child message to use parent root run, got %#v", event)
		}
		if event.Agent.Role != events.AgentRoleSubAgent || event.Payload["backend"] != "runtime_process" {
			t.Fatalf("expected runtime_process subagent event, got %#v", event)
		}
		sawChildMessage = true
	}
	if !sawChildMessage {
		t.Fatal("expected bridged child process output")
	}
}

func TestProcessPoolSubAgentReusesChild(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 64)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	created := 0
	rt.newProcessSubAgent = func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
		created++
		return fakeProcessSubAgent{}, nil
	}
	rt.processPool = newSubAgentProcessPool(1, rt.newProcessSubAgent)
	ctx := context.Background()

	for _, runID := range []string{"run_pool_subagent_1", "run_pool_subagent_2"} {
		sendRequest(t, ctx, rt, jsonrpc.ID("reply_"+runID), methods.AgentReply, methods.ReplyParams{
			RunID: runID,
			Session: methods.ReplySession{
				ID: "session_pool_subagent_test",
			},
			Input: methods.ReplyInput{
				Text: "spawn pooled planner",
			},
			Options: methods.ReplyOptions{
				SpawnSubAgents:  true,
				SubAgentBackend: "process_pool",
			},
		})
		waitForResponse(t, lines, jsonrpc.ID("reply_"+runID))
		runEvents := waitForEventsUntilFinish(t, lines)
		var sawPooledEvent bool
		for _, event := range runEvents {
			if event.Agent.Role == events.AgentRoleSubAgent && event.Payload["backend"] == "process_pool" {
				sawPooledEvent = true
			}
		}
		if !sawPooledEvent {
			t.Fatalf("expected process_pool subagent event for %s", runID)
		}
	}
	if created != 1 {
		t.Fatalf("expected process pool to reuse one child, created %d", created)
	}
}

func TestProcessSubAgentFailureEmitsFailedUpdateAndRootFinish(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		setup   func(*Runtime, error)
	}{
		{
			name:    "runtime_process_create_failure",
			backend: "runtime_process",
			setup: func(rt *Runtime, failure error) {
				rt.newProcessSubAgent = func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
					return nil, failure
				}
			},
		},
		{
			name:    "process_pool_start_failure",
			backend: "process_pool",
			setup: func(rt *Runtime, failure error) {
				rt.newProcessSubAgent = func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
					return failingProcessSubAgent{err: failure}, nil
				}
				rt.processPool = newSubAgentProcessPool(1, rt.newProcessSubAgent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()

			lines := make(chan []byte, 32)
			go readJSONLines(t, reader, lines)

			rt := New(strings.NewReader(""), writer, io.Discard, "test")
			failure := errors.New(tt.name + " boom")
			tt.setup(rt, failure)

			runID := "run_" + tt.name
			ctx := context.Background()
			sendRequest(t, ctx, rt, jsonrpc.ID("reply_"+tt.name), methods.AgentReply, methods.ReplyParams{
				RunID: runID,
				Session: methods.ReplySession{
					ID: "session_" + tt.name,
				},
				Input: methods.ReplyInput{
					Text: "spawn failing process planner",
				},
				Options: methods.ReplyOptions{
					SpawnSubAgents:  true,
					SubAgentBackend: tt.backend,
				},
			})
			waitForResponse(t, lines, jsonrpc.ID("reply_"+tt.name))
			runEvents := waitForEventsUntilFinish(t, lines)

			var failed events.Envelope
			var sawFailed bool
			for i, event := range runEvents {
				if event.RootRunID != runID {
					t.Fatalf("event %d root run id = %q; want %q", i, event.RootRunID, runID)
				}
				if event.RootSeq != uint64(i+1) {
					t.Fatalf("event %d root seq = %d; want %d", i, event.RootSeq, i+1)
				}
				if event.Type == events.EventSubAgentUpdate && event.Payload["status"] == "failed" {
					failed = event
					sawFailed = true
				}
			}
			if !sawFailed {
				t.Fatalf("expected failed subagent_update, got %#v", runEvents)
			}
			if failed.Agent.Role != events.AgentRoleSubAgent || failed.ParentRunID != runID || failed.RunID == runID {
				t.Fatalf("expected failed event to be scoped to subagent under root run, got %#v", failed)
			}
			if failed.Payload["backend"] != tt.backend {
				t.Fatalf("failed backend = %#v; want %q", failed.Payload["backend"], tt.backend)
			}
			if failed.Payload["summary"] != tt.backend+" planner subagent failed" {
				t.Fatalf("failed summary = %#v", failed.Payload["summary"])
			}
			errText, _ := failed.Payload["error"].(string)
			if !strings.Contains(errText, failure.Error()) {
				t.Fatalf("failed error = %#v; want it to contain %q", failed.Payload["error"], failure.Error())
			}

			finish := runEvents[len(runEvents)-1]
			if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
				t.Fatalf("expected root run to finish completed, got %#v", finish)
			}
			if finish.RootRunID != runID || finish.RunID != runID {
				t.Fatalf("expected root finish to stay on root run, got %#v", finish)
			}
		})
	}
}

type fakeProcessSubAgent struct{}

func (fakeProcessSubAgent) Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error {
	onEvent(events.Envelope{
		RootRunID: childParams.RunID,
		RunID:     childParams.RunID,
		SessionID: childParams.Session.ID,
		Type:      events.EventMessageDelta,
		Agent: events.AgentRef{
			AgentID: "root",
			Role:    events.AgentRoleRoot,
			Path:    []string{"root"},
			Name:    "root",
		},
		Payload: map[string]any{
			"delta": "child process output",
		},
	})
	onEvent(events.Envelope{
		RootRunID: childParams.RunID,
		RunID:     childParams.RunID,
		SessionID: childParams.Session.ID,
		Type:      events.EventFinish,
		Payload: map[string]any{
			"status": "completed",
		},
	})
	return nil
}

func (fakeProcessSubAgent) Cancel(ctx context.Context, runID string, reason string) error {
	return nil
}

func (fakeProcessSubAgent) Close(ctx context.Context) error {
	return nil
}

type failingProcessSubAgent struct {
	err error
}

func (f failingProcessSubAgent) Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error {
	return f.err
}

func (failingProcessSubAgent) Cancel(ctx context.Context, runID string, reason string) error {
	return nil
}

func (failingProcessSubAgent) Close(ctx context.Context) error {
	return nil
}

func readJSONLines(t *testing.T, reader io.Reader, lines chan<- []byte) {
	t.Helper()
	decoder := json.NewDecoder(reader)
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return
		}
		lines <- raw
	}
}

func sendRequest(t *testing.T, ctx context.Context, rt *Runtime, id jsonrpc.ID, method string, params any) {
	t.Helper()
	request, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.handleLine(ctx, raw); err != nil {
		t.Fatal(err)
	}
}

func waitForResponse(t *testing.T, lines <-chan []byte, id jsonrpc.ID) jsonrpc.Response {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for response %s", id)
		case raw := <-lines:
			var probe struct {
				ID     jsonrpc.ID `json:"id"`
				Method string     `json:"method"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatal(err)
			}
			if probe.Method != "" || probe.ID != id {
				continue
			}
			var resp jsonrpc.Response
			if err := json.Unmarshal(raw, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Error != nil {
				t.Fatalf("response %s returned error %d: %s", id, resp.Error.Code, resp.Error.Message)
			}
			return resp
		}
	}
}

func waitForEventType(t *testing.T, lines <-chan []byte, typ events.EventType) events.Envelope {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for event %s", typ)
		case raw := <-lines:
			var probe struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatal(err)
			}
			if probe.Method != methods.AgentEvent {
				continue
			}
			var note jsonrpc.Notification
			if err := json.Unmarshal(raw, &note); err != nil {
				t.Fatal(err)
			}
			var event events.Envelope
			if err := json.Unmarshal(note.Params, &event); err != nil {
				t.Fatal(err)
			}
			if event.Type == typ {
				return event
			}
		}
	}
}

func waitForEventsUntilFinish(t *testing.T, lines <-chan []byte) []events.Envelope {
	t.Helper()
	var items []events.Envelope
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for finish event")
		case raw := <-lines:
			var probe struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatal(err)
			}
			if probe.Method != methods.AgentEvent {
				continue
			}
			var note jsonrpc.Notification
			if err := json.Unmarshal(raw, &note); err != nil {
				t.Fatal(err)
			}
			var event events.Envelope
			if err := json.Unmarshal(note.Params, &event); err != nil {
				t.Fatal(err)
			}
			items = append(items, event)
			if event.Type == events.EventFinish {
				return items
			}
		}
	}
}

func assertEventSequence(t *testing.T, actual []events.Envelope, expected []events.EventType) {
	t.Helper()
	cursor := 0
	for _, event := range actual {
		if cursor < len(expected) && event.Type == expected[cursor] {
			cursor++
		}
	}
	if cursor != len(expected) {
		types := make([]string, 0, len(actual))
		for _, event := range actual {
			types = append(types, string(event.Type))
		}
		t.Fatalf("expected event sequence %v, actual %v", expected, types)
	}
}

func waitForResponseAndEvent(t *testing.T, lines <-chan []byte, id jsonrpc.ID, typ events.EventType) events.Envelope {
	t.Helper()
	var gotResponse bool
	var gotEvent bool
	var matched events.Envelope
	deadline := time.After(2 * time.Second)
	for {
		if gotResponse && gotEvent {
			return matched
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for response %s and event %s", id, typ)
		case raw := <-lines:
			var probe struct {
				ID     jsonrpc.ID `json:"id"`
				Method string     `json:"method"`
			}
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatal(err)
			}
			switch {
			case probe.Method == "" && probe.ID == id:
				var resp jsonrpc.Response
				if err := json.Unmarshal(raw, &resp); err != nil {
					t.Fatal(err)
				}
				if resp.Error != nil {
					t.Fatalf("response %s returned error %d: %s", id, resp.Error.Code, resp.Error.Message)
				}
				gotResponse = true
			case probe.Method == methods.AgentEvent:
				var note jsonrpc.Notification
				if err := json.Unmarshal(raw, &note); err != nil {
					t.Fatal(err)
				}
				var event events.Envelope
				if err := json.Unmarshal(note.Params, &event); err != nil {
					t.Fatal(err)
				}
				if event.Type == typ {
					gotEvent = true
					matched = event
				}
			}
		}
	}
}
