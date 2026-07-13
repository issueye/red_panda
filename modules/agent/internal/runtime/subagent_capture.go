package runtime

import (
	"context"
	"fmt"
	"redpanda/agent/internal/subagent"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
)

// outcomes can be explained to the parent agent.
type subagentRunCapture struct {
	maxTurns  int
	backend   string
	name      string
	task      string
	fileCount int
	scopePath string

	message   strings.Builder
	reasoning strings.Builder

	FinishStatus  string
	FinishMessage string
	LastError     string

	MessageDeltas     int
	ReasoningDeltas   int
	ToolStarted       int
	ToolFinished      int
	ToolFailed        int
	RecoveredFallback bool

	startedTools []string
	failedTools  []string
	eventTypes   []string
}

func (c *subagentRunCapture) Observe(event events.Envelope) {
	if c == nil {
		return
	}
	if typ := string(event.Type); typ != "" && len(c.eventTypes) < 24 {
		if len(c.eventTypes) == 0 || c.eventTypes[len(c.eventTypes)-1] != typ {
			c.eventTypes = append(c.eventTypes, typ)
		}
	}

	switch event.Type {
	case events.EventMessageDelta:
		delta, _ := event.Payload["delta"].(string)
		if delta == "" {
			return
		}
		kind := events.StreamMessage
		if event.Stream != nil && event.Stream.Kind != "" {
			kind = event.Stream.Kind
		}
		// Child stdout is collected regardless of agent role; bridged events still
		// keep their subagent identity for the parent UI.
		if kind == events.StreamReasoning {
			c.ReasoningDeltas++
			if c.reasoning.Len() < 8*1024 {
				c.reasoning.WriteString(delta)
			}
			return
		}
		c.MessageDeltas++
		if recovered, _ := event.Payload["recovered"].(bool); recovered {
			c.RecoveredFallback = true
		}
		c.message.WriteString(delta)
	case events.EventMessage:
		if text, ok := event.Payload["text"].(string); ok && text != "" {
			c.MessageDeltas++
			c.message.WriteString(text)
		} else if text, ok := event.Payload["message"].(string); ok && text != "" {
			c.MessageDeltas++
			c.message.WriteString(text)
		}
	case events.EventReasoningDelta:
		if delta, ok := event.Payload["delta"].(string); ok && delta != "" {
			c.ReasoningDeltas++
			if c.reasoning.Len() < 8*1024 {
				c.reasoning.WriteString(delta)
			}
		}
	case events.EventError:
		if msg, ok := event.Payload["message"].(string); ok && msg != "" {
			c.LastError = msg
		} else if msg, ok := event.Payload["error"].(string); ok && msg != "" {
			c.LastError = msg
		}
		if status, ok := event.Payload["status"].(string); ok && status != "" && c.FinishStatus == "" {
			c.FinishStatus = status
		}
	case events.EventToolStarted:
		c.ToolStarted++
		name := firstPayloadString(event.Payload, "tool_name", firstPayloadString(event.Payload, "display_name", "tool"))
		if len(c.startedTools) < 12 {
			c.startedTools = append(c.startedTools, name)
		}
	case events.EventToolFinished:
		c.ToolFinished++
	case events.EventToolFailed:
		c.ToolFailed++
		name := firstPayloadString(event.Payload, "tool_name", "tool")
		errText := firstPayloadString(event.Payload, "error", firstPayloadString(event.Payload, "message", "failed"))
		if len(c.failedTools) < 8 {
			c.failedTools = append(c.failedTools, name+": "+errText)
		}
		if c.LastError == "" {
			c.LastError = name + ": " + errText
		}
	case events.EventFinish:
		if status, ok := event.Payload["status"].(string); ok {
			c.FinishStatus = status
		}
		if msg, ok := event.Payload["message"].(string); ok && msg != "" {
			c.FinishMessage = msg
		}
		if msg, ok := event.Payload["error"].(string); ok && msg != "" && c.LastError == "" {
			c.LastError = msg
		}
		// Some providers only put the final answer on finish.
		if text, ok := event.Payload["text"].(string); ok && text != "" && c.message.Len() == 0 {
			c.message.WriteString(text)
		}
	}
}

