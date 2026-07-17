package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"redpanda/gateway/internal/gateway/infra/eventhub"
	"redpanda/gateway/internal/gateway/model"
	"redpanda/gateway/internal/gateway/repository"
	"redpanda/protocol/methods"
	protows "redpanda/protocol/ws"
)

const (
	scheduleNameMaxRunes   = 64
	schedulePromptMaxRunes = 32 * 1024
	defaultScheduleMaxInflight = 3
)

// defaultScheduleAllowlist is the safe unattended tool surface (docs/43).
// Filtering is enforced via tool_allowlist; tool_policy must be a Runtime-valid
// value (risk_based / allow_all / deny_all / ask_all) — not the storage synonym "allowlist".
var defaultScheduleAllowlist = []string{
	"workspace.read_file",
	"workspace.list",
	"workspace.grep",
	"workspace.diff_file",
	"memory.list",
	"memory.create",
	"web.search",
	"web.fetch",
}

// scheduleToolPolicy maps API/storage values to Runtime tool_policy.
// Historical synonym "allowlist" means risk_based + tool_allowlist.
func scheduleToolPolicy(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deny_all", "allow_all", "ask_all", "risk_based":
		return strings.ToLower(strings.TrimSpace(raw))
	case "allowlist", "default", "":
		return "risk_based"
	default:
		return "risk_based"
	}
}

// schedulePermissionMode maps API/storage values to Runtime permission_mode.
// Historical synonym "deny" means deny_all (unattended: no interactive approval).
func schedulePermissionMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "deny_all", "allow_all", "strict", "permissive":
		return strings.ToLower(strings.TrimSpace(raw))
	case "deny", "":
		return "deny_all"
	default:
		return "deny_all"
	}
}

type runStarter interface {
	Start(ctx context.Context, payload protows.RunStartPayload) (StartRunResult, error)
}

// ScheduleService owns CRUD, next_run computation, and fire dispatch.
type ScheduleService struct {
	repos   repository.Set
	hub     *eventhub.Hub
	runs    runStarter
	clock   Clock
	mu      sync.Mutex
	inflight int
	maxInflight int
}

type ScheduleCreateRequest struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	Enabled           *bool          `json:"enabled"`
	ScheduleKind      string         `json:"schedule_kind"`
	CronExpr          string         `json:"cron_expr"`
	IntervalSec       int            `json:"interval_sec"`
	RunAt             string         `json:"run_at"`
	Timezone          string         `json:"timezone"`
	Prompt            string         `json:"prompt"`
	RunKind           string         `json:"run_kind"`
	SessionMode       string         `json:"session_mode"`
	SessionID         string         `json:"session_id"`
	WorkspaceRoot     string         `json:"workspace_root"`
	ProviderProfileID string         `json:"provider_profile_id"`
	ToolPolicy        string         `json:"tool_policy"`
	ToolAllowlist     []string       `json:"tool_allowlist"`
	ToolDenylist      []string       `json:"tool_denylist"`
	PermissionMode    string         `json:"permission_mode"`
	OverlapPolicy     string         `json:"overlap_policy"`
	MaxRuns           int            `json:"max_runs"`
	RunOptions        map[string]any `json:"run_options"`
}

type ScheduleUpdateRequest struct {
	Name              *string        `json:"name"`
	Description       *string        `json:"description"`
	Enabled           *bool          `json:"enabled"`
	ScheduleKind      *string        `json:"schedule_kind"`
	CronExpr          *string        `json:"cron_expr"`
	IntervalSec       *int           `json:"interval_sec"`
	RunAt             *string        `json:"run_at"`
	Timezone          *string        `json:"timezone"`
	Prompt            *string        `json:"prompt"`
	RunKind           *string        `json:"run_kind"`
	SessionMode       *string        `json:"session_mode"`
	SessionID         *string        `json:"session_id"`
	WorkspaceRoot     *string        `json:"workspace_root"`
	ProviderProfileID *string        `json:"provider_profile_id"`
	ToolPolicy        *string        `json:"tool_policy"`
	ToolAllowlist     []string       `json:"tool_allowlist"`
	ToolDenylist      []string       `json:"tool_denylist"`
	PermissionMode    *string        `json:"permission_mode"`
	OverlapPolicy     *string        `json:"overlap_policy"`
	MaxRuns           *int           `json:"max_runs"`
	RunOptions        map[string]any `json:"run_options"`
}

