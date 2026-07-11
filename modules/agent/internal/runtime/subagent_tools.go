package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

const (
	// Turns reserved for reasoning + writing the final analysis report after file work.
	subagentSummaryTurns = 8
	// Fallback only when neither max_turns nor file_count/path is provided.
	defaultSubagentToolTurns = 16
)

var subagentRunDenylist = []string{
	"subagent.run",
	"subagent.list",
	"subagent.cancel",
	"subagent.reset",
	"subagent.pool_status",
	"subagent.pool_resize",
	"subagent.pool_reset",
	"skill.run",
}

var subagentNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// recommendedSubagentTurns is file_count + summary/analysis allowance.
// There is no artificial maximum; budget scales with the directory size.
func recommendedSubagentTurns(fileCount int) int {
	if fileCount < 1 {
		fileCount = 1
	}
	return fileCount + subagentSummaryTurns
}

// effectiveSubagentToolTurns picks the specialist budget.
// Priority: explicit max_turns > file_count formula > default.
// No hard maximum cap: turns scale with files being analyzed.
func effectiveSubagentToolTurns(explicit int, fileCount int) int {
	if explicit > 0 {
		return explicit
	}
	if fileCount > 0 {
		return recommendedSubagentTurns(fileCount)
	}
	return defaultSubagentToolTurns
}

func (r *Runtime) executeSubagentRun(ctx context.Context, runCtx ToolRunContext, call tools.Call) (string, error) {
	if runCtx.Reply == nil {
		return "", fmt.Errorf("subagent requires reply context")
	}
	task := strings.TrimSpace(stringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("subagent task is required")
	}
	name := strings.TrimSpace(stringArg(call.Arguments, "name"))
	if name == "" {
		name = "worker"
	}
	name = sanitizeSubagentName(name)

	// Budget = file_count + summary turns. Parent should pass file_count (from workspace.stats)
	// or path (auto-counted). Explicit max_turns still wins when provided.
	fileCount := intArg(call.Arguments, "file_count", 0)
	scopePath := strings.TrimSpace(stringArg(call.Arguments, "path"))
	if fileCount <= 0 && scopePath != "" && runCtx.WorkingDir != "" {
		if stats, err := computeWorkspaceStats(runCtx.WorkingDir, scopePath, 4); err == nil {
			fileCount = stats.TotalFiles
		}
	}
	explicitTurns := intArg(call.Arguments, "max_turns", 0)
	maxTurns := effectiveSubagentToolTurns(explicitTurns, fileCount)

	params := *runCtx.Reply
	subAgentID := fmt.Sprintf("worker_%s_%d", name, time.Now().UnixNano())
	agentName := name
	childRunID := params.RunID + ":subagent:" + subAgentID
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Specialist workers always use the process pool so workers can be reused and reset.
	backend := "process_pool"
	if requested := normalizedSubAgentBackend(params.Options.SubAgentBackend); requested == "runtime_process" {
		// Explicit one-shot process is still allowed when parent opts out of pooling.
		backend = "runtime_process"
	}

	r.registerSubAgent(params, subAgentID, agentName, backend, cancel)
	_ = r.emitAgentEvent(ctx, params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"status":      "running",
		"summary":     "subagent started: " + truncateSummary(task, 80),
		"backend":     backend,
		"task":        task,
		"file_count":  fileCount,
		"max_turns":   maxTurns,
		"path":        scopePath,
	})

	child, release, err := r.acquireProcessSubAgent(childCtx, params, subAgentID, backend)
	if err != nil {
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, err)
		return "", err
	}
	reusable := false
	defer func() {
		if release != nil {
			release(reusable)
		}
	}()

	childTask := task
	if scopePath != "" && !strings.Contains(strings.ToLower(task), strings.ToLower(scopePath)) {
		childTask = fmt.Sprintf("Scope path: %s\nFile count budget: %d files => max_turns=%d (files + %d summary turns).\n\n%s",
			scopePath, fileCount, maxTurns, subagentSummaryTurns, task)
	} else if fileCount > 0 {
		childTask = fmt.Sprintf("File count budget: %d files => max_turns=%d (files + %d summary turns).\n\n%s",
			fileCount, maxTurns, subagentSummaryTurns, task)
	}

	childParams := params
	childParams.RunID = childRunID
	childParams.Session.Conversation = nil
	childParams.Input.Text = childTask
	childParams.Options.MemoryContext = &methods.MemoryContext{
		Context: fmt.Sprintf(
			"You are a focused subagent named %q. Complete only the assigned task using workspace tools as needed. "+
				"Your tool-turn budget is %d (derived from file_count=%d plus %d turns for analysis summary). "+
				"Return a clear final report for the parent agent. Do not spawn nested subagents.",
			agentName, maxTurns, fileCount, subagentSummaryTurns,
		),
	}
	childParams.Options.SpawnSubAgents = false
	childParams.Options.SubAgentBackend = ""
	childParams.Options.ToolDenylist = appendUniqueStrings(childParams.Options.ToolDenylist, subagentRunDenylist...)
	// Budget scales with directory file count; no artificial maximum.
	childParams.Options.MaxToolTurns = maxTurns

	capture := &subagentRunCapture{
		maxTurns:  maxTurns,
		backend:   backend,
		name:      agentName,
		task:      task,
		fileCount: fileCount,
		scopePath: scopePath,
	}
	err = child.Start(childCtx, childParams, func(event events.Envelope) {
		capture.Observe(event)
		r.bridgeProcessSubAgentEvent(context.Background(), params, subAgentID, agentName, backend, event)
	})
	if err != nil {
		if childCtx.Err() != nil {
			r.finishSubAgent(params.RunID, subAgentID, "cancelled", "subagent cancelled", childCtx.Err().Error())
			_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
				"subagent_id": subAgentID,
				"name":        agentName,
				"status":      "cancelled",
				"summary":     "subagent cancelled",
				"backend":     backend,
			})
			return "", childCtx.Err()
		}
		detail := capture.FailureError(fmt.Sprintf("subagent process error: %v", err))
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}
	if capture.FinishStatus != "" && capture.FinishStatus != "completed" {
		detail := capture.FailureError(fmt.Sprintf("subagent finished with status %s", capture.FinishStatus))
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}

	result := strings.TrimSpace(truncateToolOutput(capture.FinalText()))
	if result == "" {
		detail := capture.FailureError("subagent returned an empty final report")
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}

	reusable = true
	r.finishSubAgent(params.RunID, subAgentID, "completed", "subagent completed", "")
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id": subAgentID,
		"name":        agentName,
		"status":      "completed",
		"summary":     "subagent completed: " + truncateSummary(task, 80),
		"backend":     backend,
	})
	return result, nil
}

