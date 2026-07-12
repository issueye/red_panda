package service

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
)

const (
	agentMaxPromptRunes = 20_000
	agentMaxDescRunes   = 2_000
	agentMaxTurnsCap    = 48
)

type AgentDefinitionService struct {
	repos repository.Set
}

type AgentDefinitionDTO struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"`
	Name            string    `json:"name"`
	NameZH          string    `json:"name_zh,omitempty"`
	Kind            string    `json:"kind"`
	Phase           string    `json:"phase"`
	Description     string    `json:"description,omitempty"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	ToolAllowlist   []string  `json:"tool_allowlist,omitempty"`
	ToolDenylist    []string  `json:"tool_denylist,omitempty"`
	DefaultMaxTurns int       `json:"default_max_turns"`
	Enabled         bool      `json:"enabled"`
	Builtin         bool      `json:"builtin"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AgentDefinitionCreate struct {
	Key             string
	Name            string
	NameZH          string
	Phase           string
	Description     string
	SystemPrompt    string
	ToolAllowlist   []string
	ToolDenylist    []string
	DefaultMaxTurns int
	Enabled         *bool
	SortOrder       int
}

type AgentDefinitionUpdate struct {
	Name            *string
	NameZH          *string
	Phase           *string
	Description     *string
	SystemPrompt    *string
	ToolAllowlist   *[]string
	ToolDenylist    *[]string
	DefaultMaxTurns *int
	Enabled         *bool
	SortOrder       *int
}

func NewAgentDefinitionService(repos repository.Set) AgentDefinitionService {
	return AgentDefinitionService{repos: repos}
}

func (s AgentDefinitionService) List() ([]AgentDefinitionDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return nil, err
	}
	rows, err := s.repos.Agents.List(200)
	if err != nil {
		return nil, err
	}
	out := make([]AgentDefinitionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentDefinitionDTO(row))
	}
	return out, nil
}

func (s AgentDefinitionService) ListEnabled() ([]AgentDefinitionDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return nil, err
	}
	rows, err := s.repos.Agents.ListEnabled(200)
	if err != nil {
		return nil, err
	}
	out := make([]AgentDefinitionDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentDefinitionDTO(row))
	}
	return out, nil
}

func (s AgentDefinitionService) Get(id string) (AgentDefinitionDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return AgentDefinitionDTO{}, err
	}
	row, err := s.repos.Agents.Get(id)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	return agentDefinitionDTO(row), nil
}

func (s AgentDefinitionService) GetByKey(key string) (AgentDefinitionDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return AgentDefinitionDTO{}, err
	}
	row, err := s.repos.Agents.GetByKey(key)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	return agentDefinitionDTO(row), nil
}

func (s AgentDefinitionService) Create(input AgentDefinitionCreate) (AgentDefinitionDTO, error) {
	key, err := normalizeAgentKey(input.Key)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	// Disallow colliding with reserved goal-* builtin keys unless re-creating (create always custom).
	if strings.HasPrefix(key, "goal-") {
		return AgentDefinitionDTO{}, fmt.Errorf("key prefix goal- is reserved for builtin specialists")
	}
	phase := normalizeAgentPhase(input.Phase)
	turns := normalizeAgentMaxTurns(input.DefaultMaxTurns)
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	desc, err := clampRunes(strings.TrimSpace(input.Description), agentMaxDescRunes)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	prompt, err := clampRunes(strings.TrimSpace(input.SystemPrompt), agentMaxPromptRunes)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	row, err := s.repos.Agents.Create(model.AgentDefinition{
		Key:             key,
		Name:            strings.TrimSpace(input.Name),
		NameZH:          strings.TrimSpace(input.NameZH),
		Kind:            "custom",
		Phase:           phase,
		Description:     desc,
		SystemPrompt:    prompt,
		ToolAllowlist:   cleanStringList(input.ToolAllowlist),
		ToolDenylist:    appendDefaultAgentDenylist(cleanStringList(input.ToolDenylist)),
		DefaultMaxTurns: turns,
		Enabled:         enabled,
		Builtin:         false,
		SortOrder:       input.SortOrder,
	})
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	return agentDefinitionDTO(row), nil
}

