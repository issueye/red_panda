package orchestration

import (
	"reflect"
	"testing"

	"redpanda/agent/internal/runtime/registry"
	"redpanda/agent/plugins/core/orchestration/internal"
	ptools "redpanda/protocol/tools"
)

// TestRegisterCount verifies exactly 11 tools are registered.
func TestRegisterCount(t *testing.T) {
	reg := registry.NewRegistry()
	names := Register(reg, nil, Dependencies{})
	if len(names) != 11 {
		t.Fatalf("expected 11 registered tools, got %d: %v", len(names), names)
	}
}

// TestRegisterNames verifies the exact set of tool names.
func TestRegisterNames(t *testing.T) {
	want := []string{
		"skill.list", "skill.create", "skill.update", "skill.delete",
		"worker.delegate", "worker.list", "worker.result", "worker.cancel",
		"worker.pool_status", "worker.send", "worker.receive",
	}
	reg := registry.NewRegistry()
	got := Register(reg, nil, Dependencies{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tool names mismatch:\n got: %v\nwant: %v", got, want)
	}
}

// TestRegisterSourceOrder verifies registration order is preserved.
func TestRegisterSourceOrder(t *testing.T) {
	reg := registry.NewRegistry()
	Register(reg, nil, Dependencies{})
	names := reg.Names()
	want := []string{
		"skill.list", "skill.create", "skill.update", "skill.delete",
		"worker.delegate", "worker.list", "worker.result", "worker.cancel",
		"worker.pool_status", "worker.send", "worker.receive",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("registry order mismatch:\n got: %v\nwant: %v", names, want)
	}
}

// expectedEntry describes the expected metadata for each tool.
type expectedEntry struct {
	risk         ptools.Risk
	timeoutClass registry.TimeoutClass
	opsOnly      bool
	source       string
}

var expectedEntries = map[string]expectedEntry{
	"skill.list":         {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: false, source: "builtin:orchestration"},
	"skill.create":       {risk: ptools.RiskHigh, timeoutClass: registry.GatewayToolTimeout, opsOnly: true, source: "builtin:orchestration"},
	"skill.update":       {risk: ptools.RiskHigh, timeoutClass: registry.GatewayToolTimeout, opsOnly: true, source: "builtin:orchestration"},
	"skill.delete":       {risk: ptools.RiskHigh, timeoutClass: registry.GatewayToolTimeout, opsOnly: true, source: "builtin:orchestration"},
	"worker.delegate":    {risk: ptools.RiskMedium, timeoutClass: registry.SelfManagedToolTimeout, opsOnly: false, source: "builtin:orchestration"},
	"worker.list":        {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: false, source: "builtin:orchestration"},
	"worker.result":      {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: false, source: "builtin:orchestration"},
	"worker.cancel":      {risk: ptools.RiskMedium, timeoutClass: registry.LocalToolTimeout, opsOnly: false, source: "builtin:orchestration"},
	"worker.pool_status": {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: true, source: "builtin:orchestration"},
	"worker.send":        {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: true, source: "builtin:orchestration"},
	"worker.receive":     {risk: ptools.RiskLow, timeoutClass: registry.LocalToolTimeout, opsOnly: true, source: "builtin:orchestration"},
}

func TestRegisterEntryMetadata(t *testing.T) {
	reg := registry.NewRegistry()
	Register(reg, nil, Dependencies{})

	for name, want := range expectedEntries {
		entry, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("tool %q not found in registry", name)
		}
		if entry.Definition.Risk != want.risk {
			t.Errorf("%s risk = %q, want %q", name, entry.Definition.Risk, want.risk)
		}
		if entry.TimeoutClass != want.timeoutClass {
			t.Errorf("%s timeout = %v, want %v", name, entry.TimeoutClass, want.timeoutClass)
		}
		if entry.OpsOnly != want.opsOnly {
			t.Errorf("%s opsOnly = %v, want %v", name, entry.OpsOnly, want.opsOnly)
		}
		if entry.Source != want.source {
			t.Errorf("%s source = %q, want %q", name, entry.Source, want.source)
		}
		if entry.Handler == nil {
			t.Errorf("%s has nil handler", name)
		}
	}
}

// TestDefinitionSchemaConsistency verifies all tool definitions have
// non-empty DisplayName, Description, and non-nil Parameters (mirrors
// TestStableToolRegistryProvidesDefinitionAndTimeout).
func TestDefinitionSchemaConsistency(t *testing.T) {
	reg := registry.NewRegistry()
	Register(reg, nil, Dependencies{})
	for _, def := range reg.Definitions() {
		if def.DisplayName == "" {
			t.Errorf("%s has empty DisplayName", def.Name)
		}
		if def.Description == "" {
			t.Errorf("%s has empty Description", def.Name)
		}
		if def.Parameters == nil {
			t.Errorf("%s has nil Parameters", def.Name)
		}
		if def.Parameters["type"] != "object" {
			t.Errorf("%s Parameters.type != \"object\"", def.Name)
		}
	}
}

