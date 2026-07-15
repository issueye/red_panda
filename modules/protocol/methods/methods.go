package methods

import (
	"encoding/json"
	"time"

	protocolmcp "redpanda/protocol/mcp"
)

const (
	CoreInitialize = "core.initialize"
	CorePing       = "core.ping"
	CoreShutdown   = "core.shutdown"

	// Skills management remains under agent.* namespace for catalog operations.
	AgentSkills       = "agent.skills"
	AgentSkillLoad    = "agent.skill.load"
	AgentSkillCreate  = "agent.skill.create"
	AgentSkillUpdate  = "agent.skill.update"
	AgentSkillDelete  = "agent.skill.delete"
	MCPDiscover       = "mcp.discover"
	PermissionResolve = "permission.resolve"

	// Gateway-backed state tools (memory/todo/goal/context) — stable internal RPCs.
	MemoryToolExecute  = "memory.tool.execute"
	TodoToolExecute    = "todo.tool.execute"
	GoalToolExecute    = "goal.tool.execute"
	ContextToolExecute = "context.tool.execute"

	// v0.2 primary execution protocol (no legacy subagent/root model).
	RunExecute             = "run.execute"
	RunCancel              = "run.cancel"
	RunPause               = "run.pause"
	RunResume              = "run.resume_execution"
	RunEvent               = "run.event"
	WorkerList             = "worker.list"
	WorkerAssignmentCancel = "worker.assignment.cancel"
	WorkerMessageSend      = "worker.message.send"
	WorkerMessageReceive   = "worker.message.receive"
	WorkerPoolStatus       = "worker.pool.status"
)

type WorkerState string

const (
	WorkerStateReady     WorkerState = "ready"
	WorkerStateBusy      WorkerState = "busy"
	WorkerStateDraining  WorkerState = "draining"
	WorkerStateUnhealthy WorkerState = "unhealthy"
	WorkerStateStopped   WorkerState = "stopped"
)

type AssignmentStatus string

const (
	AssignmentStatusQueued            AssignmentStatus = "queued"
	AssignmentStatusRunning           AssignmentStatus = "running"
	AssignmentStatusPaused            AssignmentStatus = "paused"
	AssignmentStatusWaitingPermission AssignmentStatus = "waiting_permission"
	AssignmentStatusCompleted         AssignmentStatus = "completed"
	AssignmentStatusFailed            AssignmentStatus = "failed"
	AssignmentStatusCancelled         AssignmentStatus = "cancelled"
)

type MessageKind string

const (
	MessageKindRequest MessageKind = "request"
	MessageKindUpdate  MessageKind = "update"
	MessageKindResult  MessageKind = "result"
	MessageKindControl MessageKind = "control"
)

// WorkerRef describes a stable Worker slot. ProfileKey is assignment-scoped;
// it is empty while the Worker is idle.
type WorkerRef struct {
	ID                  string      `json:"id"`
	State               WorkerState `json:"state,omitempty"`
	CurrentAssignmentID string      `json:"current_assignment_id,omitempty"`
	ProfileKey          string      `json:"profile_key,omitempty"`
	MailboxDepth        int         `json:"mailbox_depth,omitempty"`
	MailboxCapacity     int         `json:"mailbox_capacity,omitempty"`
	Healthy             bool        `json:"healthy"`
}

