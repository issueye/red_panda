package worker

import (
	"fmt"
	"strings"

	"redpanda/protocol/events"
)

const maxReasoningPreviewBytes = 8 * 1024

type CaptureOptions struct {
	MaxTurns  int
	Backend   string
	Name      string
	Task      string
	FileCount int
	ScopePath string
}

type Capture struct {
	options CaptureOptions

	currentMessage strings.Builder
	finalMessage   string
	reasoning      strings.Builder

	finishStatus      string
	finishMessage     string
	lastError         string
	recoveredFallback bool
	stats             ExecutionStats

	messageDeltas   int
	reasoningDeltas int
	toolStarted     int
	toolFinished    int
	toolFailed      int

	startedTools []string
	failedTools  []string
	eventTypes   []string
}

func NewCapture(options CaptureOptions) *Capture {
	return &Capture{options: options}
}

func (c *Capture) Observe(event events.Envelope) {
	c.observe(event.Type, event.Stream, event.Payload)
}

func (c *Capture) ObserveV2(event events.EnvelopeV2) {
	c.observe(event.Type, event.Stream, event.Payload)
}

func (c *Capture) observe(typ events.EventType, stream *events.StreamRef, payload map[string]any) {
	if c == nil {
		return
	}
	if eventType := string(typ); eventType != "" && len(c.eventTypes) < 24 {
		if len(c.eventTypes) == 0 || c.eventTypes[len(c.eventTypes)-1] != eventType {
			c.eventTypes = append(c.eventTypes, eventType)
		}
	}

	switch typ {
	case events.EventMessageDelta:
		delta, _ := payload["delta"].(string)
		kind := events.StreamMessage
		if stream != nil && stream.Kind != "" {
			kind = stream.Kind
		}
		if kind == events.StreamReasoning {
			if delta == "" {
				return
			}
			c.reasoningDeltas++
			c.appendReasoning(delta)
			return
		}
		if delta != "" {
			c.messageDeltas++
		}
		// A successful text-only final-answer retry is still a usable report.
		// Only deterministic/legacy recovery placeholders are fallbacks. Treat an
		// unknown kind conservatively as a fallback for compatibility with events
		// emitted before recovery_kind was introduced.
		if recovered, _ := payload["recovered"].(bool); recovered &&
			payloadString(payload, "recovery_kind", "fallback") != "final_answer_retry" {
			c.recoveredFallback = true
		}
		c.currentMessage.WriteString(delta)
		if stream != nil && stream.Final {
			c.finalMessage = strings.TrimSpace(c.currentMessage.String())
		}
	case events.EventMessage:
		if text := payloadString(payload, "text", ""); text != "" {
			c.messageDeltas++
			c.currentMessage.WriteString(text)
			c.finalMessage = strings.TrimSpace(c.currentMessage.String())
		} else if message := payloadString(payload, "message", ""); message != "" {
			c.messageDeltas++
			c.currentMessage.WriteString(message)
			c.finalMessage = strings.TrimSpace(c.currentMessage.String())
		}
	case events.EventReasoningDelta:
		if delta := payloadString(payload, "delta", ""); delta != "" {
			c.reasoningDeltas++
			c.appendReasoning(delta)
		}
	case events.EventError:
		c.lastError = payloadString(payload, "message", payloadString(payload, "error", c.lastError))
		if status := payloadString(payload, "status", ""); status != "" && c.finishStatus == "" {
			c.finishStatus = status
		}
	case events.EventToolStarted:
		c.toolStarted++
		// Text before a tool call is progress narration, not the Worker report.
		c.currentMessage.Reset()
		c.finalMessage = ""
		name := payloadString(payload, "tool_name", payloadString(payload, "display_name", "tool"))
		if len(c.startedTools) < 12 {
			c.startedTools = append(c.startedTools, name)
		}
	case events.EventToolFinished:
		c.toolFinished++
	case events.EventToolFailed:
		c.toolFailed++
		c.currentMessage.Reset()
		c.finalMessage = ""
		name := payloadString(payload, "tool_name", "tool")
		errText := payloadString(payload, "error", payloadString(payload, "message", "failed"))
		if len(c.failedTools) < 8 {
			c.failedTools = append(c.failedTools, name+": "+errText)
		}
		if c.lastError == "" {
			c.lastError = name + ": " + errText
		}
	case events.EventFinish:
		c.finishStatus = payloadString(payload, "status", c.finishStatus)
		c.finishMessage = payloadString(payload, "message", c.finishMessage)
		if c.lastError == "" {
			c.lastError = payloadString(payload, "error", "")
		}
		c.stats = executionStatsFromPayload(payload, c.options.MaxTurns)
		if text := payloadString(payload, "text", ""); text != "" && c.finalMessage == "" {
			c.finalMessage = strings.TrimSpace(text)
		} else if c.finalMessage == "" {
			c.finalMessage = strings.TrimSpace(c.currentMessage.String())
		}
	}
}

func (c *Capture) FinalText() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.finalMessage)
}

func (c *Capture) Stats() ExecutionStats {
	if c == nil {
		return ExecutionStats{}
	}
	return c.stats
}