// subagentRunCapture collects diagnostics from a child runtime so empty/failed
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

	MessageDeltas   int
	ReasoningDeltas int
	ToolStarted     int
	ToolFinished    int
	ToolFailed      int

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
		parts = append(parts, fmt.Sprintf("turns_formula=file_count(%d)+summary(%d)", c.fileCount, subagentSummaryTurns))
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
		parts = append(parts, "task="+truncateSummary(c.task, 120))
	}
	// Reasoning can help the parent understand silent failures, but keep it short.
	if reasoning := strings.TrimSpace(c.reasoning.String()); reasoning != "" && c.FinalText() == "" {
		parts = append(parts, "reasoning_preview="+truncateSummary(reasoning, 240))
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

func sanitizeSubagentName(name string) string {
	cleaned := strings.Trim(subagentNameSanitizer.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
	if cleaned == "" {
		return "worker"
	}
	if len(cleaned) > 48 {
		return cleaned[:48]
	}
	return cleaned
}

func truncateSummary(text string, max int) string {
	text = strings.Join(strings.Fields(text), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max] + "…"
}

func (r *Runtime) executeSubagentList(runCtx ToolRunContext, call tools.Call) (string, error) {
	runID := strings.TrimSpace(stringArg(call.Arguments, "run_id"))
	if runID == "" && runCtx.Reply != nil {
		runID = runCtx.Reply.RunID
	}
	subAgentID := strings.TrimSpace(stringArg(call.Arguments, "subagent_id"))
	items := r.subAgentRecords(methods.SubAgentsParams{
		RunID:      runID,
		SubAgentID: subAgentID,
	})
	payload := map[string]any{
		"count": len(items),
		"items": items,
		"pool":  r.processPool.Status(),
	}
	return marshalToolJSON(payload)
}

func (r *Runtime) executeSubagentCancel(runCtx ToolRunContext, call tools.Call) (string, error) {
	subAgentID := strings.TrimSpace(stringArg(call.Arguments, "subagent_id"))
	if subAgentID == "" {
		return "", fmt.Errorf("subagent_id is required")
	}
	runID := strings.TrimSpace(stringArg(call.Arguments, "run_id"))
	if runID == "" && runCtx.Reply != nil {
		runID = runCtx.Reply.RunID
	}
	if runID == "" {
		return "", fmt.Errorf("run_id is required")
	}
	record := r.lookupSubAgentRecord(runID, subAgentID)
	cancelled := r.cancelSubAgent(runID, subAgentID)
	if cancelled {
		r.finishSubAgent(runID, subAgentID, "cancelled", "subagent cancelled by parent", "")
		if runCtx.Reply != nil {
			name := subAgentID
			if record != nil && record.Name != "" {
				name = record.Name
			}
			backend := "process_pool"
			if record != nil && record.Backend != "" {
				backend = record.Backend
			}
			_ = r.emitAgentEvent(context.Background(), *runCtx.Reply, subAgentRef(subAgentID, name), events.EventSubAgentUpdate, nil, map[string]any{
				"subagent_id": subAgentID,
				"name":        name,
				"status":      "cancelled",
				"summary":     "subagent cancelled by parent agent",
				"backend":     backend,
			})
		}
	}
	return marshalToolJSON(map[string]any{
		"cancelled":   cancelled,
		"run_id":      runID,
		"subagent_id": subAgentID,
	})
}

// executeSubagentReset cancels a running subagent (if any) and marks it reset so
// the parent can start a fresh specialist without leaving a stuck running state.
func (r *Runtime) executeSubagentReset(runCtx ToolRunContext, call tools.Call) (string, error) {
	subAgentID := strings.TrimSpace(stringArg(call.Arguments, "subagent_id"))
	if subAgentID == "" {
		return "", fmt.Errorf("subagent_id is required")
	}
	runID := strings.TrimSpace(stringArg(call.Arguments, "run_id"))
	if runID == "" && runCtx.Reply != nil {
		runID = runCtx.Reply.RunID
	}
	if runID == "" {
		return "", fmt.Errorf("run_id is required")
	}

	record := r.lookupSubAgentRecord(runID, subAgentID)
	if record == nil {
		return "", fmt.Errorf("subagent %s not found", subAgentID)
	}
	cancelled := r.cancelSubAgent(runID, subAgentID)
	// Force terminal state even if the subagent already completed/failed.
	r.forceFinishSubAgent(runID, subAgentID, "reset", "subagent reset by parent", "")
	// Clear registry so a later specialist can start cleanly under a new id.
	r.removeSubAgent(runID, subAgentID)

	if runCtx.Reply != nil {
		name := record.Name
		if name == "" {
			name = subAgentID
		}
		backend := record.Backend
		if backend == "" {
			backend = "process_pool"
		}
		_ = r.emitAgentEvent(context.Background(), *runCtx.Reply, subAgentRef(subAgentID, name), events.EventSubAgentUpdate, nil, map[string]any{
			"subagent_id": subAgentID,
			"name":        name,
			"status":      "reset",
			"summary":     "subagent reset by parent agent",
			"backend":     backend,
		})
	}

	// Optionally recycle idle pool workers so the next run is cold-start clean.
	resetPool := boolArg(call.Arguments, "reset_pool", false)
	var pool any
	if resetPool {
		pool = r.processPool.Reset(context.Background())
	} else {
		pool = r.processPool.Status()
	}

	return marshalToolJSON(map[string]any{
		"reset":       true,
		"cancelled":   cancelled,
		"run_id":      runID,
		"subagent_id": subAgentID,
		"pool":        pool,
	})
}

func (r *Runtime) executeSubagentPoolStatus() (string, error) {
	return marshalToolJSON(map[string]any{
		"backend": "process_pool",
		"pool":    r.processPool.Status(),
	})
}

func (r *Runtime) executeSubagentPoolResize(call tools.Call) (string, error) {
	limit := intArg(call.Arguments, "size", 0)
	if limit <= 0 {
		return "", fmt.Errorf("size must be between 1 and %d", maxSubAgentPoolSize)
	}
	status := r.processPool.SetLimit(limit)
	return marshalToolJSON(map[string]any{
		"resized": true,
		"pool":    status,
	})
}

func (r *Runtime) executeSubagentPoolReset() (string, error) {
	status := r.processPool.Reset(context.Background())
	return marshalToolJSON(map[string]any{
		"reset": true,
		"pool":  status,
	})
}

func (r *Runtime) lookupSubAgentRecord(rootRunID string, subAgentID string) *methods.SubAgentRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.subagents[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return nil
	}
	record := state.record
	return &record
}

func (r *Runtime) removeSubAgent(rootRunID string, subAgentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.subagents[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return
	}
	delete(r.subagents, subAgentID)
}

func (r *Runtime) forceFinishSubAgent(rootRunID string, subAgentID string, status string, summary string, errText string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.subagents[subAgentID]
	if state == nil || state.record.RootRunID != rootRunID {
		return
	}
	state.record.Status = status
	state.record.Summary = summary
	state.record.Error = errText
	state.record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if state.cancel != nil {
		// Keep cancel nil after reset so later cancel is a no-op.
		state.cancel = nil
	}
}

func marshalToolJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
