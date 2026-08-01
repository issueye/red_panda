package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"redpanda/agent/internal/provider"
	agenttools "redpanda/agent/internal/tools"
	"redpanda/agent/internal/worker"
	"strings"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

// loopEndReason 表示单个提供方与工具分段循环的结构化退出原因。
// 在多分段外层循环中，不能将其简化为字符串形式的“completed”。
type loopEndReason string

const (
	loopEndNoTools    loopEndReason = "no_tools"
	loopEndMaxTurns   loopEndReason = "max_turns"
	loopEndToolBudget loopEndReason = "tool_budget_reached"
	loopEndCancelled  loopEndReason = "cancelled"
	loopEndFailed     loopEndReason = "failed"
	loopEndBudget     loopEndReason = "budget_exhausted"
	// Empty provider responses after tool execution are often transient. A
	// small segment-wide cap lets the model resume the unfinished task without
	// turning an empty-response provider into an unbounded retry loop.
	maxEmptyContinuationAttempts = 2
)

const (
	maxToolsPerProviderTurn = 8
	toolBudgetPerTurn       = 4
)

type providerExecutionStats struct {
	MaxTurns           int
	LoopTurns          int
	ProviderRequests   int
	ToolCallsRequested int
	ToolCallsExecuted  int
	ToolCallBudget     int
	MaxTurnsReached    bool
	ToolBudgetReached  bool
}

// providerSegmentResult 表示一次 runProviderLoopSegment 的结果。
type providerSegmentResult struct {
	Reason    loopEndReason
	ToolTurns int
	Stats     providerExecutionStats
	// History 是为 carry_summarized 和下一分段累计的工具交互记录。
	History []provider.ToolExchange
}

func finishStatusFromLoopEnd(reason loopEndReason) string {
	switch reason {
	case loopEndCancelled:
		return "cancelled"
	case loopEndFailed, loopEndBudget:
		return "failed"
	case loopEndNoTools, loopEndMaxTurns, loopEndToolBudget:
		// 分段生成可恢复答案时（包括达到最大回合数后的综合），对外结束状态仍为“completed”。
		// 区分性的原因会以 loop_end_reason 保留在结束载荷中。
		return "completed"
	default:
		return "completed"
	}
}

