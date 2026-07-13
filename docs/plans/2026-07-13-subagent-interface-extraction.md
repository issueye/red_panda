# Subagent Interface Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 通过接口倒置将通用子代理状态、输出收集和进程执行流程集中到 `modules/agent/internal/subagent`，同时保留 Runtime 对 Goal、事件流和工具入口的所有权。

**Architecture:** `internal/subagent` 定义并消费最小端口接口，提供 `Registry`、`Capture` 和 `Coordinator`；`internal/runtime` 负责构造完整的 `RunSpec`，并通过轻量适配器提供进程与事件能力。依赖方向始终保持 `runtime -> subagent -> protocol`，禁止 `subagent` 导入 `runtime`。

**Tech Stack:** Go 1.25、stdio/IPC JSON-RPC、`context`、进程池、Go table-driven tests、`go test -race`

**Status:** Complete，2026-07-13 已通过 Agent 全量测试、`go vet` 和关键包 race 测试。

---

## 1. 背景与范围

当前子代理代码分为两层：

- `modules/agent/internal/subagent`：进程通信、进程池、预算和拒绝策略。
- `modules/agent/internal/runtime/subagent*.go`：运行状态、事件桥接、Goal 专家配置、工具入口和执行编排。

这种分层方向正确，但 Runtime 中仍保存了可以复用的状态机与输出收集逻辑。本计划只抽离不依赖 Goal 和 Runtime 私有状态的部分，不改变现有用户行为。

### 1.1 成功标准

1. `internal/subagent` 不导入 `internal/runtime`、`internal/tools` 或 Gateway 包。
2. `Runtime` 不再直接保存 `map[string]*runtimeSubAgent`。
3. 子代理输出收集和报告校验不再定义在 Runtime 包中。
4. 普通 `subagent.run` 的进程获取、启动、收集、释放和状态流转由 `subagent.Coordinator` 统一执行。
5. Goal 专家解析、阶段校验和 `ReplyParams` 构造仍由 Runtime 负责。
6. 工具名称、JSON-RPC DTO、事件载荷和 UI 可见行为保持兼容。
7. `go test ./...` 与 `go test -race ./internal/subagent ./internal/runtime` 通过。

### 1.2 非目标

- 不修改 `subagent.run`、`subagent.list`、`subagent.cancel`、`subagent.reset` 的工具架构。
- 不修改 Gateway 或 Desktop 协议。
- 不调整 Goal 专家提示词和回合预算公式。
- 不把 `skill_subagent.go` 移入 `subagent` 包；技能只在后续按需要复用 Coordinator。
- 不实现新的操作系统沙箱或远程执行后端。
- 不引入通用事件总线、依赖注入框架或巨大 `SubagentHost` 接口。

## 2. 架构决策

### ADR-1：接口由 `subagent` 消费方定义

**决定：** 在 `internal/subagent` 中定义 `ProcessProvider` 与 `EventSink`，由 Runtime 适配器实现。

**原因：** Coordinator 知道自己真正需要的最小能力，接口不会暴露整个 Runtime，也不会形成反向依赖。

**拒绝方案：** 定义包含注册、Goal、Gateway、事件、进程池等十几个方法的 `RuntimeHost`。该方案只是隐藏耦合，不能真正建立包边界。

### ADR-2：Runtime 构造完整 ChildParams

**决定：** Goal 专家解析、阶段校验、上下文注入和工具权限处理继续发生在 Runtime；Coordinator 接收已经准备好的 `RunSpec.ChildParams`。

**原因：** `subagent` 包不应理解 Goal、Memory、Todo 或 Runtime 的会话策略。

### ADR-3：Registry 使用具体类型

**决定：** `Registry` 是并发安全的具体类型，不为它额外定义接口。

**原因：** 当前只有一个内存实现，没有替换需求。为测试而引入接口会增加无价值的间接层。

### ADR-4：迁移期间保持事件兼容

**决定：** Runtime 的 `EventSink` 适配器继续调用现有 `emitAgentEvent`，Coordinator 不自行构造 Runtime 顶层事件序列。

**原因：** 事件流是 UI 和父运行的外部契约，抽离不应改变事件顺序或载荷。

## 3. 目标结构

