package methods

import (
	"encoding/json"

	protocolmcp "redpanda/protocol/mcp"
)

const (
	// EnvSkillsDir is the Gateway-owned, read-only built-in skill directory
	// shared with Agent processes.
	EnvSkillsDir = "RED_PANDA_SKILLS_DIR"

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
	MCPCall           = "mcp.call"
	PermissionResolve = "permission.resolve"

	// ScheduleToolExecute is also defined in schedule.go for discoverability.
	// StateToolExecute is the sole Runtime→Gateway state-tool RPC (docs/47 E-cutover).
	// Removed methods: memory/todo/goal/context.tool.execute.
	StateToolExecute = "state.tool.execute"

	// State tool domains for StateToolExecuteParams.Domain.
	StateToolDomainMemory = "memory"
	StateToolDomainTodo   = "todo"
	// StateToolDomainSchedule is also defined in schedule.go.

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

// RunExecuteParams starts one top-level Run under the v0.2 Worker model.
type RunExecuteParams struct {
	RunID   string            `json:"run_id"`
	Session ReplySession      `json:"session"`
	Input   ReplyInput        `json:"input"`
	Options RunExecuteOptions `json:"options"`
}

type RunExecuteOptions struct {
	ProviderProfileID string `json:"provider_profile_id,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`
	ProviderBaseURL   string `json:"provider_base_url,omitempty"`
	ProviderAPIKey    string `json:"provider_api_key,omitempty"`
	// ProviderHTTPProxy is an optional outbound HTTP(S)/SOCKS5 proxy applied to
	// this profile's provider requests (distinct from WebHTTPProxy which only
	// governs web.search/web.fetch). Empty means use environment proxy.
	ProviderHTTPProxy   string                        `json:"provider_http_proxy,omitempty"`
	ProviderStream      *bool                         `json:"provider_stream,omitempty"`
	Model               string                        `json:"model,omitempty"`
	EnableThinking      bool                          `json:"enable_thinking,omitempty"`
	ReasoningEffort     string                        `json:"reasoning_effort,omitempty"`
	ProviderCacheMode   string                        `json:"provider_cache_mode,omitempty"`
	ProviderCacheKey    bool                          `json:"provider_cache_key_supported,omitempty"`
	ProviderCacheRetain string                        `json:"provider_cache_retention,omitempty"`
	ProviderMinCache    int                           `json:"provider_min_cache_tokens,omitempty"`
	PermissionMode      string                        `json:"permission_mode,omitempty"`
	ToolPolicy          string                        `json:"tool_policy,omitempty"`
	ToolAllowlist       []string                      `json:"tool_allowlist,omitempty"`
	ToolDenylist        []string                      `json:"tool_denylist,omitempty"`
	EmitToolEvents      bool                          `json:"emit_tool_events"`
	RequirePermission   bool                          `json:"require_permission,omitempty"`
	MemoryContext       *MemoryContext                `json:"memory_context,omitempty"`
	TodoContext         *TodoContext                  `json:"todo_context,omitempty"`
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
	// SupportsVision reflects whether the resolved provider profile can accept
	// image attachments (docs/51 §6.5). Gateway sets this; Runtime uses it to
	// decide whether to map image_ref blocks to provider multimodal parts.
	SupportsVision bool `json:"supports_vision,omitempty"`
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

// MCPCallParams is a management-path tools/call (Settings try-call).
// It does not require a run binding; Runtime uses Manager.CallTool directly.
type MCPCallParams struct {
	WorkspaceRoot string                      `json:"workspace_root,omitempty"`
	Server        protocolmcp.MCPServerConfig `json:"server"`
	ToolName      string                      `json:"tool_name"`
	Arguments     map[string]any              `json:"arguments,omitempty"`
}

// MCPCallResult is the outcome of a management-path MCP tools/call.
type MCPCallResult struct {
	Output        string `json:"output,omitempty"`
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
	DurationMS    int64  `json:"duration_ms,omitempty"`
	StderrSummary string `json:"stderr_summary,omitempty"`
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
	Text        string            `json:"text"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
}

// InputAttachment is the per-run attachment reference (docs/51 §5.3). The
// Desktop→Gateway wire only carries AttachmentID or Path. The Gateway→Runtime
// wire may additionally inline MIME/DataB64 (ephemeral, never persisted).
type InputAttachment struct {
	AttachmentID string `json:"attachment_id,omitempty"`
	Path         string `json:"path,omitempty"`
	MIME         string `json:"mime,omitempty"`
	DataB64      string `json:"data_b64,omitempty"` // Gateway→Runtime only
	ByteSize     int64  `json:"byte_size,omitempty"`
}

type ReplyOptions struct {
	ProviderProfileID string `json:"provider_profile_id,omitempty"`
	ProviderName      string `json:"provider_name,omitempty"`
	ProviderBaseURL   string `json:"provider_base_url,omitempty"`
	ProviderAPIKey    string `json:"provider_api_key,omitempty"`
	// ProviderHTTPProxy mirrors RunExecuteOptions.ProviderHTTPProxy: an optional
	// HTTP(S)/SOCKS5 proxy for provider requests. Distinct from WebHTTPProxy.
	ProviderHTTPProxy   string         `json:"provider_http_proxy,omitempty"`
	ProviderStream      *bool          `json:"provider_stream,omitempty"`
	Model               string         `json:"model,omitempty"`
	EnableThinking      bool           `json:"enable_thinking,omitempty"`
	ReasoningEffort     string         `json:"reasoning_effort,omitempty"`
	ProviderCacheMode   string         `json:"provider_cache_mode,omitempty"`
	ProviderCacheKey    bool           `json:"provider_cache_key_supported,omitempty"`
	ProviderCacheRetain string         `json:"provider_cache_retention,omitempty"`
	ProviderMinCache    int            `json:"provider_min_cache_tokens,omitempty"`
	PermissionMode      string         `json:"permission_mode,omitempty"`
	ToolPolicy          string         `json:"tool_policy,omitempty"`
	ToolAllowlist       []string       `json:"tool_allowlist,omitempty"`
	ToolDenylist        []string       `json:"tool_denylist,omitempty"`
	EmitToolEvents      bool           `json:"emit_tool_events"`
	RequirePermission   bool           `json:"require_permission,omitempty"`
	MemoryContext       *MemoryContext `json:"memory_context,omitempty"`
	// TodoContext is the session checklist injected into provider messages.
	TodoContext *TodoContext `json:"todo_context,omitempty"`
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

type Message struct {
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   []ContentBlock `json:"content"`
	CreatedAt string         `json:"created_at,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	// image_ref fields (when Type == "image_ref", docs/51 §5.2). Exactly one of
	// AttachmentID or Path must be set. These are reference-only; base64/data
	// is never persisted in a message block.
	AttachmentID string `json:"attachment_id,omitempty"`
	Path         string `json:"path,omitempty"` // workspace-relative (current session workspace)
	MIME         string `json:"mime,omitempty"`
	Alt          string `json:"alt,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	ByteSize     int64  `json:"byte_size,omitempty"`
}

type ReplyAccepted struct {
	Accepted bool   `json:"accepted"`
	RunID    string `json:"run_id"`
}

type CancelParams struct {
	RunID  string `json:"run_id"`
	Reason string `json:"reason,omitempty"`
}

// Managed skill discovery and management (Gateway skills plus workspace .codex/skills).

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
