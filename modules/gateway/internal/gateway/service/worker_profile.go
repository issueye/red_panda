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
	workerProfileMaxPromptRunes = 20_000
	workerProfileMaxDescRunes   = 2_000
	workerProfileMaxTurns       = 48
)

type WorkerProfileService struct {
	repos repository.Set
}

type WorkerProfileDTO struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"`
	Name            string    `json:"name"`
	NameZH          string    `json:"name_zh,omitempty"`
	Kind            string    `json:"kind"`
	// Phase is a capability tag (legacy field name; not a Goal pipeline stage).
	Phase           string    `json:"phase"`
	Description     string    `json:"description,omitempty"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	ToolAllowlist   []string  `json:"tool_allowlist,omitempty"`
	ToolDenylist    []string  `json:"tool_denylist,omitempty"`
	DefaultMaxTurns int       `json:"default_max_turns"`
	Enabled         bool      `json:"enabled"`
	Builtin         bool      `json:"builtin"`
	SortOrder       int       `json:"sort_order"`
	MetadataJSON    string    `json:"metadata_json,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type WorkerProfileCreate struct {
	Key             string
	Name            string
	NameZH          string
	Phase           string
	Description     string
	SystemPrompt    string
	Provider        string
	Model           string
	ToolAllowlist   []string
	ToolDenylist    []string
	DefaultMaxTurns int
	Enabled         *bool
	SortOrder       int
	MetadataJSON    string
}

type WorkerProfileUpdate struct {
	Name            *string
	NameZH          *string
	Phase           *string
	Description     *string
	SystemPrompt    *string
	Provider        *string
	Model           *string
	ToolAllowlist   *[]string
	ToolDenylist    *[]string
	DefaultMaxTurns *int
	Enabled         *bool
	SortOrder       *int
	MetadataJSON    *string
}

func NewWorkerProfileService(repos repository.Set) WorkerProfileService {
	return WorkerProfileService{repos: repos}
}

func (s WorkerProfileService) List() ([]WorkerProfileDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return nil, err
	}
	rows, err := s.repos.WorkerProfiles.List(200)
	if err != nil {
		return nil, err
	}
	return workerProfileDTOs(rows), nil
}

func (s WorkerProfileService) ListEnabled() ([]WorkerProfileDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return nil, err
	}
	rows, err := s.repos.WorkerProfiles.ListEnabled(200)
	if err != nil {
		return nil, err
	}
	return workerProfileDTOs(rows), nil
}

func (s WorkerProfileService) Get(id string) (WorkerProfileDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return WorkerProfileDTO{}, err
	}
	row, err := s.repos.WorkerProfiles.Get(id)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	return workerProfileDTO(row), nil
}

func (s WorkerProfileService) GetByKey(key string) (WorkerProfileDTO, error) {
	if err := s.EnsureBuiltins(); err != nil {
		return WorkerProfileDTO{}, err
	}
	row, err := s.repos.WorkerProfiles.GetByKey(key)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	return workerProfileDTO(row), nil
}

func (s WorkerProfileService) Create(input WorkerProfileCreate) (WorkerProfileDTO, error) {
	key, err := normalizeWorkerProfileKey(input.Key)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	if strings.HasPrefix(key, "goal-") {
		return WorkerProfileDTO{}, fmt.Errorf("key prefix goal- is reserved for builtin worker profiles")
	}
	description, err := clampWorkerProfileText(strings.TrimSpace(input.Description), workerProfileMaxDescRunes)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	prompt, err := clampWorkerProfileText(strings.TrimSpace(input.SystemPrompt), workerProfileMaxPromptRunes)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	row, err := s.repos.WorkerProfiles.Create(model.WorkerProfile{
		Key:             key,
		Name:            strings.TrimSpace(input.Name),
		NameZH:          strings.TrimSpace(input.NameZH),
		Kind:            "custom",
		Phase:           normalizeWorkerProfilePhase(input.Phase),
		Description:     description,
		SystemPrompt:    prompt,
		Provider:        strings.TrimSpace(input.Provider),
		Model:           strings.TrimSpace(input.Model),
		ToolAllowlist:   cleanWorkerProfileTools(input.ToolAllowlist),
		ToolDenylist:    cleanWorkerProfileTools(input.ToolDenylist),
		DefaultMaxTurns: normalizeWorkerProfileMaxTurns(input.DefaultMaxTurns),
		Enabled:         enabled,
		Builtin:         false,
		SortOrder:       input.SortOrder,
		MetadataJSON:    strings.TrimSpace(input.MetadataJSON),
	})
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	return workerProfileDTO(row), nil
}

