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
		opsOnly  bool
		expected ToolDecisionAction
	}{
		{name: "denylist wins", options: methods.ReplyOptions{ToolDenylist: []string{"shell.exec"}, ToolPolicy: "allow_all"}, call: highRiskShell, expected: ToolDecisionDeny},
		{name: "allowlist blocks missing tool", options: methods.ReplyOptions{ToolAllowlist: []string{"workspace.read_file"}, ToolPolicy: "allow_all"}, call: highRiskShell, expected: ToolDecisionDeny},
		{name: "allow all permits high risk", options: methods.ReplyOptions{ToolPolicy: "allow_all"}, call: highRiskShell, expected: ToolDecisionAllow},
		{name: "ask all prompts low risk", options: methods.ReplyOptions{ToolPolicy: "ask_all"}, call: lowRiskRead, expected: ToolDecisionRequirePermission},
		{name: "strict risk based prompts high risk", options: methods.ReplyOptions{PermissionMode: "strict", ToolPolicy: "risk_based"}, call: highRiskShell, expected: ToolDecisionRequirePermission},
		{name: "risk based allows low risk", options: methods.ReplyOptions{PermissionMode: "strict"}, call: lowRiskRead, expected: ToolDecisionAllow},
		{name: "hidden ops tool denied", options: methods.ReplyOptions{ToolPolicy: "allow_all"}, call: ptools.Call{Name: "skill.create", Risk: ptools.RiskHigh}, opsOnly: true, expected: ToolDecisionDeny},
		{name: "debug enables ops tool", options: methods.ReplyOptions{ToolPolicy: "allow_all", DebugTools: true}, call: ptools.Call{Name: "skill.create", Risk: ptools.RiskHigh}, opsOnly: true, expected: ToolDecisionAllow},
		{name: "allowlist enables ops tool", options: methods.ReplyOptions{ToolPolicy: "allow_all", ToolAllowlist: []string{"skill.create"}}, call: ptools.Call{Name: "skill.create", Risk: ptools.RiskHigh}, opsOnly: true, expected: ToolDecisionAllow},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("RED_PANDA_DEBUG_TOOLS", "")
			decision := EvaluateToolPolicy(test.options, test.call, test.opsOnly)
			if decision.Action != test.expected {
				t.Fatalf("expected %s, got %s (%s)", test.expected, decision.Action, decision.Reason)
			}
		})
	}
}

func TestEvaluateToolPolicyHonorsDebugEnvironmentForOpsTool(t *testing.T) {
	t.Setenv("RED_PANDA_DEBUG_TOOLS", "1")
	decision := EvaluateToolPolicy(methods.ReplyOptions{ToolPolicy: "allow_all"}, ptools.Call{
		Name: "skill.create", Risk: ptools.RiskHigh,
	}, true)
	if decision.Action != ToolDecisionAllow {
		t.Fatalf("expected allow with debug environment, got %s (%s)", decision.Action, decision.Reason)
	}
}