func (r *Runtime) emitMemoryInjected(ctx context.Context, params methods.ReplyParams) {
	if params.Options.MemoryContext == nil || len(params.Options.MemoryContext.Items) == 0 {
		return
	}
	ids := make([]string, 0, len(params.Options.MemoryContext.Items))
	for _, item := range params.Options.MemoryContext.Items {
		if item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	_ = r.emitEvent(ctx, params, events.EventMemoryInjected, nil, map[string]any{
		"memory_ids":    ids,
		"count":         len(params.Options.MemoryContext.Items),
		"context_chars": len(params.Options.MemoryContext.Context),
	})
}

func (r *Runtime) emitSkillsInjected(ctx context.Context, params methods.ReplyParams) {
	if params.Options.SkillsContext == nil {
		return
	}
	names := make([]string, 0, len(params.Options.SkillsContext.Items))
	for _, item := range params.Options.SkillsContext.Items {
		if item.Name != "" {
			names = append(names, item.Name)
		}
	}
	_ = r.emitEvent(ctx, params, events.EventSkillsInjected, nil, map[string]any{
		"skill_names":   names,
		"count":         len(names),
		"context_chars": len(params.Options.SkillsContext.Context),
	})
}

// runProviderLoop 是为仅需旧版字符串状态的调用方和测试保留的轻量包装。
// 新代码应优先使用 runProviderLoopSegment。
func (r *Runtime) runProviderLoop(ctx context.Context, params methods.ReplyParams, input string, history []provider.ToolExchange, messageID string, streamID string, streamSeq *uint64) string {
	seg := r.runProviderLoopSegment(ctx, params, input, history, messageID, streamID, streamSeq)
	return finishStatusFromLoopEnd(seg.Reason)
}

// runProviderLoopSegment 执行一个提供方与工具预算分段。
// 它不清理运行快照，也不发送 EventFinish；终止清理和结束事件由外层
// emitRun 或 Goal 多分段控制器负责。
func (r *Runtime) runProviderLoopSegment(ctx context.Context, params methods.ReplyParams, input string, history []provider.ToolExchange, messageID string, streamID string, streamSeq *uint64) providerSegmentResult {
	maxTurns := effectiveProviderToolTurns(params.Options)
	stats := providerExecutionStats{MaxTurns: maxTurns, ToolCallBudget: maxTurns * toolBudgetPerTurn}
	turnsUsed := 0
	finish := func(reason loopEndReason, rounds [][]provider.ToolExchange) providerSegmentResult {
		stats.LoopTurns = turnsUsed
		stats.MaxTurnsReached = reason == loopEndMaxTurns
		stats.ToolBudgetReached = reason == loopEndToolBudget
		return providerSegmentResult{Reason: reason, ToolTurns: stats.LoopTurns, Stats: stats, History: flattenToolRounds(rounds)}
	}
	runRegistry := r.prepareRegistryForRun(ctx, params)
	providerInput := input
	emptyContinuationAttempts := 0
	var rounds [][]provider.ToolExchange
	if len(history) > 0 {
		// 初始循环前工具（如有）视为一个回合。
		rounds = append(rounds, append([]provider.ToolExchange(nil), history...))
	}
	// Snapshot all prompt and tool-definition inputs once. Tool turns may only
	// append transcript; they must not refresh Todo or reorder the prefix.
	if ctxTodos := r.todoContextForRun(params.RunID); ctxTodos != nil {
		params.Options.TodoContext = ctxTodos
	}
	definitions := provider.CanonicalToolDefinitions(runRegistry.FilterDefinitions(runRegistry.Definitions(), params.Options))
	baseRequest := newPromptComposer().compose(params, providerInput, definitions, nil)
	baseRequest.Options = provider.WithRegistry(baseRequest.Options, r.providers)
	for turn := 0; turn < maxTurns; turn++ {
		if err := r.runStates.WaitIfPaused(ctx, params.RunID); err != nil {
			return finish(loopEndCancelled, rounds)
		}
		var requestedCalls []tools.Call
		emittedText := false
		flatHistory := flattenToolRounds(rounds)
		request := baseRequest
		request.ToolHistory = append([]provider.ToolExchange(nil), flatHistory...)
		request.ToolRounds = cloneToolRounds(rounds)
		stats.ProviderRequests++
		err := r.completeProvider(ctx, params, request, func(chunk provider.ProviderChunk) error {
			if err := r.runStates.WaitIfPaused(ctx, params.RunID); err != nil {
				return err
			}
			return r.consumeProviderChunk(ctx, params, chunk, messageID, streamID, streamSeq, &requestedCalls, &emittedText, len(flatHistory) > 0)
		})
		turnsUsed++
		if err != nil {
			if ctx.Err() != nil {
				return finish(loopEndCancelled, rounds)
			}
			_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
				"message":       err.Error(),
				"status":        "failed",
				"provider_name": r.provider.Name(),
			})
			return finish(loopEndFailed, rounds)
		}
		if ctx.Err() != nil {
			return finish(loopEndCancelled, rounds)
		}
		if len(requestedCalls) == 0 {
			if !emittedText && len(flatHistory) > 0 {
				if emptyContinuationAttempts < maxEmptyContinuationAttempts && turn+1 < maxTurns {
					emptyContinuationAttempts++
					providerInput = emptyProviderContinuationPrompt(input, emptyContinuationAttempts)
					baseRequest = newPromptComposer().compose(params, providerInput, definitions, nil)
					baseRequest.Options = provider.WithRegistry(baseRequest.Options, r.providers)
					continue
				}
				// 不使用工具重试一次，迫使模型生成最终答案。
				stats.ProviderRequests++
				if r.retryFinalAnswer(ctx, params, providerInput, rounds, messageID, streamID, streamSeq) {
					return finish(loopEndNoTools, rounds)
				}
				fallback := recoveryAnswerForRun(params, flatHistory)
				_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
					StreamID: streamID,
					Kind:     events.StreamMessage,
					Seq:      *streamSeq,
					Final:    true,
				}, map[string]any{
					"message_id":    messageID,
					"delta":         fallback,
					"provider_name": r.provider.Name(),
					"recovered":     true,
					"recovery_kind": "fallback",
				})
				(*streamSeq)++
			}
			return finish(loopEndNoTools, rounds)
		}
		// 在历史记录中保留调用顺序；多个 worker.delegate 工作进程可并发执行。
		if err := r.runStates.WaitIfPaused(ctx, params.RunID); err != nil {
			return finish(loopEndCancelled, rounds)
		}
		stats.ToolCallsRequested += len(requestedCalls)
		remaining := stats.ToolCallBudget - stats.ToolCallsExecuted
		allowed, rejected := boundedToolCalls(requestedCalls, remaining)
		exchanges, cancelled := r.executeToolBatch(ctx, params, allowed)
		stats.ToolCallsExecuted += len(allowed)
		exchanges = append(exchanges, rejected...)
		if len(exchanges) > 0 {
			rounds = append(rounds, exchanges)
		}
		if cancelled {
			return finish(loopEndCancelled, rounds)
		}
		if stats.ToolCallsExecuted >= stats.ToolCallBudget {
			stats.ProviderRequests++
			if !r.retryFinalAnswer(ctx, params, input, rounds, messageID, streamID, streamSeq) {
				fallback := recoveryAnswerForRun(params, flattenToolRounds(rounds))
				_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
					StreamID: streamID, Kind: events.StreamMessage, Seq: *streamSeq, Final: true,
				}, map[string]any{
					"message_id": messageID, "delta": fallback, "provider_name": r.provider.Name(),
					"recovered": true, "recovery_kind": "fallback", "tool_call_budget": stats.ToolCallBudget,
				})
				(*streamSeq)++
			}
			return finish(loopEndToolBudget, rounds)
		}
	}
	// 工具执行后预算耗尽时，仍尝试仅文本的最终综合。
	// 原因保持为 max_turns，以便 Goal 外层循环开启下一分段。
	if len(rounds) > 0 {
		stats.ProviderRequests++
		if r.retryFinalAnswer(ctx, params, input, rounds, messageID, streamID, streamSeq) {
			return finish(loopEndMaxTurns, rounds)
		}
		fallback := recoveryAnswerForRun(params, flattenToolRounds(rounds))
		_ = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
			StreamID: streamID,
			Kind:     events.StreamMessage,
			Seq:      *streamSeq,
			Final:    true,
		}, map[string]any{
			"message_id":    messageID,
			"delta":         fallback,
			"provider_name": r.provider.Name(),
			"recovered":     true,
			"recovery_kind": "fallback",
			"max_turns":     maxTurns,
		})
		(*streamSeq)++
		return finish(loopEndMaxTurns, rounds)
	}
	_ = r.emitEvent(ctx, params, events.EventError, nil, map[string]any{
		"message":   fmt.Sprintf("provider exceeded tool turn limit (%d)", maxTurns),
		"status":    "failed",
		"max_turns": maxTurns,
	})
	return finish(loopEndFailed, rounds)
}

