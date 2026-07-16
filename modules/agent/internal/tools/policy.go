package tools

import (
	"fmt"
	"os"
	"strings"

	"redpanda/protocol/methods"
	"redpanda/protocol/permission"
	ptools "redpanda/protocol/tools"
)

// opsOnlyTools 会注册给桌面端、命令行和调试用途，但默认不暴露给提供方工具架构（检查项 O1/O3；docs/41 W1-2）。
// 可通过 RED_PANDA_DEBUG_TOOLS=1、ReplyOptions.DebugTools 或显式 tool_allowlist 项启用。
var opsOnlyTools = map[string]struct{}{
	"skill.create":       {},
	"skill.update":       {},
	"skill.delete":       {},
	"worker.pool_status": {},
	"worker.pool_resize": {},
	"worker.pool_reset":  {},
	// Worker mailbox messaging is advanced orchestration; default Goal/chat paths
	// use worker.delegate + shared context.* instead (docs/41 W1-2).
	"worker.send":    {},
	"worker.receive": {},
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
	// 显式允许单个运维工具，无需开放全部调试工具。
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
	// opsToolExposed 可将客户端显式允许列表视为运维工具的启用信号。
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

// goalModeDefaultAllowlist 在目标处于活动状态时生效（检查项 O8；docs/41 W0-3 / W1-1）。
// 它让长程任务保持聚焦：排除 memory、会话 todo（与 goal.actions 语义重叠）、
// 多写路径中的 edit/patch（默认主推 write_file + diff 预览）、以及运维/消息工具。
// 若客户端也提供 tool_allowlist，则取二者交集。
var goalModeDefaultAllowlist = []string{
	"workspace.read_file",
	"workspace.list",
	"workspace.stats",
	"workspace.grep",
	"workspace.write_file",
	"workspace.diff_file",
	// workspace.edit_file / workspace.apply_patch: opt-in via client allowlist or non-goal chat.
	"shell.exec",
	// todo.* intentionally omitted: Goal uses goal.plan actions (docs/41 W0-3).
	"goal.create",
	"goal.plan",
	"goal.observe",
	"goal.assess",
	"goal.finish",
	"goal.list",
	"context.read",
	"context.search",
	"context.write",
	"context.replace",
	"context.delete",
	"worker.delegate",
	"worker.list",
	"worker.cancel",
	// worker.send/receive are ops-only (docs/41 W1-2).
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

// effectiveToolAllowlist 返回工具架构和策略实际使用的允许列表。
// 目标模式下，未提供客户端列表时注入默认列表；提供时与目标默认列表求交集，
// 因而客户端只能进一步收紧权限。
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
	// 复制选项，让 opsToolExposed 和拒绝列表检查仍能看到原始值，
	// 同时允许列表校验使用实际生效的列表。
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