type AssignmentRecord struct {
	ID             string           `json:"id"`
	RunID          string           `json:"run_id"`
	WorkerID       string           `json:"worker_id"`
	OriginWorkerID string           `json:"origin_worker_id,omitempty"`
	ProfileKey     string           `json:"profile_key,omitempty"`
	Task           string           `json:"task"`
	Status         AssignmentStatus `json:"status"`
	Result         string           `json:"result,omitempty"`
	Error          string           `json:"error,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	StartedAt      *time.Time       `json:"started_at,omitempty"`
	FinishedAt     *time.Time       `json:"finished_at,omitempty"`
}

type PoolSnapshot struct {
	Configured        int                `json:"configured"`
	Ready             int                `json:"ready"`
	Busy              int                `json:"busy"`
	Draining          int                `json:"draining"`
	Unhealthy         int                `json:"unhealthy"`
	Stopped           int                `json:"stopped"`
	Queued            int                `json:"queued"`
	Running           int                `json:"running"`
	WaitingPermission int                `json:"waiting_permission"`
	Paused            int                `json:"paused"`
	Workers           []WorkerRef        `json:"workers"`
	Assignments       []AssignmentRecord `json:"assignments"`
}

type WorkerMessage struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	FromWorkerID     string          `json:"from_worker_id"`
	FromAssignmentID string          `json:"from_assignment_id"`
	ToWorkerID       string          `json:"to_worker_id"`
	ToAssignmentID   string          `json:"to_assignment_id"`
	Kind             MessageKind     `json:"kind"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	ReplyTo          string          `json:"reply_to,omitempty"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        time.Time       `json:"created_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
}

// RunExecuteParams starts one top-level Run under the v0.2 Worker model.
type RunExecuteParams struct {
	RunID   string            `json:"run_id"`
	Session ReplySession      `json:"session"`
	Input   ReplyInput        `json:"input"`
	Options RunExecuteOptions `json:"options"`
}

type RunExecuteOptions struct {
	ProviderProfileID   string                        `json:"provider_profile_id,omitempty"`
	ProviderName        string                        `json:"provider_name,omitempty"`
	ProviderBaseURL     string                        `json:"provider_base_url,omitempty"`
	ProviderAPIKey      string                        `json:"provider_api_key,omitempty"`
	Model               string                        `json:"model,omitempty"`
	PermissionMode      string                        `json:"permission_mode,omitempty"`
	ToolPolicy          string                        `json:"tool_policy,omitempty"`
	ToolAllowlist       []string                      `json:"tool_allowlist,omitempty"`
	ToolDenylist        []string                      `json:"tool_denylist,omitempty"`
	EmitToolEvents      bool                          `json:"emit_tool_events"`
	RequirePermission   bool                          `json:"require_permission,omitempty"`
	MemoryContext       *MemoryContext                `json:"memory_context,omitempty"`
	TodoContext         *TodoContext                  `json:"todo_context,omitempty"`
	GoalContext         *GoalContext                  `json:"goal_context,omitempty"`
	GoalsEnabled        *bool                         `json:"goals_enabled,omitempty"`
	GoalID              string                        `json:"goal_id,omitempty"`
	ContinueGoal        bool                          `json:"continue_goal,omitempty"`
	SkillsContext       *SkillsContext                `json:"skills_context,omitempty"`
	WebSearchMaxResults int                           `json:"web_search_max_results,omitempty"`
	WebFetchMaxBytes    int                           `json:"web_fetch_max_bytes,omitempty"`
	WebSearchProvider   string                        `json:"web_search_provider,omitempty"`
	WebTavilyAPIKey     string                        `json:"web_tavily_api_key,omitempty"`
	WebHTTPProxy        string                        `json:"web_http_proxy,omitempty"`
	MaxToolTurns        int                           `json:"max_tool_turns,omitempty"`
	LogLLMRequests      bool                          `json:"log_llm_requests,omitempty"`
	WorkerPoolSize      int                           `json:"worker_pool_size,omitempty"`
	WorkerProfiles      []WorkerProfileRef            `json:"worker_profiles,omitempty"`
	WorkerContext       *WorkerExecutionContext       `json:"worker_context,omitempty"`
	DebugTools          bool                          `json:"debug_tools,omitempty"`
	MCPServers          []protocolmcp.MCPServerConfig `json:"mcp_servers,omitempty"`
}

// WorkerExecutionContext is trusted Runtime-to-Runtime execution metadata.
// It is transported over IPC and must never be populated from model tool args.
type WorkerExecutionContext struct {
	WorkerID      string `json:"worker_id"`
	AssignmentID  string `json:"assignment_id"`
	RunID         string `json:"run_id"`
	ProxyMessages bool   `json:"proxy_messages"`
}