func boundedToolCalls(calls []tools.Call, remaining int) ([]tools.Call, []provider.ToolExchange) {
	limit := remaining
	if limit > maxToolsPerProviderTurn {
		limit = maxToolsPerProviderTurn
	}
	if limit < 0 {
		limit = 0
	}
	if limit > len(calls) {
		limit = len(calls)
	}
	allowed := append([]tools.Call(nil), calls[:limit]...)
	rejected := make([]provider.ToolExchange, 0, len(calls)-limit)
	for _, call := range calls[limit:] {
		rejected = append(rejected, provider.ToolExchange{Call: call, Result: tools.Result{
			ToolCallID: call.ID,
			Name:       call.Name,
			Status:     tools.CallStatusFailed,
			Error:      fmt.Sprintf("tool call budget exceeded: at most %d calls per model turn and %d calls remain", maxToolsPerProviderTurn, remaining),
		}})
	}
	return allowed, rejected
}

func emptyProviderContinuationPrompt(input string, attempt int) string {
	return input + fmt.Sprintf("\n\n[System] The previous provider response ended empty after tool execution (continuation attempt %d/%d). "+
		"The task is not complete. Continue working from the tool history now. Use additional tools when needed, then return a complete final answer. Do not stop after planning or todo updates.",
		attempt, maxEmptyContinuationAttempts)
}

