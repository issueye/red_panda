package runtime

import (
	"context"
	"sync"

	"redpanda/agent/internal/provider"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type toolBatchItem struct {
	index      int
	call       tools.Call
	invocation agenttools.ToolInvocation
	result     tools.Result
	ok         bool
}

// executeToolBatch 顺序执行非子代理工具，随后并行执行全部 subagent.run 工具，
// 使多个区域的分析可并发进行。
func (r *Runtime) executeToolBatch(ctx context.Context, params methods.ReplyParams, calls []tools.Call) ([]provider.ToolExchange, bool) {
	items := make([]toolBatchItem, len(calls))
	var serial []int
	var parallel []int

	for index, call := range calls {
		items[index] = toolBatchItem{index: index, call: call}
		if call.Name == "subagent.run" {
			parallel = append(parallel, index)
		} else {
			serial = append(serial, index)
		}
	}

	runOne := func(index int) {
		call := items[index].call
		invocation, err := r.tools.InvocationFromCall(params.RunID, index, call, r.mcpDefinitionsForRun(params.RunID)...)
		if err != nil {
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
		items[index].invocation = invocation
		result, _, ok := r.executeTool(ctx, params, invocation)
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
		call := item.call
		if item.invocation.Call.ID != "" {
			call = item.invocation.Call
		}
		// 跳过从未执行的槽位，例如并行启动前已取消的槽位。
		if item.result.ToolCallID == "" && item.result.Name == "" && item.result.Error == "" && item.result.Status == "" {
			if item.call.ID == "" && item.call.Name == "" {
				continue
			}
			// 仍将已取消或未启动的调用记为失败，保持模型上下文连续。
			if item.result.Status == "" {
				item.result = tools.Result{
					ToolCallID: item.call.ID,
					Name:       item.call.Name,
					Status:     tools.CallStatusFailed,
					Error:      "tool was not executed",
				}
			}
		}
		history = append(history, provider.ToolExchange{Call: call, Result: item.result})
	}
	return history
}
