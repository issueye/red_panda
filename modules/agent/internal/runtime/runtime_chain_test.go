package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type chainedToolProvider struct {
	requests []provider.ProviderRequest
}

func TestRecoveryAnswerForWorkerDoesNotDuplicateToolOutput(t *testing.T) {
	history := []ToolExchange{{
		Call: tools.Call{Name: "workspace.read_file", DisplayName: "Read file"},
		Result: tools.Result{
			Name:   "workspace.read_file",
			Status: tools.CallStatusCompleted,
			Output: "large file contents",
		},
	}}
	// v0.2: delegated worker is identified by WorkerContext (not legacy RunID suffix).
	childParams := methods.ReplyParams{
		RunID: "run-delegated",
		Options: methods.ReplyOptions{
			WorkerContext: &methods.WorkerExecutionContext{
				WorkerID:      "worker-02",
				AssignmentID:  "assignment-042",
				RunID:         "run-delegated",
				ProxyMessages: true,
			},
		},
	}
	child := recoveryAnswerForRun(childParams, history)
	if strings.Contains(child, "large file contents") || !strings.Contains(child, "工具卡片") {
		t.Fatalf("delegated worker recovery should be concise: %q", child)
	}
	root := recoveryAnswerForRun(methods.ReplyParams{RunID: "root"}, history)
	if !strings.Contains(root, "large file contents") {
		t.Fatalf("root recovery should retain useful output: %q", root)
	}
}

func (*chainedToolProvider) Name() string {
	return "chained-tool-test"
}

func (p *chainedToolProvider) Complete(_ context.Context, req provider.ProviderRequest, emit func(provider.ProviderChunk) error) error {
	req.ToolHistory = append([]provider.ToolExchange(nil), req.ToolHistory...)
	p.requests = append(p.requests, req)

	switch len(req.ToolHistory) {
	case 0:
		return emit(provider.ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_chain_list",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	case 1:
		return emit(provider.ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_chain_read",
			Name:      "workspace.read_file",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": "note.txt"},
		}}})
	default:
		if err := emit(provider.ProviderChunk{Delta: "tool chain completed"}); err != nil {
			return err
		}
		return emit(provider.ProviderChunk{Final: true})
	}
}

type ProviderRequest = provider.ProviderRequest
type ProviderChunk = provider.ProviderChunk
type ToolExchange = provider.ToolExchange

func TestRuntimeProviderExecutesChainedToolCalls(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "note.txt"), []byte("chain result"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	provider := &chainedToolProvider{}
	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	sendRequest(t, context.Background(), rt, "run_chain", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_chain_test",
		Session: methods.ReplySession{
			ID:         "session_chain_test",
			WorkingDir: tempDir,
		},
		Input: methods.ReplyInput{Text: "inspect note.txt"},
	})

	waitForResponse(t, lines, "run_chain")
	runEvents := waitForEventsUntilFinish(t, lines)

	var started, finished int
	for _, event := range runEvents {
		switch event.Type {
		case events.EventToolStarted:
			started++
		case events.EventToolFinished:
			finished++
		}
	}
	if started != 2 || finished != 2 {
		t.Fatalf("tool event counts: started=%d finished=%d, want 2 each", started, finished)
	}

	finish := runEvents[len(runEvents)-1]
	if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
		t.Fatalf("finish event = %#v, want completed", finish)
	}

	if len(provider.requests) != 3 {
		t.Fatalf("provider request count = %d, want 3", len(provider.requests))
	}
	for index, wantHistory := range []int{0, 1, 2} {
		if got := len(provider.requests[index].ToolHistory); got != wantHistory {
			t.Fatalf("provider request %d history length = %d, want %d", index+1, got, wantHistory)
		}
	}

	history := provider.requests[2].ToolHistory
	if history[0].Call.Name != "workspace.list" || history[0].Result.Status != tools.CallStatusCompleted {
		t.Fatalf("first tool exchange = %#v, want completed workspace.list", history[0])
	}
	if history[1].Call.Name != "workspace.read_file" || history[1].Result.Status != tools.CallStatusCompleted {
		t.Fatalf("second tool exchange = %#v, want completed workspace.read_file", history[1])
	}
	if !strings.Contains(history[1].Result.Output, "chain result") {
		t.Fatalf("read tool output = %q, want file contents", history[1].Result.Output)
	}
}

type recoverAfterToolFailureProvider struct {
	requests []ProviderRequest
}

func (*recoverAfterToolFailureProvider) Name() string {
	return "recover-after-tool-failure"
}

