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
	RootRunID   string `gorm:"uniqueIndex:idx_run_seq"`
	RootSeq     uint64 `gorm:"uniqueIndex:idx_run_seq"`
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
	LastEventType string
	LastRootSeq   uint64
	MessageCount  int
	ToolCount     int
	Error         string
	StartedAt     time.Time
	FinishedAt    *time.Time
	UpdatedAt     time.Time
}

// Goal is a session-scoped long-horizon objective with pipeline phase and budgets.
type Goal struct {
	ID                     string `gorm:"primaryKey"`
	SessionID              string `gorm:"index;uniqueIndex:idx_goals_one_active,where:status = 'active'"`
	Title                  string
	Objective              string `gorm:"type:text"`
	SuccessCriteria        string `gorm:"type:text"`
	Status                 string `gorm:"index"` // pending|active|paused|succeeded|failed|cancelled
	PipelinePhase          string `gorm:"index"` // analyze|plan|execute|verify|evaluate|report
	PauseReason            string
	FailReason             string
	AnalysisSummary        string `gorm:"type:text"`
	CheckpointSummary      string `gorm:"type:text"`
	ProgressNote           string `gorm:"type:text"`
	ReportJSON             string `gorm:"type:text"`
	ReportMarkdown         string `gorm:"type:text"`
	MaxSegmentsPerRun      int
	MaxToolTurnsPerSegment int
	MaxTotalToolTurns      int
	MaxWallTimeSec         int
	MaxAutoContinues       int
	UsedToolTurns          int
	UsedSegments           int
	UsedAutoContinues      int
	UsedWallTimeSec        int
	ActiveRunID            string `gorm:"index"`
	LastRunID              string `gorm:"index"`
	SourceRunID            string
	SourceToolCallID       string
	StartedAt              *time.Time
	FinishedAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
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

type ToolCall struct {
	ID            string `gorm:"primaryKey"`
	RootRunID     string `gorm:"index"`
	SessionID     string `gorm:"index"`
	AgentID       string
	AgentRole     string
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
	RootSeq       uint64
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

// AgentDefinition is a managed specialist / agent profile (builtin goal specialists or custom).
// Runtime subagent.run matches by Key (e.g. goal-analyst).
type AgentDefinition struct {
	ID              string `gorm:"primaryKey"`
	Key             string `gorm:"uniqueIndex:idx_agent_definitions_key,where:deleted_at IS NULL"`
	Name            string
	NameZH          string
	Kind            string `gorm:"index"` // builtin | custom
	Phase           string `gorm:"index"` // analyze | plan | execute | verify | evaluate | custom | general
	Description     string
	SystemPrompt    string   `gorm:"type:text"`
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
