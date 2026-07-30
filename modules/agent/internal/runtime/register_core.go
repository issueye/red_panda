package runtime

// register_core.go — P1 core plugin registration for the ToolRegistry.
//
// All 4 P1 plugin domains are registered here at Runtime construction time.
// Plugin handlers use the registry.HandlerFunc signature defined in
// modules/agent/internal/runtime/registry/registry.go.

import (
	"context"
	"fmt"

	"redpanda/agent/internal/runtime/hooks"
	"redpanda/agent/internal/runtime/registry"
	agenttools "redpanda/agent/internal/tools"
	coding "redpanda/agent/plugins/core/coding"
	orchestration "redpanda/agent/plugins/core/orchestration"
	state "redpanda/agent/plugins/core/state"
	workspace "redpanda/agent/plugins/core/workspace"
	"redpanda/protocol/methods"
	"redpanda/protocol/tools"
)

func initCorePlugins(rt *Runtime, reg *registry.Registry, bus *hooks.ExtensionBus) {
	// Register order determines the public tool order returned by Definitions().
	// (Locked by TestStableToolRegistryPreservesPublicOrder.)
	workspace.Register(reg, bus)
	coding.Register(reg, bus)
	orchestration.Register(reg, bus, orchestration.Dependencies{
		SkillRun:         runtimeOrchestrationExecutor(rt.executeSkillRun),
		WorkerDelegate:   runtimeOrchestrationExecutor(rt.executeWorkerDelegate),
		WorkerList:       runtimeOrchestrationExecutor(rt.executeWorkerList),
		WorkerCancel:     runtimeOrchestrationExecutor(rt.executeWorkerCancel),
		WorkerPoolStatus: runtimeOrchestrationExecutor(rt.executeWorkerPoolStatus),
		WorkerSend:       runtimeOrchestrationExecutor(rt.executeWorkerSend),
		WorkerReceive:    runtimeOrchestrationExecutor(rt.executeWorkerReceive),
	})
	state.Register(reg, bus, state.Dependencies{
		Todo:   rt.executeTodoRegistryTool,
		Memory: rt.executeMemoryRegistryTool,
	})
	registerSystemHooks(bus)
}

func registerSystemHooks(bus *hooks.ExtensionBus) {
	bus.MustRegister(hooks.HookPermissionCheck, hooks.HookOrderSystem, "builtin:permission", func(_ *hooks.HookContext, event map[string]any) *hooks.HookResult {
		action, _ := event["action"].(string)
		switch agenttools.ToolDecisionAction(action) {
		case agenttools.ToolDecisionAllow, agenttools.ToolDecisionRequirePermission:
			return nil
		case agenttools.ToolDecisionDeny:
			reason, _ := event["reason"].(string)
			if reason == "" {
				reason = "tool permission denied"
			}
			return &hooks.HookResult{Block: true, Reason: reason}
		default:
			return &hooks.HookResult{Block: true, Reason: fmt.Sprintf("invalid permission action %q", action)}
		}
	})
}

func runtimeOrchestrationExecutor(execute func(context.Context, agenttools.ToolRunContext, tools.Call) (string, error)) orchestration.HostExecutor {
	return func(ctx context.Context, tc *registry.ToolContext, call tools.Call) (string, error) {
		return execute(ctx, runtimeToolRunContext(tc), call)
	}
}

func (r *Runtime) executeTodoRegistryTool(ctx context.Context, tc *registry.ToolContext, name string, args map[string]any) (*tools.Result, error) {
	result, err := r.todoExecutor(ctx, methods.TodoToolExecuteParams{
		RunID: tc.RunID, SessionID: tc.SessionID, WorkspaceRoot: tc.WorkingDir,
		ToolCallID: tc.ToolCallID, ToolName: name, Arguments: args,
	})
	if err == nil {
		result.Output, err = requireRuntimeGatewayToolCompleted("todo", result.Status, result.Output)
	}
	return runtimeToolResult(name, result.Output, err), err
}

func (r *Runtime) executeMemoryRegistryTool(ctx context.Context, tc *registry.ToolContext, name string, args map[string]any) (*tools.Result, error) {
	result, err := r.executeMemoryTool(ctx, methods.MemoryToolExecuteParams{
		RunID: tc.RunID, SessionID: tc.SessionID, WorkspaceRoot: tc.WorkingDir,
		ToolCallID: tc.ToolCallID, ToolName: name, Arguments: args,
	})
	if err == nil {
		result.Output, err = requireRuntimeGatewayToolCompleted("memory", result.Status, result.Output)
	}
	return runtimeToolResult(name, result.Output, err), err
}

func runtimeToolRunContext(tc *registry.ToolContext) agenttools.ToolRunContext {
	return agenttools.ToolRunContext{
		WorkingDir: tc.WorkingDir, RunID: tc.RunID, SessionID: tc.SessionID,
		AssignmentID: tc.AssignmentID, WorkerID: tc.WorkerID, Reply: tc.Reply,
	}
}

func runtimeToolResult(name, output string, err error) *tools.Result {
	result := &tools.Result{Name: name, Status: tools.CallStatusCompleted, Output: output}
	if err != nil {
		result.Status = tools.CallStatusFailed
		result.Error = err.Error()
	}
	return result
}

func requireRuntimeGatewayToolCompleted(domain, status, output string) (string, error) {
	if status != "" && status != "completed" {
		return output, fmt.Errorf("%s tool returned status %s", domain, status)
	}
	return output, nil
}