func (p *recoverAfterToolFailureProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	req.ToolHistory = append([]ToolExchange(nil), req.ToolHistory...)
	p.requests = append(p.requests, req)

	switch len(req.ToolHistory) {
	case 0:
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_missing_file",
			Name:      "workspace.read_file",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": "missing.json"},
		}}})
	case 1:
		if req.ToolHistory[0].Result.Status != tools.CallStatusFailed {
			return fmt.Errorf("expected first tool to fail, got %#v", req.ToolHistory[0].Result)
		}
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_fallback_list",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	default:
		if err := emit(ProviderChunk{Delta: "recovered after tool failure"}); err != nil {
			return err
		}
		return emit(ProviderChunk{Final: true})
	}
}

type emptyAfterToolsProvider struct {
	requests []ProviderRequest
}

type continueAfterEmptyProvider struct {
	emptyResponses int
}

func (*continueAfterEmptyProvider) Name() string { return "continue-after-empty" }

func (p *continueAfterEmptyProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	switch len(req.ToolHistory) {
	case 0:
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_plan_before_empty",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	case 1:
		if len(req.Tools) == 0 {
			return emit(ProviderChunk{Final: true})
		}
		if p.emptyResponses == 0 {
			p.emptyResponses++
			return emit(ProviderChunk{Final: true})
		}
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_continue_after_empty",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	default:
		if err := emit(ProviderChunk{Delta: "continued after empty provider response"}); err != nil {
			return err
		}
		return emit(ProviderChunk{Final: true})
	}
}

type toolCallOnlyFinalProvider struct{}

func (*toolCallOnlyFinalProvider) Name() string { return "tool-call-only-final" }

func (*toolCallOnlyFinalProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	if len(req.ToolHistory) == 0 {
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_list_before_invalid_final",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	}
	if len(req.Tools) > 0 {
		return emit(ProviderChunk{Final: true})
	}
	if err := emit(ProviderChunk{Delta: "<tool_call><function=workspace__read><parameter=path>main.go</parameter></function></tool_call>"}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

func (*emptyAfterToolsProvider) Name() string { return "empty-after-tools" }

func (p *emptyAfterToolsProvider) Complete(_ context.Context, req ProviderRequest, emit func(ProviderChunk) error) error {
	req.ToolHistory = append([]ToolExchange(nil), req.ToolHistory...)
	p.requests = append(p.requests, req)
	if len(req.ToolHistory) == 0 {
		return emit(ProviderChunk{ToolCalls: []tools.Call{{
			ID:        "tool_list_empty",
			Name:      "workspace.list",
			Risk:      tools.RiskLow,
			Arguments: map[string]any{"path": ".", "max_depth": 1},
		}}})
	}
	// 模拟模型仅以空最终消息结束工具循环。
	if err := emit(ProviderChunk{Delta: ""}); err != nil {
		return err
	}
	return emit(ProviderChunk{Final: true})
}

func TestRuntimeRecoversWhenProviderReturnsEmptyAfterTools(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "note.txt"), []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	provider := &emptyAfterToolsProvider{}
	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	sendRequest(t, context.Background(), rt, "run_empty", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_empty_after_tools",
		Session: methods.ReplySession{
			ID:         "session_empty_after_tools",
			WorkingDir: tempDir,
		},
		Input: methods.ReplyInput{Text: "list then go silent"},
	})

	waitForResponse(t, lines, "run_empty")
	runEvents := waitForEventsUntilFinish(t, lines)

	var recovered string
	for _, event := range runEvents {
		if event.Type != events.EventMessageDelta {
			continue
		}
		if delta, _ := event.Payload["delta"].(string); event.Payload["recovered"] == true || strings.Contains(delta, "根据工具执行结果") || strings.Contains(delta, "模型未生成最终回复") {
			recovered = delta
		}
	}
	if recovered == "" {
		t.Fatalf("expected recovered summary message, events=%#v", runEvents)
	}
	if !strings.Contains(recovered, "note.txt") && !strings.Contains(strings.ToLower(recovered), "list") {
		t.Fatalf("recovered summary should mention tool result: %q", recovered)
	}
	finish := runEvents[len(runEvents)-1]
	if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
		t.Fatalf("finish event = %#v, want completed", finish)
	}
}

