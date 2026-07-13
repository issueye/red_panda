package runtime

import (
	"context"
	"sync"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

type toolBatchItem struct {
	index      int
	call       tools.Call
	invocation ToolInvocation
	result     tools.Result
	ok         bool
}

// executeToolBatch runs non-subagent tools sequentially, then runs all
// subagent.run tools in parallel so multi-area analysis can proceed concurrently.
func (r *Runtime) executeToolBatch(ctx context.Context, params methods.ReplyParams, calls []tools.Call) ([]ToolExchange, bool) {
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

func batchToHistory(items []toolBatchItem) []ToolExchange {
	history := make([]ToolExchange, 0, len(items))
	for _, item := range items {
		call := item.call
		if item.invocation.Call.ID != "" {
			call = item.invocation.Call
		}
		// Skip slots never executed (e.g. cancelled before parallel start).
		if item.result.ToolCallID == "" && item.result.Name == "" && item.result.Error == "" && item.result.Status == "" {
			if item.call.ID == "" && item.call.Name == "" {
				continue
			}
			// Still record cancelled/not-started calls as failed for model continuity.
			if item.result.Status == "" {
				item.result = tools.Result{
					ToolCallID: item.call.ID,
					Name:       item.call.Name,
					Status:     tools.CallStatusFailed,
					Error:      "tool was not executed",
				}
			}
		}
		history = append(history, ToolExchange{Call: call, Result: item.result})
	}
	return history
}
