package methods

// Schedule-related protocol DTOs and internal RPC names (v0.2.1).

const (
	ScheduleToolExecute = "schedule.tool.execute"

	StateToolDomainSchedule = "schedule"

	ScheduleKindOneShot  = "one_shot"
	ScheduleKindInterval = "interval"
	ScheduleKindCron     = "cron"

	ScheduleSessionNewEach  = "new_each_run"
	ScheduleSessionFixed    = "fixed_session"
	ScheduleRunKindChat     = "chat"
	ScheduleRunKindGoal     = "goal"
	ScheduleOverlapSkip     = "skip"
	ScheduleMissedSkip      = "skip_missed"
	ScheduleCatchUpOne      = "one"
	ScheduleCatchUpNone     = "none"
)

// ScheduleDTO is the shared HTTP / tool shape for a scheduled task definition.
type ScheduleDTO struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description,omitempty"`
	Enabled            bool     `json:"enabled"`
	ScheduleKind       string   `json:"schedule_kind"`
	CronExpr           string   `json:"cron_expr,omitempty"`
	IntervalSec        int      `json:"interval_sec,omitempty"`
	RunAt              string   `json:"run_at,omitempty"`
	Timezone           string   `json:"timezone,omitempty"`
	Prompt             string   `json:"prompt"`
	RunKind            string   `json:"run_kind,omitempty"`
	SessionMode        string   `json:"session_mode,omitempty"`
	SessionID          string   `json:"session_id,omitempty"`
	WorkspaceRoot      string   `json:"workspace_root,omitempty"`
	ProviderProfileID  string   `json:"provider_profile_id,omitempty"`
	ToolPolicy         string   `json:"tool_policy,omitempty"`
	ToolAllowlist      []string `json:"tool_allowlist,omitempty"`
	ToolDenylist       []string `json:"tool_denylist,omitempty"`
	PermissionMode     string   `json:"permission_mode,omitempty"`
	OverlapPolicy      string   `json:"overlap_policy,omitempty"`
	MissedPolicy       string   `json:"missed_policy,omitempty"`
	MaxRuns            int      `json:"max_runs,omitempty"`
	RunCount           int      `json:"run_count"`
	LastRunID          string   `json:"last_run_id,omitempty"`
	LastStatus         string   `json:"last_status,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
	LastFiredAt        string   `json:"last_fired_at,omitempty"`
	NextRunAt          string   `json:"next_run_at,omitempty"`
	CreatedAt          string   `json:"created_at,omitempty"`
	UpdatedAt          string   `json:"updated_at,omitempty"`
}

// ScheduleRunDTO is one fire attempt / audit row.
type ScheduleRunDTO struct {
	ID           string `json:"id"`
	ScheduleID   string `json:"schedule_id"`
	RunID        string `json:"run_id,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	Status       string `json:"status"`
	SkipReason   string `json:"skip_reason,omitempty"`
	ScheduledFor string `json:"scheduled_for,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Error        string `json:"error,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
}

// ScheduleToolExecuteParams is Runtime → Gateway for schedule.* tools.
type ScheduleToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// ScheduleToolExecuteResult is returned to Runtime after schedule tool work.
type ScheduleToolExecuteResult struct {
	OK       bool            `json:"ok"`
	Output   string          `json:"output,omitempty"`
	Schedule *ScheduleDTO    `json:"schedule,omitempty"`
	Items    []ScheduleDTO   `json:"items,omitempty"`
	Runs     []ScheduleRunDTO `json:"runs,omitempty"`
	RunID    string          `json:"run_id,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// AsScheduleParams projects the unified state-tool envelope onto schedule params.
func (p StateToolExecuteParams) AsScheduleParams() ScheduleToolExecuteParams {
	return ScheduleToolExecuteParams{
		RunID: p.RunID, SessionID: p.SessionID, WorkspaceRoot: p.WorkspaceRoot,
		ToolCallID: p.ToolCallID, ToolName: p.ToolName, Arguments: p.Arguments,
	}
}
