package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

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
			"messages": []any{map[string]any{"role": "user", "content": "composed"}},
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
		messages, err := providerMessagesFromHook(event["messages"])
		if err != nil || len(messages) != 1 || messages[0].Content != "composed" {
			t.Fatalf("provider hook received messages %#v: %v", messages, err)
		}
		return &hooks.HookResult{Transform: map[string]any{
			"messages": []any{map[string]any{"role": "user", "content": "rewritten"}},
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
		Messages: []provider.Message{{Role: "user", Content: "original"}},
		Options:  provider.RequestOptions{ProviderAPIKey: "provider-secret", Model: "model-test"},
	}
	err := rt.completeProvider(context.Background(), methods.ReplyParams{RunID: "run_provider", Session: methods.ReplySession{ID: "session_provider"}}, request, func(provider.ProviderChunk) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !recorder.called || len(recorder.request.Messages) != 1 || recorder.request.Messages[0].Content != "rewritten" {
		t.Fatalf("request messages = %#v", recorder.request.Messages)
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
