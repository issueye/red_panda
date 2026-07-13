package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func (r *Runtime) executeSubagentList(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	runID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "run_id"))
	if runID == "" && runCtx.Reply != nil {
		runID = runCtx.Reply.RunID
	}
	subAgentID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "subagent_id"))
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

func (r *Runtime) executeSubagentCancel(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	subAgentID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "subagent_id"))
	if subAgentID == "" {
		return "", fmt.Errorf("subagent_id is required")
	}
	runID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "run_id"))
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

// executeSubagentReset 取消正在运行的子代理（如有）并将其标记为已重置，
// 使父代理可启动新的专家，且不会遗留卡住的运行状态。
func (r *Runtime) executeSubagentReset(runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	subAgentID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "subagent_id"))
	if subAgentID == "" {
		return "", fmt.Errorf("subagent_id is required")
	}
	runID := strings.TrimSpace(agenttools.StringArg(call.Arguments, "run_id"))
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
	// 即使子代理已完成或失败，也强制写入终止状态。
	r.forceFinishSubAgent(runID, subAgentID, "reset", "subagent reset by parent", "")
	// 清理注册表，使后续专家可使用新 ID 干净启动。
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

	// 可选地回收空闲进程池工作进程，使下一次运行获得干净的冷启动环境。
	resetPool := agenttools.BoolArg(call.Arguments, "reset_pool", false)
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
	limit := agenttools.IntArg(call.Arguments, "size", 0)
	if limit <= 0 {
		return "", fmt.Errorf("size must be between 1 and %d", subagent.MaxPoolSize)
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
	record, ok := r.subagents.Lookup(rootRunID, subAgentID)
	if !ok {
		return nil
	}
	return &record
}

func (r *Runtime) removeSubAgent(rootRunID string, subAgentID string) {
	r.subagents.Remove(rootRunID, subAgentID)
}

func (r *Runtime) forceFinishSubAgent(rootRunID string, subAgentID string, status string, summary string, errText string) {
	r.subagents.ForceFinish(rootRunID, subAgentID, status, summary, errText)
}

func marshalToolJSON(value any) (string, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
