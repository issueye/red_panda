package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/protocol/events"
	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type splitUsageProvider struct{}

func (*splitUsageProvider) Name() string { return "split-usage" }

func (*splitUsageProvider) Complete(_ context.Context, _ provider.Request, emit func(provider.ProviderChunk) error) error {
	if err := emit(provider.ProviderChunk{Usage: &provider.ProviderUsage{InputTokens: 100, CacheWriteTokens: 20}}); err != nil {
		return err
	}
	if err := emit(provider.ProviderChunk{Delta: "done", Usage: &provider.ProviderUsage{OutputTokens: 7, CacheReadTokens: 80}}); err != nil {
		return err
	}
	return emit(provider.ProviderChunk{Final: true})
}

type providerHookRecorder struct {
	request provider.Request
	err     error
	called  bool
}

func (p *providerHookRecorder) Name() string { return "hook_test" }

func (p *providerHookRecorder) Complete(_ context.Context, request provider.Request, emit func(provider.ProviderChunk) error) error {
	p.called = true
	p.request = request
	if p.err != nil {
		return p.err
	}
	return emit(provider.ProviderChunk{Delta: "hello", ToolCalls: []tools.Call{{ID: "call_1", Name: "test.run"}}, Final: true})
}

func TestProviderHooksTransformSafeRequestAndObserveResponse(t *testing.T) {
	recorder := &providerHookRecorder{}
	rt := &Runtime{provider: recorder, bus: hooks.NewBus(), out: io.Discard, log: io.Discard}
	var after map[string]any
	rt.bus.MustRegister(hooks.HookContextCompose, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, _ map[string]any) *hooks.HookResult {
		return &hooks.HookResult{Transform: map[string]any{
			"prompt": map[string]any{"turn_tail": []any{map[string]any{"role": "user", "content": "composed"}}},
		}}
	})
	rt.bus.MustRegister(hooks.HookBeforeProviderRequest, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "provider-secret") {
			t.Fatalf("provider secret leaked to hook: %s", raw)
		}
		prompt, _ := event["prompt"].(map[string]any)
		messages, err := providerMessagesFromHook(prompt["turn_tail"])
		if err != nil || len(messages) != 1 || messages[0].Content != "composed" {
			t.Fatalf("provider hook received messages %#v: %v", messages, err)
		}
		return &hooks.HookResult{Transform: map[string]any{
			"prompt": map[string]any{"turn_tail": []any{map[string]any{"role": "user", "content": "rewritten"}}},
			"tools": []any{map[string]any{
				"name": "test.changed", "display_name": "Changed", "risk": "low",
				"parameters": map[string]any{"type": "object"},
			}},
			"headers": map[string]any{"OpenAI-Project": "project-1", "Authorization": "Bearer attacker"},
		}}
	})
	rt.bus.MustRegister(hooks.HookAfterProviderResponse, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		after = event
		return nil
	})

	request := provider.Request{
		Prompt:  provider.PromptEnvelope{TurnTail: []provider.Message{{Role: "user", Content: "original"}}},
		Options: provider.RequestOptions{ProviderAPIKey: "provider-secret", Model: "model-test"},
	}
	err := rt.completeProvider(context.Background(), methods.ReplyParams{RunID: "run_provider", Session: methods.ReplySession{ID: "session_provider"}}, request, func(provider.ProviderChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	recordedMessages := recorder.request.Prompt.FlattenMessages()
	if !recorder.called || len(recordedMessages) != 1 || recordedMessages[0].Content != "rewritten" {
		t.Fatalf("request messages = %#v", recordedMessages)
	}
	if len(recorder.request.Tools) != 1 || recorder.request.Tools[0].Name != "test.changed" {
		t.Fatalf("request tools = %#v", recorder.request.Tools)
	}
	if len(recorder.request.Options.Headers) != 1 || recorder.request.Options.Headers["OpenAI-Project"] != "project-1" {
		t.Fatalf("request headers = %#v", recorder.request.Options.Headers)
	}
	if after["success"] != true || after["chunk_count"] != 1 || after["tool_call_count"] != 1 {
		t.Fatalf("response event = %#v", after)
	}
}

func TestProviderBeforeHookBlockSkipsProvider(t *testing.T) {
	recorder := &providerHookRecorder{}
	rt := &Runtime{provider: recorder, bus: hooks.NewBus(), out: io.Discard, log: io.Discard}
	rt.bus.MustRegister(hooks.HookBeforeProviderRequest, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, _ map[string]any) *hooks.HookResult {
		return &hooks.HookResult{Block: true, Reason: "provider blocked"}
	})
	err := rt.completeProvider(context.Background(), methods.ReplyParams{}, provider.Request{}, func(provider.ProviderChunk) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "provider blocked") || recorder.called {
		t.Fatalf("err = %v, called = %v", err, recorder.called)
	}
}

func TestProviderAfterHookRunsOnFailure(t *testing.T) {
	recorder := &providerHookRecorder{err: errors.New("provider failed")}
	rt := &Runtime{provider: recorder, bus: hooks.NewBus(), out: io.Discard, log: io.Discard}
	called := false
	rt.bus.MustRegister(hooks.HookAfterProviderResponse, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		called = true
		if event["success"] != false || event["error"] != "provider failed" {
			t.Fatalf("response event = %#v", event)
		}
		return nil
	})
	err := rt.completeProvider(context.Background(), methods.ReplyParams{}, provider.Request{}, func(provider.ProviderChunk) error { return nil })
	if err == nil || !called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestCompleteProviderAggregatesUsageIntoOneRunEvent(t *testing.T) {
	var output bytes.Buffer
	rt := &Runtime{provider: &splitUsageProvider{}, bus: hooks.NewBus(), out: &output, log: io.Discard}
	request := provider.Request{
		RunID: "run-usage", SessionID: "session-usage",
		Prompt:  provider.PromptEnvelope{CacheEpoch: "epoch-usage"},
		Options: provider.RequestOptions{ProviderName: "openai_compatible", Model: "model-usage"},
	}
	var downstreamUsage int
	err := rt.completeProvider(context.Background(), methods.ReplyParams{
		RunID: "run-usage", Session: methods.ReplySession{ID: "session-usage"},
	}, request, func(chunk provider.ProviderChunk) error {
		if chunk.Usage != nil {
			downstreamUsage++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if downstreamUsage != 0 {
		t.Fatalf("usage leaked as %d downstream chunks", downstreamUsage)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("usage notifications = %d, output=%s", len(lines), output.String())
	}
	var note jsonrpc.Notification
	if err := json.Unmarshal([]byte(lines[0]), &note); err != nil {
		t.Fatal(err)
	}
	var event events.EnvelopeV2
	if err := json.Unmarshal(note.Params, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != events.EventUsage || event.Payload["input_tokens"] != float64(100) ||
		event.Payload["output_tokens"] != float64(7) || event.Payload["cache_read_tokens"] != float64(80) ||
		event.Payload["cache_write_tokens"] != float64(20) || event.Payload["cache_hit_ratio"] != float64(0.8) {
		t.Fatalf("usage event = %#v", event)
	}
}
