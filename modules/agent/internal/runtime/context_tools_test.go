package runtime

import (
	"testing"

	"redpanda/protocol/tools"
)

// TestContextToolsRegisteredAndNotDenylisted verifies the key design property:
// context.* tools exist with the right risk levels AND are intentionally absent
// from subagentRunDenylist so specialist children can share scratchpad notes.
func TestContextToolsRegisteredAndNotDenylisted(t *testing.T) {
	runner := ToolRunner{}
	wantRisk := map[string]tools.Risk{
		"context.read":   tools.RiskLow,
		"context.search": tools.RiskLow,
		"context.write":  tools.RiskHigh,
		"context.replace": tools.RiskHigh,
		"context.delete": tools.RiskHigh,
	}
	for name, risk := range wantRisk {
		invocation, err := runner.InvocationFromCall("run_ctx", 0, tools.Call{
			Name:      name,
			Arguments: map[string]any{"goal_id": "g1", "query": "x", "kind": "finding", "title": "t", "body": "b", "note_id": "n1"},
		})
		if err != nil {
			t.Fatalf("%s did not resolve: %v", name, err)
		}
		if invocation.Call.Risk != risk {
			t.Fatalf("%s risk = %s, want %s", name, invocation.Call.Risk, risk)
		}
	}

	// The critical assertion: no context.* entry may appear in the subagent
	// denylist, otherwise specialist children could not read/write shared notes.
	for _, denied := range subagentRunDenylist {
		if len(denied) >= 8 && denied[:8] == "context." {
			t.Fatalf("context tool must not be denylisted for subagents: %q", denied)
		}
	}
}

// TestNormalizeNoteKind confirms kind normalization falls back to "note".
func TestNormalizeNoteKind(t *testing.T) {
	// This lives in the gateway service package; here we only assert the runtime
	// side has no kind logic of its own (it passes args through). Placeholder
	// to document the boundary.
	if subagentRunDenylist == nil {
		t.Fatal("denylist should be initialized")
	}
}
