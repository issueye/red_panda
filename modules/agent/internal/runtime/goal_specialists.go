package runtime

import (
	"fmt"
	"redpanda/agent/internal/worker"
	"strings"

	"redpanda/protocol/methods"
)

// goalSpecialist 是内置 Goal 执行角色。
// 当 worker.delegate 的 profile_key 匹配 Key（如 goal-analyst）时应用。
type goalSpecialist struct {
	Key             string
	NameZH          string
	Capability      string
	DefaultMaxTurns int
	// 非空时作为允许列表：仅暴露这些工具，拒绝列表仍然生效。
	Allowlist []string
	// ExtraDenylist 会叠加到 worker.DelegatedDenylist。
	ExtraDenylist []string
	SystemPrompt  string
	// CapMaxTurns 硬性限制专业子代理预算，0 表示仅使用 effectiveWorkerToolTurns。
	CapMaxTurns int
}

var workspaceReadTools = []string{
	"workspace.read_file",
	"workspace.list",
	"workspace.grep",
	"workspace.diff_file",
	"workspace.stats",
}

var workspaceWriteTools = []string{
	"workspace.write_file",
	"workspace.edit_file",
	"workspace.apply_patch",
}

// contextShareTools 是目标暂存区工具。使用允许列表的专业 Worker 必须显式包含这些工具。
var contextShareTools = []string{
	"context.read",
	"context.search",
	"context.write",
	"context.replace",
}

func withContextShareTools(base ...string) []string {
	out := make([]string, 0, len(base)+len(contextShareTools))
	out = append(out, base...)
	out = append(out, contextShareTools...)
	return out
}

// defaultEnabledGoalSpecialists is the W4-A default roster (docs/41):
// research → build → review. Planner/evaluator stay in the catalog for
// opt-in re-enable via Gateway Worker Profiles.
var defaultEnabledGoalSpecialists = map[string]bool{
	"goal-analyst":     true,
	"goal-implementer": true,
	"goal-verifier":    true,
	"goal-planner":     false,
	"goal-evaluator":   false,
}

