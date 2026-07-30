package runtime

import (
	"context"
	"sync"

	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/provider"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type toolBatchItem struct {
	index  int
	call   tools.Call
	result tools.Result
	ok     bool
}

// resolveToolCall checks whether a tool name is known (registry or MCP) and
// normalises legacy aliases. It also back-fills DisplayName and Risk from the
// tool definition when they are empty — this mirrors the behaviour of the old
// ToolRunner.InvocationFromCall, which is relied on by permission-policy tests.
func (r *Runtime) resolveToolCall(call *tools.Call) error {
	call.Name = agenttools.CanonicalToolName(call.Name)
	entry, ok := r.registry.Lookup(call.Name)
	if ok {
		if call.DisplayName == "" {
			call.DisplayName = entry.Definition.DisplayName
		}
		if call.Risk == "" {
			call.Risk = entry.Definition.Risk
		}
		return nil
	}
	if agenttools.IsMCPToolName(call.Name) {
		if call.DisplayName == "" {
			call.DisplayName = call.Name
		}
		if call.Risk == "" {
			call.Risk = tools.RiskHigh
		}
		return nil
	}
	return nil // executeTool handles unknown-tool case itself
}

// executeToolBatch 顺序执行普通工具，随后并行执行全部委派工具，
// 使入口 Assignment 能同时使用多个空闲 Worker。
func (r *Runtime) executeToolBatch(ctx context.Context, params methods.ReplyParams, calls []tools.Call) ([]provider.ToolExchange, bool) {
	items := make([]toolBatchItem, len(calls))
	var serial []int
	var parallel []int

	for index, call := range calls {
		items[index] = toolBatchItem{index: index, call: call}
		if call.Name == "worker.delegate" {
			parallel = append(parallel, index)
		} else {
			serial = append(serial, index)
		}
	}

	runOne := func(index int) {
		call := items[index].call
		if err := r.resolveToolCall(&call); err != nil {
			failed := tools.Result{
				ToolCallID: call.ID,
				Name:       call.Name,
				Status:     tools.CallStatusFailed,
				Error:      err.Error(),
			}
			_ = r.emitEvent(ctx, params, events.EventToolFailed, nil, toolResultPayload(failed))
			items[index].result = failed
			items[index].ok = false
			return
		}
		items[index].call = call
		result, _, ok := r.executeTool(ctx, params, call)
		items[index].result = result
		items[index].ok = ok
	}

	for _, index := range serial {
		if ctx.Err() != nil {
			return batchToHistory(items), true
		}
		runOne(index)
	}

	if len(parallel) > 0 {
		var wg sync.WaitGroup
		for _, index := range parallel {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if ctx.Err() != nil {
					return
				}
				runOne(i)
			}(index)
		}
		wg.Wait()
	}

	return batchToHistory(items), ctx.Err() != nil
}

func batchToHistory(items []toolBatchItem) []provider.ToolExchange {
	history := make([]provider.ToolExchange, 0, len(items))
	for _, item := range items {
		if item.result.ToolCallID == "" && item.result.Name == "" && item.result.Error == "" && item.result.Status == "" {
			if item.call.ID == "" && item.call.Name == "" {
				continue
			}
			if item.result.Status == "" {
				item.result = tools.Result{
					ToolCallID: item.call.ID,
					Name:       item.call.Name,
					Status:     tools.CallStatusFailed,
					Error:      "tool was not executed",
				}
			}
		}
		history = append(history, provider.ToolExchange{Call: item.call, Result: item.result})
	}
	return history
}
