package runtime

import (
	"context"
	"io"
	"testing"

	"redpanda/agent/internal/provider"
	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/methods"
)

type runHookProvider struct {
	request provider.Request
}

func (p *runHookProvider) Name() string { return "run_hook_test" }

func (p *runHookProvider) Complete(_ context.Context, request provider.Request, emit func(provider.ProviderChunk) error) error {
	p.request = request
	return emit(provider.ProviderChunk{Delta: "done", Final: true})
}

func TestRunHooksWrapExecutionAndCanRewriteInput(t *testing.T) {
	model := &runHookProvider{}
	rt := &Runtime{
		provider: model,
		registry: registry.NewRegistry(),
		bus:      hooks.NewBus(),
		out:      io.Discard,
		log:      io.Discard,
	}
	var order []string
	var endEvent map[string]any
	rt.bus.MustRegister(hooks.HookRunStart, hooks.HookOrderPlugin, "js:test", func(ctx *hooks.HookContext, event map[string]any) *hooks.HookResult {
		order = append(order, "start")
		if ctx.RunID != "run_hooks" || ctx.SessionID != "session_hooks" || event["input"] != "original" {
			t.Fatalf("start context = %#v, event = %#v", ctx, event)
		}
		return &hooks.HookResult{Transform: map[string]any{"input": "rewritten"}}
	})
	rt.bus.MustRegister(hooks.HookRunEnd, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		order = append(order, "end")
		endEvent = event
		return nil
	})

	rt.emitRun(context.Background(), methods.ReplyParams{
		RunID:   "run_hooks",
		Session: methods.ReplySession{ID: "session_hooks"},
		Input:   methods.ReplyInput{Text: "original"},
	})
	if len(order) != 2 || order[0] != "start" || order[1] != "end" {
		t.Fatalf("hook order = %#v", order)
	}
	if model.request.Input != "rewritten" {
		t.Fatalf("provider input = %q", model.request.Input)
	}
	if endEvent["status"] != "completed" || endEvent["loop_end_reason"] != string(loopEndNoTools) {
		t.Fatalf("run.end event = %#v", endEvent)
	}
}
