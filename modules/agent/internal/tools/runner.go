package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"redpanda/protocol/methods"
	ptools "redpanda/protocol/tools"
)

type MemoryToolExecutor func(context.Context, methods.MemoryToolExecuteParams) (methods.MemoryToolExecuteResult, error)
type TodoToolExecutor func(context.Context, methods.TodoToolExecuteParams) (methods.TodoToolExecuteResult, error)
type GoalToolExecutor func(context.Context, methods.GoalToolExecuteParams) (methods.GoalToolExecuteResult, error)
type ContextToolExecutor func(context.Context, methods.ContextToolExecuteParams) (methods.ContextToolExecuteResult, error)
type SkillRunExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)
type WorkerToolExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)
type MCPToolExecutor func(context.Context, ToolRunContext, ptools.Call) (string, error)

type ToolRunner struct {
	MemoryExecutor   MemoryToolExecutor
	TodoExecutor     TodoToolExecutor
	GoalExecutor     GoalToolExecutor
	ContextExecutor  ContextToolExecutor
	SkillExecutor    SkillRunExecutor
	WorkerDelegate   WorkerToolExecutor
	WorkerList       WorkerToolExecutor
	WorkerCancel     WorkerToolExecutor
	WorkerPoolStatus WorkerToolExecutor
	WorkerSend       WorkerToolExecutor
	WorkerReceive    WorkerToolExecutor
	MCPExecutor      MCPToolExecutor
}

type ToolRunContext struct {
	WorkingDir   string
	RunID        string
	SessionID    string
	AssignmentID string
	WorkerID     string
	Reply        *methods.ReplyParams
}

type ToolInvocation struct {
	Call ptools.Call
}

// slashToolsEnabled 控制 /read、/shell 等临时触发器。默认关闭，
// 以免将普通聊天文本解析为工具（检查项 R6）。本地冒烟脚本和单元测试可设置
// RED_PANDA_SLASH_TOOLS=1 启用。
func slashToolsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RED_PANDA_SLASH_TOOLS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (ToolRunner) Parse(text string, runID string) (ToolInvocation, bool) {
	if !slashToolsEnabled() {
		return ToolInvocation{}, false
	}
	line := strings.TrimSpace(text)
	switch {
	case strings.HasPrefix(line, "/tool read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool read "))), true
	case strings.HasPrefix(line, "/read "):
		return readInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/read "))), true
	case line == "/tool list" || strings.HasPrefix(line, "/tool list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool list"))), true
	case line == "/list" || strings.HasPrefix(line, "/list "):
		return listInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/list"))), true
	case strings.HasPrefix(line, "/tool grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool grep "))), true
	case strings.HasPrefix(line, "/grep "):
		return grepInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/grep "))), true
	case strings.HasPrefix(line, "/tool shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool shell "))), true
	case strings.HasPrefix(line, "/shell "):
		return shellInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/shell "))), true
	case strings.HasPrefix(line, "/tool write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool write "))), true
	case strings.HasPrefix(line, "/write "):
		return writeInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/write "))), true
	case strings.HasPrefix(line, "/tool edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool edit "))), true
	case strings.HasPrefix(line, "/edit "):
		return editInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/edit "))), true
	case strings.HasPrefix(line, "/tool diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool diff "))), true
	case strings.HasPrefix(line, "/diff "):
		return diffInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/diff "))), true
	case strings.HasPrefix(line, "/tool patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/tool patch "))), true
	case strings.HasPrefix(line, "/patch "):
		return patchInvocation(runID, strings.TrimSpace(strings.TrimPrefix(line, "/patch "))), true
	default:
		return ToolInvocation{}, false
	}
}