// WorkerProfileRef is an execution policy attached to a Run. It is
// configuration only and never identifies a Worker slot.
type WorkerProfileRef struct {
	Key               string   `json:"key"`
	Name              string   `json:"name,omitempty"`
	NameZH            string   `json:"name_zh,omitempty"`
	Description       string   `json:"description,omitempty"`
	Phase             string   `json:"phase,omitempty"`
	SystemPrompt      string   `json:"system_prompt,omitempty"`
	ProviderProfileID string   `json:"provider_profile_id,omitempty"`
	ProviderName      string   `json:"provider_name,omitempty"`
	Model             string   `json:"model,omitempty"`
	ToolPolicy        string   `json:"tool_policy,omitempty"`
	ToolAllowlist     []string `json:"tool_allowlist,omitempty"`
	ToolDenylist      []string `json:"tool_denylist,omitempty"`
	DefaultMaxTurns   int      `json:"default_max_turns,omitempty"`
	Enabled           bool     `json:"enabled"`
}

type RunExecuteResult struct {
	Accepted     bool   `json:"accepted"`
	RunID        string `json:"run_id"`
	AssignmentID string `json:"assignment_id"`
	WorkerID     string `json:"worker_id"`
}

type RunCancelParams struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

type RunCancelResult struct {
	Accepted  bool   `json:"accepted"`
	RunID     string `json:"run_id"`
	Cancelled int    `json:"cancelled"`
}

type RunPauseParams struct {
	RunID         string `json:"run_id"`
	Reason        string `json:"reason,omitempty"`
	DelegatedOnly bool   `json:"delegated_only,omitempty"`
}

type RunPauseResult struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"run_id"`
	Paused   int    `json:"paused"`
}

type RunResumeParams struct {
	RunID         string `json:"run_id"`
	DelegatedOnly bool   `json:"delegated_only,omitempty"`
}

type RunResumeResult struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"run_id"`
	Resumed  int    `json:"resumed"`
}

type WorkerListParams struct {
	RunID        string `json:"run_id,omitempty"`
	WorkerID     string `json:"worker_id,omitempty"`
	AssignmentID string `json:"assignment_id,omitempty"`
}

type WorkerListResult struct {
	Workers     []WorkerRef        `json:"workers"`
	Assignments []AssignmentRecord `json:"assignments"`
}

type WorkerAssignmentCancelParams struct {
	RunID        string `json:"run_id"`
	AssignmentID string `json:"assignment_id"`
	Reason       string `json:"reason,omitempty"`
}

type WorkerAssignmentCancelResult struct {
	Accepted     bool   `json:"accepted"`
	RunID        string `json:"run_id"`
	AssignmentID string `json:"assignment_id"`
	Cancelled    bool   `json:"cancelled"`
}

