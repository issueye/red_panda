package registry

import (
	"context"
	"testing"
	"time"

	ptools "redpanda/protocol/tools"
)

func mkEntry(name, src string, tc TimeoutClass, risk ptools.Risk) ToolEntry {
	return ToolEntry{
		Definition: ptools.Definition{
			Name:   name,
			Risk:   risk,
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		Handler:      func(_ context.Context, _ *ToolContext, args map[string]any) (*ptools.Result, error) { return nil, nil },
		TimeoutClass: tc,
		Source:       src,
	}
}

func TestRegistryBasicRegisterAndDefinitions(t *testing.T) {
	reg := NewRegistry()
	expectedOrder := []string{"a.tool", "b.tool", "c.tool"}

	for i, name := range expectedOrder {
		srcs := []string{"builtin:core", "js:ext1", "mcp:srv"}
		reg.Register(mkEntry(name, srcs[i], LocalToolTimeout, ptools.RiskLow))
	}

	defs := reg.Definitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 definitions, got %d", len(defs))
	}
	for i, d := range defs {
		if d.Name != expectedOrder[i] {
			t.Errorf("order[%d] = %q, want %q", i, d.Name, expectedOrder[i])
		}
	}
}

func TestRegistryOverride(t *testing.T) {
	reg := NewRegistry()
	name := "workspace.read"

	reg.Register(mkEntry(name, "builtin:workspace", LocalToolTimeout, ptools.RiskLow))
	prev := reg.Register(mkEntry(name, "js:greeter", LocalToolTimeout, ptools.RiskMedium))

	if prev == nil {
		t.Fatal("expected previous entry on override")
	}
	if prev.Definition.Name != name {
		t.Fatalf("prev name = %q", prev.Definition.Name)
	}
	if prev.Source != "builtin:workspace" {
		t.Fatalf("prev Source = %q, want builtin:workspace", prev.Source)
	}
	// Prev's Overriden must be cleared after being overridden.
	if prev.Overriden != "" {
		t.Errorf("prev.Overriden = %q, want empty", prev.Overriden)
	}

	entry, ok := reg.Lookup(name)
	if !ok {
		t.Fatal("lookup failed after override")
	}
	if entry.Source != "js:greeter" {
		t.Fatalf("current Source = %q, want js:greeter", entry.Source)
	}
	if entry.Overriden != "builtin:workspace" {
		t.Fatalf("current Overriden = %q, want builtin:workspace", entry.Overriden)
	}
}

func TestRegistryListBySource(t *testing.T) {
	reg := NewRegistry()
	reg.Register(mkEntry("builtin.one", "builtin:core", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("builtin.two", "builtin:state", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("js.hello", "js:greeter", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("mcp.fetch", "mcp:fetch", LocalToolTimeout, ptools.RiskLow))

	builtins := reg.ListBySource("builtin:")
	if len(builtins) != 2 {
		t.Fatalf("builtin tools = %d, want 2", len(builtins))
	}
	if builtins[0] != "builtin.one" || builtins[1] != "builtin.two" {
		t.Fatalf("builtins = %v", builtins)
	}

	jsTools := reg.ListBySource("js:")
	if len(jsTools) != 1 || jsTools[0] != "js.hello" {
		t.Fatalf("js tools = %v", jsTools)
	}

	none := reg.ListBySource("nonexist:")
	if len(none) != 0 {
		t.Fatalf("expected empty, got %v", none)
	}
}

func TestRegistryClear(t *testing.T) {
	reg := NewRegistry()
	reg.Register(mkEntry("builtin.read", "builtin:workspace", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("js.hello", "js:greeter", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("mcp.fetch", "mcp:fetch", LocalToolTimeout, ptools.RiskLow))

	reg.Clear("js:")

	if _, ok := reg.Lookup("builtin.read"); !ok {
		t.Fatal("builtin.read should still exist")
	}
	if _, ok := reg.Lookup("js.hello"); ok {
		t.Fatal("js.hello should have been cleared")
	}
	if _, ok := reg.Lookup("mcp.fetch"); !ok {
		t.Fatal("mcp.fetch should still exist")
	}

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("definitions after clear = %d, want 2", len(defs))
	}
}

func TestRegistryRemove(t *testing.T) {
	reg := NewRegistry()
	reg.Register(mkEntry("a", "builtin:core", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("b", "builtin:core", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("c", "builtin:core", LocalToolTimeout, ptools.RiskLow))

	reg.Remove("b")
	if _, ok := reg.Lookup("b"); ok {
		t.Fatal("b should have been removed")
	}

	defs := reg.Definitions()
	if len(defs) != 2 {
		t.Fatalf("definitions = %d, want 2", len(defs))
	}
	for _, d := range defs {
		if d.Name == "b" {
			t.Fatal("b is still in Definitions")
		}
	}

	// Removing non-existent name should be a no-op.
	reg.Remove("nonexistent")
}

func TestRegistryTimeoutDuration(t *testing.T) {
	tests := []struct {
		c    TimeoutClass
		want time.Duration
	}{
		{LocalToolTimeout, 30 * time.Second},
		{GatewayToolTimeout, 30 * time.Second},
		{SelfManagedToolTimeout, 0},
	}
	for _, tt := range tests {
		if got := tt.c.Duration(); got != tt.want {
			t.Errorf("%v.Duration() = %v, want %v", tt.c, got, tt.want)
		}
	}
}