func (runner ToolRunner) InvocationFromCall(runID string, index int, call ptools.Call, extra ...ptools.Definition) (ToolInvocation, error) {
	if call.Name == "" {
		return ToolInvocation{}, fmt.Errorf("tool name is required")
	}
	if call.ID == "" {
		call.ID = fmt.Sprintf("tool_%s_model_%d", runID, index+1)
	}
	definitions := runner.AvailableTools()
	if len(extra) > 0 {
		definitions = append(append([]ptools.Definition{}, definitions...), extra...)
	}
	for _, definition := range definitions {
		if definition.Name == call.Name {
			if call.DisplayName == "" {
				call.DisplayName = definition.DisplayName
			}
			if call.Risk == "" {
				call.Risk = definition.Risk
			}
			if call.Arguments == nil {
				call.Arguments = map[string]any{}
			}
			return ToolInvocation{Call: call}, nil
		}
	}
	// MCP 工具可能在 AvailableTools 快照之后注册，最后按前缀兜底接受。
	if IsMCPToolName(call.Name) {
		if call.DisplayName == "" {
			call.DisplayName = call.Name
		}
		if call.Risk == "" {
			call.Risk = ptools.RiskHigh
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		return ToolInvocation{Call: call}, nil
	}
	return ToolInvocation{}, fmt.Errorf("unknown tool %s", call.Name)
}

func (runner ToolRunner) Run(ctx context.Context, workingDir string, invocation ToolInvocation) (ptools.Result, string) {
	return runner.RunWithContext(ctx, ToolRunContext{WorkingDir: workingDir}, invocation)
}

func (runner ToolRunner) RunWithContext(ctx context.Context, runCtx ToolRunContext, invocation ToolInvocation) (ptools.Result, string) {
	started := time.Now()
	call := invocation.Call
	// Normalize legacy aliases once so timeout class + dispatch share one name.
	if canon := CanonicalToolName(call.Name); canon != "" {
		call.Name = canon
	}
	result := ptools.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Status:     ptools.CallStatusCompleted,
	}

	output, err := runBounded(ctx, toolTimeoutFor(call.Name), func(toolCtx context.Context) (string, error) {
		return runner.dispatchTool(toolCtx, runCtx, call)
	})

	result.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		result.Status = ptools.CallStatusFailed
		result.Error = err.Error()
		// 始终输出标准封装（即使失败），让 UI 和模型共享同一架构。
		result.Output = StandardizeToolOutput(call.Name, output, err, result.DurationMS)
		return result, result.Output
	}
	result.Output = StandardizeToolOutput(call.Name, output, nil, result.DurationMS)
	return result, result.Output
}

// toolTimeoutFor 返回工具的硬性时限；工具已自行管理期限时返回 0，
// 例如 shell、web、Worker 和 skill。
func toolTimeoutFor(name string) time.Duration {
	name = CanonicalToolName(name)
	if timeout, ok := stableToolTimeoutFor(name); ok {
		return timeout
	}
	// MCP 工具在内部管理启动、初始化和调用超时（文档 19）。
	if IsMCPToolName(name) {
		return 0
	}
	return defaultLocalToolTimeout
}

// runBounded 执行 fn 并在超时时快速失败，避免单个挂起工具冻结整个代理回合。
// timeout<=0 表示不施加外层限制，适用于自行管理期限的工具。
func runBounded(ctx context.Context, timeout time.Duration, fn func(context.Context) (string, error)) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return fn(ctx)
	}

	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type outcome struct {
		output string
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		out, err := fn(toolCtx)
		done <- outcome{output: out, err: err}
	}()

	select {
	case res := <-done:
		return res.output, res.err
	case <-toolCtx.Done():
		// 若工具在截止瞬间完成，优先返回已完成的结果。
		select {
		case res := <-done:
			return res.output, res.err
		default:
		}
		if err := toolCtx.Err(); err != nil && ctx.Err() != nil {
			return "", err
		}
		return "", fmt.Errorf("tool timed out after %s (operation did not finish; check working_dir, path, or network mounts)", timeout)
	}
}

// dispatchTool routes a call to the domain group that owns it (docs/47 Wave C).
// Groups keep exact tool-name switches so unknown names still fail closed.
func (runner ToolRunner) dispatchTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	if out, handled, err := dispatchLocalTool(ctx, runCtx, call); handled {
		return out, err
	}
	if out, handled, err := runner.dispatchOrchestrationTool(ctx, runCtx, call); handled {
		return out, err
	}
	if out, handled, err := runner.dispatchStateTool(ctx, runCtx, call); handled {
		return out, err
	}
	if out, handled, err := dispatchWebTool(ctx, runCtx, call); handled {
		return out, err
	}
	if IsMCPToolName(call.Name) {
		if runner.MCPExecutor == nil {
			return "", fmt.Errorf("MCP executor is not available")
		}
		return runner.MCPExecutor(ctx, runCtx, call)
	}
	return "", fmt.Errorf("unknown tool %s", call.Name)
}
