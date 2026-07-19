package tools

import (
	"reflect"
	"testing"

	ptools "redpanda/protocol/tools"
)

func TestStableToolRegistryPreservesPublicOrder(t *testing.T) {
	want := []string{
		"workspace.read_file", "workspace.list", "workspace.stats", "workspace.grep", "workspace.find_files", "workspace.read_files",
		"workspace.write_file", "workspace.edit_file", "workspace.diff_file", "workspace.apply_patch",
		"git.status", "git.diff", "git.log", "git.show",
		"shell.exec",
		"skill.list", "skill.create", "skill.update", "skill.delete", "skill.run",
		"worker.delegate", "worker.list", "worker.cancel", "worker.pool_status", "worker.send", "worker.receive",
		"todo.write",
		"goal.create", "goal.plan", "goal.observe", "goal.assess", "goal.finish", "goal.list",
		"todo.list",
		"memory.list", "memory.create", "memory.update", "memory.delete",
		"web.search", "web.fetch",
		"context.read", "context.search", "context.write", "context.replace", "context.delete",
	}

	definitions := (ToolRunner{}).AvailableTools()
	got := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		got = append(got, definition.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("available tool order changed:\n got: %v\nwant: %v", got, want)
	}
}

func TestStableToolRegistryProvidesDefinitionAndTimeout(t *testing.T) {
	definitions := (ToolRunner{}).AvailableTools()
	if len(definitions) == 0 {
		t.Fatal("expected stable tool definitions")
	}

	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		if definition.DisplayName == "" || definition.Description == "" || definition.Risk == "" || definition.Parameters == nil {
			t.Fatalf("incomplete definition for %s: %#v", definition.Name, definition)
		}
		if _, duplicate := seen[definition.Name]; duplicate {
			t.Fatalf("duplicate stable tool %s", definition.Name)
		}
		seen[definition.Name] = struct{}{}
		if _, ok := stableToolTimeoutFor(definition.Name); !ok {
			t.Fatalf("stable tool %s has no timeout class", definition.Name)
		}
	}

	checks := map[string]struct {
		wantRisk    ptools.Risk
		wantTimeout int64
	}{
		"workspace.read_file": {wantRisk: ptools.RiskLow, wantTimeout: int64(defaultLocalToolTimeout)},
		"git.diff":            {wantRisk: ptools.RiskLow, wantTimeout: int64(defaultLocalToolTimeout)},
		"memory.create":       {wantRisk: ptools.RiskHigh, wantTimeout: int64(defaultGatewayToolTimeout)},
		"context.write":       {wantRisk: ptools.RiskHigh, wantTimeout: int64(defaultGatewayToolTimeout)},
		"shell.exec":          {wantRisk: ptools.RiskHigh, wantTimeout: 0},
	}
	byName := make(map[string]ptools.Definition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}
	for name, check := range checks {
		if byName[name].Risk != check.wantRisk {
			t.Fatalf("%s risk = %s, want %s", name, byName[name].Risk, check.wantRisk)
		}
		if got := int64(toolTimeoutFor(name)); got != check.wantTimeout {
			t.Fatalf("%s timeout = %d, want %d", name, got, check.wantTimeout)
		}
	}
}

func TestToolTimeoutCompatibilityFallbacks(t *testing.T) {
	// Removed alias is no longer mapped to gateway timeout (docs/47 E-cutover).
	if got := toolTimeoutFor("todo_write"); got != defaultLocalToolTimeout {
		t.Fatalf("todo_write timeout = %s, want %s (alias removed)", got, defaultLocalToolTimeout)
	}
	if got := toolTimeoutFor("todo.write"); got != defaultGatewayToolTimeout {
		t.Fatalf("todo.write timeout = %s, want %s", got, defaultGatewayToolTimeout)
	}
	if got := toolTimeoutFor("mcp__server__dynamic"); got != 0 {
		t.Fatalf("dynamic MCP timeout = %s, want self-managed", got)
	}
	if got := toolTimeoutFor("unknown.tool"); got != defaultLocalToolTimeout {
		t.Fatalf("unknown tool timeout = %s, want %s", got, defaultLocalToolTimeout)
	}
}