```text
modules/agent/internal/subagent/
  process_iface.go     # 现有 Process 接口
  process.go           # 现有 stdio/IPC 实现
  pool.go              # 现有进程池
  policy.go            # 现有预算和报告策略
  capture.go           # 新增：输出和失败诊断收集
  registry.go          # 新增：状态注册、完成、取消、查询和移除
  coordinator.go       # 新增：通用进程执行生命周期

modules/agent/internal/runtime/
  subagent_run.go      # 构造 RunSpec、Goal 配置、调用 Coordinator
  subagent_adapter.go  # 新增：ProcessProvider 和 EventSink 适配器
  subagent_control.go  # 工具参数解析和 JSON 返回
  subagent_runtime.go  # 仅保留 planner 与 JSON-RPC 入口，随后按职责改名
  subagent_manager.go  # tools.SubagentManager 的薄适配层
```

## 4. 接口与数据结构草案

```go
package subagent

type ReleaseFunc func(reusable bool)

type ProcessProvider interface {
	Acquire(
		ctx context.Context,
		params methods.ReplyParams,
		subAgentID string,
		backend string,
	) (Process, ReleaseFunc, error)
}

type EventSink interface {
	EmitStatus(ctx context.Context, spec RunSpec, status StatusEvent) error
	Bridge(ctx context.Context, spec RunSpec, child events.Envelope) error
}

type RunSpec struct {
	RootRunID   string
	SubAgentID  string
	Name        string
	DisplayName string
	Backend     string
	Task        string
	FileCount   int
	ScopePath   string
	MaxTurns    int
	Parent      methods.ReplyParams
	Child       methods.ReplyParams
}

type RunResult struct {
	Text             string
	FinishStatus     string
	RecoveredFallback bool
}

type Coordinator struct {
	processes ProcessProvider
	events    EventSink
	registry  *Registry
}
```

接口不得包含 `*runtime.Runtime`，也不得包含 Goal 专用方法。

## 5. 实施任务

### Task 1：提取 Capture

**Files:**

- Create: `modules/agent/internal/subagent/capture.go`
- Create: `modules/agent/internal/subagent/capture_test.go`
- Modify: `modules/agent/internal/runtime/subagent_capture.go`
- Modify: `modules/agent/internal/runtime/subagent_tools_test.go`

**Step 1：编写失败测试**

在 `capture_test.go` 中迁移并改写以下测试：

```go
func TestCaptureBuildsActionableFailure(t *testing.T) {
	capture := NewCapture(CaptureOptions{
		MaxTurns: 16,
		Backend:  "process_pool",
		Name:     "desktop",
		Task:     "analyze desktop frontend modules",
	})
	capture.Observe(events.Envelope{
		Type: events.EventToolFailed,
		Payload: map[string]any{
			"tool_name": "workspace.read_file",
			"error":     "file not found",
		},
	})

	err := capture.FailureError("subagent returned an empty final report")
	if !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("missing failure detail: %v", err)
	}
}
```

同时覆盖：消息拼接、reasoning 限长、finish 文本回退、recovered 标记、工具调用统计。

**Step 2：运行测试并确认失败**

Run:

```powershell
cd modules/agent
go test ./internal/subagent -run Capture -count=1
```

Expected: FAIL，提示 `NewCapture` 或 `CaptureOptions` 未定义。

**Step 3：实现最小 Capture**

将 `subagentRunCapture` 的纯逻辑移动为导出的 `subagent.Capture`。将字段初始化收敛到 `NewCapture(CaptureOptions)`，保留以下方法：

```go
func (c *Capture) Observe(event events.Envelope)
func (c *Capture) FinalText() string
func (c *Capture) FailureError(headline string) error
func (c *Capture) FinishStatus() string
func (c *Capture) RecoveredFallback() bool
```

不要把 `failWorkerSubAgent` 移入该文件，因为它依赖 Runtime 事件发送。

**Step 4：更新 Runtime 调用点**

在 `subagent_run.go` 使用 `subagent.NewCapture`。删除 Runtime 中已经迁移的类型和测试，只保留失败事件适配逻辑。

**Step 5：运行测试**

Run:

```powershell
go test ./internal/subagent ./internal/runtime -run "Capture|SubagentRun" -count=1
```

Expected: PASS。

**Step 6：提交**

```powershell
git add modules/agent/internal/subagent/capture.go modules/agent/internal/subagent/capture_test.go modules/agent/internal/runtime/subagent_capture.go modules/agent/internal/runtime/subagent_run.go modules/agent/internal/runtime/subagent_tools_test.go
git commit -m "refactor(agent): move subagent capture into core package"
```

### Task 2：提取并发安全 Registry

**Files:**

- Create: `modules/agent/internal/subagent/registry.go`
- Create: `modules/agent/internal/subagent/registry_test.go`
- Modify: `modules/agent/internal/runtime/runtime.go`
- Modify: `modules/agent/internal/runtime/subagent_manager.go`
- Modify: `modules/agent/internal/runtime/subagent_runtime.go`
- Modify: `modules/agent/internal/runtime/subagent_control.go`

