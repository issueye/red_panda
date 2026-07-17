package model

import "time"

type Session struct {
	ID              string `gorm:"primaryKey"`
	Name            string
	WorkspaceRoot   string
	Status          string
	ParentID        string
	Kind            string
	SourceSessionID string
	ForkPointSeq    uint64
	ForkPointRunID  string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

type Message struct {
	ID              string `gorm:"primaryKey"`
	SessionID       string `gorm:"index"`
	Role            string
	ContentJSON     string
	Seq             uint64
	RunID           string `gorm:"index"`
	SourceMessageID string
	MetadataJSON    string
	CreatedAt       time.Time
}

type SessionLineage struct {
	ID              string `gorm:"primaryKey"`
	SourceSessionID string `gorm:"index"`
	TargetSessionID string `gorm:"index"`
	Operation       string `gorm:"index"`
	ForkPointSeq    uint64
	ForkPointRunID  string
	SourceStartSeq  uint64
	SourceEndSeq    uint64
	MetadataJSON    string
	CreatedAt       time.Time
}

type SessionCompaction struct {
	ID               string `gorm:"primaryKey"`
	SourceSessionID  string `gorm:"index"`
	TargetSessionID  string `gorm:"index"`
	Status           string `gorm:"index"`
	SourceStartSeq   uint64
	SourceEndSeq     uint64
	SummaryMessageID string
	SummaryJSON      string
	Error            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type MemoryRecord struct {
	ID              string `gorm:"primaryKey"`
	Scope           string `gorm:"index"`
	Kind            string `gorm:"index"`
	Status          string `gorm:"index"`
	Title           string
	Content         string
	Confidence      string `gorm:"index"`
	WorkspaceRoot   string `gorm:"index"`
	SessionID       string `gorm:"index"`
	RunID           string `gorm:"index"`
	SourceEventID   string
	SourceMessageID string
	Source          string `gorm:"index"`
	MetadataJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// TodoItem is a session-scoped operational checklist entry.
type TodoItem struct {
	ID               string `gorm:"primaryKey"`
	ClientKey        string `gorm:"index"`
	SessionID        string `gorm:"index"`
	Content          string
	Status           string `gorm:"index"`
	SortOrder        int
	Priority         string
	ActiveForm       string
	SourceRunID      string `gorm:"index"`
	SourceToolCallID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CompletedAt      *time.Time
}

type RunEvent struct {
	ID          string `gorm:"primaryKey"`
	RunID       string `gorm:"uniqueIndex:idx_v2_run_seq"`
	RunSeq      uint64 `gorm:"uniqueIndex:idx_v2_run_seq"`
	Type        string
	PayloadJSON string
	CreatedAt   time.Time
}

type RunRecord struct {
	ID            string `gorm:"primaryKey"`
	SessionID     string `gorm:"index"`
	WorkspaceRoot string
	RuntimeMode   string
	Status        string `gorm:"index"`
	Input         string
	GoalID        string `gorm:"index"`
	// TriggerSource identifies non-user starters (e.g. "schedule"); empty = interactive.
	TriggerSource string `gorm:"index"`
	// TriggerRef is the source entity id (e.g. schedule id).
	TriggerRef    string `gorm:"index"`
	LastEventType string
	LastRootSeq   uint64
	MessageCount  int
	ToolCount     int
	Error         string
	StartedAt     time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

// ScheduledTask is a Gateway-owned durable schedule that fires run.start equivalents.
type ScheduledTask struct {
	ID                string `gorm:"primaryKey"`
	Name              string `gorm:"index"`
	Description       string
	Enabled           bool   `gorm:"index"`
	ScheduleKind      string `gorm:"index"` // one_shot|interval|cron
	CronExpr          string
	IntervalSec       int
	RunAt             *time.Time
	Timezone          string
	Prompt            string `gorm:"type:text"`
	RunKind           string // chat|goal
	SessionMode       string // new_each_run|fixed_session
	SessionID         string `gorm:"index"`
	WorkspaceRoot     string `gorm:"index"`
	ProviderProfileID string
	RunOptionsJSON    string `gorm:"type:text"`
	ToolPolicy        string
	ToolAllowlistJSON string `gorm:"type:text"`
	ToolDenylistJSON  string `gorm:"type:text"`
	PermissionMode    string
	OverlapPolicy     string
	MissedPolicy      string
	MaxRuns           int
	RunCount          int
	LastRunID         string `gorm:"index"`
	LastStatus        string `gorm:"index"`
	LastError         string `gorm:"type:text"`
	LastFiredAt       *time.Time
	NextRunAt         *time.Time `gorm:"index"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time `gorm:"index"`
}

// ScheduledTaskRun is one fire attempt audit row linked to an optional run_id.
type ScheduledTaskRun struct {
	ID           string `gorm:"primaryKey"`
	ScheduleID   string `gorm:"index"`
	RunID        string `gorm:"index"`
	SessionID    string `gorm:"index"`
	Status       string `gorm:"index"` // starting|running|succeeded|failed|skipped|cancelled
	SkipReason   string
	ScheduledFor time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Error        string `gorm:"type:text"`
	CreatedAt    time.Time
}

// Goal is a session-scoped long-horizon objective with feedback-control state and budgets.
type Goal struct {
	ID                     string `gorm:"primaryKey"`
	SessionID              string `gorm:"index;uniqueIndex:idx_goals_one_active,where:status = 'active'"`
	Title                  string
	Objective              string `gorm:"type:text"`
	Status                 string `gorm:"index"` // pending|active|paused|succeeded|failed|cancelled
	PauseReason            string
	FailReason             string
	ReportMarkdown         string `gorm:"type:text"`
	MaxSegmentsPerRun      int
	MaxToolTurnsPerSegment int
	MaxTotalToolTurns      int
	MaxWallTimeSec         int
	UsedToolTurns          int
	UsedSegments           int
	UsedWallTimeSec        int
	ActiveRunID            string `gorm:"index"`
	LastRunID              string `gorm:"index"`
	SourceRunID            string
	SourceToolCallID       string
	StartedAt              *time.Time
	FinishedAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
	// Feedback-control projection.
	CriteriaJSON       string `gorm:"type:text"`
	ConstraintsJSON    string `gorm:"type:text"`
	Strategy           string `gorm:"type:text"`
	CurrentActionID    string `gorm:"index"`
	CurrentAction      string `gorm:"type:text"`
	LastObservation    string `gorm:"type:text"`
	LastAssessmentJSON string `gorm:"type:text"`
	LastDecision       string `gorm:"type:text"`
	OutcomeSummary     string `gorm:"type:text"`
	Iteration          int
	MaxIterations      int
	StagnationCount    int
	MaxStagnation      int
	Version            int
}

// GoalAction is a Goal-scoped, revisable unit of work. It intentionally does
// not reuse TodoItem because session checklists and outcome execution have
// different ownership and lifecycle rules.
type GoalAction struct {
	ID          string `gorm:"primaryKey"`
	GoalID      string `gorm:"index;uniqueIndex:idx_goal_action_key"`
	SessionID   string `gorm:"index"`
	ActionKey   string `gorm:"uniqueIndex:idx_goal_action_key"`
	Title       string
	Description string `gorm:"type:text"`
	Acceptance  string `gorm:"type:text"`
	Status      string `gorm:"index"` // queued|active|done|blocked|dropped
	Result      string `gorm:"type:text"`
	Evidence    string `gorm:"type:text"`
	Attempt     int
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FinishedAt  *time.Time
}

// GoalEvent is the append-only decision journal behind the Goal projection.
type GoalEvent struct {
	ID        string `gorm:"primaryKey"`
	GoalID    string `gorm:"index;uniqueIndex:idx_goal_event_seq"`
	SessionID string `gorm:"index"`
	RunID     string `gorm:"index"`
	Seq       int    `gorm:"uniqueIndex:idx_goal_event_seq"`
	Kind      string `gorm:"index"`
	Summary   string `gorm:"type:text"`
	Payload   string `gorm:"type:text"`
	CreatedAt time.Time
}

// GoalSegment makes Runtime segment accounting idempotent across retries.
type GoalSegment struct {
	ID           string `gorm:"primaryKey"`
	GoalID       string `gorm:"index;uniqueIndex:idx_goal_run_segment"`
	RunID        string `gorm:"index;uniqueIndex:idx_goal_run_segment"`
	SegmentIndex int    `gorm:"uniqueIndex:idx_goal_run_segment"`
	ToolTurns    int
	CreatedAt    time.Time
}

// GoalNote is a structured scratchpad entry scoped to a goal, shared across
// segments, runs, and specialist subagents via the context.* tools.
type GoalNote struct {
	ID        string `gorm:"primaryKey"`
	GoalID    string `gorm:"index;uniqueIndex:idx_goal_note_seq"`
	SessionID string `gorm:"index"`
	Seq       int    `gorm:"uniqueIndex:idx_goal_note_seq"`
	Kind      string `gorm:"index"`
	Title     string
	Body      string `gorm:"type:text"`
	Phase     string `gorm:"index"`
	Source    string
	RunID     string `gorm:"index"`
	Pinned    bool   `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ToolCall struct {
	ID            string `gorm:"primaryKey"`
	RunID         string `gorm:"index"`
	SessionID     string `gorm:"index"`
	WorkerID      string `gorm:"index"`
	AssignmentID  string `gorm:"index"`
	ProfileKey    string `gorm:"index"`
	ToolName      string `gorm:"index"`
	DisplayName   string
	Risk          string
	Policy        string
	PolicyReason  string
	ArgumentsJSON string
	Status        string `gorm:"index"`
	Output        string
	Error         string
	ExitCode      int
	DurationMS    int64
	StartedSeq    uint64
	FinishedSeq   uint64
	StartedAt     time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

type Workspace struct {
	ID           string `gorm:"primaryKey"`
	Root         string `gorm:"uniqueIndex"`
	Name         string
	LastOpenedAt time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PermissionRequest struct {
	ID            string `gorm:"primaryKey"`
	RunID         string `gorm:"index"`
	SessionID     string `gorm:"index"`
	ToolCallID    string
	ToolName      string
	Risk          string
	Summary       string
	Detail        string
	ArgumentsJSON string
	Status        string `gorm:"index"`
	Decision      string
	Reason        string
	RunSeq        uint64
	CreatedAt     time.Time
	ResolvedAt    *time.Time
	UpdatedAt     time.Time
}

type ProviderProfile struct {
	ID       string `gorm:"primaryKey"`
	Name     string
	Provider string `gorm:"index"`
	BaseURL  string
	Model    string
	// MaxTokens is the context window budget used for Desktop usage ring and auto-compact.
	// Zero means unset (no budget tracking / no auto-compact).
	MaxTokens    int
	APIKeySecret string
	IsDefault    bool `gorm:"index"`
	Active       bool `gorm:"index"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

type MCPTimeouts struct {
	StartMS      int `json:"start_ms,omitempty"`
	InitializeMS int `json:"initialize_ms,omitempty"`
	ListMS       int `json:"list_ms,omitempty"`
	CallMS       int `json:"call_ms,omitempty"`
	ShutdownMS   int `json:"shutdown_ms,omitempty"`
}

type MCPServerConfig struct {
	ID            string `gorm:"primaryKey"`
	Name          string `gorm:"uniqueIndex:idx_mcp_server_configs_name,where:deleted_at IS NULL"`
	Command       string
	Args          []string          `gorm:"column:args_json;serializer:json;type:text"`
	Env           map[string]string `gorm:"column:env_json;serializer:json;type:text"`
	CWD           string            `gorm:"column:cwd"`
	Enabled       bool              `gorm:"index"`
	Timeouts      MCPTimeouts       `gorm:"column:timeouts_json;serializer:json;type:text"`
	ToolAllowlist []string          `gorm:"column:tool_allowlist_json;serializer:json;type:text"`
	RiskOverrides map[string]string `gorm:"column:risk_overrides_json;serializer:json;type:text"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time `gorm:"index"`
}

// WorkerProfile configures how a Worker executes an Assignment. It is not a
// runtime Worker instance and therefore carries no Worker or Assignment state.
type WorkerProfile struct {
	ID              string `gorm:"primaryKey"`
	Key             string `gorm:"uniqueIndex:idx_worker_profiles_key,where:deleted_at IS NULL"`
	Name            string
	NameZH          string
	Kind            string `gorm:"index"` // builtin | custom
	// Phase is a capability tag for grouping/display only (docs/41 W3-4).
	// Wire/API field remains "phase" for compatibility; it is NOT a Goal pipeline stage.
	// Preferred values: research|strategy|build|review|assess|general|custom
	// (legacy analyze|plan|execute|verify|evaluate still accepted).
	Phase           string `gorm:"index"`
	Description     string
	SystemPrompt    string `gorm:"type:text"`
	Provider        string
	Model           string
	ToolAllowlist   []string `gorm:"column:tool_allowlist_json;serializer:json;type:text"`
	ToolDenylist    []string `gorm:"column:tool_denylist_json;serializer:json;type:text"`
	DefaultMaxTurns int
	Enabled         bool `gorm:"index"`
	Builtin         bool `gorm:"index"`
	SortOrder       int
	MetadataJSON    string `gorm:"type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time `gorm:"index"`
}