func TestRuntimeContinuesAfterEmptyResponseFollowingTools(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 64)
	go readJSONLines(t, reader, lines)

	provider := &continueAfterEmptyProvider{}
	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	sendRequest(t, context.Background(), rt, "run_continue_empty", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_continue_after_empty",
		Session: methods.ReplySession{
			ID:         "session_continue_after_empty",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "plan and inspect the workspace"},
	})

	waitForResponse(t, lines, "run_continue_empty")
	runEvents := waitForEventsUntilFinish(t, lines)

	var continuedText string
	var continuedTool bool
	for _, event := range runEvents {
		switch event.Type {
		case events.EventToolFinished:
			if event.Payload["tool_call_id"] == "tool_continue_after_empty" {
				continuedTool = true
			}
		case events.EventMessageDelta:
			if delta, _ := event.Payload["delta"].(string); strings.Contains(delta, "continued after empty provider response") {
				continuedText = delta
			}
		}
	}
	if !continuedTool {
		t.Fatalf("expected runtime to continue with tools after the empty response, events=%#v", runEvents)
	}
	if continuedText == "" {
		t.Fatalf("expected useful final response after continuation, events=%#v", runEvents)
	}
	if provider.emptyResponses != 1 {
		t.Fatalf("empty responses = %d, want 1", provider.emptyResponses)
	}
}

func TestRuntimeRejectsToolCallMarkupAsFinalAnswer(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = &toolCallOnlyFinalProvider{}

	sendRequest(t, context.Background(), rt, "run_invalid_final", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_invalid_final",
		Session: methods.ReplySession{
			ID:         "session_invalid_final",
			WorkingDir: t.TempDir(),
		},
		Input: methods.ReplyInput{Text: "analyze project"},
	})

	waitForResponse(t, lines, "run_invalid_final")
	runEvents := waitForEventsUntilFinish(t, lines)

	var recovered string
	for _, event := range runEvents {
		if event.Type != events.EventMessageDelta {
			continue
		}
		delta, _ := event.Payload["delta"].(string)
		if strings.Contains(delta, "<tool_call>") {
			t.Fatalf("textual tool call leaked as final answer: %q", delta)
		}
		if event.Payload["recovered"] == true || strings.Contains(delta, "根据工具执行结果") {
			recovered = delta
		}
	}
	if recovered == "" {
		// v0.2: simple fallback; do not depend on removed summarizeEventTypes helper
		t.Fatalf("expected deterministic fallback summary")
	}
	finish := runEvents[len(runEvents)-1]
	if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
		t.Fatalf("finish = %#v, want completed", finish)
	}
}

func TestRuntimeProviderContinuesAfterToolFailure(t *testing.T) {
	tempDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tempDir, "note.txt"), []byte("still here"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()

	lines := make(chan []byte, 32)
	go readJSONLines(t, reader, lines)

	provider := &recoverAfterToolFailureProvider{}
	rt := New(strings.NewReader(""), writer, io.Discard, "test")
	rt.provider = provider

	sendRequest(t, context.Background(), rt, "run_recover", methods.RunExecute, methods.RunExecuteParams{
		RunID: "run_recover_test",
		Session: methods.ReplySession{
			ID:         "session_recover_test",
			WorkingDir: tempDir,
		},
		Input: methods.ReplyInput{Text: "read missing then recover"},
	})

	waitForResponse(t, lines, "run_recover")
	runEvents := waitForEventsUntilFinish(t, lines)

	var toolFailed, toolFinished int
	var sawRecovery bool
	for _, event := range runEvents {
		switch event.Type {
		case events.EventToolFailed:
			toolFailed++
		case events.EventToolFinished:
			toolFinished++
		case events.EventMessageDelta:
			if delta, _ := event.Payload["delta"].(string); strings.Contains(delta, "recovered after tool failure") {
				sawRecovery = true
			}
		case events.EventError:
			t.Fatalf("did not expect run-level error after recoverable tool failure: %#v", event.Payload)
		}
	}
	if toolFailed != 1 {
		t.Fatalf("tool failed count = %d, want 1", toolFailed)
	}
	if toolFinished != 1 {
		t.Fatalf("tool finished count = %d, want 1 (fallback list)", toolFinished)
	}
	if !sawRecovery {
		t.Fatal("expected provider to continue and emit recovery message")
	}

	finish := runEvents[len(runEvents)-1]
	if finish.Type != events.EventFinish || finish.Payload["status"] != "completed" {
		t.Fatalf("finish event = %#v, want completed", finish)
	}
	if len(provider.requests) != 3 {
		t.Fatalf("provider request count = %d, want 3", len(provider.requests))
	}
	if got := provider.requests[1].ToolHistory[0].Result.Status; got != tools.CallStatusFailed {
		t.Fatalf("history after first failure status = %s, want failed", got)
	}
	if got := provider.requests[2].ToolHistory[1].Result.Status; got != tools.CallStatusCompleted {
		t.Fatalf("fallback tool status = %s, want completed", got)
	}
}