**Step 1：编写状态转换测试**

覆盖以下不变量：

```go
func TestRegistryTerminalStateIsSticky(t *testing.T) {
	registry := NewRegistry()
	registry.Register(Registration{SubAgentID: "s1", RootRunID: "r1"})
	if !registry.Finish("r1", "s1", "completed", "done", "") {
		t.Fatal("expected first finish to succeed")
	}
	if registry.Finish("r1", "s1", "failed", "late failure", "boom") {
		t.Fatal("terminal state must not be overwritten")
	}
}
```

另行测试：错误 root run 不能读取或取消、Cancel 只调用一次、ForceFinish 可用于 reset、Remove 后查询为空、List 返回值不可修改内部状态。

**Step 2：确认测试失败**

```powershell
go test ./internal/subagent -run Registry -count=1
```

Expected: FAIL，提示 `NewRegistry` 未定义。

**Step 3：实现 Registry**

`Registry` 内部持有独立互斥锁和状态映射：

```go
type Registry struct {
	mu     sync.Mutex
	states map[string]*State
}
```

提供 `Register`、`Finish`、`ForceFinish`、`Lookup`、`List`、`Cancel`、`Remove`。所有返回的 `methods.SubAgentRecord` 必须复制，不能暴露内部指针。

**Step 4：替换 Runtime 字段**

将：

```go
subagents map[string]*runtimeSubAgent
```

替换为：

```go
subagents *subagent.Registry
```

在 `New` 中调用 `subagent.NewRegistry()`。删除 `runtimeSubAgent`，让 Runtime 的辅助方法暂时委托 Registry，保持调用点不变。

**Step 5：运行并发测试**

```powershell
go test -race ./internal/subagent ./internal/runtime -run "Registry|SubAgentQueryAndCancel|SubagentReset" -count=1
```

Expected: PASS，无 race 报告。

**Step 6：提交**

```powershell
git add modules/agent/internal/subagent/registry.go modules/agent/internal/subagent/registry_test.go modules/agent/internal/runtime/runtime.go modules/agent/internal/runtime/subagent_manager.go modules/agent/internal/runtime/subagent_runtime.go modules/agent/internal/runtime/subagent_control.go
git commit -m "refactor(agent): centralize subagent lifecycle registry"
```

### Task 3：定义 Coordinator 端口契约

**Files:**

- Create: `modules/agent/internal/subagent/coordinator.go`
- Create: `modules/agent/internal/subagent/coordinator_test.go`

**Step 1：编写编译期接口测试和生命周期测试**

创建 fake `ProcessProvider`、fake `EventSink` 和 fake `Process`，断言：

1. Acquire 收到 ChildParams 与 backend。
2. running 状态先于 child event。
3. 成功时以 `reusable=true` 释放。
4. 启动失败或无可用报告时以 `reusable=false` 释放。
5. context 取消时 Registry 最终为 cancelled。

**Step 2：确认测试失败**

```powershell
go test ./internal/subagent -run Coordinator -count=1
```

Expected: FAIL，提示 Coordinator 契约尚未定义。

**Step 3：定义最小端口和 DTO**

实现第 4 节中的 `ProcessProvider`、`EventSink`、`RunSpec`、`RunResult`、`StatusEvent`。增加构造函数：

```go
func NewCoordinator(processes ProcessProvider, events EventSink, registry *Registry) (*Coordinator, error)
```

依赖为 nil 时返回明确错误，不在运行中触发 panic。

**Step 4：实现 Coordinator.Run**

```go
func (c *Coordinator) Run(ctx context.Context, spec RunSpec) (RunResult, error)
```

实现严格顺序：注册、running 事件、Acquire、Start、Capture、校验、完成事件、Release。用 `defer` 保证每条路径只释放一次。

**Step 5：运行测试**

```powershell
go test -race ./internal/subagent -run Coordinator -count=1
```

Expected: PASS。

**Step 6：提交**

```powershell
git add modules/agent/internal/subagent/coordinator.go modules/agent/internal/subagent/coordinator_test.go
git commit -m "feat(agent): add interface-driven subagent coordinator"
```

### Task 4：实现 Runtime 适配器

**Files:**

- Create: `modules/agent/internal/runtime/subagent_adapter.go`
- Create: `modules/agent/internal/runtime/subagent_adapter_test.go`
- Modify: `modules/agent/internal/runtime/runtime.go`

