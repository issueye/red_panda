package state

import (
	"testing"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	ptools "redpanda/protocol/tools"
)

func TestRegisterMetadata(t *testing.T) {
	reg := registry.NewRegistry()
	names := Register(reg, hooks.NewBus(), Dependencies{})
	if len(names) != 8 {
		t.Fatalf("names = %d, want 8", len(names))
	}
	for _, name := range names {
		entry, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("missing entry %s", name)
		}
		if entry.Source != "builtin:state" {
			t.Fatalf("%s source = %q", name, entry.Source)
		}
		if (name == "web.search" || name == "web.fetch") && entry.Definition.Risk != ptools.RiskHigh {
			t.Fatalf("%s risk = %s, want high", name, entry.Definition.Risk)
		}
	}
}