// carrySummarizedHistory 压缩供下一 Goal 分段使用的工具交互记录。
// 最近 K 次交互保留截断输出，仅按规则处理，不调用 LLM。
func carrySummarizedHistory(history []provider.ToolExchange, k int, maxRunes int) []provider.ToolExchange {
	if k <= 0 || len(history) == 0 {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 500
	}
	start := 0
	if len(history) > k {
		start = len(history) - k
	}
	out := make([]provider.ToolExchange, 0, len(history)-start)
	for _, ex := range history[start:] {
		cp := ex
		if len(cp.Result.Output) > maxRunes {
			// 输出为字符串，按 UTF-8 字符截断以保证中日韩文本预算安全。
			runes := []rune(cp.Result.Output)
			if len(runes) > maxRunes {
				cp.Result.Output = string(runes[:maxRunes]) + "…"
			}
		}
		out = append(out, cp)
	}
	return out
}

func (r *Runtime) consumeProviderChunk(
	ctx context.Context,
	params methods.ReplyParams,
	chunk provider.ProviderChunk,
	messageID string,
	streamID string,
	streamSeq *uint64,
	requestedCalls *[]tools.Call,
	emittedText *bool,
	deferEmptyFinal bool,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if chunk.ReasoningDelta != "" {
		err := r.emitEvent(ctx, params, events.EventReasoningDelta, &events.StreamRef{
			StreamID: streamID + ":reasoning",
			Kind:     events.StreamReasoning,
			Seq:      *streamSeq,
			Final:    false,
		}, map[string]any{
			"message_id":    messageID,
			"delta":         chunk.ReasoningDelta,
			"provider_name": r.provider.Name(),
		})
		(*streamSeq)++
		if err != nil {
			return err
		}
	}
	if len(chunk.ToolCalls) > 0 {
		*requestedCalls = append(*requestedCalls, chunk.ToolCalls...)
		return nil
	}
	// Streaming providers commonly emit Markdown line breaks as standalone
	// "\n" / "\n\n" deltas. Only discard a truly empty chunk; trimming here
	// flattens headings, lists, and tables into one paragraph.
	if chunk.Delta == "" && !chunk.Final {
		return nil
	}
	if strings.TrimSpace(chunk.Delta) != "" {
		*emittedText = true
	}
	// 空 Final 标记会关闭根消息流；直接在此关闭消息流。
	if chunk.Delta == "" && chunk.Final {
		// Do not close the stream for a provider response that contained neither
		// text nor tool calls. The outer loop may issue a bounded continuation
		// request and must keep the same assistant stream open for that response.
		if !*emittedText && (deferEmptyFinal || len(*requestedCalls) > 0) {
			return nil
		}
		err := r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
			StreamID: streamID,
			Kind:     events.StreamMessage,
			Seq:      *streamSeq,
			Final:    true,
		}, map[string]any{
			"message_id":    messageID,
			"delta":         "",
			"provider_name": r.provider.Name(),
		})
		(*streamSeq)++
		return err
	}
	err := r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamMessage,
		Seq:      *streamSeq,
		Final:    chunk.Final,
	}, map[string]any{
		"message_id":    messageID,
		"delta":         chunk.Delta,
		"provider_name": r.provider.Name(),
	})
	(*streamSeq)++
	return err
}

// retryFinalAnswer 在不使用工具的前提下再次请求模型生成面向用户的答案。
func (r *Runtime) retryFinalAnswer(
	ctx context.Context,
	params methods.ReplyParams,
	input string,
	rounds [][]provider.ToolExchange,
	messageID string,
	streamID string,
	streamSeq *uint64,
) bool {
	if ctx.Err() != nil || len(rounds) == 0 {
		return false
	}
	recoveryPrompt := input + "\n\n[System] All tools for this turn have finished. " +
		"Write a complete, helpful final answer for the user based on the tool results above. " +
		"Do not call tools. Respond in the user's language. " +
		"If some tools failed, still summarize what succeeded and what is known."
	var answer strings.Builder
	returnedToolCalls := false
	request := newPromptComposer().compose(params, recoveryPrompt, nil, rounds)
	request.Options = provider.WithRegistry(request.Options, r.providers)
	err := r.completeProvider(ctx, params, request, func(chunk provider.ProviderChunk) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(chunk.ToolCalls) > 0 {
			returnedToolCalls = true
		}
		answer.WriteString(chunk.Delta)
		return nil
	})
	text := strings.TrimSpace(answer.String())
	if err != nil || returnedToolCalls || !worker.ReportUsable(text) {
		return false
	}
	err = r.emitEvent(ctx, params, events.EventMessageDelta, &events.StreamRef{
		StreamID: streamID,
		Kind:     events.StreamMessage,
		Seq:      *streamSeq,
		Final:    true,
	}, map[string]any{
		"message_id":    messageID,
		"delta":         text,
		"provider_name": r.provider.Name(),
		"recovered":     true,
		"recovery_kind": "final_answer_retry",
	})
	(*streamSeq)++
	return err == nil
}

