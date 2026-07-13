package runtime

import (
	"fmt"
	"redpanda/agent/internal/subagent"
	"strings"

	"redpanda/protocol/methods"
)

// goalSpecialist is a built-in Goal pipeline role (docs/32 §2.10).
// Applied when subagent.run name matches Key (e.g. goal-analyst).
type goalSpecialist struct {
	Key             string
	NameZH          string
	Phase           string
	DefaultMaxTurns int
	// Allowlist when non-empty: only these tools are exposed (plus denylist still applies).
	Allowlist []string
	// ExtraDenylist is merged on top of subagent.RunDenylist.
	ExtraDenylist []string
	SystemPrompt  string
	// CapMaxTurns hard-caps specialist budget (0 = use effectiveSubagentToolTurns only).
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

// contextShareTools are goal scratchpad tools. Specialists with an allowlist must
// include these explicitly — otherwise policy hides them even though they are
// absent from subagent.RunDenylist.
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

// builtinGoalSpecialists are the five phase experts. Keys match Gateway agent_definitions.
var builtinGoalSpecialists = map[string]goalSpecialist{
	"goal-analyst": {
		Key: "goal-analyst", NameZH: "目标分析师", Phase: "analyze",
		DefaultMaxTurns: 12, CapMaxTurns: 24,
		Allowlist:     withContextShareTools(append(append([]string{}, workspaceReadTools...), "web.search", "web.fetch")...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec"),
		SystemPrompt: `You are goal-analyst for red_panda (目标分析师).

Role: analyze the user request and codebase only. Read-only for workspace files.

Rules:
- Do NOT modify files or run write/shell commands.
- Do NOT call goal.* or todo.* tools.
- Do NOT spawn nested subagents.
- Use context.write / context.replace to persist key findings on the shared goal scratchpad.
- Produce a clear final report the parent can trust.

Preferred final report shape (JSON in a fenced block is ideal):
{
  "intent": "...",
  "in_scope": ["..."],
  "out_of_scope": ["..."],
  "constraints": ["..."],
  "success_signals": ["..."],
  "risks": ["..."],
  "suggested_approach": "...",
  "trivial": false,
  "key_paths": ["..."]
}

If the request is trivial one-shot Q&A, set "trivial": true and explain why a Goal is unnecessary.`,
	},
	"goal-planner": {
		Key: "goal-planner", NameZH: "目标规划师", Phase: "plan",
		DefaultMaxTurns: 8, CapMaxTurns: 12,
		Allowlist:     withContextShareTools(workspaceReadTools...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec", "web.search", "web.fetch"),
		SystemPrompt: `You are goal-planner for red_panda (目标规划师).

Role: turn analysis into an executable plan. Read-only for workspace files.

Rules:
- Do NOT write files or claim tools that create goals/todos (parent owns goal.write / todo.write).
- Use context.read for prior analyst findings; context.write for plan decisions on the shared scratchpad.
- Steps must be verifiable and ordered; keep granularity practical.
- success_criteria must be checkable (tests, files, behaviors).

Preferred final report JSON:
{
  "title": "...",
  "objective": "...",
  "success_criteria": "multi-line criteria",
  "steps": [{"id":"1","content":"...","verify_hint":"..."}],
  "notes": "..."
}`,
	},
	"goal-implementer": {
		Key: "goal-implementer", NameZH: "目标实施者", Phase: "execute",
		DefaultMaxTurns: 24, CapMaxTurns: 48,
		// No allowlist → all tools except denylist.
		ExtraDenylist: []string{"web.search", "web.fetch", "skill.run", "skill.create", "skill.update", "skill.delete"},
		SystemPrompt: `You are goal-implementer for red_panda (目标实施者).

Role: implement ONLY the current assigned step. Prefer minimal diffs.

Rules:
- Stay inside the step scope; do not rewrite unrelated modules.
- Do NOT call goal.* / todo.* / subagent.* (parent owns session state).
- End with a concrete ImplementationReport the verifier can check.

Preferred final report JSON:
{
  "step_id": "...",
  "done_claim": true,
  "changes": [{"path":"...","summary":"..."}],
  "commands_run": ["..."],
  "blockers": [],
  "notes_for_verifier": "..."
}`,
	},
	"goal-verifier": {
		Key: "goal-verifier", NameZH: "目标验证者", Phase: "verify",
		DefaultMaxTurns: 12, CapMaxTurns: 16,
		Allowlist:     withContextShareTools(append(append([]string{}, workspaceReadTools...), "shell.exec")...),
		ExtraDenylist: append([]string{}, workspaceWriteTools...),
		SystemPrompt: `You are goal-verifier for red_panda (目标验证者).

Role: skeptically verify the current step with evidence (read/tests/commands).

Rules:
- Prefer evidence over the implementer's claims.
- Do NOT edit product source in v1 (no write/edit/apply_patch).
- shell.exec is only for tests/builds that validate the step.
- Use context.read for implementer handoff notes; context.write for verification outcomes.
- Do NOT call goal.* / todo.* / subagent.*.

Preferred final report JSON:
{
  "step_id": "...",
  "passed": true,
  "evidence": [{"kind":"test|read|command","detail":"..."}],
  "failures": [],
  "retry_suggestion": "..."
}`,
	},
	"goal-evaluator": {
		Key: "goal-evaluator", NameZH: "目标终评官", Phase: "evaluate",
		DefaultMaxTurns: 8, CapMaxTurns: 12,
		Allowlist:     withContextShareTools(workspaceReadTools...),
		ExtraDenylist: append(append([]string{}, workspaceWriteTools...), "shell.exec", "web.search", "web.fetch"),
		SystemPrompt: `You are goal-evaluator for red_panda (目标终评官).

Role: evaluate the whole goal against success_criteria and draft the user-facing completion report.

Rules:
- Read-only for workspace files and shell. Do not write files or run shell.
- Use context.read for shared findings across the goal; context.write for the final evaluation notes.
- Compare evidence from prior specialist reports and the workspace.
- Do NOT call goal.complete (parent does after publishing the report).

Preferred final report JSON:
{
  "verdict": "succeeded|partial|failed",
  "criteria": [{"item":"...","result":"met|partial|not_met|blocked","evidence":"..."}],
  "steps_summary": [{"id":"...","status":"completed","note":"..."}],
  "risks": ["..."],
  "followups": ["..."],
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

// resolveGoalSpecialist merges Gateway-managed agent_definitions over builtin
// specialists. Prompt, default max turns, phase, and display name come from
// Gateway when present; tool allow/deny policy stays Runtime-owned (context
// share tools, write isolation) so Settings cannot accidentally strip safety.
func resolveGoalSpecialist(defs []methods.AgentDefinitionRef, name string) (goalSpecialist, bool) {
	base, ok := lookupGoalSpecialist(name)
	if !ok {
		return goalSpecialist{}, false
	}
	key := normalizeGoalSpecialistKey(name)
	for _, def := range defs {
		if normalizeGoalSpecialistKey(def.Key) != key {
			continue
		}
		if !def.Enabled {
			return goalSpecialist{}, false
		}
		if prompt := strings.TrimSpace(def.SystemPrompt); prompt != "" {
			base.SystemPrompt = prompt
		}
		if def.DefaultMaxTurns > 0 {
			base.DefaultMaxTurns = def.DefaultMaxTurns
		}
		if phase := strings.TrimSpace(def.Phase); phase != "" {
			base.Phase = phase
		}
		if nameZH := strings.TrimSpace(def.NameZH); nameZH != "" {
			base.NameZH = nameZH
		}
		if display := strings.TrimSpace(def.Name); display != "" && base.NameZH == "" {
			base.NameZH = display
		}
		return base, true
	}
	return base, true
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
	current := strings.TrimSpace(state.Goal.PipelinePhase)
	if current == "" {
		current = "analyze"
	}
	if current != spec.Phase {
		return fmt.Errorf(
			"%s is only valid in goal phase %q; current phase is %q",
			spec.Key,
			spec.Phase,
			current,
		)
	}
	return nil
}

// Child agents must never inherit the root Goal binding. Otherwise each child
// starts its own multi-segment Goal loop and can multiply its tool budget.
func disableGoalPipelineForChild(options *methods.ReplyOptions) {
	if options == nil {
		return
	}
	disabled := false
	options.GoalsEnabled = &disabled
	options.GoalID = ""
	options.GoalContext = nil
}

// applyGoalSpecialist configures child ReplyParams for a phase specialist and
// injects a brief of the parent goal (objective + shared notes) so the child is
// not context-blind. Returns adjusted maxTurns.
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

	// Session-scoped tools stay root-only.
	child.Options.TodoContext = nil
	disableGoalPipelineForChild(&child.Options)
	child.Options.SpawnSubAgents = false
	child.Options.SubAgentBackend = ""
	child.Options.MaxToolTurns = maxTurns

	// Denylist: always include global subagent denylist + specialist extras.
	// Note: context.* tools are intentionally NOT denylisted, and specialists
	// with an allowlist must also list them (see withContextShareTools).
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, subagent.RunDenylist...)
	child.Options.ToolDenylist = appendUniqueStrings(child.Options.ToolDenylist, spec.ExtraDenylist...)

	if len(spec.Allowlist) > 0 {
		// Allowlist is authoritative for exposed tools; keep parent allowlist intersection if set.
		if len(child.Options.ToolAllowlist) > 0 {
			child.Options.ToolAllowlist = intersectStrings(child.Options.ToolAllowlist, spec.Allowlist)
		} else {
			child.Options.ToolAllowlist = append([]string{}, spec.Allowlist...)
		}
	}

	// System role: specialist prompt + task framing.
	roleBlock := strings.TrimSpace(spec.SystemPrompt)
	if roleBlock == "" {
		roleBlock = fmt.Sprintf("You are specialist %q.", spec.Key)
	}
	budgetNote := fmt.Sprintf(
		"\n\nSpecialist key=%s phase=%s (%s). Tool-turn budget=%d. "+
			"Return one final report for the parent. Do not nest subagents.",
		spec.Key, spec.Phase, spec.NameZH, maxTurns,
	)

	// Inject a brief of the parent goal's shared notes so the specialist is not
	// context-blind. Includes the goal_id so the child can call context.read
	// itself for deeper detail. Best-effort: empty when no notes/gateway.
	brief := ""
	if strings.TrimSpace(goalID) != "" {
		brief = "\n\n" + r.goalNotesBrief(parentRunID, sessionID, goalID, objective)
	}
	// Role text lives in SpecialistContext — not MemoryContext — so project/session
	// memory is not overwritten or mislabeled as "memory".
	child.Options.MemoryContext = nil
	child.Options.SpecialistContext = &methods.SpecialistContext{
		Kind:    "specialist",
		Context: roleBlock + budgetNote + brief,
	}

	// Ensure task text still carries the user assignment.
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

// goalSpecialistDisplayName returns Chinese label for UI events when known.
func goalSpecialistDisplayName(name string) string {
	if spec, ok := lookupGoalSpecialist(name); ok && spec.NameZH != "" {
		return spec.NameZH
	}
	return name
}