**Step 1：编写适配器测试**

测试 `runtimeProcessProvider` 根据 backend 做出以下选择：

- `process_pool` 调用 `ProcessPool.Acquire`。
- `runtime_process` 调用 `newProcessSubAgent`，Release 时关闭进程。
- 未知 backend 返回错误，而不是静默回退。

测试 `runtimeEventSink`：status 事件保留现有 `subagent_id`、`name`、`display_name`、`backend`、`goal_phase`；Bridge 重写 stream ID 并忽略 child finish。

**Step 2：确认测试失败**

```powershell
go test ./internal/runtime -run SubagentAdapter -count=1
```

Expected: FAIL，提示适配器类型未定义。

**Step 3：实现适配器**

适配器可以保存 `*Runtime`，但该类型只能存在于 Runtime 包：

```go
type runtimeProcessProvider struct{ runtime *Runtime }
type runtimeEventSink struct{ runtime *Runtime }
```

它们实现 `subagent.ProcessProvider` 和 `subagent.EventSink`。不要在 `subagent` 包中添加 Runtime 特例。

**Step 4：初始化 Coordinator**

在 `Runtime` 中增加：

```go
subagentCoordinator *subagent.Coordinator
```

在进程池和 Registry 初始化后构造 Coordinator。

**Step 5：运行测试**

```powershell
go test ./internal/runtime -run "SubagentAdapter|SubagentProcessPool" -count=1
```

Expected: PASS。

**Step 6：提交**

```powershell
git add modules/agent/internal/runtime/subagent_adapter.go modules/agent/internal/runtime/subagent_adapter_test.go modules/agent/internal/runtime/runtime.go
git commit -m "refactor(agent): adapt runtime to subagent coordinator ports"
```

### Task 5：迁移普通 subagent.run 编排

**Files:**

- Modify: `modules/agent/internal/runtime/subagent_run.go`
- Modify: `modules/agent/internal/runtime/subagent_capture.go`
- Modify: `modules/agent/internal/runtime/subagent_tools_test.go`
- Modify: `modules/agent/internal/subagent/coordinator_test.go`

**Step 1：增加行为锁定测试**

在现有 Runtime 测试中明确断言：

- Child conversation 和 memory 被清空。
- Goal 专家仍获得 SpecialistContext 和阶段约束。
- 子代理事件顺序不变。
- 可用最终文本仍作为 `subagent.run` 工具结果返回。
- tool-call-only、recovered fallback 和空结果仍失败。

**Step 2：运行测试建立绿灯基线**

```powershell
go test ./internal/runtime -run "RuntimeSubagentRun|GoalSpecialist|SubagentRunCapture" -count=1
```

Expected: PASS。若失败，先修复测试环境，不开始迁移。

**Step 3：将 executeSubagentRun 收敛为规格构造器**

保留以下逻辑：参数解析、名称清理、Goal 专家解析、阶段校验、文件统计、预算计算、ChildParams 构造、Goal 配置。

删除以下重复逻辑：注册、Acquire、Start、Capture、Release、finish 状态流转。改为：

```go
result, err := r.subagentCoordinator.Run(childCtx, spec)
if err != nil {
	return "", err
}
return strings.TrimSpace(agenttools.TruncateToolOutput(result.Text)), nil
```

**Step 4：运行聚焦测试**

```powershell
go test -race ./internal/subagent ./internal/runtime -run "Coordinator|RuntimeSubagentRun|GoalSpecialist" -count=1
```

Expected: PASS。

**Step 5：运行完整 Agent 测试**

```powershell
go test ./...
```

Expected: PASS。

**Step 6：提交**

```powershell
git add modules/agent/internal/runtime/subagent_run.go modules/agent/internal/runtime/subagent_capture.go modules/agent/internal/runtime/subagent_tools_test.go modules/agent/internal/subagent/coordinator_test.go
git commit -m "refactor(agent): run worker subagents through coordinator"
```

### Task 6：迁移控制面到 Registry

**Files:**

- Modify: `modules/agent/internal/runtime/subagent_control.go`
- Modify: `modules/agent/internal/runtime/subagent_runtime.go`
- Modify: `modules/agent/internal/runtime/subagent_manager.go`
- Modify: `modules/agent/internal/runtime/runtime_test.go`
- Modify: `modules/agent/internal/runtime/subagent_tools_test.go`

**Step 1：增加 list/cancel/reset 兼容测试**

分别验证：默认使用当前 run ID、错误 root run 无法取消、cancel 发送一次 cancelled 事件、reset 强制终态并移除记录、reset_pool 行为不变。

