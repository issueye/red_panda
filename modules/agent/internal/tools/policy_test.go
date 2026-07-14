package tools

import (
	"testing"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

func TestEvaluateToolPolicy(t *testing.T) {
	highRiskShell := ptools.Call{Name: "shell.exec", Risk: ptools.RiskHigh}
	lowRiskRead := ptools.Call{Name: "workspace.read_file", Risk: ptools.RiskLow}

	tests := []struct {
		name     string
		options  methods.ReplyOptions
		call     ptools.Call
		expected ToolDecisionAction
	}{
		{
			name: "denylist wins",
			options: methods.ReplyOptions{
				ToolDenylist: []string{"shell.exec"},
				ToolPolicy:   "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionDeny,
		},
		{
			name: "allowlist blocks missing tool",
			options: methods.ReplyOptions{
				ToolAllowlist: []string{"workspace.read_file"},
				ToolPolicy:    "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionDeny,
		},
		{
			name: "allow all permits high risk",
			options: methods.ReplyOptions{
				ToolPolicy: "allow_all",
			},
			call:     highRiskShell,
			expected: ToolDecisionAllow,
		},
		{
			name: "ask all prompts low risk",
			options: methods.ReplyOptions{
				ToolPolicy: "ask_all",
			},
			call:     lowRiskRead,
			expected: ToolDecisionRequirePermission,
		},
		{
			name: "strict risk based prompts high risk",
			options: methods.ReplyOptions{
				PermissionMode: "strict",
				ToolPolicy:     "risk_based",
			},
			call:     highRiskShell,
			expected: ToolDecisionRequirePermission,
		},
		{
			name: "risk based allows low risk",
			options: methods.ReplyOptions{
				PermissionMode: "strict",
			},
			call:     lowRiskRead,
			expected: ToolDecisionAllow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := EvaluateToolPolicy(test.options, test.call)
			if decision.Action != test.expected {
				t.Fatalf("expected %s, got %s (%s)", test.expected, decision.Action, decision.Reason)
			}
		})
	}
}

func TestAvailableToolsForOptionsHidesDeniedAndUnlistedTools(t *testing.T) {
	definitions := []ptools.Definition{
		{Name: "workspace.read_file"},
		{Name: "skill.run"},
		{Name: "shell.exec"},
	}
	filtered := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		ToolAllowlist: []string{"workspace.read_file", "skill.run"},
		ToolDenylist:  []string{"skill.run"},
	})
	if len(filtered) != 1 || filtered[0].Name != "workspace.read_file" {
		t.Fatalf("filtered tools = %#v, want workspace.read_file only", filtered)
	}
}

func TestAvailableToolsHidesOpsOnlyToolsByDefault(t *testing.T) {
	t.Setenv("RED_PANDA_DEBUG_TOOLS", "")
	definitions := []ptools.Definition{
		{Name: "workspace.read_file"},
		{Name: "skill.run"},
		{Name: "skill.create"},
		{Name: "skill.update"},
		{Name: "skill.delete"},
		{Name: "worker.delegate"},
		{Name: "worker.pool_status"},
		{Name: "worker.pool_resize"},
		{Name: "worker.pool_reset"},
	}
	filtered := AvailableToolsForOptions(definitions, methods.ReplyOptions{})
	names := map[string]bool{}
	for _, d := range filtered {
		names[d.Name] = true
	}
	for _, keep := range []string{"workspace.read_file", "skill.run", "worker.delegate"} {
		if !names[keep] {
			t.Fatalf("expected %s in default tools: %#v", keep, names)
		}
	}
	for _, hide := range []string{"skill.create", "skill.update", "skill.delete", "worker.pool_status", "worker.pool_resize", "worker.pool_reset"} {
		if names[hide] {
			t.Fatalf("ops tool %s should be hidden by default: %#v", hide, names)
		}
	}

	// 显式允许列表启用单个运维工具。
	one := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		ToolAllowlist: []string{"skill.create", "workspace.read_file"},
	})
	if len(one) != 2 {
		t.Fatalf("allowlist opt-in = %#v", one)
	}

	// DebugTools 开放全部运维工具。
	debug := AvailableToolsForOptions(definitions, methods.ReplyOptions{DebugTools: true})
	if len(debug) != len(definitions) {
		t.Fatalf("debug tools len = %d, want %d", len(debug), len(definitions))
	}
}

func TestEvaluateToolPolicyDeniesOpsToolsWhenHidden(t *testing.T) {
	t.Setenv("RED_PANDA_DEBUG_TOOLS", "")
	decision := EvaluateToolPolicy(methods.ReplyOptions{ToolPolicy: "allow_all"}, ptools.Call{
		Name: "skill.create", Risk: ptools.RiskHigh,
	})
	if decision.Action != ToolDecisionDeny {
		t.Fatalf("expected deny for hidden ops tool, got %s (%s)", decision.Action, decision.Reason)
	}
	decision = EvaluateToolPolicy(methods.ReplyOptions{ToolPolicy: "allow_all", DebugTools: true}, ptools.Call{
		Name: "skill.create", Risk: ptools.RiskHigh,
	})
	if decision.Action != ToolDecisionAllow {
		t.Fatalf("expected allow when DebugTools, got %s (%s)", decision.Action, decision.Reason)
	}
}

func TestGoalModeDefaultAllowlistTightensTools(t *testing.T) {
	t.Setenv("RED_PANDA_DEBUG_TOOLS", "")
	enabled := true
	definitions := []ptools.Definition{
		{Name: "workspace.read_file"},
		{Name: "shell.exec"},
		{Name: "goal.create"},
		{Name: "context.read"},
		{Name: "worker.delegate"},
		{Name: "memory.create"},
		{Name: "skill.run"},
		{Name: "skill.create"},
		{Name: "web.search"},
	}
	// 已绑定目标且客户端未提供允许列表时，使用 Goal 默认集合。
	filtered := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		GoalsEnabled: &enabled,
	})
	names := map[string]bool{}
	for _, d := range filtered {
		names[d.Name] = true
	}
	for _, keep := range []string{"workspace.read_file", "shell.exec", "goal.create", "context.read", "worker.delegate", "skill.run", "web.search"} {
		if !names[keep] {
			t.Fatalf("goal mode should keep %s: %#v", keep, names)
		}
	}
	if names["memory.create"] {
		t.Fatalf("goal mode should hide memory.create by default: %#v", names)
	}
	if names["skill.create"] {
		t.Fatalf("goal mode should still hide ops skill.create: %#v", names)
	}

	// 客户端允许列表与目标默认值求交集，不能超出默认范围。
	narrow := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		GoalsEnabled:  &enabled,
		ToolAllowlist: []string{"workspace.read_file", "memory.create"},
	})
	if len(narrow) != 1 || narrow[0].Name != "workspace.read_file" {
		t.Fatalf("intersect should drop memory.create: %#v", narrow)
	}

	// 非目标聊天保留记忆工具，但排除仅限运维的工具。
	disabled := false
	open := AvailableToolsForOptions(definitions, methods.ReplyOptions{GoalsEnabled: &disabled})
	openNames := map[string]bool{}
	for _, d := range open {
		openNames[d.Name] = true
	}
	if !openNames["memory.create"] {
		t.Fatalf("non-goal chat should expose memory.create: %#v", openNames)
	}
	if openNames["goal.create"] {
		t.Fatalf("goals disabled should hide goal.create: %#v", openNames)
	}
}
