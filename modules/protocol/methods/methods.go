package methods

import protocolmcp "redpanda/protocol/mcp"

const (
	CoreInitialize      = "core.initialize"
	CorePing            = "core.ping"
	CoreShutdown        = "core.shutdown"
	AgentTools          = "agent.tools"
	AgentReply          = "agent.reply"
	AgentCancel         = "agent.cancel"
	AgentSubAgents      = "agent.subagents"
	AgentSubAgentCancel = "agent.subagent.cancel"
	AgentSkills         = "agent.skills"
	AgentSkillLoad      = "agent.skill.load"
	AgentSkillCreate    = "agent.skill.create"
	AgentSkillUpdate    = "agent.skill.update"
	AgentSkillDelete    = "agent.skill.delete"
	MCPDiscover         = "mcp.discover"
	PermissionResolve   = "permission.resolve"
	AgentEvent          = "agent.event"
	MemoryToolExecute   = "memory.tool.execute"
	TodoToolExecute     = "todo.tool.execute"
	GoalToolExecute     = "goal.tool.execute"
)

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
	SpawnSubAgents    bool           `json:"spawn_subagents,omitempty"`
	SubAgentBackend   string         `json:"subagent_backend,omitempty"`
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
	GoalID            string `json:"goal_id"`
	Title             string `json:"title,omitempty"`
	Objective         string `json:"objective"`
	SuccessCriteria   string `json:"success_criteria,omitempty"`
	Status            string `json:"status"`
	PipelinePhase     string `json:"pipeline_phase,omitempty"`
	AnalysisSummary   string `json:"analysis_summary,omitempty"`
	CheckpointSummary string `json:"checkpoint_summary,omitempty"`
	ProgressNote      string `json:"progress_note,omitempty"`
	CurrentStep       string `json:"current_step,omitempty"`
	UsedToolTurns     int    `json:"used_tool_turns"`
	MaxTotalToolTurns int    `json:"max_total_tool_turns"`
	UsedSegments      int    `json:"used_segments"`
	MaxSegmentsPerRun int    `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int    `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int    `json:"used_wall_time_sec"`
	MaxWallTimeSec    int    `json:"max_wall_time_sec"`
	Context           string `json:"context,omitempty"`
}

// GoalDTO is the shared goal shape for tools, HTTP, and events.
type GoalDTO struct {
	ID                string `json:"id"`
	SessionID         string `json:"session_id"`
	Title             string `json:"title,omitempty"`
	Objective         string `json:"objective"`
	SuccessCriteria   string `json:"success_criteria,omitempty"`
	Status            string `json:"status"`
	PipelinePhase     string `json:"pipeline_phase,omitempty"`
	PauseReason       string `json:"pause_reason,omitempty"`
	FailReason        string `json:"fail_reason,omitempty"`
	AnalysisSummary   string `json:"analysis_summary,omitempty"`
	CheckpointSummary string `json:"checkpoint_summary,omitempty"`
	ProgressNote      string `json:"progress_note,omitempty"`
	ReportMarkdown    string `json:"report_markdown,omitempty"`
	UsedToolTurns     int    `json:"used_tool_turns"`
	MaxTotalToolTurns int    `json:"max_total_tool_turns"`
	UsedSegments      int    `json:"used_segments"`
	MaxSegmentsPerRun int    `json:"max_segments_per_run"`
	MaxToolTurnsSeg   int    `json:"max_tool_turns_per_segment"`
	UsedWallTimeSec   int    `json:"used_wall_time_sec"`
	MaxWallTimeSec    int    `json:"max_wall_time_sec"`
	ActiveRunID       string `json:"active_run_id,omitempty"`
	LastRunID         string `json:"last_run_id,omitempty"`
	CreatedAt         string `json:"created_at,omitempty"`
	UpdatedAt         string `json:"updated_at,omitempty"`
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

type SubAgentsParams struct {
	RunID      string `json:"run_id,omitempty"`
	SubAgentID string `json:"subagent_id,omitempty"`
}

type SubAgentRecord struct {
	SubAgentID      string `json:"subagent_id"`
	Name            string `json:"name"`
	Backend         string `json:"backend"`
	Status          string `json:"status"`
	RootRunID       string `json:"root_run_id"`
	ParentRunID     string `json:"parent_run_id,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
	ChildRunID      string `json:"child_run_id,omitempty"`
	Summary         string `json:"summary,omitempty"`
	Error           string `json:"error,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type SubAgentsResult struct {
	Items []SubAgentRecord `json:"items"`
}

type SubAgentCancelParams struct {
	RunID      string `json:"run_id"`
	SubAgentID string `json:"subagent_id"`
	Reason     string `json:"reason,omitempty"`
}

type SubAgentCancelResult struct {
	Accepted   bool   `json:"accepted"`
	RunID      string `json:"run_id"`
	SubAgentID string `json:"subagent_id"`
	Cancelled  bool   `json:"cancelled"`
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
