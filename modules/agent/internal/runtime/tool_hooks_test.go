package runtime

import (
	"context"
	"io"
	"testing"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	"redpanda/protocol/tools"
)

func TestToolHooksTransformArgumentsAndResult(t *testing.T) {
	rt := newToolHookTestRuntime()
	var received map[string]any
	rt.registry.MustRegister(registry.ToolEntry{
		Definition: tools.Definition{Name: "test.echo", DisplayName: "Echo", Risk: tools.RiskLow},
		Source:     "builtin:test",
		Handler: func(_ context.Context, _ *registry.ToolContext, args map[string]any) (*tools.Result, error) {
			received = args
			return &tools.Result{Status: tools.CallStatusCompleted, Output: "original"}, nil
		},
	})
	rt.bus.MustRegister(hooks.HookToolCall, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, _ map[string]any) *hooks.HookResult {
		return &hooks.HookResult{Transform: map[string]any{"arguments": map[string]any{"value": "transformed"}}}
	})
	rt.bus.MustRegister(hooks.HookToolResult, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		if event["output"] != "original" {
			t.Fatalf("tool.result input output = %v", event["output"])
		}
		return &hooks.HookResult{Transform: map[string]any{"output": "wrapped"}}
	})

	result, output, ok := rt.executeTool(context.Background(), toolHookTestParams(), tools.Call{
		ID: "call_1", Name: "test.echo", DisplayName: "Echo", Risk: tools.RiskLow,
		Arguments: map[string]any{"value": "original"},
	})
	if !ok || result.Status != tools.CallStatusCompleted || result.Output != "wrapped" || output != "wrapped" {
		t.Fatalf("result = %#v, output = %q, ok = %v", result, output, ok)
	}
	if received["value"] != "transformed" {
		t.Fatalf("handler arguments = %#v", received)
	}
}

func TestToolCallHookBlockSkipsHandler(t *testing.T) {
	rt := newToolHookTestRuntime()
	called := false
	registerToolHookTestEntry(rt, tools.RiskLow, func() { called = true })
	rt.bus.MustRegister(hooks.HookToolCall, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, _ map[string]any) *hooks.HookResult {
		return &hooks.HookResult{Block: true, Reason: "blocked by test hook"}
	})

	result, _, ok := rt.executeTool(context.Background(), toolHookTestParams(), toolHookTestCall(tools.RiskLow))
	if ok || result.Status != tools.CallStatusDenied || result.Error != "blocked by test hook" || called {
		t.Fatalf("result = %#v, ok = %v, called = %v", result, ok, called)
	}
}

func TestPermissionHookCanTightenAllowedDecision(t *testing.T) {
	rt := newToolHookTestRuntime()
	called := false
	registerToolHookTestEntry(rt, tools.RiskLow, func() { called = true })
	rt.bus.MustRegister(hooks.HookPermissionCheck, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		if event["action"] != string(agenttools.ToolDecisionAllow) {
			t.Fatalf("permission action = %v", event["action"])
		}
		return &hooks.HookResult{Block: true, Reason: "plugin denied tool"}
	})

	result, _, ok := rt.executeTool(context.Background(), toolHookTestParams(), toolHookTestCall(tools.RiskLow))
	if ok || result.Error != "plugin denied tool" || called {
		t.Fatalf("result = %#v, ok = %v, called = %v", result, ok, called)
	}
}

func TestPermissionHookCanResolvePermissionRequirement(t *testing.T) {
	rt := newToolHookTestRuntime()
	called := false
	registerToolHookTestEntry(rt, tools.RiskHigh, func() { called = true })
	rt.bus.MustRegister(hooks.HookPermissionCheck, hooks.HookOrderPlugin, "js:test", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		if event["action"] != string(agenttools.ToolDecisionRequirePermission) {
			t.Fatalf("permission action = %v", event["action"])
		}
		return &hooks.HookResult{Transform: map[string]any{
			"action": string(agenttools.ToolDecisionAllow),
			"reason": "approved by test hook",
		}}
	})

	params := toolHookTestParams()
	params.Options.PermissionMode = "strict"
	result, _, ok := rt.executeTool(context.Background(), params, toolHookTestCall(tools.RiskHigh))
	if !ok || result.Status != tools.CallStatusCompleted || !called {
		t.Fatalf("result = %#v, ok = %v, called = %v", result, ok, called)
	}
}

func TestToolResultHookRejectsInvalidStatus(t *testing.T) {
	result := toolResultFromHook(hooks.HookOutcome{Event: map[string]any{"status": "unknown"}}, tools.Result{Status: tools.CallStatusCompleted})
	if result.Status != tools.CallStatusFailed || result.Error != "tool.result hook produced invalid status" {
		t.Fatalf("result = %#v", result)
	}
}

func newToolHookTestRuntime() *Runtime {
	rt := &Runtime{
		out:         io.Discard,
		log:         io.Discard,
		permissions: map[string]chan permission.ResolveParams{},
		registry:    registry.NewRegistry(),
		bus:         hooks.NewBus(),
	}
	registerSystemHooks(rt.bus)
	return rt
}

func registerToolHookTestEntry(rt *Runtime, risk tools.Risk, called func()) {
	rt.registry.MustRegister(registry.ToolEntry{
		Definition: tools.Definition{Name: "test.run", DisplayName: "Run", Risk: risk},
		Source:     "builtin:test",
		Handler: func(_ context.Context, _ *registry.ToolContext, _ map[string]any) (*tools.Result, error) {
			called()
			return &tools.Result{Status: tools.CallStatusCompleted, Output: "ok"}, nil
		},
	})
}

func toolHookTestParams() methods.ReplyParams {
	return methods.ReplyParams{RunID: "run_hooks", Session: methods.ReplySession{ID: "session_hooks"}}
}

func toolHookTestCall(risk tools.Risk) tools.Call {
	return tools.Call{ID: "call_hooks", Name: "test.run", DisplayName: "Run", Risk: risk}
}
