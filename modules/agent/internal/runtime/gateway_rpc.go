package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"redpanda/protocol/jsonrpc"
	"redpanda/protocol/methods"
)

// callGatewayResult 调用 Gateway RPC，并将响应解组到调用方提供的结果对象。
// 统一该流程可避免各类 Gateway 工具各自处理传输和解组错误。
func (r *Runtime) callGatewayResult(ctx context.Context, method string, params any, out any) error {
	raw, err := r.callGateway(ctx, method, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return err
	}
	return nil
}

// callStateTool invokes the unified state.tool.execute RPC (docs/41 W2-1/W2-3)
// and unmarshals into a domain-specific result type. Domain services still
// return Memory/Todo/Goal/Context shapes so typed fields (Items, Goal, Notes)
// remain available without a payload migration.
func (r *Runtime) callStateTool(ctx context.Context, domain string, runID, sessionID, workspaceRoot, toolCallID, toolName string, arguments map[string]any, out any) error {
	params := methods.NewStateToolParams(domain, runID, sessionID, workspaceRoot, toolCallID, toolName, arguments)
	return r.callGatewayResult(ctx, methods.StateToolExecute, params, out)
}

func (r *Runtime) executeMemoryTool(ctx context.Context, req methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	var result methods.MemoryToolExecuteResult
	if err := r.callStateTool(ctx, methods.StateToolDomainMemory, req.RunID, req.SessionID, req.WorkspaceRoot, req.ToolCallID, req.ToolName, req.Arguments, &result); err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) todoExecutor(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	result, err := r.executeTodoTool(ctx, req)
	if err != nil {
		return result, err
	}
	if methods.IsTodoWriteTool(req.ToolName) {
		r.setRunTodos(req.RunID, result.Items)
	}
	return result, nil
}

func (r *Runtime) executeTodoTool(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	var result methods.TodoToolExecuteResult
	if err := r.callStateTool(ctx, methods.StateToolDomainTodo, req.RunID, req.SessionID, req.WorkspaceRoot, req.ToolCallID, req.ToolName, req.Arguments, &result); err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) goalExecutor(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	result, err := r.executeGoalTool(ctx, req)
	if err != nil {
		return result, err
	}
	// goal.list is read-only and often returns multiple goals without a single
	// snapshot to bind; every mutating tool (including internal segment_end)
	// refreshes the per-run Goal projection when result.Goal is set.
	name := strings.TrimSpace(req.ToolName)
	if name != "goal.list" {
		r.applyGoalToolResult(req.RunID, name, result)
	}
	return result, nil
}

func (r *Runtime) executeGoalTool(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	var result methods.GoalToolExecuteResult
	if err := r.callStateTool(ctx, methods.StateToolDomainGoal, req.RunID, req.SessionID, req.WorkspaceRoot, req.ToolCallID, req.ToolName, req.Arguments, &result); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	return result, nil
}

// executeContextTool 将 context.*（目标暂存区）工具调用转发至 Gateway。
// 根运行和 specialist Worker 均可使用它，因为 context.* 被有意排除在 Worker.RunDenylist 之外。
func (r *Runtime) executeContextTool(ctx context.Context, req methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	var result methods.ContextToolExecuteResult
	if err := r.callStateTool(ctx, methods.StateToolDomainContext, req.RunID, req.SessionID, req.WorkspaceRoot, req.ToolCallID, req.ToolName, req.Arguments, &result); err != nil {
		return methods.ContextToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) callGateway(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := r.nextGatewayRequestID()
	req, err := jsonrpc.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan jsonrpc.Response, 1)
	r.mu.Lock()
	r.gatewayPending[id] = ch
	_, err = fmt.Fprintln(r.out, string(raw))
	r.mu.Unlock()
	if err != nil {
		r.removeGatewayPending(id)
		return nil, err
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		r.removeGatewayPending(id)
		return nil, ctx.Err()
	case <-timer.C:
		r.removeGatewayPending(id)
		return nil, fmt.Errorf("gateway request %s timed out", method)
	case resp, ok := <-ch:
		if !ok {
			return nil, errors.New("gateway response channel closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("gateway error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (r *Runtime) nextGatewayRequestID() jsonrpc.ID {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextGatewayID++
	return jsonrpc.ID(fmt.Sprintf("rt_%d", r.nextGatewayID))
}

func (r *Runtime) removeGatewayPending(id jsonrpc.ID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.gatewayPending, id)
}