type ScheduleTriggerResult struct {
	ScheduleRunID string `json:"schedule_run_id"`
	RunID         string `json:"run_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Status        string `json:"status"`
	SkipReason    string `json:"skip_reason,omitempty"`
	Error         string `json:"error,omitempty"`
}

func NewScheduleService(repos repository.Set, hub *eventhub.Hub, runs runStarter) ScheduleService {
	return ScheduleService{
		repos:       repos,
		hub:         hub,
		runs:        runs,
		clock:       realClock{},
		maxInflight: defaultScheduleMaxInflight,
	}
}

// WithClock injects a test clock.
func (s ScheduleService) WithClock(c Clock) ScheduleService {
	s.clock = c
	return s
}

func (s ScheduleService) Create(req ScheduleCreateRequest) (methods.ScheduleDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > scheduleNameMaxRunes {
		return methods.ScheduleDTO{}, fmt.Errorf("name exceeds %d characters", scheduleNameMaxRunes)
	}
	if _, err := s.repos.Schedules.GetByName(name); err == nil {
		return methods.ScheduleDTO{}, fmt.Errorf("schedule name %q already exists", name)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("prompt is required")
	}
	if utf8.RuneCountInString(prompt) > schedulePromptMaxRunes {
		return methods.ScheduleDTO{}, fmt.Errorf("prompt exceeds size limit")
	}
	kind := strings.TrimSpace(req.ScheduleKind)
	if kind == "" {
		kind = methods.ScheduleKindInterval
	}
	var runAt *time.Time
	if strings.TrimSpace(req.RunAt) != "" {
		t, err := parseFlexibleTime(req.RunAt, req.Timezone)
		if err != nil {
			return methods.ScheduleDTO{}, err
		}
		runAt = &t
	}
	if err := validateScheduleKindFields(kind, req.IntervalSec, req.CronExpr, runAt); err != nil {
		return methods.ScheduleDTO{}, err
	}
	sessionMode := strings.TrimSpace(req.SessionMode)
	if sessionMode == "" {
		sessionMode = methods.ScheduleSessionNewEach
	}
	if sessionMode != methods.ScheduleSessionNewEach && sessionMode != methods.ScheduleSessionFixed {
		return methods.ScheduleDTO{}, fmt.Errorf("session_mode must be new_each_run or fixed_session")
	}
	if sessionMode == methods.ScheduleSessionFixed && strings.TrimSpace(req.SessionID) == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("session_id is required for fixed_session")
	}
	runKind := strings.TrimSpace(req.RunKind)
	if runKind == "" {
		runKind = methods.ScheduleRunKindChat
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	toolPolicy := scheduleToolPolicy(req.ToolPolicy)
	permissionMode := schedulePermissionMode(req.PermissionMode)
	overlap := strings.TrimSpace(req.OverlapPolicy)
	if overlap == "" {
		overlap = methods.ScheduleOverlapSkip
	}
	allowlist := req.ToolAllowlist
	if len(allowlist) == 0 {
		allowlist = append([]string(nil), defaultScheduleAllowlist...)
	}
	maxRuns := req.MaxRuns
	if kind == methods.ScheduleKindOneShot {
		maxRuns = 1
	}
	workspace := strings.TrimSpace(req.WorkspaceRoot)
	if sessionMode == methods.ScheduleSessionFixed {
		session, err := s.repos.Sessions.Get(strings.TrimSpace(req.SessionID))
		if err != nil {
			return methods.ScheduleDTO{}, fmt.Errorf("session not found")
		}
		if workspace == "" {
			workspace = session.WorkspaceRoot
		}
	}
	if workspace == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("workspace_root is required")
	}

	optionsJSON := ""
	if len(req.RunOptions) > 0 {
		raw, err := json.Marshal(req.RunOptions)
		if err != nil {
			return methods.ScheduleDTO{}, fmt.Errorf("invalid run_options")
		}
		optionsJSON = string(raw)
	}
	allowJSON, _ := json.Marshal(allowlist)
	denyJSON, _ := json.Marshal(req.ToolDenylist)

	now := s.now()
	row := model.ScheduledTask{
		Name:              name,
		Description:       strings.TrimSpace(req.Description),
		Enabled:           enabled,
		ScheduleKind:      kind,
		CronExpr:          strings.TrimSpace(req.CronExpr),
		IntervalSec:       req.IntervalSec,
		RunAt:             runAt,
		Timezone:          strings.TrimSpace(req.Timezone),
		Prompt:            prompt,
		RunKind:           runKind,
		SessionMode:       sessionMode,
		SessionID:         strings.TrimSpace(req.SessionID),
		WorkspaceRoot:     workspace,
		ProviderProfileID: strings.TrimSpace(req.ProviderProfileID),
		RunOptionsJSON:    optionsJSON,
		ToolPolicy:        toolPolicy,
		ToolAllowlistJSON: string(allowJSON),
		ToolDenylistJSON:  string(denyJSON),
		PermissionMode:    permissionMode,
		OverlapPolicy:     overlap,
		MissedPolicy:      methods.ScheduleMissedSkip,
		MaxRuns:           maxRuns,
		LastStatus:        "idle",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if enabled {
		// Interval: first fire after one full interval from now (avoid create→immediate storm).
		base := now
		if kind == methods.ScheduleKindInterval {
			// next = now + interval handled inside compute when LastFiredAt nil
		}
		if kind == methods.ScheduleKindOneShot && runAt != nil {
			// ok
		}
		next, err := computeNextRunAt(row, base.Add(-time.Nanosecond))
		if err != nil {
			return methods.ScheduleDTO{}, err
		}
		// For interval first schedule, use now+interval explicitly.
		if kind == methods.ScheduleKindInterval {
			t := now.Add(time.Duration(row.IntervalSec) * time.Second)
			next = &t
		}
		row.NextRunAt = next
	}
	created, err := s.repos.Schedules.Create(row)
	if err != nil {
		return methods.ScheduleDTO{}, err
	}
	return scheduleDTO(created), nil
}

func (s ScheduleService) Update(id string, req ScheduleUpdateRequest) (methods.ScheduleDTO, error) {
	row, err := s.repos.Schedules.Get(id)
	if err != nil {
		return methods.ScheduleDTO{}, fmt.Errorf("schedule not found")
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return methods.ScheduleDTO{}, fmt.Errorf("name is required")
		}
		if existing, err := s.repos.Schedules.GetByName(name); err == nil && existing.ID != row.ID {
			return methods.ScheduleDTO{}, fmt.Errorf("schedule name %q already exists", name)
		}
		row.Name = name
	}
	if req.Description != nil {
		row.Description = strings.TrimSpace(*req.Description)
	}
	if req.ScheduleKind != nil {
		row.ScheduleKind = strings.TrimSpace(*req.ScheduleKind)
	}
	if req.CronExpr != nil {
		row.CronExpr = strings.TrimSpace(*req.CronExpr)
	}
	if req.IntervalSec != nil {
		row.IntervalSec = *req.IntervalSec
	}
	if req.RunAt != nil {
		if strings.TrimSpace(*req.RunAt) == "" {
			row.RunAt = nil
		} else {
			tz := row.Timezone
			if req.Timezone != nil {
				tz = *req.Timezone
			}
			t, err := parseFlexibleTime(*req.RunAt, tz)
			if err != nil {
				return methods.ScheduleDTO{}, err
			}
			row.RunAt = &t
		}
	}
	if req.Timezone != nil {
		row.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.Prompt != nil {
		prompt := strings.TrimSpace(*req.Prompt)
		if prompt == "" {
			return methods.ScheduleDTO{}, fmt.Errorf("prompt is required")
		}
		row.Prompt = prompt
	}
	if req.RunKind != nil {
		row.RunKind = strings.TrimSpace(*req.RunKind)
	}
	if req.SessionMode != nil {
		row.SessionMode = strings.TrimSpace(*req.SessionMode)
	}
	if req.SessionID != nil {
		row.SessionID = strings.TrimSpace(*req.SessionID)
	}
	if req.WorkspaceRoot != nil {
		row.WorkspaceRoot = strings.TrimSpace(*req.WorkspaceRoot)
	}
	if req.ProviderProfileID != nil {
		row.ProviderProfileID = strings.TrimSpace(*req.ProviderProfileID)
	}
	if req.ToolPolicy != nil {
		row.ToolPolicy = scheduleToolPolicy(*req.ToolPolicy)
	}
	if req.ToolAllowlist != nil {
		raw, _ := json.Marshal(req.ToolAllowlist)
		row.ToolAllowlistJSON = string(raw)
	}
	if req.ToolDenylist != nil {
		raw, _ := json.Marshal(req.ToolDenylist)
		row.ToolDenylistJSON = string(raw)
	}
	if req.PermissionMode != nil {
		row.PermissionMode = schedulePermissionMode(*req.PermissionMode)
	}
	if req.OverlapPolicy != nil {
		row.OverlapPolicy = strings.TrimSpace(*req.OverlapPolicy)
	}
	if req.MaxRuns != nil {
		row.MaxRuns = *req.MaxRuns
	}
	if req.RunOptions != nil {
		raw, err := json.Marshal(req.RunOptions)
		if err != nil {
			return methods.ScheduleDTO{}, fmt.Errorf("invalid run_options")
		}
		row.RunOptionsJSON = string(raw)
	}
	if err := validateScheduleKindFields(row.ScheduleKind, row.IntervalSec, row.CronExpr, row.RunAt); err != nil {
		return methods.ScheduleDTO{}, err
	}
	if row.SessionMode == methods.ScheduleSessionFixed && row.SessionID == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("session_id is required for fixed_session")
	}
	if row.WorkspaceRoot == "" {
		return methods.ScheduleDTO{}, fmt.Errorf("workspace_root is required")
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}
	now := s.now()
	if row.Enabled {
		next, err := s.recomputeNext(row, now)
		if err != nil {
			return methods.ScheduleDTO{}, err
		}
		row.NextRunAt = next
	} else {
		row.NextRunAt = nil
	}
	updated, err := s.repos.Schedules.Update(row)
	if err != nil {
		return methods.ScheduleDTO{}, err
	}
	return scheduleDTO(updated), nil
}

func (s ScheduleService) Get(id string) (methods.ScheduleDTO, error) {
	row, err := s.repos.Schedules.Get(id)
	if err != nil {
		return methods.ScheduleDTO{}, fmt.Errorf("schedule not found")
	}
	return scheduleDTO(row), nil
}

func (s ScheduleService) List(enabledOnly bool, limit int) ([]methods.ScheduleDTO, error) {
	rows, err := s.repos.Schedules.List(enabledOnly, limit)
	if err != nil {
		return nil, err
	}
	out := make([]methods.ScheduleDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleDTO(row))
	}
	return out, nil
}

func (s ScheduleService) Delete(id string) (methods.ScheduleDTO, error) {
	row, err := s.repos.Schedules.SoftDelete(id)
	if err != nil {
		return methods.ScheduleDTO{}, fmt.Errorf("schedule not found")
	}
	return scheduleDTO(row), nil
}

func (s ScheduleService) SetEnabled(id string, enabled bool) (methods.ScheduleDTO, error) {
	row, err := s.repos.Schedules.Get(id)
	if err != nil {
		return methods.ScheduleDTO{}, fmt.Errorf("schedule not found")
	}
	row.Enabled = enabled
	now := s.now()
	if enabled {
		next, err := s.recomputeNext(row, now)
		if err != nil {
			return methods.ScheduleDTO{}, err
		}
		row.NextRunAt = next
	} else {
		row.NextRunAt = nil
	}
	updated, err := s.repos.Schedules.Update(row)
	if err != nil {
		return methods.ScheduleDTO{}, err
	}
	return scheduleDTO(updated), nil
}

func (s ScheduleService) ListRuns(scheduleID string, limit int) ([]methods.ScheduleRunDTO, error) {
	if _, err := s.repos.Schedules.Get(scheduleID); err != nil {
		return nil, fmt.Errorf("schedule not found")
	}
	rows, err := s.repos.Schedules.ListRuns(scheduleID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]methods.ScheduleRunDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, scheduleRunDTO(row))
	}
	return out, nil
}

func (s ScheduleService) recomputeNext(row model.ScheduledTask, now time.Time) (*time.Time, error) {
	if row.ScheduleKind == methods.ScheduleKindInterval && row.LastFiredAt == nil {
		t := now.Add(time.Duration(row.IntervalSec) * time.Second)
		return &t, nil
	}
	return computeNextRunAt(row, now)
}

func (s ScheduleService) now() time.Time {
	if s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

func parseFlexibleTime(value, tz string) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04",
	}
	loc, err := loadLocation(tz)
	if err != nil {
		return time.Time{}, err
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid run_at time %q", value)
}

func scheduleDTO(row model.ScheduledTask) methods.ScheduleDTO {
	dto := methods.ScheduleDTO{
		ID:                row.ID,
		Name:              row.Name,
		Description:       row.Description,
		Enabled:           row.Enabled,
		ScheduleKind:      row.ScheduleKind,
		CronExpr:          row.CronExpr,
		IntervalSec:       row.IntervalSec,
		Timezone:          row.Timezone,
		Prompt:            row.Prompt,
		RunKind:           row.RunKind,
		SessionMode:       row.SessionMode,
		SessionID:         row.SessionID,
		WorkspaceRoot:     row.WorkspaceRoot,
		ProviderProfileID: row.ProviderProfileID,
		ToolPolicy:        row.ToolPolicy,
		PermissionMode:    row.PermissionMode,
		OverlapPolicy:     row.OverlapPolicy,
		MissedPolicy:      row.MissedPolicy,
		MaxRuns:           row.MaxRuns,
		RunCount:          row.RunCount,
		LastRunID:         row.LastRunID,
		LastStatus:        row.LastStatus,
		LastError:         row.LastError,
		CreatedAt:         row.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:         row.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if row.RunAt != nil {
		dto.RunAt = row.RunAt.UTC().Format(time.RFC3339Nano)
	}
	if row.LastFiredAt != nil {
		dto.LastFiredAt = row.LastFiredAt.UTC().Format(time.RFC3339Nano)
	}
	if row.NextRunAt != nil {
		dto.NextRunAt = row.NextRunAt.UTC().Format(time.RFC3339Nano)
	}
	_ = json.Unmarshal([]byte(row.ToolAllowlistJSON), &dto.ToolAllowlist)
	_ = json.Unmarshal([]byte(row.ToolDenylistJSON), &dto.ToolDenylist)
	return dto
}

func scheduleRunDTO(row model.ScheduledTaskRun) methods.ScheduleRunDTO {
	dto := methods.ScheduleRunDTO{
		ID:           row.ID,
		ScheduleID:   row.ScheduleID,
		RunID:        row.RunID,
		SessionID:    row.SessionID,
		Status:       row.Status,
		SkipReason:   row.SkipReason,
		ScheduledFor: row.ScheduledFor.UTC().Format(time.RFC3339Nano),
		Error:        row.Error,
		CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	if row.StartedAt != nil {
		dto.StartedAt = row.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if row.FinishedAt != nil {
		dto.FinishedAt = row.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return dto
}

func decodeStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}