type WorkerMessageSendParams struct {
	ToWorkerID     string          `json:"to_worker_id"`
	ToAssignmentID string          `json:"to_assignment_id"`
	Kind           MessageKind     `json:"kind"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	ReplyTo        string          `json:"reply_to,omitempty"`
	Payload        json.RawMessage `json:"payload"`
}

type WorkerMessageSendResult struct {
	Accepted bool          `json:"accepted"`
	Message  WorkerMessage `json:"message"`
}

type WorkerMessageReceiveParams struct {
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

type WorkerMessageReceiveResult struct {
	Found   bool          `json:"found"`
	Message WorkerMessage `json:"message"`
}

type WorkerPoolStatusParams struct{}

type WorkerPoolStatusResult struct {
	Pool PoolSnapshot `json:"pool"`
}

type PeerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Capability struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
}

type InitializeParams struct {
	ProtocolVersion string       `json:"protocol_version"`
	Client          PeerInfo     `json:"client"`
	WorkspaceRoot   string       `json:"workspace_root,omitempty"`
	Environment     Environment  `json:"environment"`
	Capabilities    []Capability `json:"capabilities"`
}

type Environment struct {
	PermissionMode  string `json:"permission_mode,omitempty"`
	DefaultProvider string `json:"default_provider,omitempty"`
}

type InitializeResult struct {
	ProtocolVersion string       `json:"protocol_version"`
	Server          PeerInfo     `json:"server"`
	Capabilities    []Capability `json:"capabilities"`
}

type PingParams struct {
	Nonce string `json:"nonce,omitempty"`
}

type PingResult struct {
	Nonce  string `json:"nonce,omitempty"`
	Status string `json:"status"`
}

type MCPDiscoverParams struct {
	WorkspaceRoot string                        `json:"workspace_root,omitempty"`
	Servers       []protocolmcp.MCPServerConfig `json:"servers"`
}

type ReplyParams struct {
	RunID   string       `json:"run_id"`
	Session ReplySession `json:"session"`
	Input   ReplyInput   `json:"input"`
	Options ReplyOptions `json:"options"`
}

type ReplySession struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	WorkingDir   string    `json:"working_dir"`
	Conversation []Message `json:"conversation"`
}

type ReplyInput struct {
	Text string `json:"text"`
}

type ReplyOptions struct {
	ProviderProfileID string         `json:"provider_profile_id,omitempty"`
	ProviderName      string         `json:"provider_name,omitempty"`
	ProviderBaseURL   string         `json:"provider_base_url,omitempty"`
	ProviderAPIKey    string         `json:"provider_api_key,omitempty"`
	Model             string         `json:"model,omitempty"`
	PermissionMode    string         `json:"permission_mode,omitempty"`
	ToolPolicy        string         `json:"tool_policy,omitempty"`
	ToolAllowlist     []string       `json:"tool_allowlist,omitempty"`
	ToolDenylist      []string       `json:"tool_denylist,omitempty"`
	EmitToolEvents    bool           `json:"emit_tool_events"`
	RequirePermission bool           `json:"require_permission,omitempty"`
	MemoryContext     *MemoryContext `json:"memory_context,omitempty"`
	// TodoContext is the session checklist injected into provider messages.
	TodoContext *TodoContext `json:"todo_context,omitempty"`
	// GoalContext is the active/bound goal for long-horizon runs.
	GoalContext *GoalContext `json:"goal_context,omitempty"`
	// GoalsEnabled when false omits goal tools (nil/true = enabled).
	GoalsEnabled *bool `json:"goals_enabled,omitempty"`
	// GoalID binds this run to an existing goal (pending/paused → active).
	GoalID string `json:"goal_id,omitempty"`
	// ContinueGoal binds the latest paused (or pending) goal for the session.
	ContinueGoal bool `json:"continue_goal,omitempty"`
	// SkillsContext is refreshed on every conversation start so newly created
	// managed skills are immediately visible to the model.
	SkillsContext       *SkillsContext `json:"skills_context,omitempty"`
	WebSearchMaxResults int            `json:"web_search_max_results,omitempty"`
	WebFetchMaxBytes    int            `json:"web_fetch_max_bytes,omitempty"`
	// WebSearchProvider selects the web.search backend: auto | tavily | duckduckgo.
	// auto prefers Tavily when an API key is configured, otherwise DuckDuckGo.
	WebSearchProvider string `json:"web_search_provider,omitempty"`
	// WebTavilyAPIKey is the Tavily API key (tvly-...). Prefer settings over env.
	WebTavilyAPIKey string `json:"web_tavily_api_key,omitempty"`
	// WebHTTPProxy is an optional HTTP(S) proxy for web.search / web.fetch
	// (e.g. http://127.0.0.1:7890). Empty means use environment proxy settings.
	WebHTTPProxy string `json:"web_http_proxy,omitempty"`
	// MaxToolTurns limits provider↔tool loops per root reply. Zero means runtime default.
	MaxToolTurns int `json:"max_tool_turns,omitempty"`
	// LogLLMRequests writes each outbound provider request body to the local
	// diagnostic log directory (API keys are never written). Toggle from Desktop settings.
	LogLLMRequests bool `json:"log_llm_requests,omitempty"`
	// WorkerProfiles is the v0.2 execution-policy snapshot. Runtime keeps it
	// internally after decoding run.execute so delegated Assignments can apply
	// provider/model/tool policy without consulting Gateway again.
	WorkerProfiles []WorkerProfileRef `json:"worker_profiles,omitempty"`
	// DebugTools exposes ops-only tools (worker.pool_*, skill.create/update/delete)
	// to the provider. Can also be enabled via RED_PANDA_DEBUG_TOOLS=1.
	DebugTools bool `json:"debug_tools,omitempty"`
	// SpecialistContext is role/brief system text for delegated workers and skills.
	// It is NOT long-term memory — use MemoryContext for that.
	SpecialistContext *SpecialistContext `json:"specialist_context,omitempty"`
	// WorkerContext is trusted IPC metadata used by delegated Runtime processes.
	WorkerContext *WorkerExecutionContext `json:"worker_context,omitempty"`
	// MCPServers is the Gateway-enabled MCP config snapshot for this reply (docs/36 D2).
	// Runtime discovers tools and dispatches tools/call; Gateway never starts MCP processes.
	MCPServers []protocolmcp.MCPServerConfig `json:"mcp_servers,omitempty"`
}

// SpecialistContext carries ephemeral role instructions for delegated workers and skills.
type SpecialistContext struct {
	// Kind is a free-form label: specialist | worker | skill.
	Kind    string `json:"kind,omitempty"`
	Context string `json:"context,omitempty"`
}

type MemoryContext struct {
	Items   []MemoryItem `json:"items,omitempty"`
	Context string       `json:"context,omitempty"`
}

// SkillsContext is a catalog of managed workspace skills for one reply.
type SkillsContext struct {
	Items   []SkillSummary `json:"items,omitempty"`
	Context string         `json:"context,omitempty"`
}

type MemoryItem struct {
	ID      string `json:"id"`
	Scope   string `json:"scope"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Content string `json:"content,omitempty"`
}

type MemoryToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

type MemoryToolExecuteResult struct {
	Status   string           `json:"status"`
	Output   string           `json:"output,omitempty"`
	RecordID string           `json:"record_id,omitempty"`
	Items    []MemoryToolItem `json:"items,omitempty"`
}

type MemoryToolItem struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Title      string `json:"title"`
	Content    string `json:"content,omitempty"`
	Confidence string `json:"confidence,omitempty"`
}

// TodoContext is the formatted session task list for one reply turn.
type TodoContext struct {
	Items   []TodoItemDTO `json:"items,omitempty"`
	Context string        `json:"context,omitempty"`
}

// TodoItemDTO is the shared todo item shape for tools, HTTP, and events.
type TodoItemDTO struct {
	ID         string `json:"id"`
	ClientKey  string `json:"client_key,omitempty"`
	Content    string `json:"content"`
	Status     string `json:"status"`
	SortOrder  int    `json:"sort_order"`
	Priority   string `json:"priority,omitempty"`
	ActiveForm string `json:"active_form,omitempty"`
}

// TodoToolExecuteParams is Runtime -> Gateway for todo.write / todo.list.
type TodoToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// TodoToolExecuteResult is Gateway -> Runtime for todo tools.
type TodoToolExecuteResult struct {
	Status    string        `json:"status"`
	Output    string        `json:"output,omitempty"`
	Items     []TodoItemDTO `json:"items,omitempty"`
	OpenCount int           `json:"open_count,omitempty"`
}

