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
		{domain: "", tool: "todo_write", wantErr: true}, // removed alias
		{domain: "", tool: "segment_end", wantErr: true}, // removed internal name
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
	if !StateToolRequiresSession(StateToolDomainTodo) {
		t.Fatal("todo must require session")
	}
}

func TestStateToolParamsProjections(t *testing.T) {
	p := NewStateToolParams(StateToolDomainMemory, "run_1", "sess_1", "/ws", "tc_1", "memory.list", map[string]any{"x": 1})
	mem := p.AsMemoryParams()
	if mem.WorkspaceRoot != "/ws" || mem.ToolCallID != "tc_1" {
		t.Fatalf("memory projection mismatch: %#v", mem)
	}
}