func flattenToolRounds(rounds [][]provider.ToolExchange) []provider.ToolExchange {
	if len(rounds) == 0 {
		return nil
	}
	total := 0
	for _, round := range rounds {
		total += len(round)
	}
	out := make([]provider.ToolExchange, 0, total)
	for _, round := range rounds {
		out = append(out, round...)
	}
	return out
}

func cloneToolRounds(rounds [][]provider.ToolExchange) [][]provider.ToolExchange {
	if len(rounds) == 0 {
		return nil
	}
	result := make([][]provider.ToolExchange, 0, len(rounds))
	for _, round := range rounds {
		result = append(result, append([]provider.ToolExchange(nil), round...))
	}
	return result
}

// synthesizeToolAnswer 在提供方结束工具循环却未输出自然语言回复时，构建面向用户的可见摘要。
// 优先使用标准工具封装中的 text/data，绝不静默丢弃结果。
func synthesizeToolAnswer(history []provider.ToolExchange) string {
	if len(history) == 0 {
		return "工具已执行，但模型未生成最终回复。请重试一次。"
	}

	for _, exchange := range history {
		if exchange.Call.Name != "web.search" && exchange.Result.Name != "web.search" {
			continue
		}
		if text := extractSearchAnswer(exchange.Result.Output); text != "" {
			return text
		}
	}

	var b strings.Builder
	b.WriteString("根据工具执行结果整理如下：\n\n")
	for _, exchange := range history {
		name := strings.TrimSpace(exchange.Call.DisplayName)
		if name == "" {
			name = strings.TrimSpace(exchange.Call.Name)
		}
		if name == "" {
			name = "tool"
		}
		content := readableToolResultText(exchange.Result)
		if content == "" {
			content = "（无输出）"
		}
		if len(content) > 1200 {
			content = content[:1200] + "…\n[完整内容见工具卡片，未删除]"
		}
		b.WriteString("### ")
		b.WriteString(name)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func recoveryAnswerForRun(params methods.ReplyParams, history []provider.ToolExchange) string {
	if params.Options.WorkerContext != nil {
		return "子代理已完成工具调用，但未生成可用的最终报告。工具结果已保留在工具卡片中。"
	}
	return synthesizeToolAnswer(history)
}

func readableToolResultText(result tools.Result) string {
	if env, ok := agenttools.ParseStandardToolResult(result.Output); ok {
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
		if strings.TrimSpace(env.Error) != "" {
			return strings.TrimSpace(env.Error)
		}
		if env.Data != nil {
			return agenttools.PreferReadableText(env.Data, "")
		}
	}
	return strings.TrimSpace(result.Output)
}

func extractSearchAnswer(raw string) string {
	if env, ok := agenttools.ParseStandardToolResult(raw); ok {
		if m, ok := env.Data.(map[string]any); ok {
			if built := formatSearchData(m); built != "" {
				return built
			}
		}
		if strings.TrimSpace(env.Text) != "" {
			return strings.TrimSpace(env.Text)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return ""
	}
	return formatSearchData(parsed)
}

func formatSearchData(v map[string]any) string {
	answer, _ := v["answer"].(string)
	items, _ := v["items"].([]any)
	if strings.TrimSpace(answer) == "" && len(items) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(answer) != "" {
		b.WriteString(strings.TrimSpace(answer))
		b.WriteString("\n\n")
	}
	if len(items) > 0 {
		b.WriteString("参考来源：\n")
		limit := len(items)
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			item, ok := items[i].(map[string]any)
			if !ok {
				continue
			}
			title, _ := item["title"].(string)
			url, _ := item["url"].(string)
			snippet, _ := item["snippet"].(string)
			b.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, strings.TrimSpace(title), strings.TrimSpace(url)))
			if snip := strings.TrimSpace(snippet); snip != "" {
				b.WriteString("   ")
				b.WriteString(agenttools.CompactOneLine(snip, 160))
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}
