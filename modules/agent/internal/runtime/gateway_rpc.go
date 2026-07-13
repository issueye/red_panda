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

func (r *Runtime) executeMemoryTool(ctx context.Context, req methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error) {
	var result methods.MemoryToolExecuteResult
	if err := r.callGatewayResult(ctx, methods.MemoryToolExecute, req, &result); err != nil {
		return methods.MemoryToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) todoExecutor(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	result, err := r.executeTodoTool(ctx, req)
	if err != nil {
		return result, err
	}
	name := strings.TrimSpace(req.ToolName)
	if name == "todo.write" || name == "todo_write" {
		r.setRunTodos(req.RunID, result.Items)
	}
	return result, nil
}

func (r *Runtime) executeTodoTool(ctx context.Context, req methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error) {
	var result methods.TodoToolExecuteResult
	if err := r.callGatewayResult(ctx, methods.TodoToolExecute, req, &result); err != nil {
		return methods.TodoToolExecuteResult{}, err
	}
	return result, nil
}

func (r *Runtime) goalExecutor(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	result, err := r.executeGoalTool(ctx, req)
	if err != nil {
		return result, err
	}
	name := strings.TrimSpace(req.ToolName)
	if name != "goal.list" && name != "segment_end" {
		r.applyGoalToolResult(req.RunID, name, result)
	} else if name == "segment_end" {
		r.applyGoalToolResult(req.RunID, name, result)
	}
	return result, nil
}

func (r *Runtime) executeGoalTool(ctx context.Context, req methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error) {
	var result methods.GoalToolExecuteResult
	if err := r.callGatewayResult(ctx, methods.GoalToolExecute, req, &result); err != nil {
		return methods.GoalToolExecuteResult{}, err
	}
	return result, nil
}

// executeContextTool 将 context.*（目标暂存区）工具调用转发至 Gateway。
// 根运行和专业子代理均可使用它，因为 context.* 被有意排除在 Worker.RunDenylist 之外。
func (r *Runtime) executeContextTool(ctx context.Context, req methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error) {
	var result methods.ContextToolExecuteResult
	if err := r.callGatewayResult(ctx, methods.ContextToolExecute, req, &result); err != nil {
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