func (s AgentDefinitionService) Update(id string, input AgentDefinitionUpdate) (AgentDefinitionDTO, error) {
	current, err := s.repos.Agents.Get(id)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	next := current
	if input.Name != nil {
		next.Name = strings.TrimSpace(*input.Name)
	}
	if input.NameZH != nil {
		next.NameZH = strings.TrimSpace(*input.NameZH)
	}
	if input.Phase != nil {
		if current.Builtin {
			// Builtin phase is fixed.
		} else {
			next.Phase = normalizeAgentPhase(*input.Phase)
		}
	}
	if input.Description != nil {
		desc, err := clampRunes(strings.TrimSpace(*input.Description), agentMaxDescRunes)
		if err != nil {
			return AgentDefinitionDTO{}, err
		}
		next.Description = desc
	}
	if input.SystemPrompt != nil {
		prompt, err := clampRunes(strings.TrimSpace(*input.SystemPrompt), agentMaxPromptRunes)
		if err != nil {
			return AgentDefinitionDTO{}, err
		}
		next.SystemPrompt = prompt
	}
	if input.ToolAllowlist != nil {
		next.ToolAllowlist = cleanStringList(*input.ToolAllowlist)
	}
	if input.ToolDenylist != nil {
		next.ToolDenylist = appendDefaultAgentDenylist(cleanStringList(*input.ToolDenylist))
	}
	if input.DefaultMaxTurns != nil {
		next.DefaultMaxTurns = normalizeAgentMaxTurns(*input.DefaultMaxTurns)
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		next.SortOrder = *input.SortOrder
	}
	row, err := s.repos.Agents.Update(next)
	if err != nil {
		return AgentDefinitionDTO{}, err
	}
	return agentDefinitionDTO(row), nil
}

func (s AgentDefinitionService) Delete(id string) error {
	return s.repos.Agents.SoftDelete(id)
}

// EnsureBuiltins seeds Goal pipeline specialists (docs/32 §2.10).
func (s AgentDefinitionService) EnsureBuiltins() error {
	for _, seed := range builtinAgentSeeds() {
		if _, err := s.repos.Agents.UpsertByKey(seed); err != nil {
			return err
		}
	}
	return nil
}

