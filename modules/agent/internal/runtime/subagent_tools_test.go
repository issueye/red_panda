package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestSubagentRunCaptureBuildsActionableEmptyResultError(t *testing.T) {
	capture := &subagentRunCapture{
		maxTurns: 16,
		backend:  "process_pool",
		name:     "desktop",
		task:     "analyze desktop frontend modules",
	}
	capture.Observe(events.Envelope{
		Type: events.EventToolStarted,
		Payload: map[string]any{
			"tool_name": "workspace.read_file",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventToolFailed,
		Payload: map[string]any{
			"tool_name": "workspace.read_file",
			"error":     "file not found",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventError,
		Payload: map[string]any{
			"message": "provider exceeded tool turn limit (16)",
			"status":  "failed",
		},
	})
	capture.Observe(events.Envelope{
		Type: events.EventFinish,
		Payload: map[string]any{
			"status": "failed",
		},
	})

	err := capture.FailureError("subagent returned an empty final report")
	text := err.Error()
	for _, want := range []string{
		"subagent returned an empty final report",
		"subagent=desktop",
		"backend=process_pool",
		"max_turns=16",
		"finish_status=failed",
		"tools_failed=1",
		"last_error=",
		"file not found",
		"hint=",
		"max_turns",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("error missing %q:\n%s", want, text)
		}
	}
}

func TestSubagentRunCaptureCollectsMessageWithoutRootRoleFilter(t *testing.T) {
	capture := &subagentRunCapture{name: "backend"}
	capture.Observe(events.Envelope{
		Type: events.EventMessageDelta,
		Agent: events.AgentRef{
			Role: events.AgentRoleSubAgent,
			Name: "backend",
		},
		Stream: &events.StreamRef{Kind: events.StreamMessage},
		Payload: map[string]any{
			"delta": "hello from child",
		},
	})
	if got := capture.FinalText(); got != "hello from child" {
		t.Fatalf("FinalText = %q", got)
	}
}

func TestAvailableToolsIncludesSubagentRun(t *testing.T) {
	var found bool
	for _, definition := range (ToolRunner{}).AvailableTools() {
		if definition.Name == "subagent.run" {
			found = true
			if definition.Risk != tools.RiskMedium {
				t.Fatalf("subagent.run risk = %s, want medium", definition.Risk)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected subagent.run in AvailableTools")
	}
}

func TestSubagentProcessPoolStatusResizeAndReset(t *testing.T) {
	created := 0
	pool := newSubAgentProcessPool(2, func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
		created++
		return fakeProcessSubAgent{}, nil
	})

	child, release, err := pool.Acquire(context.Background(), methods.ReplyParams{}, "s1")
	if err != nil || child == nil || release == nil {
		t.Fatalf("acquire failed: child=%v err=%v", child, err)
	}
	release(true) // return to idle
	status := pool.Status()
	if status.Limit != 2 || status.Idle != 1 || status.InUse != 0 {
		t.Fatalf("status after release = %#v", status)
	}

	resized := pool.SetLimit(4)
	if resized.Limit != 4 {
		t.Fatalf("resized limit = %d, want 4", resized.Limit)
	}

	reset := pool.Reset(context.Background())
	if reset.Idle != 0 {
		t.Fatalf("reset should clear idle workers, got %#v", reset)
	}
}

func TestAvailableToolsIncludeSubagentManagement(t *testing.T) {
	want := map[string]bool{
		"subagent.run":         false,
		"subagent.list":        false,
		"subagent.cancel":      false,
		"subagent.reset":       false,
		"subagent.pool_status": false,
		"subagent.pool_resize": false,
		"subagent.pool_reset":  false,
	}
	for _, definition := range (ToolRunner{}).AvailableTools() {
		if _, ok := want[definition.Name]; ok {
			want[definition.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("missing tool %s", name)
		}
	}
}

func TestEffectiveSubagentToolTurnsUsesFileCountFormula(t *testing.T) {
	if got := effectiveSubagentToolTurns(0, 0); got != defaultSubagentToolTurns {
		t.Fatalf("empty inputs = %d, want default %d", got, defaultSubagentToolTurns)
	}
	if got := effectiveSubagentToolTurns(0, 120); got != 120+subagentSummaryTurns {
		t.Fatalf("file_count formula = %d, want %d", got, 120+subagentSummaryTurns)
	}
	if got := effectiveSubagentToolTurns(200, 120); got != 200 {
		t.Fatalf("explicit max_turns should win, got %d", got)
	}
	if got := recommendedSubagentTurns(1); got != 1+subagentSummaryTurns {
		t.Fatalf("single file formula = %d", got)
	}
}

func TestRuntimeSubagentRunToolReturnsChildFinalText(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 64)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.newProcessSubAgent = func(ctx context.Context, params methods.ReplyParams, subAgentID string) (processSubAgent, error) {
		return taskAwareFakeSubAgent{taskPrefix: "desktop"}, nil
	}
	rt.provider = subagentToolProvider{}

	sendRequest(t, context.Background(), rt, "reply_subagent_tool", methods.AgentReply, methods.ReplyParams{
		RunID: "run_subagent_tool",
		Session: methods.ReplySession{
			ID:         "session_subagent_tool",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "spawn specialists"},
		Options: methods.ReplyOptions{
			ToolPolicy:      "allow_all",
			SubAgentBackend: "runtime_process",
			MaxToolTurns:    4,
		},
	})

	waitForResponse(t, lines, "reply_subagent_tool")
	runEvents := waitForEventsUntilFinish(t, lines)

	var sawSubagentCompleted bool
	var sawParentSummary bool
	for _, event := range runEvents {
		switch event.Type {
		case events.EventSubAgentUpdate:
			if event.Payload["status"] == "completed" && strings.Contains(stringValue(event.Payload["name"]), "desktop") {
				sawSubagentCompleted = true
			}
		case events.EventMessageDelta:
			if strings.Contains(stringValue(event.Payload["delta"]), "desktop report") {
				sawParentSummary = true
			}
		}
	}
	if !sawSubagentCompleted {
		t.Fatalf("expected completed desktop subagent update, events=%#v", summarizeEventTypes(runEvents))
	}
	if !sawParentSummary {
		t.Fatalf("expected parent to receive subagent result, events=%#v", summarizeEventTypes(runEvents))
	}
	finish := runEvents[len(runEvents)-1]
	if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
		t.Fatalf("finish = %#v, want completed", finish)
	}
}

type subagentToolProvider struct{}

func (subagentToolProvider) Name() string { return "subagent-tool-test" }

func (subagentToolProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if len(req.ToolHistory) == 0 {
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:   "call_subagent_desktop",
			Name: "subagent.run",
			Arguments: map[string]any{
				"name": "desktop",
				"task": "analyze desktop frontend",
			},
		}}})
	}
	last := req.ToolHistory[len(req.ToolHistory)-1]
	text := "parent summary using " + last.Result.Output
	if err := emit(ProviderChunk{Delta: text}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

type taskAwareFakeSubAgent struct {
	taskPrefix string
}

func (f taskAwareFakeSubAgent) Start(ctx context.Context, childParams methods.ReplyParams, onEvent func(events.Envelope)) error {
	delta := f.taskPrefix + " report: " + childParams.Input.Text
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
		Payload: map[string]any{"delta": delta},
	})
	onEvent(events.Envelope{
		RootRunID: childParams.RunID,
		RunID:     childParams.RunID,
		SessionID: childParams.Session.ID,
		Type:      events.EventFinish,
		Payload:   map[string]any{"status": "completed"},
	})
	return nil
}

func (taskAwareFakeSubAgent) Cancel(ctx context.Context, runID string, reason string) error {
	return nil
}

func (taskAwareFakeSubAgent) Close(ctx context.Context) error {
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func summarizeEventTypes(items []events.Envelope) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, string(item.Type))
	}
	return out
}
