package methods

import "testing"

func TestResolveStateToolDomain(t *testing.T) {
	tests := []struct {
		domain, tool string
		want         string
		wantErr      bool
	}{
		{domain: "memory", tool: "", want: StateToolDomainMemory},
		{domain: "TODO", tool: "anything", want: StateToolDomainTodo},
		{domain: "", tool: "memory.list", want: StateToolDomainMemory},
		{domain: "", tool: "todo.write", want: StateToolDomainTodo},
		{domain: "", tool: "todo_write", want: StateToolDomainTodo},
		{domain: "", tool: "goal.assess", want: StateToolDomainGoal},
		{domain: "", tool: "segment_end", want: StateToolDomainGoal},
		{domain: "", tool: "context.read", want: StateToolDomainContext},
		{domain: "unknown", tool: "memory.list", wantErr: true},
		{domain: "", tool: "shell.exec", wantErr: true},
		{domain: "", tool: "", wantErr: true},
	}
	for _, test := range tests {
		got, err := ResolveStateToolDomain(test.domain, test.tool)
		if test.wantErr {
			if err == nil {
				t.Fatalf("domain=%q tool=%q: expected error, got %q", test.domain, test.tool, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("domain=%q tool=%q: %v", test.domain, test.tool, err)
		}
		if got != test.want {
			t.Fatalf("domain=%q tool=%q: got %q want %q", test.domain, test.tool, got, test.want)
		}
	}
}

func TestStateToolRequiresSession(t *testing.T) {
	if StateToolRequiresSession(StateToolDomainMemory) {
		t.Fatal("memory must not require session in envelope")
	}
	for _, domain := range []string{StateToolDomainTodo, StateToolDomainGoal, StateToolDomainContext} {
		if !StateToolRequiresSession(domain) {
			t.Fatalf("%s must require session", domain)
		}
	}
}

func TestStateToolParamsProjections(t *testing.T) {
	p := NewStateToolParams(StateToolDomainGoal, "run_1", "sess_1", "/ws", "tc_1", "goal.list", map[string]any{"x": 1})
	goal := p.AsGoalParams()
	if goal.RunID != "run_1" || goal.SessionID != "sess_1" || goal.ToolName != "goal.list" || goal.Arguments["x"] != 1 {
		t.Fatalf("goal projection mismatch: %#v", goal)
	}
	mem := p.AsMemoryParams()
	if mem.WorkspaceRoot != "/ws" || mem.ToolCallID != "tc_1" {
		t.Fatalf("memory projection mismatch: %#v", mem)
	}
}