func builtinAgentSeeds() []model.AgentDefinition {
	readonlyDeny := []string{
		"workspace.write_file", "workspace.edit_file", "workspace.apply_patch",
		"goal.write", "goal.update", "goal.checkpoint", "goal.complete", "goal.list",
		"todo.write", "todo.list",
		"subagent.run", "subagent.list", "subagent.cancel", "subagent.reset",
		"skill.run", "skill.create", "skill.update",
	}
	implementDeny := []string{
		"goal.write", "goal.update", "goal.checkpoint", "goal.complete", "goal.list",
		"todo.write", "todo.list",
		"subagent.run", "subagent.list", "subagent.cancel", "subagent.reset",
		"skill.run", "skill.create", "skill.update",
		"web.search", "web.fetch",
	}
	return []model.AgentDefinition{
		{
			ID: "agent_builtin_goal_analyst", Key: "goal-analyst", Name: "Goal Analyst", NameZH: "目标分析师",
			Kind: "builtin", Phase: "analyze", Builtin: true, Enabled: true, SortOrder: 10,
			DefaultMaxTurns: 12,
			Description:     "分析用户输入与工作区：意图、范围、约束与成功信号。只读，不改仓库。",
			SystemPrompt:    "You are goal-analyst for red_panda. Read-only. Produce structured AnalysisReport JSON: intent, in_scope, out_of_scope, constraints, success_signals, risks, suggested_approach, trivial, key_paths. Do not modify files. Do not claim work is done.",
			ToolDenylist:    append([]string{"shell.exec"}, readonlyDeny...),
		},
		{
			ID: "agent_builtin_goal_planner", Key: "goal-planner", Name: "Goal Planner", NameZH: "目标规划师",
			Kind: "builtin", Phase: "plan", Builtin: true, Enabled: true, SortOrder: 20,
			DefaultMaxTurns: 8,
			Description:     "将分析结果拆成可验证步骤与成功标准草案。只读；由 root 写入 goal/todo。",
			SystemPrompt:    "You are goal-planner. Read-only. Output PlanReport JSON: title, objective, success_criteria, steps[{id,content,verify_hint}], notes. Do not write files or call goal/todo tools.",
			ToolDenylist:    append([]string{"shell.exec"}, readonlyDeny...),
		},
		{
			ID: "agent_builtin_goal_implementer", Key: "goal-implementer", Name: "Goal Implementer", NameZH: "目标实施者",
			Kind: "builtin", Phase: "execute", Builtin: true, Enabled: true, SortOrder: 30,
			DefaultMaxTurns: 24,
			Description:     "仅执行当前 in_progress 步骤；最小改动并报告变更。",
			SystemPrompt:    "You are goal-implementer. Do ONLY the assigned current step. Prefer minimal diffs. End with ImplementationReport JSON: step_id, done_claim, changes, commands_run, blockers, notes_for_verifier.",
			ToolDenylist:    implementDeny,
		},
		{
			ID: "agent_builtin_goal_verifier", Key: "goal-verifier", Name: "Goal Verifier", NameZH: "目标验证者",
			Kind: "builtin", Phase: "verify", Builtin: true, Enabled: true, SortOrder: 40,
			DefaultMaxTurns: 12,
			Description:     "用测试/读回验证当前步骤；默认不改业务代码。",
			SystemPrompt:    "You are goal-verifier. Be skeptical. Gather evidence via read/tests. Output VerifyReport JSON: step_id, passed, evidence, failures, retry_suggestion. Do not edit product source in v1.",
			ToolDenylist: append([]string{
				"workspace.write_file", "workspace.edit_file", "workspace.apply_patch",
			}, readonlyDeny...),
		},
		{
			ID: "agent_builtin_goal_evaluator", Key: "goal-evaluator", Name: "Goal Evaluator", NameZH: "目标终评官",
			Kind: "builtin", Phase: "evaluate", Builtin: true, Enabled: true, SortOrder: 50,
			DefaultMaxTurns: 8,
			Description:     "对照成功标准终评并起草完成报告 Markdown。",
			SystemPrompt:    "You are goal-evaluator. Compare all evidence to success_criteria. Output EvaluationReport JSON: verdict(succeeded|partial|failed), criteria[], steps_summary[], risks, followups, report_markdown. Do not write files.",
			ToolDenylist:    append([]string{"shell.exec"}, readonlyDeny...),
		},
	}
}

func agentDefinitionDTO(row model.AgentDefinition) AgentDefinitionDTO {
	return AgentDefinitionDTO{
		ID:              row.ID,
		Key:             row.Key,
		Name:            row.Name,
		NameZH:          row.NameZH,
		Kind:            row.Kind,
		Phase:           row.Phase,
		Description:     row.Description,
		SystemPrompt:    row.SystemPrompt,
		ToolAllowlist:   row.ToolAllowlist,
		ToolDenylist:    row.ToolDenylist,
		DefaultMaxTurns: row.DefaultMaxTurns,
		Enabled:         row.Enabled,
		Builtin:         row.Builtin,
		SortOrder:       row.SortOrder,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func normalizeAgentKey(raw string) (string, error) {
	key := strings.TrimSpace(strings.ToLower(raw))
	key = strings.ReplaceAll(key, "_", "-")
	key = strings.ReplaceAll(key, " ", "-")
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return "", fmt.Errorf("key must be lowercase letters, digits, or hyphens")
	}
	if len(key) > 64 {
		return "", fmt.Errorf("key too long")
	}
	return key, nil
}

func normalizeAgentPhase(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "analyze", "plan", "execute", "verify", "evaluate", "general", "custom":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "custom"
	}
}

func normalizeAgentMaxTurns(n int) int {
	if n <= 0 {
		return 12
	}
	if n > agentMaxTurnsCap {
		return agentMaxTurnsCap
	}
	return n
}

func cleanStringList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		v := strings.TrimSpace(item)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func appendDefaultAgentDenylist(items []string) []string {
	base := []string{
		"goal.write", "goal.update", "goal.checkpoint", "goal.complete", "goal.list",
		"todo.write", "todo.list",
		"subagent.run", "subagent.list", "subagent.cancel", "subagent.reset",
	}
	return cleanStringList(append(append([]string{}, items...), base...))
}

func clampRunes(value string, max int) (string, error) {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value, nil
	}
	return "", fmt.Errorf("text exceeds %d runes", max)
}
