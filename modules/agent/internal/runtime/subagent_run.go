package runtime

import (
	"context"
	"fmt"
	"redpanda/agent/internal/subagent"
	agenttools "redpanda/agent/internal/tools"
	"strings"
	"time"

	"redpanda/protocol/events"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func (r *Runtime) executeSubagentRun(ctx context.Context, runCtx agenttools.ToolRunContext, call tools.Call) (string, error) {
	if runCtx.Reply == nil {
		return "", fmt.Errorf("subagent requires reply context")
	}
	task := strings.TrimSpace(agenttools.StringArg(call.Arguments, "task"))
	if task == "" {
		return "", fmt.Errorf("subagent task is required")
	}
	name := strings.TrimSpace(agenttools.StringArg(call.Arguments, "name"))
	if name == "" {
		name = "worker"
	}
	name = subagent.SanitizeName(name)
	specialist, isSpecialist := resolveGoalSpecialist(runCtx.Reply.Options.AgentDefinitions, name)
	displayName := goalSpecialistDisplayName(name)
	if isSpecialist && strings.TrimSpace(specialist.NameZH) != "" {
		displayName = specialist.NameZH
	}
	if isSpecialist {
		if err := r.validateGoalSpecialistPhase(runCtx.Reply.RunID, specialist); err != nil {
			return "", err
		}
	}

	// 预算 = file_count + 摘要回合。父代理应传入 file_count（来自 workspace.stats）
	// 或 path（自动统计）；提供显式 max_turns 时仍以其为准。
	fileCount := agenttools.IntArg(call.Arguments, "file_count", 0)
	scopePath := strings.TrimSpace(agenttools.StringArg(call.Arguments, "path"))
	if fileCount <= 0 && scopePath != "" && runCtx.WorkingDir != "" {
		if stats, err := agenttools.ComputeWorkspaceStats(runCtx.WorkingDir, scopePath, 4); err == nil {
			fileCount = stats.TotalFiles
		}
	}
	explicitTurns := agenttools.IntArg(call.Arguments, "max_turns", 0)
	maxTurns := subagent.EffectiveToolTurns(explicitTurns, fileCount)
	// 父代理未提供预算时，Goal 阶段专家使用角色默认值。
	if isSpecialist && explicitTurns <= 0 && fileCount <= 0 {
		maxTurns = specialist.DefaultMaxTurns
	}

	params := *runCtx.Reply
	subAgentID := fmt.Sprintf("worker_%s_%d", name, time.Now().UnixNano())
	agentName := name
	childRunID := params.RunID + ":subagent:" + subAgentID
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 专家工作进程始终使用进程池，以便复用和重置。
	backend := "process_pool"
	if requested := normalizedSubAgentBackend(params.Options.SubAgentBackend); requested == "runtime_process" {
		// 父代理显式不使用池化时，仍可启动一次性进程。
		backend = "runtime_process"
	}

	r.registerSubAgent(params, subAgentID, agentName, backend, cancel)
	startSummary := "subagent started: " + subagent.TruncateSummary(task, 80)
	if isSpecialist {
		startSummary = displayName + " 已启动: " + subagent.TruncateSummary(task, 60)
	}
	_ = r.emitAgentEvent(ctx, params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id":     subAgentID,
		"name":            agentName,
		"display_name":    displayName,
		"status":          "running",
		"summary":         startSummary,
		"backend":         backend,
		"task":            task,
		"file_count":      fileCount,
		"max_turns":       maxTurns,
		"path":            scopePath,
		"goal_specialist": isSpecialist,
		"goal_phase":      specialist.Phase,
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
			scopePath, fileCount, maxTurns, subagent.SummaryTurns, task)
	} else if fileCount > 0 {
		childTask = fmt.Sprintf("File count budget: %d files => max_turns=%d (files + %d summary turns).\n\n%s",
			fileCount, maxTurns, subagent.SummaryTurns, task)
	}

	childParams := params
	childParams.RunID = childRunID
	childParams.Session.Conversation = nil
	childParams.Input.Text = childTask
	// 工作进程不继承根记忆，角色文本仅存于 SpecialistContext。
	childParams.Options.MemoryContext = nil
	childParams.Options.SpecialistContext = &methods.SpecialistContext{
		Kind: "worker",
		Context: fmt.Sprintf(
			"You are a focused subagent named %q. Complete only the assigned task using workspace tools as needed. "+
				"Your tool-turn budget is %d (derived from file_count=%d plus %d turns for analysis summary). "+
				"Return a clear final report for the parent agent. Do not spawn nested subagents.",
			agentName, maxTurns, fileCount, subagent.SummaryTurns,
		),
	}
	childParams.Options.TodoContext = nil
	disableGoalPipelineForChild(&childParams.Options)
	childParams.Options.SpawnSubAgents = false
	childParams.Options.SubAgentBackend = ""
	childParams.Options.ToolDenylist = appendUniqueStrings(childParams.Options.ToolDenylist, subagent.RunDenylist...)
	// 预算随目录文件数量变化，没有人为上限，除非专家配置了上限。
	childParams.Options.MaxToolTurns = maxTurns

	if isSpecialist {
		parentGoalID := strings.TrimSpace(params.Options.GoalID)
		parentObjective := ""
		if params.Options.GoalContext != nil {
			parentObjective = params.Options.GoalContext.Objective
			if parentGoalID == "" {
				parentGoalID = strings.TrimSpace(params.Options.GoalContext.GoalID)
			}
		}
		maxTurns = r.applyGoalSpecialist(&childParams, specialist, task, maxTurns, params.RunID, params.Session.ID, parentGoalID, parentObjective)
	}

	capture := subagent.NewCapture(subagent.CaptureOptions{
		MaxTurns:  maxTurns,
		Backend:   backend,
		Name:      agentName,
		Task:      task,
		FileCount: fileCount,
		ScopePath: scopePath,
	})
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
	if capture.FinishStatus() != "" && capture.FinishStatus() != "completed" {
		detail := capture.FailureError(fmt.Sprintf("subagent finished with status %s", capture.FinishStatus()))
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}

	result := strings.TrimSpace(agenttools.TruncateToolOutput(capture.FinalText()))
	if result == "" {
		detail := capture.FailureError("subagent returned an empty final report")
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}
	if capture.RecoveredFallback() {
		detail := capture.FailureError("subagent used a recovery fallback instead of a final report")
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}
	if !subagent.ReportUsable(result) {
		detail := capture.FailureError("subagent returned tool calls instead of a final report")
		r.failWorkerSubAgent(params, subAgentID, agentName, backend, detail)
		return "", detail
	}

	reusable = true
	r.finishSubAgent(params.RunID, subAgentID, "completed", "subagent completed", "")
	doneSummary := "subagent completed: " + subagent.TruncateSummary(task, 80)
	if isSpecialist {
		doneSummary = displayName + " 已完成: " + subagent.TruncateSummary(task, 60)
	}
	_ = r.emitAgentEvent(context.Background(), params, subAgentRef(subAgentID, agentName), events.EventSubAgentUpdate, nil, map[string]any{
		"subagent_id":     subAgentID,
		"name":            agentName,
		"display_name":    displayName,
		"status":          "completed",
		"summary":         doneSummary,
		"backend":         backend,
		"goal_specialist": isSpecialist,
		"goal_phase":      specialist.Phase,
	})
	return result, nil
}
