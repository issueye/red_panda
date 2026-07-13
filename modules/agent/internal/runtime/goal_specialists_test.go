package runtime

import (
	"strings"
	"testing"

	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func TestLookupGoalSpecialists(t *testing.T) {
	for _, key := range []string{
		"goal-analyst", "goal-planner", "goal-implementer", "goal-verifier", "goal-evaluator",
	} {
		spec, ok := lookupGoalSpecialist(key)
		if !ok || spec.Key != key {
			t.Fatalf("missing specialist %s", key)
		}
		if strings.TrimSpace(spec.SystemPrompt) == "" {
			t.Fatalf("empty prompt for %s", key)
		}
		if spec.DefaultMaxTurns <= 0 {
			t.Fatalf("bad default turns for %s", key)
		}
	}
	// Normalization
	if _, ok := lookupGoalSpecialist("Goal_Analyst"); !ok {
		t.Fatal("expected normalize Goal_Analyst -> goal-analyst")
	}
	if isGoalSpecialistName("random-worker") {
		t.Fatal("random-worker should not be specialist")
	}
}

func TestResolveGoalSpecialistPrefersGatewayPromptAndTurns(t *testing.T) {
	builtin, ok := lookupGoalSpecialist("goal-analyst")
	if !ok {
		t.Fatal("builtin analyst")
	}
	defs := []methods.AgentDefinitionRef{{
		Key:             "goal-analyst",
		NameZH:          "自定义分析师",
		Phase:           "analyze",
		SystemPrompt:    "CUSTOM PROMPT FROM GATEWAY SETTINGS",
		DefaultMaxTurns: 7,
		Enabled:         true,
	}}
	spec, ok := resolveGoalSpecialist(defs, "goal-analyst")
	if !ok {
		t.Fatal("resolve failed")
	}
	if spec.SystemPrompt != "CUSTOM PROMPT FROM GATEWAY SETTINGS" {
		t.Fatalf("prompt = %q, want gateway override", spec.SystemPrompt)
	}
	if spec.DefaultMaxTurns != 7 {
		t.Fatalf("turns = %d, want 7", spec.DefaultMaxTurns)
	}
	if spec.NameZH != "自定义分析师" {
		t.Fatalf("name_zh = %q", spec.NameZH)
	}
	// Tool policy stays Runtime-owned.
	if len(spec.Allowlist) != len(builtin.Allowlist) {
		t.Fatalf("allowlist should remain builtin: got %v want %v", spec.Allowlist, builtin.Allowlist)
	}
	for _, name := range contextShareTools {
		found := false
		for _, a := range spec.Allowlist {
			if a == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("context tool %s missing after gateway merge", name)
		}
	}
}

func TestResolveGoalSpecialistDisabledDefinition(t *testing.T) {
	defs := []methods.AgentDefinitionRef{{
		Key:     "goal-analyst",
		Enabled: false,
	}}
	if _, ok := resolveGoalSpecialist(defs, "goal-analyst"); ok {
		t.Fatal("disabled gateway definition must not resolve")
	}
}

func TestResolveGoalSpecialistFallsBackWithoutCatalog(t *testing.T) {
	spec, ok := resolveGoalSpecialist(nil, "goal-planner")
	if !ok || spec.Key != "goal-planner" {
		t.Fatalf("fallback missing: %#v", spec)
	}
	if !strings.Contains(spec.SystemPrompt, "goal-planner") {
		t.Fatalf("expected builtin prompt, got %q", spec.SystemPrompt)
	}
}

func TestApplyGoalSpecialistAnalystIsReadOnly(t *testing.T) {
	spec, ok := lookupGoalSpecialist("goal-analyst")
	if !ok {
		t.Fatal("analyst")
	}
	params := methods.ReplyParams{
		Options: methods.ReplyOptions{
			ToolDenylist: []string{"custom.denied"},
		},
		Input: methods.ReplyInput{Text: "analyze login"},
	}
	turns := (&Runtime{}).applyGoalSpecialist(&params, spec, "analyze login", 0, "run_test", "sess_test", "", "")
	if turns != spec.DefaultMaxTurns {
		t.Fatalf("turns = %d, want %d", turns, spec.DefaultMaxTurns)
	}
	if params.Options.GoalContext != nil || params.Options.TodoContext != nil {
		t.Fatal("goal/todo context must be nil for specialists")
	}
	if params.Options.SpawnSubAgents {
		t.Fatal("spawn subagents must be false")
	}
	// Allowlist must include reads + context share tools and exclude writes via availableToolsForOptions.
	defs := []tools.Definition{
		{Name: "workspace.read_file"},
		{Name: "workspace.write_file"},
		{Name: "shell.exec"},
		{Name: "goal.write"},
		{Name: "todo.write"},
		{Name: "subagent.run"},
		{Name: "web.search"},
		{Name: "context.read"},
		{Name: "context.search"},
		{Name: "context.write"},
		{Name: "context.replace"},
	}
	filtered := availableToolsForOptions(defs, params.Options)
	names := map[string]bool{}
	for _, d := range filtered {
		names[d.Name] = true
	}
	if !names["workspace.read_file"] {
		t.Fatalf("read should be allowed: %#v", filtered)
	}
	if !names["web.search"] {
		t.Fatalf("web.search should be allowed for analyst: %#v", filtered)
	}
	for _, ctxTool := range contextShareTools {
		if !names[ctxTool] {
			t.Fatalf("%s should be allowed for analyst: %#v", ctxTool, filtered)
		}
	}
	for _, denied := range []string{"workspace.write_file", "shell.exec", "goal.write", "todo.write", "subagent.run"} {
		if names[denied] {
			t.Fatalf("%s should be denied for analyst: %#v", denied, filtered)
		}
	}
	if !strings.Contains(params.Options.MemoryContext.Context, "goal-analyst") {
		t.Fatalf("system prompt missing specialist key: %s", params.Options.MemoryContext.Context)
	}
}

func TestApplyGoalSpecialistImplementerAllowsWriteDeniesGoal(t *testing.T) {
	spec, _ := lookupGoalSpecialist("goal-implementer")
	params := methods.ReplyParams{Options: methods.ReplyOptions{}}
	_ = (&Runtime{}).applyGoalSpecialist(&params, spec, "implement step", 100, "run_test", "sess_test", "", "")
	// Cap to CapMaxTurns
	if params.Options.MaxToolTurns != spec.CapMaxTurns {
		t.Fatalf("max turns = %d, want cap %d", params.Options.MaxToolTurns, spec.CapMaxTurns)
	}
	defs := []tools.Definition{
		{Name: "workspace.write_file"},
		{Name: "shell.exec"},
		{Name: "goal.write"},
		{Name: "todo.write"},
		{Name: "web.search"},
		{Name: "subagent.run"},
		{Name: "context.read"},
		{Name: "context.write"},
	}
	filtered := availableToolsForOptions(defs, params.Options)
	names := map[string]bool{}
	for _, d := range filtered {
		names[d.Name] = true
	}
	if !names["workspace.write_file"] || !names["shell.exec"] {
		t.Fatalf("implementer should allow write/shell: %#v", filtered)
	}
	if !names["context.read"] || !names["context.write"] {
		t.Fatalf("implementer should allow context tools (no allowlist): %#v", filtered)
	}
	for _, denied := range []string{"goal.write", "todo.write", "web.search", "subagent.run"} {
		if names[denied] {
			t.Fatalf("%s should be denied: %#v", denied, filtered)
		}
	}
}

func TestApplyGoalSpecialistVerifierDeniesWrite(t *testing.T) {
	spec, _ := lookupGoalSpecialist("goal-verifier")
	params := methods.ReplyParams{Options: methods.ReplyOptions{}}
	_ = (&Runtime{}).applyGoalSpecialist(&params, spec, "verify", 0, "run_test", "sess_test", "", "")
	defs := []tools.Definition{
		{Name: "workspace.read_file"},
		{Name: "shell.exec"},
		{Name: "workspace.write_file"},
		{Name: "workspace.apply_patch"},
		{Name: "context.read"},
		{Name: "context.write"},
	}
	filtered := availableToolsForOptions(defs, params.Options)
	names := map[string]bool{}
	for _, d := range filtered {
		names[d.Name] = true
	}
	if !names["workspace.read_file"] || !names["shell.exec"] {
		t.Fatalf("verifier should allow read/shell: %#v", filtered)
	}
	if !names["context.read"] || !names["context.write"] {
		t.Fatalf("verifier should allow context share tools: %#v", filtered)
	}
	if names["workspace.write_file"] || names["workspace.apply_patch"] {
		t.Fatalf("verifier must deny writes: %#v", filtered)
	}
}

func TestAllAllowlistedSpecialistsExposeContextShareTools(t *testing.T) {
	defs := make([]tools.Definition, 0, len(contextShareTools)+2)
	for _, name := range contextShareTools {
		defs = append(defs, tools.Definition{Name: name})
	}
	defs = append(defs, tools.Definition{Name: "workspace.read_file"}, tools.Definition{Name: "goal.write"})

	for _, key := range []string{"goal-analyst", "goal-planner", "goal-verifier", "goal-evaluator"} {
		spec, ok := lookupGoalSpecialist(key)
		if !ok {
			t.Fatalf("missing %s", key)
		}
		if len(spec.Allowlist) == 0 {
			t.Fatalf("%s expected non-empty allowlist", key)
		}
		params := methods.ReplyParams{Options: methods.ReplyOptions{}}
		_ = (&Runtime{}).applyGoalSpecialist(&params, spec, "task", 0, "run_test", "sess_test", "", "")
		filtered := availableToolsForOptions(defs, params.Options)
		names := map[string]bool{}
		for _, d := range filtered {
			names[d.Name] = true
		}
		for _, ctxTool := range contextShareTools {
			if !names[ctxTool] {
				t.Fatalf("%s missing %s in available tools: allowlist=%v filtered=%v", key, ctxTool, spec.Allowlist, names)
			}
		}
		if names["goal.write"] {
			t.Fatalf("%s must not expose goal.write", key)
		}
	}
}

func TestGoalSpecialistDisplayNameZH(t *testing.T) {
	if got := goalSpecialistDisplayName("goal-verifier"); got != "目标验证者" {
		t.Fatalf("display = %q", got)
	}
	if got := goalSpecialistDisplayName("other"); got != "other" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestGoalSpecialistMustMatchCurrentPipelinePhase(t *testing.T) {
	rt := &Runtime{}
	rt.setRunGoal("run_goal_phase", &runGoalState{
		Goal: methods.GoalDTO{
			ID:            "goal_1",
			Status:        "active",
			PipelinePhase: "analyze",
		},
		BoundToThisRun: true,
	})

	analyst, _ := lookupGoalSpecialist("goal-analyst")
	if err := rt.validateGoalSpecialistPhase("run_goal_phase", analyst); err != nil {
		t.Fatalf("analyst should be valid in analyze: %v", err)
	}
	planner, _ := lookupGoalSpecialist("goal-planner")
	if err := rt.validateGoalSpecialistPhase("run_goal_phase", planner); err == nil || !strings.Contains(err.Error(), "current phase") {
		t.Fatalf("planner should be rejected in analyze, got %v", err)
	}
}

func TestDisableGoalPipelineForChildClearsInheritedBinding(t *testing.T) {
	enabled := true
	options := methods.ReplyOptions{
		GoalsEnabled: &enabled,
		GoalID:       "goal_parent",
		GoalContext:  &methods.GoalContext{GoalID: "goal_parent"},
	}

	disableGoalPipelineForChild(&options)
	if options.GoalsEnabled == nil || *options.GoalsEnabled {
		t.Fatalf("child goals enabled = %#v, want false", options.GoalsEnabled)
	}
	if options.GoalID != "" || options.GoalContext != nil {
		t.Fatalf("child retained goal binding: id=%q context=%#v", options.GoalID, options.GoalContext)
	}
}
