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
		{Name: "worker.send"},
		{Name: "worker.receive"},
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
	for _, hide := range []string{
		"skill.create", "skill.update", "skill.delete",
		"worker.send", "worker.receive",
		"worker.pool_status", "worker.pool_resize", "worker.pool_reset",
	} {
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

	// 委托 Worker 可经 allowlist 重新打开 mailbox 工具（docs/41 W1-2）。
	mailbox := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		ToolAllowlist: []string{"workspace.read_file", "worker.send", "worker.receive"},
	})
	mailboxNames := map[string]bool{}
	for _, d := range mailbox {
		mailboxNames[d.Name] = true
	}
	if !mailboxNames["worker.send"] || !mailboxNames["worker.receive"] {
		t.Fatalf("allowlist should re-enable mailbox tools: %#v", mailboxNames)
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

func TestContextToolsRemovedWithGoalFeature(t *testing.T) {
	// With the Goal feature removed, context.* and goal.* tools no longer exist.
	// Only the allowlist (pass-through) and denylist filtering remain, and no
	// goal-mode tightening is applied. This test pins that behavior: providing
	// a generic ToolAllowlist simply filters definitions, with no goal gating.
	definitions := []ptools.Definition{
		{Name: "workspace.read_file"},
		{Name: "memory.create"},
		{Name: "todo.write"},
	}
	open := AvailableToolsForOptions(definitions, methods.ReplyOptions{
		ToolAllowlist: []string{"workspace.read_file", "memory.create", "todo.write"},
	})
	openNames := map[string]bool{}
	for _, d := range open {
		openNames[d.Name] = true
	}
	if !openNames["memory.create"] {
		t.Fatalf("non-goal chat should expose memory.create: %#v", openNames)
	}
	if !openNames["todo.write"] {
		t.Fatalf("non-goal chat should expose todo.write: %#v", openNames)
	}
	if !openNames["workspace.read_file"] {
		t.Fatalf("non-goal chat should expose workspace.read_file: %#v", openNames)
	}
}