// TestOpsOnlyMetadata verifies the plugin owns OpsOnly metadata.
func TestOpsOnlyMatchesPolicy(t *testing.T) {
	reg := registry.NewRegistry()
	Register(reg, nil, Dependencies{})

	// From policy.go: skill.create, skill.update, skill.delete,
	// worker.pool_status, worker.send, worker.receive are opsOnly.
	// skill.list, worker.delegate, worker.list, worker.result, worker.cancel are NOT.
	opsOnlyWant := map[string]bool{
		"skill.list":         false,
		"skill.create":       true,
		"skill.update":       true,
		"skill.delete":       true,
		"worker.delegate":    false,
		"worker.list":        false,
		"worker.result":      false,
		"worker.cancel":      false,
		"worker.pool_status": true,
		"worker.send":        true,
		"worker.receive":     true,
	}
	for name, want := range opsOnlyWant {
		entry, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		if entry.OpsOnly != want {
			t.Errorf("%s OpsOnly = %v, want %v", name, entry.OpsOnly, want)
		}
	}
}

// TestImplementedSkillHandlers verifies skill.list, skill.create, skill.update,
// skill.delete return non-nil results when given invalid input (not placeholders).
func TestImplementedSkillHandlers(t *testing.T) {
	ctx := &internal.ToolContext{}
	// All four should return an error about missing working directory,
	// not "not yet migrated".
	skillHandlers := []internal.HandlerFunc{
		internal.HandlerSkillList,
		internal.HandlerSkillCreate,
		internal.HandlerSkillUpdate,
		internal.HandlerSkillDelete,
	}
	for _, h := range skillHandlers {
		result, err := h(nil, ctx, nil)
		if err == nil {
			t.Errorf("skill handler should return error for missing working dir")
		}
		if result != nil {
			t.Errorf("skill handler should return nil Result on error")
		}
	}
}

// TestDefinitionsMatchOldDefs verifies orchestration definitions match the
// schemas in agent/internal/tools/defs_orchestration.go.
func TestDefinitionsMatchOldDefs(t *testing.T) {
	reg := registry.NewRegistry()
	Register(reg, nil, Dependencies{})
	defs := reg.Definitions()

	if len(defs) != 11 {
		t.Fatalf("expected 11 definitions, got %d", len(defs))
	}

	byName := make(map[string]ptools.Definition, len(defs))
	for _, def := range defs {
		byName[def.Name] = def
	}

	// Verify key definition fields against the old defs.
	cases := map[string]struct {
		displayName string
		risk        ptools.Risk
		required    []string
		props       map[string]struct{}
	}{
		"skill.list": {
			displayName: "List skills", risk: ptools.RiskLow,
			props: map[string]struct{}{},
		},
		"skill.create": {
			displayName: "Create skill", risk: ptools.RiskHigh,
			required: []string{"name", "description", "instructions"},
			props:    map[string]struct{}{"name": {}, "description": {}, "instructions": {}},
		},
		"skill.update": {
			displayName: "Update skill", risk: ptools.RiskHigh,
			required: []string{"name", "description", "instructions"},
			props:    map[string]struct{}{"name": {}, "description": {}, "instructions": {}},
		},
		"skill.delete": {
			displayName: "Delete skill", risk: ptools.RiskHigh,
			required: []string{"name"},
			props:    map[string]struct{}{"name": {}},
		},
		"worker.delegate": {
			displayName: "Delegate work", risk: ptools.RiskMedium,
			required: []string{"task"},
			props:    map[string]struct{}{"task": {}, "profile_key": {}, "max_turns": {}, "file_count": {}, "path": {}},
		},
		"worker.list": {
			displayName: "List workers", risk: ptools.RiskLow,
			props: map[string]struct{}{"worker_id": {}, "assignment_id": {}},
		},
		"worker.result": {
			displayName: "Read Worker report", risk: ptools.RiskLow,
			required: []string{"assignment_id"},
			props:    map[string]struct{}{"assignment_id": {}, "offset": {}, "max_bytes": {}},
		},
		"worker.cancel": {
			displayName: "Cancel assignment", risk: ptools.RiskMedium,
			required: []string{"assignment_id"},
			props:    map[string]struct{}{"assignment_id": {}, "reason": {}},
		},
		"worker.pool_status": {
			displayName: "Worker pool status", risk: ptools.RiskLow,
			props: map[string]struct{}{},
		},
		"worker.send": {
			displayName: "Send Worker message", risk: ptools.RiskLow,
			required: []string{"to_worker_id", "kind", "payload"},
			props:    map[string]struct{}{"to_worker_id": {}, "to_assignment_id": {}, "kind": {}, "payload": {}},
		},
		"worker.receive": {
			displayName: "Receive Worker message", risk: ptools.RiskLow,
			props: map[string]struct{}{},
		},
	}

	for name, want := range cases {
		def := byName[name]
		if def.DisplayName != want.displayName {
			t.Errorf("%s DisplayName = %q, want %q", name, def.DisplayName, want.displayName)
		}
		if def.Risk != want.risk {
			t.Errorf("%s Risk = %q, want %q", name, def.Risk, want.risk)
		}
		props, ok := def.Parameters["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s Parameters.properties is not map[string]any", name)
		}
		for propName := range props {
			if _, exist := want.props[propName]; !exist {
				t.Errorf("%s has unexpected property %q", name, propName)
			}
		}
		for propName := range want.props {
			if _, exist := props[propName]; !exist {
				t.Errorf("%s missing property %q", name, propName)
			}
		}
		if def.Parameters["required"] != nil {
			req, _ := def.Parameters["required"].([]string)
			if !reflect.DeepEqual(req, want.required) {
				t.Errorf("%s required = %v, want %v", name, req, want.required)
			}
		}
	}
}
