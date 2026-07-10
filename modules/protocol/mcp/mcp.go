package mcp

const (
	DefaultStartTimeoutMS      = 10000
	DefaultInitializeTimeoutMS = 10000
	DefaultListTimeoutMS       = 10000
	DefaultCallTimeoutMS       = 30000
	DefaultShutdownTimeoutMS   = 3000
)

// MCPServerConfig is the durable configuration passed from Gateway to Runtime.
type MCPServerConfig struct {
	Name          string            `json:"name"`
	Command       string            `json:"command"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	CWD           string            `json:"cwd,omitempty"`
	Enabled       bool              `json:"enabled"`
	Timeouts      MCPTimeouts       `json:"timeouts,omitempty"`
	ToolAllowlist []string          `json:"tool_allowlist,omitempty"`
	RiskOverrides map[string]string `json:"risk_overrides,omitempty"`
}

type MCPTimeouts struct {
	StartMS      int `json:"start_ms,omitempty"`
	InitializeMS int `json:"initialize_ms,omitempty"`
	ListMS       int `json:"list_ms,omitempty"`
	CallMS       int `json:"call_ms,omitempty"`
	ShutdownMS   int `json:"shutdown_ms,omitempty"`
}

// Normalized fills omitted timeout values while preserving provided values for
// the validation layer to accept or reject.
func (t MCPTimeouts) Normalized() MCPTimeouts {
	if t.StartMS == 0 {
		t.StartMS = DefaultStartTimeoutMS
	}
	if t.InitializeMS == 0 {
		t.InitializeMS = DefaultInitializeTimeoutMS
	}
	if t.ListMS == 0 {
		t.ListMS = DefaultListTimeoutMS
	}
	if t.CallMS == 0 {
		t.CallMS = DefaultCallTimeoutMS
	}
	if t.ShutdownMS == 0 {
		t.ShutdownMS = DefaultShutdownTimeoutMS
	}
	return t
}

func DefaultTimeouts() MCPTimeouts {
	return MCPTimeouts{}.Normalized()
}

type MCPServerCreateRequest struct {
	MCPServerConfig
}

type MCPServerUpdateRequest struct {
	MCPServerConfig
}

type MCPServerResponse struct {
	ID string `json:"id"`
	MCPServerConfig
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type MCPServerListResponse struct {
	Servers []MCPServerResponse `json:"servers"`
}

type MCPServerDeleteResponse struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}
