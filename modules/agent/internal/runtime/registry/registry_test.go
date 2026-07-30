package registry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

func mkEntry(name, src string, tc TimeoutClass, risk ptools.Risk) ToolEntry {
	return ToolEntry{
		Definition: ptools.Definition{
			Name:       name,
			Risk:       risk,
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
	prev, err := reg.Register(mkEntry(name, "js:greeter", LocalToolTimeout, ptools.RiskMedium))
	if err != nil {
		t.Fatalf("register override: %v", err)
	}

	if prev == nil {
		t.Fatal("expected previous entry on override")
	}
	if prev.Definition.Name != name {
		t.Fatalf("prev name = %q", prev.Definition.Name)
	}
	if prev.Source != "builtin:workspace" {
		t.Fatalf("prev Source = %q, want builtin:workspace", prev.Source)
	}
	if prev.Overridden != "" {
		t.Errorf("prev.Overridden = %q, want empty", prev.Overridden)
	}

	entry, ok := reg.Lookup(name)
	if !ok {
		t.Fatal("lookup failed after override")
	}
	if entry.Source != "js:greeter" {
		t.Fatalf("current Source = %q, want js:greeter", entry.Source)
	}
	if entry.Overridden != "builtin:workspace" {
		t.Fatalf("current Overridden = %q, want builtin:workspace", entry.Overridden)
	}
}

func TestRegistryRejectsInvalidEntry(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Register(mkEntry("bad name", "builtin:core", LocalToolTimeout, ptools.RiskLow)); err == nil {
		t.Fatal("expected invalid tool name error")
	}
	if _, err := reg.Register(mkEntry("valid.name", "plugin:legacy", LocalToolTimeout, ptools.RiskLow)); err == nil {
		t.Fatal("expected invalid source error")
	}
	entry := mkEntry("valid.name", "builtin:core", LocalToolTimeout, ptools.RiskLow)
	entry.Handler = nil
	if _, err := reg.Register(entry); err == nil {
		t.Fatal("expected nil handler error")
	}
}

func TestRegistryClearSourceRestoresOverrideStack(t *testing.T) {
	reg := NewRegistry()
	name := "workspace.read"
	reg.Register(mkEntry(name, "builtin:workspace", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry(name, "js:first", LocalToolTimeout, ptools.RiskMedium))
	reg.Register(mkEntry(name, "js:second", LocalToolTimeout, ptools.RiskHigh))

	if removed := reg.ClearSource("js:second"); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	entry, ok := reg.Lookup(name)
	if !ok || entry.Source != "js:first" || entry.Overridden != "builtin:workspace" {
		t.Fatalf("after first unload = %#v, %v", entry, ok)
	}

	if removed := reg.ClearSource("js:"); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	entry, ok = reg.Lookup(name)
	if !ok || entry.Source != "builtin:workspace" || entry.Overridden != "" {
		t.Fatalf("after all JS unload = %#v, %v", entry, ok)
	}
	if names := reg.Names(); len(names) != 1 || names[0] != name {
		t.Fatalf("order changed after restore: %v", names)
	}
}

func TestRegistryReregisterSourceReplacesLayer(t *testing.T) {
	reg := NewRegistry()
	reg.Register(mkEntry("tool", "builtin:core", LocalToolTimeout, ptools.RiskLow))
	reg.Register(mkEntry("tool", "js:plugin", LocalToolTimeout, ptools.RiskMedium))
	reg.Register(mkEntry("tool", "js:plugin", SelfManagedToolTimeout, ptools.RiskHigh))

	if removed := reg.ClearSource("js:plugin"); removed != 1 {
		t.Fatalf("removed duplicate source layers = %d, want 1", removed)
	}
	entry, ok := reg.Lookup("tool")
	if !ok || entry.Source != "builtin:core" {
		t.Fatalf("expected builtin restore, got %#v, %v", entry, ok)
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

	reg.ClearSource("js:")

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

	if !reg.Remove("b", "builtin:core") {
		t.Fatal("expected b removal")
	}
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
	if reg.Remove("nonexistent", "builtin:core") {
		t.Fatal("nonexistent removal should report false")
	}
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

func TestRegistryConcurrentRegisterLookupAndClearSource(t *testing.T) {
	reg := NewRegistry()
	for i := 0; i < 10; i++ {
		reg.MustRegister(mkEntry(fmt.Sprintf("tool.%d", i), "builtin:core", LocalToolTimeout, ptools.RiskLow))
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			_, _ = reg.Register(mkEntry(fmt.Sprintf("tool.%d", i%10), fmt.Sprintf("js:worker-%d", i), LocalToolTimeout, ptools.RiskLow))
		}(i)
		go func(i int) {
			defer wg.Done()
			_, _ = reg.Lookup(fmt.Sprintf("tool.%d", i%10))
		}(i)
		go func() {
			defer wg.Done()
			reg.ClearSource("js:")
		}()
	}
	wg.Wait()
	reg.ClearSource("js:")

	for i := 0; i < 10; i++ {
		entry, ok := reg.Lookup(fmt.Sprintf("tool.%d", i))
		if !ok || entry.Source != "builtin:core" {
			t.Fatalf("builtin layer not restored for tool.%d: %#v, %v", i, entry, ok)
		}
	}
}

func TestRegistryFilterDefinitionsUsesEntryOpsOnly(t *testing.T) {
	t.Setenv("RED_PANDA_DEBUG_TOOLS", "")
	reg := NewRegistry()
	regular := mkEntry("workspace.read", "builtin:workspace", LocalToolTimeout, ptools.RiskLow)
	ops := mkEntry("skill.create", "builtin:orchestration", GatewayToolTimeout, ptools.RiskHigh)
	ops.OpsOnly = true
	reg.MustRegister(regular)
	reg.MustRegister(ops)
	definitions := reg.Definitions()

	filtered := reg.FilterDefinitions(definitions, methods.ReplyOptions{})
	if len(filtered) != 1 || filtered[0].Name != "workspace.read" {
		t.Fatalf("default definitions = %#v", filtered)
	}
	filtered = reg.FilterDefinitions(definitions, methods.ReplyOptions{ToolAllowlist: []string{"workspace.read", "skill.create"}})
	if len(filtered) != 2 {
		t.Fatalf("allowlisted definitions = %#v", filtered)
	}
	filtered = reg.FilterDefinitions(definitions, methods.ReplyOptions{DebugTools: true, ToolDenylist: []string{"workspace.read"}})
	if len(filtered) != 1 || filtered[0].Name != "skill.create" {
		t.Fatalf("debug/deny definitions = %#v", filtered)
	}
}

func TestRunScopedRegistrySnapshotsGlobalAndRestoresOverride(t *testing.T) {
	global := NewRegistry()
	global.MustRegister(mkEntry("test.tool", "builtin:test", LocalToolTimeout, ptools.RiskLow))

	scoped := NewRunScopedRegistry(global)
	global.MustRegister(mkEntry("test.later", "builtin:test", LocalToolTimeout, ptools.RiskLow))
	if _, ok := scoped.Lookup("test.later"); ok {
		t.Fatal("run snapshot changed after global registration")
	}

	scoped.MustRegister(mkEntry("test.tool", "mcp:test", SelfManagedToolTimeout, ptools.RiskHigh))
	entry, ok := scoped.Lookup("test.tool")
	if !ok || entry.Source != "mcp:test" || entry.Overridden != "builtin:test" {
		t.Fatalf("scoped override = %#v", entry)
	}
	if removed := scoped.ClearSource("mcp:test"); removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	entry, ok = scoped.Lookup("test.tool")
	if !ok || entry.Source != "builtin:test" {
		t.Fatalf("restored entry = %#v", entry)
	}
}