func (c *Capture) FinishStatus() string {
	if c == nil {
		return ""
	}
	return c.finishStatus
}

func executionStatsFromPayload(payload map[string]any, fallbackMaxTurns int) ExecutionStats {
	return ExecutionStats{
		MaxTurns:           payloadInt(payload, "max_turns", fallbackMaxTurns),
		LoopTurns:          payloadInt(payload, "loop_turns", payloadInt(payload, "tool_turns", 0)),
		ProviderRequests:   payloadInt(payload, "provider_requests", 0),
		ToolCallsRequested: payloadInt(payload, "tool_calls_requested", 0),
		ToolCallsExecuted:  payloadInt(payload, "tool_calls_executed", 0),
		ToolCallBudget:     payloadInt(payload, "tool_call_budget", 0),
		MaxTurnsReached:    payloadBool(payload, "max_turns_reached"),
		ToolBudgetReached:  payloadBool(payload, "tool_budget_reached"),
	}
}

func payloadInt(payload map[string]any, key string, fallback int) int {
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func payloadBool(payload map[string]any, key string) bool {
	value, _ := payload[key].(bool)
	return value
}

func (c *Capture) RecoveredFallback() bool {
	return c != nil && c.recoveredFallback
}

func (c *Capture) FailureError(headline string) error {
	if c == nil {
		return fmt.Errorf("%s", headline)
	}
	parts := []string{
		headline,
		fmt.Sprintf("worker_profile=%s", c.options.Name),
		fmt.Sprintf("backend=%s", c.options.Backend),
		fmt.Sprintf("max_turns=%d", c.options.MaxTurns),
	}
	if c.options.FileCount > 0 {
		parts = append(parts,
			fmt.Sprintf("file_count=%d", c.options.FileCount),
			fmt.Sprintf("turns_formula=file_count(%d)+summary(%d)", c.options.FileCount, SummaryTurns),
		)
	}
	if c.options.ScopePath != "" {
		parts = append(parts, "path="+c.options.ScopePath)
	}
	if c.finishStatus != "" {
		parts = append(parts, "finish_status="+c.finishStatus)
	}
	parts = append(parts, fmt.Sprintf(
		"stats{message_deltas=%d reasoning_deltas=%d tools_started=%d tools_finished=%d tools_failed=%d}",
		c.messageDeltas, c.reasoningDeltas, c.toolStarted, c.toolFinished, c.toolFailed,
	))
	if c.lastError != "" {
		parts = append(parts, "last_error="+c.lastError)
	}
	if c.finishMessage != "" && c.finishMessage != c.lastError {
		parts = append(parts, "finish_message="+c.finishMessage)
	}
	if len(c.failedTools) > 0 {
		parts = append(parts, "failed_tools=["+strings.Join(c.failedTools, "; ")+"]")
	} else if len(c.startedTools) > 0 {
		parts = append(parts, "tools=["+strings.Join(c.startedTools, ", ")+"]")
	}
	if len(c.eventTypes) > 0 {
		parts = append(parts, "events=["+strings.Join(c.eventTypes, " > ")+"]")
	}
	if hint := c.recoveryHint(); hint != "" {
		parts = append(parts, "hint="+hint)
	}
	if c.options.Task != "" {
		parts = append(parts, "task="+TruncateSummary(c.options.Task, 120))
	}
	if reasoning := strings.TrimSpace(c.reasoning.String()); reasoning != "" && c.FinalText() == "" {
		parts = append(parts, "reasoning_preview="+TruncateSummary(reasoning, 240))
	}
	return fmt.Errorf("%s", strings.Join(parts, " | "))
}

func (c *Capture) appendReasoning(delta string) {
	remaining := maxReasoningPreviewBytes - c.reasoning.Len()
	if remaining <= 0 {
		return
	}
	if len(delta) > remaining {
		delta = delta[:remaining]
	}
	c.reasoning.WriteString(delta)
}

func (c *Capture) recoveryHint() string {
	if c.lastError != "" && strings.Contains(strings.ToLower(c.lastError), "tool turn limit") {
		return "re-run workspace.stats on the scope path and set max_turns=file_count+summary (or pass file_count/path), then retry; do not use a small fixed budget"
	}
	if c.toolFailed > 0 && c.messageDeltas == 0 {
		return "worker hit tool failures without writing a final report; retry with a narrower task after fixing the failing tool"
	}
	if c.toolStarted > 0 && c.messageDeltas == 0 && (c.finishStatus == "" || c.finishStatus == "completed") {
		return "worker used tools but emitted no final text; retry with an explicit instruction to write a final summary after tools"
	}
	if c.messageDeltas == 0 && c.toolStarted == 0 {
		return "worker produced no tools and no text; check provider/model configuration and WorkerPool health, then retry"
	}
	if c.finishStatus == "failed" || c.finishStatus == "denied" || c.finishStatus == "cancelled" {
		return "inspect last_error/failed_tools and submit a new assignment with adjusted scope or max_turns"
	}
	return "retry with a narrower task or higher max_turns"
}

func payloadString(payload map[string]any, key string, fallback string) string {
	if value, ok := payload[key].(string); ok && value != "" {
		return value
	}
	return fallback
}