// builtinGoalSpecialists 是按当前行动需求选择的内置专家，Key 与 Worker Profiles 匹配。
var builtinGoalSpecialists = map[string]goalSpecialist{
	"goal-analyst": {
		Key: "goal-analyst", NameZH: "目标分析师", Capability: "research",
		DefaultMaxTurns: 12, CapMaxTurns: 24,
		Allowlist:     withContextShareTools(append(append([]string{}, workspaceReadTools...), "web.search", "web.fetch")...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec"),
		SystemPrompt: `You are goal-analyst for red_panda (目标分析师).

Role: research the gap and suggest a revisable action queue. Read-only for workspace files.
Parent owns goal.plan / goal.assess / goal.finish.

Rules:
- Do NOT modify files or run write/shell commands.
- Do NOT call goal.* or todo.* tools.
- Do NOT spawn nested Workers.
- Use context.write / context.replace to persist key findings on the shared goal scratchpad.
- Include concrete suggested actions with acceptance checks the parent can pass to goal.plan.

Preferred final report shape (JSON in a fenced block is ideal):
{
  "intent": "...",
  "in_scope": ["..."],
  "out_of_scope": ["..."],
  "constraints": ["..."],
  "success_signals": ["..."],
  "risks": ["..."],
  "suggested_approach": "...",
  "suggested_actions": [{"key":"action-1","title":"...","acceptance":"..."}],
  "trivial": false,
  "key_paths": ["..."]
}

If the request is trivial one-shot Q&A, set "trivial": true and explain why a Goal is unnecessary.`,
	},
	"goal-planner": {
		Key: "goal-planner", NameZH: "目标规划师", Capability: "strategy",
		DefaultMaxTurns: 8, CapMaxTurns: 12,
		Allowlist:     withContextShareTools(workspaceReadTools...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec", "web.search", "web.fetch"),
		SystemPrompt: `You are goal-planner for red_panda (目标规划师).

Role: turn the current Goal contract, evidence, and gaps into a revisable action queue. Read-only for workspace files.
(Default-disabled in W4-A; parent usually plans via goal.plan using analyst suggestions.)

Rules:
- Do NOT write files or mutate controller state (parent owns goal.plan / goal.assess).
- Use context.read for prior analyst findings; context.write for plan decisions on the shared scratchpad.
- Actions must be verifiable and ordered; keep granularity practical.

Preferred final report JSON:
{
  "title": "...",
  "objective": "...",
  "criteria": [{"id":"criterion-1","description":"..."}],
  "actions": [{"key":"action-1","title":"...","acceptance":"..."}],
  "notes": "..."
}`,
	},
	"goal-implementer": {
		Key: "goal-implementer", NameZH: "目标实施者", Capability: "build",
		DefaultMaxTurns: 24, CapMaxTurns: 48,
		// 没有允许列表时，暴露除拒绝列表外的全部工具。
		ExtraDenylist: []string{"web.search", "web.fetch", "skill.run", "skill.create", "skill.update", "skill.delete"},
		SystemPrompt: `You are goal-implementer for red_panda (目标实施者).

Role: implement ONLY the current assigned Goal action. Prefer minimal diffs.

Rules:
- Stay inside the action scope; do not rewrite unrelated modules.
- Do NOT call goal.* / todo.* / Worker.* (parent owns session state).
- End with a concrete ImplementationReport the verifier can check.

Preferred final report JSON:
{
  "action_id": "...",
  "done_claim": true,
  "changes": [{"path":"...","summary":"..."}],
  "commands_run": ["..."],
  "blockers": [],
  "notes_for_verifier": "..."
}`,
	},
	"goal-verifier": {
		Key: "goal-verifier", NameZH: "目标验证者", Capability: "review",
		DefaultMaxTurns: 12, CapMaxTurns: 16,
		Allowlist:     withContextShareTools(append(append([]string{}, workspaceReadTools...), "shell.exec")...),
		ExtraDenylist: append([]string{}, workspaceWriteTools...),
		SystemPrompt: `You are goal-verifier for red_panda (目标验证者).

Role: skeptically verify the current Goal action with evidence (read/tests/commands),
and supply criterion-level hints for the parent goal.assess.
Parent owns goal.assess / goal.finish.

Rules:
- Prefer evidence over the implementer's claims.
- Do NOT edit product source (no write/edit/apply_patch).
- shell.exec is only for tests/builds that validate the step.
- Use context.read for implementer handoff notes; context.write for verification outcomes.
- Do NOT call goal.* / todo.* / Worker.*.

Preferred final report JSON:
{
  "action_id": "...",
  "passed": true,
  "evidence": [{"kind":"test|read|command","detail":"..."}],
  "failures": [],
  "retry_suggestion": "...",
  "criteria_hints": [{"id":"criterion-1","status":"met|not_met|blocked","evidence":"..."}],
  "assessment_summary": "short verdict suitable for goal.assess",
  "evidence_summary": "concise evidence suitable for goal.assess"
}`,
	},
	"goal-evaluator": {
		Key: "goal-evaluator", NameZH: "目标终评官", Capability: "assess",
		DefaultMaxTurns: 8, CapMaxTurns: 12,
		Allowlist:     withContextShareTools(workspaceReadTools...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec", "web.search", "web.fetch"),
		SystemPrompt: `You are goal-evaluator for red_panda (目标终评官).

Role: evaluate the whole Goal against every contract criterion and draft the user-facing completion report.
(Default-disabled in W4-A; parent usually assesses via goal.assess using verifier evidence.)

Rules:
- Read-only for workspace files and shell. Do not write files or run shell.
- Use context.read for shared findings across the goal; context.write for the final evaluation notes.
- Do NOT call goal.finish (parent does after persisting a satisfied assessment).

Preferred final report JSON:
{
  "verdict": "succeeded|partial|failed",
  "criteria": [{"item":"...","result":"met|partial|not_met|blocked","evidence":"..."}],
  "assessment_summary": "final verdict against the contract criteria",
  "evidence": "concrete evidence covering the criteria",
  "report_markdown": "## 目标完成报告\\n..."
}`,
	},
}

func lookupGoalSpecialist(name string) (goalSpecialist, bool) {
	key := normalizeGoalSpecialistKey(name)
	if key == "" {
		return goalSpecialist{}, false
	}
	spec, ok := builtinGoalSpecialists[key]
	return spec, ok
}

// resolveGoalSpecialist 将 Gateway 管理的 Worker Profiles 合并到内置专家配置。
// 存在时，提示词、默认最大回合、阶段和显示名称取自 Gateway；工具允许和拒绝策略仍由 Runtime
// 管理（共享上下文工具与写入隔离），避免设置意外移除安全约束。
//
// W4-A: when Gateway sends a catalog, only specialists present and enabled there
// resolve. With no catalog, only defaultEnabledGoalSpecialists resolve.
func resolveGoalSpecialist(profiles []methods.WorkerProfileRef, name string) (goalSpecialist, bool) {
	base, ok := lookupGoalSpecialist(name)
	if !ok {
		return goalSpecialist{}, false
	}
	key := normalizeGoalSpecialistKey(name)
	if len(profiles) == 0 {
		if !defaultEnabledGoalSpecialists[key] {
			return goalSpecialist{}, false
		}
		return base, true
	}
	for _, profile := range profiles {
		if normalizeGoalSpecialistKey(profile.Key) != key {
			continue
		}
		if !profile.Enabled {
			return goalSpecialist{}, false
		}
		if prompt := strings.TrimSpace(profile.SystemPrompt); prompt != "" {
			base.SystemPrompt = prompt
		}
		if profile.DefaultMaxTurns > 0 {
			base.DefaultMaxTurns = profile.DefaultMaxTurns
		}
		if capability := strings.TrimSpace(profile.Phase); capability != "" {
			base.Capability = capability
		}
		if nameZH := strings.TrimSpace(profile.NameZH); nameZH != "" {
			base.NameZH = nameZH
		}
		if display := strings.TrimSpace(profile.Name); display != "" && base.NameZH == "" {
			base.NameZH = display
		}
		return base, true
	}
	// Catalog present but this specialist was not attached (disabled / omitted).
	return goalSpecialist{}, false
}

func normalizeGoalSpecialistKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.ReplaceAll(key, "_", "-")
	return key
}

func isGoalSpecialistName(name string) bool {
	_, ok := lookupGoalSpecialist(name)
	return ok
}

func (r *Runtime) validateGoalSpecialistPhase(runID string, spec goalSpecialist) error {
	state := r.getRunGoal(runID)
	if state == nil || strings.TrimSpace(state.Goal.ID) == "" || state.Terminal {
		return fmt.Errorf("%s requires an active goal", spec.Key)
	}
	return nil
}

// 子代理绝不能继承根 Goal 绑定，否则每个子代理都会启动自己的多分段 Goal 循环，
// 并可能成倍放大工具预算。
func disableGoalPipelineForChild(options *methods.ReplyOptions) {
	if options == nil {
		return
	}
	disabled := false
	options.GoalsEnabled = &disabled
	options.GoalID = ""
	options.GoalContext = nil
}

// applyGoalSpecialist 为阶段专家配置子代理 ReplyParams，并注入父目标摘要（目标与共享笔记），
// 避免子代理缺少上下文；返回调整后的 maxTurns。
func (r *Runtime) applyGoalSpecialist(child *methods.ReplyParams, spec goalSpecialist, task string, maxTurns int, parentRunID string, sessionID string, goalID string, objective string) int {
	if child == nil {
		return maxTurns
	}
	if maxTurns <= 0 {
		maxTurns = spec.DefaultMaxTurns
	}
	if spec.CapMaxTurns > 0 && maxTurns > spec.CapMaxTurns {
		maxTurns = spec.CapMaxTurns
	}
	if maxTurns < 1 {
		maxTurns = 1
	}

	// 会话级工具仅供根代理使用。
	child.Options.TodoContext = nil
	disableGoalPipelineForChild(&child.Options)
	child.Options.MaxToolTurns = maxTurns

	// 拒绝列表始终包含全局子代理拒绝列表和专家额外项。
	// context.* 工具被有意排除在拒绝列表外；使用允许列表的专家也必须列出它们（见 withContextShareTools）。
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, worker.DelegatedDenylist...)
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, spec.ExtraDenylist...)

	if len(spec.Allowlist) > 0 {
		// 允许列表决定暴露的工具；若父代理设置了允许列表，则保留交集。
		if len(child.Options.ToolAllowlist) > 0 {
			child.Options.ToolAllowlist = intersectStrings(child.Options.ToolAllowlist, spec.Allowlist)
		} else {
			child.Options.ToolAllowlist = append([]string{}, spec.Allowlist...)
		}
	}

	// 系统角色内容：专家提示词和任务框架。
	roleBlock := strings.TrimSpace(spec.SystemPrompt)
	if roleBlock == "" {
		roleBlock = fmt.Sprintf("You are specialist %q.", spec.Key)
	}
	budgetNote := fmt.Sprintf(
		"\n\nSpecialist key=%s capability=%s (%s). Tool-turn budget=%d. "+
			"Return one final report for the parent. Do not nest Workers.",
		spec.Key, spec.Capability, spec.NameZH, maxTurns,
	)

	// 注入父目标共享笔记摘要，避免专家缺少上下文。摘要包含 goal_id，
	// 子代理可自行调用 context.read 获取细节。尽力而为：无笔记或 Gateway 时为空。
	brief := ""
	if strings.TrimSpace(goalID) != "" {
		brief = "\n\n" + r.goalNotesBrief(parentRunID, sessionID, goalID, objective)
	}
	// 角色文本存于 SpecialistContext 而非 MemoryContext，避免覆盖项目或会话记忆，
	// 也避免被错误标记为“记忆”。
	child.Options.MemoryContext = nil
	child.Options.SpecialistContext = &methods.SpecialistContext{
		Kind:    "specialist",
		Context: roleBlock + budgetNote + brief,
	}

	// 确保任务文本仍包含用户分配的任务。
	if strings.TrimSpace(task) != "" && !strings.Contains(child.Input.Text, task) {
		child.Input.Text = strings.TrimSpace(task) + "\n\n" + strings.TrimSpace(child.Input.Text)
	}
	return maxTurns
}

func intersectStrings(a, b []string) []string {
	set := map[string]struct{}{}
	for _, item := range b {
		set[strings.TrimSpace(item)] = struct{}{}
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

// goalSpecialistDisplayName 为已知角色返回 UI 事件使用的中文名称。
func goalSpecialistDisplayName(name string) string {
	if spec, ok := lookupGoalSpecialist(name); ok && spec.NameZH != "" {
		return spec.NameZH
	}
	return name
}