func (s WorkerProfileService) Update(id string, input WorkerProfileUpdate) (WorkerProfileDTO, error) {
	current, err := s.repos.WorkerProfiles.Get(id)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	next := current
	if input.Name != nil {
		next.Name = strings.TrimSpace(*input.Name)
	}
	if input.NameZH != nil {
		next.NameZH = strings.TrimSpace(*input.NameZH)
	}
	if input.Phase != nil && !current.Builtin {
		next.Phase = normalizeWorkerProfilePhase(*input.Phase)
	}
	if input.Description != nil {
		next.Description, err = clampWorkerProfileText(strings.TrimSpace(*input.Description), workerProfileMaxDescRunes)
		if err != nil {
			return WorkerProfileDTO{}, err
		}
	}
	if input.SystemPrompt != nil {
		next.SystemPrompt, err = clampWorkerProfileText(strings.TrimSpace(*input.SystemPrompt), workerProfileMaxPromptRunes)
		if err != nil {
			return WorkerProfileDTO{}, err
		}
	}
	if input.Provider != nil {
		next.Provider = strings.TrimSpace(*input.Provider)
	}
	if input.Model != nil {
		next.Model = strings.TrimSpace(*input.Model)
	}
	if input.ToolAllowlist != nil {
		next.ToolAllowlist = cleanWorkerProfileTools(*input.ToolAllowlist)
	}
	if input.ToolDenylist != nil {
		next.ToolDenylist = cleanWorkerProfileTools(*input.ToolDenylist)
	}
	if input.DefaultMaxTurns != nil {
		next.DefaultMaxTurns = normalizeWorkerProfileMaxTurns(*input.DefaultMaxTurns)
	}
	if input.Enabled != nil {
		next.Enabled = *input.Enabled
	}
	if input.SortOrder != nil {
		next.SortOrder = *input.SortOrder
	}
	if input.MetadataJSON != nil {
		next.MetadataJSON = strings.TrimSpace(*input.MetadataJSON)
	}
	row, err := s.repos.WorkerProfiles.Update(next)
	if err != nil {
		return WorkerProfileDTO{}, err
	}
	return workerProfileDTO(row), nil
}

func (s WorkerProfileService) Delete(id string) error {
	return s.repos.WorkerProfiles.SoftDelete(id)
}

func (s WorkerProfileService) EnsureBuiltins() error {
	for _, seed := range builtinWorkerProfileSeeds() {
		if _, err := s.repos.WorkerProfiles.UpsertBuiltin(seed); err != nil {
			return err
		}
	}
	return nil
}

