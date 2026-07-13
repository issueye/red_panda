package tools

import (
	"fmt"
	"os"
	"strings"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	ptools "redpanda/protocol/tools"
)

// opsOnlyTools are registered for Desktop/CLI/debug use but hidden from the
// default provider tool schema (checklist O1/O3). Enable with RED_PANDA_DEBUG_TOOLS=1,
// ReplyOptions.DebugTools, or an explicit tool_allowlist entry.
var opsOnlyTools = map[string]struct{}{
	"subagent.pool_status": {},
	"subagent.pool_resize": {},
	"subagent.pool_reset":  {},
	"skill.create":         {},
	"skill.update":         {},
	"skill.delete":         {},
}

func isOpsOnlyTool(name string) bool {
	_, ok := opsOnlyTools[strings.TrimSpace(name)]
	return ok
}

func debugToolsEnabled(options methods.ReplyOptions) bool {
	if options.DebugTools {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_DEBUG_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func opsToolExposed(options methods.ReplyOptions, name string) bool {
	if !isOpsOnlyTool(name) {
		return true
	}
	if debugToolsEnabled(options) {
		return true
	}
	// Explicit allowlist opt-in for a single ops tool without opening all debug tools.
	return ContainsString(options.ToolAllowlist, name)
}

type ToolDecisionAction string

const (
	ToolDecisionAllow             ToolDecisionAction = "allow"
	ToolDecisionRequirePermission ToolDecisionAction = "require_permission"
	ToolDecisionDeny              ToolDecisionAction = "deny"
)

type ToolDecision struct {
	Action ToolDecisionAction
	Reason string
}

func EvaluateToolPolicy(options methods.ReplyOptions, call ptools.Call) ToolDecision {
	if options.GoalsEnabled != nil && !*options.GoalsEnabled && strings.HasPrefix(call.Name, "goal.") {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "goals are disabled"}
	}
	if ContainsString(options.ToolDenylist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is denied by tool_denylist"}
	}
	allowlist := effectiveToolAllowlist(options)
	if len(allowlist) > 0 && !ContainsString(allowlist, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool is not in tool_allowlist"}
	}
	// opsToolExposed may treat explicit client allowlist as opt-in for ops tools.
	eff := options
	eff.ToolAllowlist = allowlist
	if !opsToolExposed(eff, call.Name) {
		return ToolDecision{Action: ToolDecisionDeny, Reason: "ops tool is not enabled (set debug_tools or RED_PANDA_DEBUG_TOOLS)"}
	}

	switch options.ToolPolicy {
	case "deny_all":
		return ToolDecision{Action: ToolDecisionDeny, Reason: "tool_policy denied all tools"}
	case "allow_all":
		return ToolDecision{Action: ToolDecisionAllow, Reason: "tool_policy allowed all tools"}
	case "ask_all":
		return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "tool_policy requires permission for all tools"}
	case "risk_based", "":
		return riskBasedDecision(options.PermissionMode, call.Risk)
	default:
		return ToolDecision{
			Action: ToolDecisionDeny,
			Reason: fmt.Sprintf("unknown tool_policy %q", options.ToolPolicy),
		}
	}
}

func riskBasedDecision(permissionMode string, risk ptools.Risk) ToolDecision {
	if risk == ptools.RiskLow {
		return ToolDecision{Action: ToolDecisionAllow, Reason: "low risk tool"}
	}
	switch permission.Mode(permissionMode) {
	case permission.ModeAllowAll:
		return ToolDecision{Action: ToolDecisionAllow, Reason: "permission_mode allow_all"}
	case permission.ModeDenyAll:
		return ToolDecision{Action: ToolDecisionDeny, Reason: "permission_mode deny_all"}
	case permission.ModePermissive:
		if risk == ptools.RiskHigh {
			return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "high risk tool in permissive mode"}
		}
		return ToolDecision{Action: ToolDecisionAllow, Reason: "non-high risk tool in permissive mode"}
	default:
		return ToolDecision{Action: ToolDecisionRequirePermission, Reason: "strict permission mode"}
	}
}

func ContainsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

// goalModeDefaultAllowlist is applied when a Goal is active (checklist O8).
// It keeps long-horizon runs focused and excludes memory / ops-only tools.
// Client tool_allowlist is intersected with this set when both are present.
var goalModeDefaultAllowlist = []string{
	"workspace.read_file",
	"workspace.list",
	"workspace.stats",
	"workspace.grep",
	"workspace.write_file",
	"workspace.edit_file",
	"workspace.diff_file",
	"workspace.apply_patch",
	"shell.exec",
	"todo.write",
	"todo.list",
	"goal.write",
	"goal.update",
	"goal.checkpoint",
	"goal.complete",
	"goal.list",
	"context.read",
	"context.search",
	"context.write",
	"context.replace",
	"context.delete",
	"subagent.run",
	"subagent.list",
	"subagent.cancel",
	"subagent.reset",
	"skill.list",
	"skill.run",
	"web.search",
	"web.fetch",
}

func goalModeTightensTools(options methods.ReplyOptions) bool {
	if options.GoalsEnabled != nil && !*options.GoalsEnabled {
		return false
	}
	if options.GoalsEnabled != nil && *options.GoalsEnabled {
		return true
	}
	if options.ContinueGoal {
		return true
	}
	if strings.TrimSpace(options.GoalID) != "" {
		return true
	}
	if options.GoalContext != nil && strings.TrimSpace(options.GoalContext.GoalID) != "" {
		return true
	}
	return false
}

// effectiveToolAllowlist returns the allowlist used for schema + policy.
// Goal mode injects a default allowlist when the client did not send one;
// an explicit client list is intersected with the Goal default so clients
// can only tighten further.
func effectiveToolAllowlist(options methods.ReplyOptions) []string {
	client := options.ToolAllowlist
	if !goalModeTightensTools(options) {
		return client
	}
	if len(client) == 0 {
		out := make([]string, len(goalModeDefaultAllowlist))
		copy(out, goalModeDefaultAllowlist)
		return out
	}
	return intersectAllowlist(client, goalModeDefaultAllowlist)
}

func intersectAllowlist(a, b []string) []string {
	set := map[string]struct{}{}
	for _, item := range b {
		item = strings.TrimSpace(item)
		if item != "" {
			set[item] = struct{}{}
		}
	}
	out := make([]string, 0, len(a))
	seen := map[string]struct{}{}
	for _, item := range a {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := set[item]; !ok {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func AvailableToolsForOptions(definitions []ptools.Definition, options methods.ReplyOptions) []ptools.Definition {
	allowlist := effectiveToolAllowlist(options)
	// Copy so opsToolExposed / denylist checks still see original options,
	// but allowlist enforcement uses the effective list.
	eff := options
	eff.ToolAllowlist = allowlist

	filtered := make([]ptools.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if options.GoalsEnabled != nil && !*options.GoalsEnabled && strings.HasPrefix(definition.Name, "goal.") {
			continue
		}
		if ContainsString(options.ToolDenylist, definition.Name) {
			continue
		}
		if len(allowlist) > 0 && !ContainsString(allowlist, definition.Name) {
			continue
		}
		if !opsToolExposed(eff, definition.Name) {
			continue
		}
		filtered = append(filtered, definition)
	}
	return filtered
}
