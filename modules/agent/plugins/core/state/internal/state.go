package internal

import (
	"context"
	"fmt"
	"strings"

	"redpanda/agent/internal/runtime/registry"
	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

// gatewayRPCStub returns a consistent placeholder result indicating that the
// Gateway RPC wire-up is not yet connected for todo/memory tools.
//
// Phase 1-2 requirement: new handlers must compile cleanly even though the
// Registry-backed ToolContext does not expose the RPC executor interface that
// the legacy ToolRunner.MemoryExecutor / ToolRunner.TodoExecutor used to carry.
// These will be replaced with real Gateway gRPC/HTTP calls in Phase 3/4
// (docs/plans/2026-07-19-convergence-wave.md Wave C).
func gatewayRPCStub(domain string, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	result := ptools.Result{
		Name:   fmt.Sprintf("%s.*", domain),
		Status: ptools.CallStatusFailed,
		Output: fmt.Sprintf(`{"domain":"%s","run_id":"%s","session_id":"%s"}`,
			domain, tc.RunID, tc.SessionID),
		Error: "gateway RPC not yet wired",
	}
	return &result, fmt.Errorf("gateway RPC not yet wired")
}

// ---------------------------------------------------------------------------
// TODO tool handlers
// ---------------------------------------------------------------------------

// HandlerTodoWrite dispatches todo.write through the Gateway.
// Original behaviour (tools/gateway.go):
//  1. Check runner.TodoExecutor != nil
//  2. Call TodoExecutor(ctx, methods.TodoToolExecuteParams{...})
//  3. requireGatewayToolCompleted("todo", result.Status, result.Output)
//
// Wire-up for this handler (via Gateway state.tool.execute RPC):
//
//	result, err := gatewayClient.StateToolExecute(ctx, methods.StateToolExecuteParams{
//	    Domain:        methods.StateToolDomainTodo,
//	    RunID:         tc.RunID,
//	    SessionID:     tc.SessionID,
//	    WorkspaceRoot: tc.WorkingDir,
//	    ToolCallID:    tc.ToolCallID,
//	    ToolName:      "todo.write",
//	    Arguments:     args,
//	})
//	status, output, err := requireGatewayToolCompleted("todo", result.Status, result.Output)
//	return WrapResult("todo.write", output, err)
func HandlerTodoWrite(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	if args == nil || len(args) == 0 {
		return &ptools.Result{
			Name:   "todo.write",
			Status: ptools.CallStatusFailed,
			Error:  "arguments are required",
		}, fmt.Errorf("arguments are required")
	}
	_ = ctx
	return gatewayRPCStub("todo", tc, args)
}

// HandlerTodoList dispatches todo.list through the Gateway.
// Wire-up (same pattern as HandlerTodoWrite):
//
//	result, err := gatewayClient.StateToolExecute(ctx, methods.StateToolExecuteParams{
//	    Domain:        methods.StateToolDomainTodo,
//	    ToolName:      "todo.list", ...
//	})
func HandlerTodoList(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	_ = args
	return gatewayRPCStub("todo", tc, args)
}

// ---------------------------------------------------------------------------
// Memory tool handlers
// ---------------------------------------------------------------------------

// HandlerMemoryList dispatches memory.list through the Gateway.
func HandlerMemoryList(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	_ = args
	return gatewayRPCStub("memory", tc, args)
}

// HandlerMemoryCreate dispatches memory.create through the Gateway.
func HandlerMemoryCreate(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	if args == nil || len(args) == 0 {
		return &ptools.Result{
			Name:   "memory.create",
			Status: ptools.CallStatusFailed,
			Error:  "arguments are required",
		}, fmt.Errorf("arguments are required")
	}
	_ = ctx
	return gatewayRPCStub("memory", tc, args)
}

// HandlerMemoryUpdate dispatches memory.update through the Gateway.
func HandlerMemoryUpdate(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	_ = ctx
	return gatewayRPCStub("memory", tc, args)
}

// HandlerMemoryDelete dispatches memory.delete through the Gateway.
func HandlerMemoryDelete(ctx context.Context, tc *registry.ToolContext, args map[string]any) (*ptools.Result, error) {
	if args == nil || len(args) == 0 {
		return &ptools.Result{
			Name:   "memory.delete",
			Status: ptools.CallStatusFailed,
			Error:  "arguments are required",
		}, fmt.Errorf("arguments are required")
	}
	_ = ctx
	return gatewayRPCStub("memory", tc, args)
}

// ---------------------------------------------------------------------------
// Helpers used during runtime integration (Phase 3/4).
// ---------------------------------------------------------------------------

// requireGatewayToolCompleted maps Gateway status into a tool error while still
// returning the output body for the model (copied from tools/gateway.go).
func requireGatewayToolCompleted(domain, status, output string) (string, error) {
	if status != "" && status != "completed" {
		return output, fmt.Errorf("%s tool returned status %s", domain, status)
	}
	return output, nil
}

// stateToolExecuteParams builds a unified Gateway envelope from a ToolContext.
func stateToolExecuteParams(domain string, tc *registry.ToolContext, args map[string]any, toolCallID string) methods.StateToolExecuteParams {
	return methods.StateToolExecuteParams{
		Domain:        domain,
		RunID:         strings.TrimSpace(tc.RunID),
		SessionID:     strings.TrimSpace(tc.SessionID),
		WorkspaceRoot: strings.TrimSpace(tc.WorkingDir),
		ToolCallID:    toolCallID,
		Arguments:     args,
	}
}