func (c *subagentRunCapture) FinalText() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.message.String())
}

func (c *subagentRunCapture) FailureError(headline string) error {
	if c == nil {
		return fmt.Errorf("%s", headline)
	}
	parts := []string{headline}
	parts = append(parts, fmt.Sprintf("subagent=%s", c.name))
	parts = append(parts, fmt.Sprintf("backend=%s", c.backend))
	parts = append(parts, fmt.Sprintf("max_turns=%d", c.maxTurns))
	if c.fileCount > 0 {
		parts = append(parts, fmt.Sprintf("file_count=%d", c.fileCount))
		parts = append(parts, fmt.Sprintf("turns_formula=file_count(%d)+summary(%d)", c.fileCount, subagent.SummaryTurns))
	}
	if c.scopePath != "" {
		parts = append(parts, "path="+c.scopePath)
	}
	if c.FinishStatus != "" {
		parts = append(parts, fmt.Sprintf("finish_status=%s", c.FinishStatus))
	}
	parts = append(parts, fmt.Sprintf(
		"stats{message_deltas=%d reasoning_deltas=%d tools_started=%d tools_finished=%d tools_failed=%d}",
		c.MessageDeltas, c.ReasoningDeltas, c.ToolStarted, c.ToolFinished, c.ToolFailed,
	))
	if c.LastError != "" {
		parts = append(parts, "last_error="+c.LastError)
	}
	if c.FinishMessage != "" && c.FinishMessage != c.LastError {
		parts = append(parts, "finish_message="+c.FinishMessage)
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
	if c.task != "" {
		parts = append(parts, "task="+subagent.TruncateSummary(c.task, 120))
	}
	// Reasoning can help the parent understand silent failures, but keep it short.
	if reasoning := strings.TrimSpace(c.reasoning.String()); reasoning != "" && c.FinalText() == "" {
		parts = append(parts, "reasoning_preview="+subagent.TruncateSummary(reasoning, 240))
	}
	return fmt.Errorf("%s", strings.Join(parts, " | "))
}

func (c *subagentRunCapture) recoveryHint() string {
	if c.LastError != "" && strings.Contains(strings.ToLower(c.LastError), "tool turn limit") {
		return "re-run workspace.stats on the scope path and set max_turns=file_count+summary (or pass file_count/path), then retry; do not use a small fixed budget"
	}
	if c.ToolFailed > 0 && c.MessageDeltas == 0 {
		return "subagent hit tool failures without writing a final report; reset it, fix the failing path/tool, and retry with a narrower task"
	}
	if c.ToolStarted > 0 && c.MessageDeltas == 0 && (c.FinishStatus == "" || c.FinishStatus == "completed") {
		return "subagent used tools but emitted no final text; retry with an explicit instruction to write a final summary after tools"
	}
	if c.MessageDeltas == 0 && c.ToolStarted == 0 {
		return "subagent produced no tools and no text; check provider/model config and pool workers (subagent.pool_reset), then retry"
	}
	if c.FinishStatus == "failed" || c.FinishStatus == "denied" || c.FinishStatus == "cancelled" {
		return "inspect last_error/failed_tools, then subagent.reset and rerun with adjusted scope or max_turns"
	}
	return "retry with a narrower task, higher max_turns, or after subagent.pool_reset"
}

func (r *Runtime) failWorkerSubAgent(params methods.ReplyParams, subAgentID string, agentName string, backend string, err error) {
	r.finishSubAgent(params.RunID, subAgentID, "failed", "subagent failed", err.Error())
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"status":      "failed",
		"summary":     "subagent failed",
		"backend":     backend,
		"error":       err.Error(),
	})
}