func builtinWorkerProfileSeeds() []model.WorkerProfile {
	readonlyDeny := []string{
		"workspace.write_file", "workspace.edit_file", "workspace.apply_patch",
		"goal.create", "goal.plan", "goal.observe", "goal.assess", "goal.finish", "goal.list",
		"todo.write", "todo.list", "worker.delegate",
		"skill.run", "skill.create", "skill.update",
	}
	implementDeny := []string{
		"goal.create", "goal.plan", "goal.observe", "goal.assess", "goal.finish", "goal.list",
		"todo.write", "todo.list", "worker.delegate",
		"skill.run", "skill.create", "skill.update", "web.search", "web.fetch",
	}
	// Default roster is three specialists (docs/41 W4-A): research → build → review.
	// Planner/evaluator remain in the catalog but disabled by default; root owns
	// goal.plan / goal.assess. Operators may re-enable them in Settings.
	return []model.WorkerProfile{
		{ID: "worker_profile_builtin_goal_analyst", Key: "goal-analyst", Name: "Goal Analyst", NameZH: "目标分析师", Kind: "builtin", Phase: "research", Builtin: true, Enabled: true, SortOrder: 10, DefaultMaxTurns: 12, Description: "调研差距、约束与风险，并给出可执行的行动建议（只读）。规划落库由 root goal.plan 完成。", SystemPrompt: "You are goal-analyst for red_panda. Investigate the assigned outcome gap. Produce evidence, constraints, risks, viable approaches, and suggested actions with acceptance checks. Do not modify files or mutate goal state. Parent will call goal.plan.", ToolDenylist: append([]string{"shell.exec"}, readonlyDeny...)},
		{ID: "worker_profile_builtin_goal_planner", Key: "goal-planner", Name: "Goal Planner", NameZH: "目标规划师", Kind: "builtin", Phase: "strategy", Builtin: true, Enabled: false, SortOrder: 20, DefaultMaxTurns: 8, Description: "默认关闭（W4-A）。规划由 root goal.plan 与分析师建议承担；需要时可在设置中重新启用。", SystemPrompt: "You are goal-planner. Propose the smallest useful actions for the assigned outcome gap, each with observable acceptance. Do not write files or mutate goal state.", ToolDenylist: append([]string{"shell.exec"}, readonlyDeny...)},
		{ID: "worker_profile_builtin_goal_implementer", Key: "goal-implementer", Name: "Goal Implementer", NameZH: "目标实施者", Kind: "builtin", Phase: "build", Builtin: true, Enabled: true, SortOrder: 30, DefaultMaxTurns: 24, Description: "执行指定 Goal action，最小改动并报告实际结果。", SystemPrompt: "You are goal-implementer. Execute only the assigned Goal action. Prefer minimal diffs and report changes, commands, blockers, and evidence for review.", ToolDenylist: implementDeny},
		{ID: "worker_profile_builtin_goal_verifier", Key: "goal-verifier", Name: "Goal Verifier", NameZH: "目标验证者", Kind: "builtin", Phase: "review", Builtin: true, Enabled: true, SortOrder: 40, DefaultMaxTurns: 12, Description: "独立验证 action 与成功标准；默认不改业务代码。终态评估由 root goal.assess 落库。", SystemPrompt: "You are goal-verifier. Independently verify the assigned claim with tests or inspection. Report concrete evidence, criterion-level status hints, failures, and remaining gaps for the parent goal.assess. Do not edit product source.", ToolDenylist: append([]string{"workspace.write_file", "workspace.edit_file", "workspace.apply_patch"}, readonlyDeny...)},
		{ID: "worker_profile_builtin_goal_evaluator", Key: "goal-evaluator", Name: "Goal Evaluator", NameZH: "目标评估者", Kind: "builtin", Phase: "assess", Builtin: true, Enabled: false, SortOrder: 50, DefaultMaxTurns: 8, Description: "默认关闭（W4-A）。终评由 root goal.assess 与验证者证据承担；需要时可在设置中重新启用。", SystemPrompt: "You are goal-evaluator. Compare all available evidence to every success criterion. Report met, not_met, or blocked with concrete evidence and remaining gaps. Do not mutate goal state.", ToolDenylist: append([]string{"shell.exec"}, readonlyDeny...)},
	}
}

func workerProfileDTOs(rows []model.WorkerProfile) []WorkerProfileDTO {
	out := make([]WorkerProfileDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, workerProfileDTO(row))
	}
	return out
}

func workerProfileDTO(row model.WorkerProfile) WorkerProfileDTO {
	return WorkerProfileDTO{ID: row.ID, Key: row.Key, Name: row.Name, NameZH: row.NameZH, Kind: row.Kind, Phase: row.Phase, Description: row.Description, SystemPrompt: row.SystemPrompt, Provider: row.Provider, Model: row.Model, ToolAllowlist: row.ToolAllowlist, ToolDenylist: row.ToolDenylist, DefaultMaxTurns: row.DefaultMaxTurns, Enabled: row.Enabled, Builtin: row.Builtin, SortOrder: row.SortOrder, MetadataJSON: row.MetadataJSON, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func normalizeWorkerProfileKey(raw string) (string, error) {
	key := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(raw)), "_", "-"), " ", "-")
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

// normalizeWorkerProfilePhase normalizes the capability tag stored in Phase
// (docs/41 W3-4). V2 capability tags are preferred; legacy pipeline names map
// through unchanged for existing custom profiles.
func normalizeWorkerProfilePhase(raw string) string {
	phase := strings.ToLower(strings.TrimSpace(raw))
	switch phase {
	// V2 capability tags used by builtin seeds.
	case "research", "strategy", "build", "review", "assess":
		return phase
	// Legacy pipeline-era labels (still accepted).
	case "analyze", "plan", "execute", "verify", "evaluate":
		return phase
	case "general", "custom":
		return phase
	default:
		return "custom"
	}
}

func normalizeWorkerProfileMaxTurns(value int) int {
	if value <= 0 {
		return 12
	}
	if value > workerProfileMaxTurns {
		return workerProfileMaxTurns
	}
	return value
}

func cleanWorkerProfileTools(items []string) []string {
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clampWorkerProfileText(value string, max int) (string, error) {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value, nil
	}
	return "", fmt.Errorf("text exceeds %d runes", max)
}
