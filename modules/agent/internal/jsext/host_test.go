package jsext

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	ptools "redpanda/protocol/tools"
)

func TestLoadRegistersToolAndHook(t *testing.T) {
	reg := registry.NewRegistry()
	bus := hooks.NewBus()
	host := NewHost(reg, bus, nil)
	plugin := loadTestPlugin(t, host, `
rp.registerTool({
  name: "demo.greet", displayName: "Greet", description: "Greets", risk: "low",
  parameters: {type: "object"}
}, (args, context) => ({output: "hello " + args.name + " from " + context.runId}));
rp.on("context.compose", (event) => ({transform: {marker: event.input + "!"}}));
`)

	result, err := reg.Execute(context.Background(), "demo.greet", &registry.ToolContext{RunID: "run-1"}, map[string]any{"name": "panda"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "hello panda from run-1" {
		t.Fatalf("output = %q", result.Output)
	}
	outcome := bus.Emit(&hooks.HookContext{Context: context.Background()}, hooks.HookContextCompose, map[string]any{"input": "ok"})
	if outcome.Event["marker"] != "ok!" {
		t.Fatalf("event = %#v", outcome.Event)
	}
	host.Unload(plugin)
	if _, ok := reg.Lookup("demo.greet"); ok {
		t.Fatal("tool remains after unload")
	}
	if len(bus.List(hooks.HookContextCompose)) != 0 {
		t.Fatal("hook remains after unload")
	}
}

func TestUnloadRestoresOverriddenTool(t *testing.T) {
	reg := registry.NewRegistry()
	reg.MustRegister(registry.ToolEntry{
		Definition: ptools.Definition{Name: "demo.greet", Risk: ptools.RiskLow},
		Handler: func(context.Context, *registry.ToolContext, map[string]any) (*ptools.Result, error) {
			return &ptools.Result{Name: "demo.greet", Status: ptools.CallStatusCompleted, Output: "builtin"}, nil
		},
		Source: "builtin:test",
	})
	host := NewHost(reg, hooks.NewBus(), nil)
	plugin := loadTestPlugin(t, host, `rp.registerTool({name:"demo.greet", risk:"low"}, () => "javascript");`)
	result, err := reg.Execute(context.Background(), "demo.greet", nil, nil)
	if err != nil || result.Output != "javascript" {
		t.Fatalf("javascript result = %#v, %v", result, err)
	}
	host.Unload(plugin)
	result, err = reg.Execute(context.Background(), "demo.greet", nil, nil)
	if err != nil || result.Output != "builtin" {
		t.Fatalf("restored result = %#v, %v", result, err)
	}
}

func TestToolTimeoutDoesNotPoisonVM(t *testing.T) {
	host := NewHost(registry.NewRegistry(), hooks.NewBus(), nil)
	host.timeout = 50 * time.Millisecond
	loadTestPlugin(t, host, `
let calls = 0;
rp.registerTool({name:"demo.loop", risk:"low"}, () => { calls++; if (calls === 1) { while (true) {} } return "recovered"; });
`)
	started := time.Now()
	if _, err := host.registry.Execute(context.Background(), "demo.loop", nil, nil); err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("interrupt took too long")
	}
	result, err := host.registry.Execute(context.Background(), "demo.loop", nil, nil)
	if err != nil || result.Output != "recovered" {
		t.Fatalf("post-timeout result = %#v, %v", result, err)
	}
}

func TestFailedLoadDoesNotCommitRegistrations(t *testing.T) {
	host := NewHost(registry.NewRegistry(), hooks.NewBus(), nil)
	path := writePlugin(t, `rp.registerTool({name:"demo.partial", risk:"low"}, () => "bad"); throw new Error("stop");`)
	if _, err := host.LoadFile(context.Background(), "broken", path, 1); err == nil {
		t.Fatal("expected load error")
	}
	if _, ok := host.registry.Lookup("demo.partial"); ok {
		t.Fatal("partial tool was committed")
	}
}

func loadTestPlugin(t *testing.T, host *Host, source string) *Plugin {
	t.Helper()
	plugin, err := host.LoadFile(context.Background(), "test", writePlugin(t, source), 1)
	if err != nil {
		t.Fatal(err)
	}
	return plugin
}

func writePlugin(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.js")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
