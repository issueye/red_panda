package tools

type Risk string

const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

type CallStatus string

const (
	CallStatusPending   CallStatus = "pending"
	CallStatusRunning   CallStatus = "running"
	CallStatusCompleted CallStatus = "completed"
	CallStatusFailed    CallStatus = "failed"
	CallStatusDenied    CallStatus = "denied"
)

type Call struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Risk        Risk           `json:"risk"`
	Arguments   map[string]any `json:"arguments,omitempty"`
}

type Definition struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name,omitempty"`
	Description string         `json:"description,omitempty"`
	Risk        Risk           `json:"risk"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type Result struct {
	ToolCallID string     `json:"tool_call_id"`
	Name       string     `json:"name"`
	Status     CallStatus `json:"status"`
	ExitCode   int        `json:"exit_code,omitempty"`
	Output     string     `json:"output,omitempty"`
	Error      string     `json:"error,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
}