// GoalContext is model-facing goal state for one reply turn.
type GoalContext struct {
	GoalID            string             `json:"goal_id"`
	Title             string             `json:"title,omitempty"`
	Objective         string             `json:"objective"`
	Status            string             `json:"status"`
	UsedToolTurns     int                `json:"used_tool_turns"`
	MaxTotalToolTurns int                `json:"max_total_tool_turns"`
	UsedSegments      int                `json:"used_segments"`
	MaxSegmentsPerRun int                `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int                `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int                `json:"used_wall_time_sec"`
	MaxWallTimeSec    int                `json:"max_wall_time_sec"`
	Context           string             `json:"context,omitempty"`
	Criteria          []GoalCriterionDTO `json:"criteria,omitempty"`
	Constraints       []string           `json:"constraints,omitempty"`
	Strategy          string             `json:"strategy,omitempty"`
	CurrentActionID   string             `json:"current_action_id,omitempty"`
	CurrentAction     string             `json:"current_action,omitempty"`
	Actions           []GoalActionDTO    `json:"actions,omitempty"`
	LastObservation   string             `json:"last_observation,omitempty"`
	LastAssessment    *GoalAssessmentDTO `json:"last_assessment,omitempty"`
	LastDecision      string             `json:"last_decision,omitempty"`
	Iteration         int                `json:"iteration"`
	MaxIterations     int                `json:"max_iterations"`
	StagnationCount   int                `json:"stagnation_count"`
	MaxStagnation     int                `json:"max_stagnation"`
}

// GoalCriterionDTO is one independently assessable condition in a Goal
// contract. Status is unknown|met|not_met|blocked.
type GoalCriterionDTO struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

// GoalActionDTO is a Goal-owned action in the controller's revisable queue.
type GoalActionDTO struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Acceptance  string `json:"acceptance,omitempty"`
	Status      string `json:"status"`
	Result      string `json:"result,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Attempt     int    `json:"attempt"`
	SortOrder   int    `json:"sort_order"`
}

// GoalAssessmentDTO is the persisted output of one feedback-control cycle.
type GoalAssessmentDTO struct {
	Verdict      string             `json:"verdict"` // progress|satisfied|blocked|no_progress
	Summary      string             `json:"summary"`
	Gap          string             `json:"gap,omitempty"`
	Decision     string             `json:"decision,omitempty"`
	Criteria     []GoalCriterionDTO `json:"criteria,omitempty"`
	ActionID     string             `json:"action_id,omitempty"`
	ActionStatus string             `json:"action_status,omitempty"`
	Evidence     string             `json:"evidence,omitempty"`
}

// GoalEventDTO is one append-only controller or lifecycle journal entry.
type GoalEventDTO struct {
	ID        string         `json:"id"`
	GoalID    string         `json:"goal_id"`
	RunID     string         `json:"run_id,omitempty"`
	Seq       int            `json:"seq"`
	Kind      string         `json:"kind"`
	Summary   string         `json:"summary"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt string         `json:"created_at"`
}

// GoalDTO is the shared goal shape for tools, HTTP, and events.
type GoalDTO struct {
	ID                string             `json:"id"`
	SessionID         string             `json:"session_id"`
	Title             string             `json:"title,omitempty"`
	Objective         string             `json:"objective"`
	Status            string             `json:"status"`
	PauseReason       string             `json:"pause_reason,omitempty"`
	FailReason        string             `json:"fail_reason,omitempty"`
	ReportMarkdown    string             `json:"report_markdown,omitempty"`
	UsedToolTurns     int                `json:"used_tool_turns"`
	MaxTotalToolTurns int                `json:"max_total_tool_turns"`
	UsedSegments      int                `json:"used_segments"`
	MaxSegmentsPerRun int                `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int                `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int                `json:"used_wall_time_sec"`
	MaxWallTimeSec    int                `json:"max_wall_time_sec"`
	ActiveRunID       string             `json:"active_run_id,omitempty"`
	LastRunID         string             `json:"last_run_id,omitempty"`
	CreatedAt         string             `json:"created_at,omitempty"`
	UpdatedAt         string             `json:"updated_at,omitempty"`
	Criteria          []GoalCriterionDTO `json:"criteria,omitempty"`
	Constraints       []string           `json:"constraints,omitempty"`
	Strategy          string             `json:"strategy,omitempty"`
	CurrentActionID   string             `json:"current_action_id,omitempty"`
	CurrentAction     string             `json:"current_action,omitempty"`
	Actions           []GoalActionDTO    `json:"actions,omitempty"`
	LastObservation   string             `json:"last_observation,omitempty"`
	LastAssessment    *GoalAssessmentDTO `json:"last_assessment,omitempty"`
	LastDecision      string             `json:"last_decision,omitempty"`
	OutcomeSummary    string             `json:"outcome_summary,omitempty"`
	Iteration         int                `json:"iteration"`
	MaxIterations     int                `json:"max_iterations"`
	StagnationCount   int                `json:"stagnation_count"`
	MaxStagnation     int                `json:"max_stagnation"`
	Version           int                `json:"version"`
}

