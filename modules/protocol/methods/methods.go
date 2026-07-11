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
	WebSearchMaxResults int           `json:"web_search_max_results,omitempty"`
	WebFetchMaxBytes    int           `json:"web_fetch_max_bytes,omitempty"`
}

type MemoryContext struct {
	Items   []MemoryItem `json:"items,omitempty"`
	Context string       `json:"context,omitempty"`
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
	WorkspaceRoot        string `json:"workspace_root"`
	Name                 string `json:"name"`
	IncludeInstructions  bool   `json:"include_instructions,omitempty"`
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
