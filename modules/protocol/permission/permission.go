package permission

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionDeny    Decision = "deny"
)

type Mode string

const (
	ModeStrict     Mode = "strict"
	ModePermissive Mode = "permissive"
	ModeAllowAll   Mode = "allow_all"
	ModeDenyAll    Mode = "deny_all"
)

type ResolveParams struct {
	PermissionID string   `json:"permission_id"`
	RunID        string   `json:"run_id,omitempty"`
	Decision     Decision `json:"decision"`
	Scope        string   `json:"scope,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

type ResolveResult struct {
	Accepted bool `json:"accepted"`
}

type RequestPayload struct {
	PermissionID string         `json:"permission_id"`
	RunID        string         `json:"run_id"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	Risk         string         `json:"risk,omitempty"`
	Summary      string         `json:"summary"`
	Detail       string         `json:"detail,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
}
