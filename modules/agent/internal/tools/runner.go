package tools

import (
	"context"
	"fmt"
	"os"
	"redpanda/agent/internal/skill"
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

func (runner ToolRunner) dispatchTool(ctx context.Context, runCtx ToolRunContext, call ptools.Call) (string, error) {
	switch call.Name {
	case "workspace.read_file":
		return runReadFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"))
	case "workspace.list":
		return runListWorkspace(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", defaultListDepth))
	case "workspace.stats":
		return runWorkspaceStats(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_depth", 4))
	case "workspace.grep":
		return runGrepWorkspace(runCtx.WorkingDir, StringArg(call.Arguments, "pattern"), StringArgDefault(call.Arguments, "path", "."), IntArg(call.Arguments, "max_matches", defaultGrepMatches))
	case "workspace.find_files":
		return runFindFiles(runCtx.WorkingDir, StringArgDefault(call.Arguments, "path", "."), StringArg(call.Arguments, "pattern"), IntArg(call.Arguments, "max_results", defaultFindFiles))
	case "workspace.read_files":
		return runReadFiles(runCtx.WorkingDir, stringListArg(call.Arguments, "paths"))
	case "workspace.write_file":
		return runWriteFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "content"))
	case "workspace.edit_file":
		return runEditFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
	case "workspace.diff_file":
		content, hasContent := stringArgPresent(call.Arguments, "content")
		return runDiffFile(runCtx.WorkingDir, StringArg(call.Arguments, "path"), content, hasContent, StringArg(call.Arguments, "old_text"), StringArg(call.Arguments, "new_text"), BoolArg(call.Arguments, "replace_all", false))
	case "workspace.apply_patch":
		return runApplyPatch(runCtx.WorkingDir, StringArg(call.Arguments, "patch"))
	case "git.status":
		return runGitStatus(ctx, runCtx.WorkingDir)
	case "git.diff":
		return runGitDiff(ctx, runCtx.WorkingDir, BoolArg(call.Arguments, "staged", false), StringArg(call.Arguments, "revision"), StringArg(call.Arguments, "path"))
	case "git.log":
		return runGitLog(ctx, runCtx.WorkingDir, IntArg(call.Arguments, "max_count", 10), StringArg(call.Arguments, "path"))
	case "git.show":
		return runGitShow(ctx, runCtx.WorkingDir, StringArg(call.Arguments, "revision"), StringArg(call.Arguments, "path"))
	case "shell.exec":
		return runShell(ctx, runCtx.WorkingDir, StringArg(call.Arguments, "command"))
	case "skill.list":
		return skill.RunList(runCtx.WorkingDir)
	case "skill.create":
		return skill.RunCreate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
	case "skill.update":
		return skill.RunUpdate(runCtx.WorkingDir, StringArg(call.Arguments, "name"), StringArg(call.Arguments, "description"), StringArg(call.Arguments, "instructions"))
	case "skill.delete":
		return skill.RunDelete(runCtx.WorkingDir, StringArg(call.Arguments, "name"))
	case "skill.run":
		if runner.SkillExecutor == nil {
			return "", fmt.Errorf("skill Worker executor is not available")
		}
		return runner.SkillExecutor(ctx, runCtx, call)
	case "worker.delegate":
		if runner.WorkerDelegate == nil {
			return "", fmt.Errorf("worker delegate executor is not available")
		}
		return runner.WorkerDelegate(ctx, runCtx, call)
	case "worker.list":
		if runner.WorkerList == nil {
			return "", fmt.Errorf("worker list executor is not available")
		}
		return runner.WorkerList(ctx, runCtx, call)
	case "worker.cancel":
		if runner.WorkerCancel == nil {
			return "", fmt.Errorf("worker cancel executor is not available")
		}
		return runner.WorkerCancel(ctx, runCtx, call)
	case "worker.pool_status":
		if runner.WorkerPoolStatus == nil {
			return "", fmt.Errorf("worker pool status executor is not available")
		}
		return runner.WorkerPoolStatus(ctx, runCtx, call)
	case "worker.send":
		if runner.WorkerSend == nil {
			return "", fmt.Errorf("worker send executor is not available")
		}
		return runner.WorkerSend(ctx, runCtx, call)
	case "worker.receive":
		if runner.WorkerReceive == nil {
			return "", fmt.Errorf("worker receive executor is not available")
		}
		return runner.WorkerReceive(ctx, runCtx, call)
	case "todo.write", "todo.list":
		return runner.runTodoTool(ctx, runCtx, call)
	case "goal.create", "goal.plan", "goal.observe", "goal.assess", "goal.finish", "goal.list":
		return runner.runGoalTool(ctx, runCtx, call)
	case "context.read", "context.search", "context.write", "context.replace", "context.delete":
		return runner.runContextTool(ctx, runCtx, call)
	case "memory.list", "memory.create", "memory.update", "memory.delete":
		return runner.runMemoryTool(ctx, runCtx, call)
	case "web.search":
		searchOpts := effectiveWebSearchOptions(runCtx)
		return runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebSearch(
				opCtx,
				StringArg(call.Arguments, "query"),
				effectiveWebResultCount(runCtx, IntArg(call.Arguments, "max_results", 0)),
				searchOpts,
			)
		})
	case "web.fetch":
		return runWebOp(ctx, func(opCtx context.Context) (string, error) {
			return runWebFetch(
				opCtx,
				StringArg(call.Arguments, "url"),
				effectiveWebFetchBytes(runCtx, IntArg(call.Arguments, "max_bytes", 0)),
				effectiveWebHTTPProxy(runCtx),
			)
		})
	default:
		if IsMCPToolName(call.Name) {
			if runner.MCPExecutor == nil {
				return "", fmt.Errorf("MCP executor is not available")
			}
			return runner.MCPExecutor(ctx, runCtx, call)
		}
		return "", fmt.Errorf("unknown tool %s", call.Name)
	}
}