**Step 2：调用 Registry 替换 Runtime 包装方法**

直接调用 `r.subagents.Lookup/List/Cancel/ForceFinish/Remove`。删除以下 Runtime 方法：

- `registerSubAgent`
- `finishSubAgent`
- `subAgentRecords`
- `cancelSubAgent`
- `lookupSubAgentRecord`
- `removeSubAgent`
- `forceFinishSubAgent`

Planner 暂时调用 Registry 和现有事件适配器，不迁移到 Coordinator，以控制本次变更范围。

**Step 3：运行测试**

```powershell
go test -race ./internal/runtime -run "SubAgentQueryAndCancel|SubagentReset|SubagentProcessPool" -count=1
```

Expected: PASS。

**Step 4：提交**

```powershell
git add modules/agent/internal/runtime/subagent_control.go modules/agent/internal/runtime/subagent_runtime.go modules/agent/internal/runtime/subagent_manager.go modules/agent/internal/runtime/runtime_test.go modules/agent/internal/runtime/subagent_tools_test.go
git commit -m "refactor(agent): route subagent controls through registry"
```

### Task 7：清理边界并验证

**Files:**

- Modify: `modules/agent/internal/runtime/subagent_runtime.go`
- Modify: `modules/agent/internal/runtime/subagent_manager.go`
- Modify: `modules/agent/internal/runtime/aliases_test.go`
- Modify: `docs/04-agent-runtime-design.md`
- Modify: `docs/10-development-status.md`

**Step 1：检查禁止依赖**

```powershell
rg -n 'internal/runtime|internal/tools' modules/agent/internal/subagent -g '*.go'
```

Expected: 无输出。

**Step 2：删除失效别名和死代码**

删除仅为迁移前测试服务的 `ProcessSubAgent` 等别名；删除空包装函数。若 `subagent_manager.go` 只剩 `tools.SubagentManager` 适配，可改名为 `subagent_tool_adapter.go`。

**Step 3：更新架构文档**

在 `docs/04-agent-runtime-design.md` 记录：

- Runtime 负责规格构造和外部事件契约。
- Coordinator 负责通用子代理执行生命周期。
- Registry 负责并发安全状态。
- Process/Pool 负责进程基础设施。

在 `docs/10-development-status.md` 更新实际落地状态。

**Step 4：格式化和静态检查**

```powershell
gofmt -w internal/subagent internal/runtime
go vet ./...
```

Expected: 无错误。

**Step 5：完整测试**

```powershell
go test ./...
go test -race ./internal/subagent ./internal/runtime
```

Expected: 全部 PASS，无 data race。

**Step 6：审查差异范围**

```powershell
git diff --check
git diff --stat
git status --short
```

Expected: 无空白错误；差异只涉及本计划列出的 Agent 与文档文件；不得包含 `.zcode/` 或 `mcps/`。

**Step 7：提交**

```powershell
git add modules/agent/internal/subagent modules/agent/internal/runtime docs/04-agent-runtime-design.md docs/10-development-status.md
git commit -m "docs(agent): document subagent coordinator boundary"
```

## 6. 失败处理与回滚点

每个 Task 都是独立提交，可按提交逆序回滚。禁止在同一提交中同时移动 Capture、Registry 和 Coordinator，以免测试失败时无法定位边界。

重点失败场景：

| 场景 | 必须保持的行为 |
| --- | --- |
| Acquire 失败 | Registry 标记 failed，发送失败状态，不复用进程 |
| Child Start 返回错误 | 捕获诊断，释放进程，根工具得到错误 |
| Parent context 取消 | 子代理标记 cancelled，不能随后被 completed 覆盖 |
| Child finish 非 completed | 返回包含工具和事件统计的可操作错误 |
| Child 无最终文本 | 失败，不把空结果当作成功 |
| EventSink 返回错误 | 执行主流程继续完成，但记录/返回可诊断信息；不得泄漏进程 |
| Release 重复调用 | 通过单一 defer 防止 active 计数损坏 |

## 7. 完成定义

- [x] `internal/subagent` 拥有 Capture、Registry、Coordinator。
- [x] Runtime 只负责 RunSpec 构造、Goal 规则和外部适配。
- [x] 没有 `subagent -> runtime` 依赖。
- [x] 没有巨大 Host 接口。
- [x] 工具与 JSON-RPC 行为兼容。
- [x] 聚焦测试、完整测试和 race 测试通过。
- [x] 架构文档与开发状态更新。
- [x] 每个迁移阶段均有独立提交。