// GoalToolExecuteParams is Runtime -> Gateway for goal.* tools.
type GoalToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// GoalToolExecuteResult is Gateway -> Runtime for goal tools.
type GoalToolExecuteResult struct {
	Status string    `json:"status"`
	Output string    `json:"output,omitempty"`
	Goal   *GoalDTO  `json:"goal,omitempty"`
	Goals  []GoalDTO `json:"goals,omitempty"`
	// CancelRunID is set when a goal tool terminalized an active Goal that had a
	// bound run. Gateway cancels that run after the tool response is returned
	// (async) so the tool RPC cannot deadlock against AgentCancel.
	CancelRunID string `json:"cancel_run_id,omitempty"`
}

// ContextToolExecuteParams is Runtime -> Gateway for context.* tools (goal scratchpad).
type ContextToolExecuteParams struct {
	RunID         string         `json:"run_id"`
	SessionID     string         `json:"session_id"`
	WorkspaceRoot string         `json:"workspace_root,omitempty"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	Arguments     map[string]any `json:"arguments,omitempty"`
}

// ContextToolExecuteResult is Gateway -> Runtime for context tools.
type ContextToolExecuteResult struct {
	Status string        `json:"status"`
	Output string        `json:"output,omitempty"`
	Notes  []GoalNoteDTO `json:"notes,omitempty"`
}

// GoalNoteDTO is the shared shape for a goal scratchpad note.
type GoalNoteDTO struct {
	ID        string `json:"id"`
	GoalID    string `json:"goal_id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Source    string `json:"source,omitempty"`
	RunID     string `json:"run_id,omitempty"`
	Seq       int    `json:"seq"`
	Pinned    int    `json:"pinned"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type Message struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   []ContentBlock `json:"content"`
	CreatedAt string         `json:"created_at,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ReplyAccepted struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"run_id"`
}

type CancelParams struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

// Managed skill discovery and management (workspace .codex/skills).

type SkillSummary struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	Path            string `json:"path"`
	HasInstructions bool   `json:"has_instructions"`
}

type SkillDetail struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions,omitempty"`
	Path         string `json:"path"`
	SizeBytes    int64  `json:"size_bytes"`
}

type SkillsListParams struct {
	WorkspaceRoot string `json:"workspace_root"`
}

type SkillsListResult struct {
	Items []SkillSummary `json:"items"`
}

type SkillLoadParams struct {
	WorkspaceRoot       string `json:"workspace_root"`
	Name                string `json:"name"`
	IncludeInstructions bool   `json:"include_instructions,omitempty"`
}

type SkillLoadResult struct {
	Skill SkillDetail `json:"skill"`
}

type SkillMutateParams struct {
	WorkspaceRoot string `json:"workspace_root"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Instructions  string `json:"instructions"`
}

type SkillMutateResult struct {
	Action string `json:"action"`
	Name   string `json:"name"`
	Path   string `json:"path"`
}

type SkillDeleteParams struct {
	WorkspaceRoot string `json:"workspace_root"`
	Name          string `json:"name"`
}

type SkillDeleteResult struct {
	Action  string `json:"action"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}
