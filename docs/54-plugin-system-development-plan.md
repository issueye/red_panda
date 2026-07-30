# v0.3.0 插件系统开发计划（Plugin Architecture Implementation Plan）

| 字段 | 值 |
| --- | --- |
| 目标版本 | `0.3.0` |
| 状态 | Active |
| 基础设计 | `docs/53-plugin-system-refactor-design.md` |
| 日期 | 2026-07-29 |

## 1. 开发原则

1. **外部协议零破坏**：HTTP/WS/JSON-RPC 方法名、事件信封 V2、MCP 工具命名全部保留。破坏性变更仅限模块内部。
2. **先基础设施后迁移**：Slice A 的新包（`registry/`、`hooks/`）必须先于工具迁移。
3. **渐进式替换**：每个改造步骤完成后立即跑 `go test ./...`，确保不积累技术债。
4. **不保留中间状态**：不做兼容层、别名、deprecated 桥接。v0.2.x 内部 API 直接删除。
5. **测试先行**：每个新包至少覆盖核心行为 + 边界（阻塞/改写/panic），回归测试 100% 通过。

## 2. 依赖图

```
Phase 1（并行）：
  ┌─ T1: runtime/registry/ (ToolRegistry + ProviderRegistry + Dispatcher) ─┐
  │    新包，不依赖现有代码改造                                               │
  └──────────────────────────────────────────────────────────────────────────┘
  ┌─ T2: runtime/hooks/ (ExtensionBus) ────────────────────────────────────┐
  │    新包，不依赖现有代码改造                                             │
  └──────────────────────────────────────────────────────────────────────────┘

Phase 2（T1 + T2 完成后并行）：
  ┌─ T3: plugins/core/workspace/ ──→ 11 个 workspace 工具 ──────────────────┐
  ├─ T4: plugins/core/coding/ ──→ 5 个 coding 工具 ─────────────────────────┐
  ├─ T5: plugins/core/orchestration/ ──→ 11 个 orchestration 工具 ─────────┐
  └─ T6: plugins/core/state/ ──→ 7 个 state 工具 ──────────────────────────┘

Phase 3（Phase 2 完成后）：
  ┌─ T7: runtime.go handleLine → Dispatcher ───────────────────────────────┐
  └─ T8: tool_execute.go dispatch → Registry.Handle() ────────────────────┘

Phase 4（T7+T8 完成后）：
  ┌─ T9: factory.go → ProviderRegistry ────────────────────────────────────┐
  ├─ T10: policy.go opsOnlyTools → ToolEntry.OpsOnly ──────────────────────┐
  ├─ T11: AvailableToolsForOptions → Registry 接口 ───────────────────────┐
  └─ T12: 测试全部通过 + 回归确认 ─────────────────────────────────────────┘
```

## 3. 任务定义

### Phase 1：基础设施（并行）

#### T1: runtime/registry/ — 注册表核心

**产出文件**：

| 文件 | 职责 | 约行数 |
| --- | --- | --- |
| `modules/agent/internal/runtime/registry/registry.go` | `ToolRegistry`、`ToolEntry`、`ToolContext`、`HandlerFunc`、`TimeoutClass` | ~250 |
| `modules/agent/internal/runtime/registry/registry_test.go` | 注册/覆盖/超时/Clear/ListBySource 测试 | ~200 |
| `modules/agent/internal/runtime/registry/provider.go` | `ProviderRegistry`、`ProviderFactory`、`Resolve` | ~120 |
| `modules/agent/internal/runtime/registry/provider_test.go` | 内置3工厂 + 新增工厂 Resolve 测试 | ~100 |
| `modules/agent/internal/runtime/registry/dispatcher.go` | `Dispatcher`（runtimeDispatcher + gatewayDispatcher）、`MethodHandler` | ~80 |
| `modules/agent/internal/runtime/registry/dispatcher_test.go` | 注册/分发/method-not-found 测试 | ~80 |

**设计要点**：

- `ToolRegistry` 使用 `sync.RWMutex` + `map[string]ToolEntry` + `[]string order` + `map[string][]string bySrc`。
- `Register(e ToolEntry) (prev *ToolEntry)`：同名覆盖，记录 `Overriden`，返回旧 entry。
- `Handle(ctx, call)`：查找 entry → `TimeoutClass` 包装 → 调用 handler → 返回。钩子链在 T2 完成后集成。
- `ListBySource(prefix)` 支持 `mcp:`/`js:`/`builtin:` 前缀过滤。
- `Clear(prefix)` 匹配 `strings.HasPrefix`，用于热重载。
- `TimeoutClass`：`LocalToolTimeout`=30s、`GatewayToolTimeout`=30s、`SelfManagedToolTimeout`=0。
- `ProviderRegistry`：`map[string]ProviderFactory` + `Resolve(options, log)`，内置 3 工厂在 `init()` 注册。
- `Dispatcher`：`map[string]MethodHandler`，`Dispatch(ctx, req)` → 查找 → 调用，未找到返回 `-32601`。

#### T2: runtime/hooks/ — 扩展总线

**产出文件**：

| 文件 | 职责 | 约行数 |
| --- | --- | --- |
| `modules/agent/internal/runtime/hooks/bus.go` | `ExtensionBus`、`HookName` 常量、`HookContext`、`HookResult`、`HookHandler` | ~200 |
| `modules/agent/internal/runtime/hooks/bus_test.go` | 注册/链式执行/Block短路/Cancel/Panic安全 测试 | ~200 |

**设计要点**：

- 12 个钩子常量（与 doc §6.5 一致）：`run.start`、`run.end`、`provider.request_before`、`provider.response_after`、`tool.call`、`tool.result`、`permission.check`、`session.before_compact`、`session.after_compact`、`context.compose`、`message.end`、`ext.project_trust`。
- `Register(name, order, handler)`：按 order 升序排列 handler。
- `Emit(name, ctx, event)`：遍历 handler → 链式传递 `Transform`（深拷贝）→ 任意 `{Block: true}` 短路后续链。
- `Emit` 中 `defer recover()` 防止 handler panic 拖垮 Runtime。
- 非阻塞钩子（`run.start`、`run.end` 等）的 `HookResult` 可忽略，仅作通知/审计。

**验收标准**：`go test ./modules/agent/internal/runtime/registry/... ./modules/agent/internal/runtime/hooks/...` 全部通过。

### Phase 2：工具迁移（并行，依赖 T1+T2）

**迁移模式**（以 workspace 为例，所有域一致）：

1. 在 `modules/agent/plugins/core/workspace/` 下新建包。
2. 每个工具实现从 `dispatch_local.go` 中对应的 switch case → 迁移为 `HandlerFunc`：

```go
// 旧签名
func runReadFile(workingDir string, path string) (string, error)
// → 新签名
func RunReadFileHandler(ctx *tools.ToolContext, args map[string]any) (*tools.Result, error)
    // 实现：从 ctx.WorkingDir/ctx.RunID 取参数；从 args 解构；返回 Result
```

3. 注册入口：`Register(reg *tools.Registry, bus *hooks.Bus) []string`，内联定义 `[]tools.ToolEntry`。
4. 原 `dispatch_*.go` 文件中的工具实现函数（`runReadFile`/`runListWorkspace` 等）移至内部子包（如 `internal/`），保持函数体逻辑不变，只改参数解构方式。

**关键约束**：

- 工具实现函数体**不改逻辑**——只改「从函数参数取参数」→「从 `args map[string]any` 取参数」。
- `opsOnlyTools` 映射为 `ToolEntry.OpsOnly`。
- 超时分类映射：
  - `strings.HasPrefix(name, "workspace.")` 等 workspace 类 → `LocalToolTimeout`
  - `strings.HasPrefix(name, "memory.")`、`strings.HasPrefix(name, "todo.")` → `GatewayToolTimeout`
  - `shell.exec`、`skill.run`、`worker.*`、`web.*` → `SelfManagedToolTimeout`
- 所有迁移后的工具仍走 `registry_test.go` 的公开顺序锁定测试。

#### T3: plugins/core/workspace/ — 11 个工具

| 旧文件 | 迁移目标 | 工具 |
| --- | --- | --- |
| `dispatch_local.go`（11 case） | `workspace/register.go` | `workspace.read_file`、`workspace.read_files`、`workspace.list`、`workspace.find_files`、`workspace.grep`、`workspace.stats`、`workspace.diff_file`、`workspace.write_file`、`workspace.edit_file`、`workspace.apply_patch` |
| `workspace_read.go` 实现 | `workspace/internal/read.go` | — |
| `workspace_write.go` 实现 | `workspace/internal/write.go` | — |
| `workspace_path.go` 工具函数 | `workspace/internal/path.go` | — |
| `workspace_const.go` 常量 | 内联到 `register.go` | — |
| `workspace_slash.go` | 删除（slash 工具不再需要） | — |

#### T4: plugins/core/coding/ — 5 个工具

| 旧文件 | 迁移目标 | 工具 |
| --- | --- | --- |
| `dispatch_local.go`（5 case） | `coding/register.go` | `git.status`、`git.diff`、`git.log`、`git.show`、`shell.exec` |
| `coding.go` 实现 | `coding/internal/coding.go` | — |
| `shell.go` 实现 | `coding/internal/shell.go` | — |

#### T5: plugins/core/orchestration/ — 11 个工具

| 旧文件 | 迁移目标 | 工具 |
| --- | --- | --- |
| `dispatch_orchestration.go` | `orchestration/register.go` | `skill.list`、`skill.create`、`skill.update`、`skill.delete`、`skill.run`、`worker.delegate`、`worker.list`、`worker.cancel`、`worker.pool_status`、`worker.send`、`worker.receive` |
| 实现函数（在 runtime 各工具文件） | 对应 orchestration/internal/ | — |

#### T6: plugins/core/state/ — 7 个工具

| 旧文件 | 迁移目标 | 工具 |
| --- | --- | --- |
| `dispatch_state.go` | `state/register.go` | `todo.write`、`todo.list`、`memory.list`、`memory.create`、`memory.update`、`memory.delete` |
| `dispatch_web.go` | `state/register.go` | `web.search`、`web.fetch` |
| 实现函数 | 对应 state/internal/ | — |

**验收标准**：所有 P1 插件注册后，`Registry.Definitions()` 返回的工具列表（名称 + 顺序 + schema）与旧 `stableToolDefinitions()` 字节级一致；所有单测（`coding_test.go`、`policy_test.go`、`web_test.go`、`registry_test.go`、`runner_test.go`）迁移后仍通过。

### Phase 3：核心替换（串行）

#### T7: runtime.go handleLine → Dispatcher

1. `runtime.go` 新建 `runtimeDispatcher *Dispatcher` 字段。
2. 内置 18 个方法在 `init()` 中注册到 `runtimeDispatcher`。
3. `handleLine` 的 switch 体替换为 `runtimeDispatcher.Dispatch(ctx, req)`。
4. `Serve` 方法体不变。

#### T8: tool_execute.go dispatch → Registry.Handle()

1. `tool_execute.go` 的 `r.tools.RunWithContext(ctx, runCtx, invocation)` 调用替换为 `r.registry.Handle(ctx, params, invocation.Call)`。
2. `ToolRunner` 的 6 个执行器字段（`MemoryExecutor`、`TodoExecutor`、`SkillExecutor`、`WorkerDelegate/List/Cancel/PoolStatus/Send/Receive`、`MCPExecutor`）移除——所有工具走 `Registry`。
3. `executeToolBatch.go` 中的 `r.tools.InvocationFromCall` + `r.executeTool` 改为通过 Registry 查找 + Handle。
4. 保留 `executeToolBatch.go` 的并行/串行分流逻辑（`worker.delegate` 并行），但执行改为 `Registry.Handle`。
5. `tool_execute.go` 的 `requestPermission` 逻辑改为触发 `HookPermissionCheck`（Phase 4）。

### Phase 4：最终收敛（T7+T8 完成后）

#### T9: factory.go → ProviderRegistry

1. 内置 3 个 provider 工厂在 `init()` 中 `ProviderRegistry.Register`。
2. `NewFromEnv` 改为 `ProviderRegistry.Resolve` 包装。
3. 删除 `providerFromOptions` 的 3-case switch。

#### T10: policy.go → ToolEntry.OpsOnly + 策略下沉

1. 删除 `opsOnlyTools` map，各域 `Register` 时设置 `OpsOnly`。
2. `EvaluateToolPolicy` 保留（策略函数），不再依赖 `opsOnlyTools`——改用 `Registry.Lookup(name).OpsOnly`。
3. `AvailableToolsForOptions` 改为 `Registry.ListBySource` 包装。

#### T11: 清理与测试

1. 删除已废弃文件：`dispatch_local.go`、`dispatch_orchestration.go`、`dispatch_state.go`、`dispatch_web.go`、`registry.go`（旧）、`policy.go` 中的废弃片段、`workspace_slash.go`。
2. `runner.go` 精简：删除 `ToolRunner` 结构体 + 执行器字段 + `dispatchTool`、`InvocationFromCall`、`RunWithContext`，保留 `Parse`（slash 工具可后续迁移到 prompt 模板）。
3. `gateway.go`（tools 包内）——评估是否删除或迁移。
4. `mcp_name.go`、`helpers.go`、`result.go`、`result_test.go`——按是否被新架构引用决定保留/迁移。
5. 全量 `go test ./...`。

## 4. 文件变更清单（汇总）

### 新增文件

| 文件 | Phase | 类型 |
| --- | --- | --- |
| `modules/agent/internal/runtime/registry/registry.go` | 1 | 新包 |
| `modules/agent/internal/runtime/registry/registry_test.go` | 1 | 测试 |
| `modules/agent/internal/runtime/registry/provider.go` | 1 | 新包 |
| `modules/agent/internal/runtime/registry/provider_test.go` | 1 | 测试 |
| `modules/agent/internal/runtime/registry/dispatcher.go` | 1 | 新包 |
| `modules/agent/internal/runtime/registry/dispatcher_test.go` | 1 | 测试 |
| `modules/agent/internal/runtime/hooks/bus.go` | 1 | 新包 |
| `modules/agent/internal/runtime/hooks/bus_test.go` | 1 | 测试 |
| `modules/agent/plugins/core/workspace/register.go` | 2 | 新包 |
| `modules/agent/plugins/core/workspace/internal/read.go` | 2 | 迁移 |
| `modules/agent/plugins/core/workspace/internal/write.go` | 2 | 迁移 |
| `modules/agent/plugins/core/workspace/internal/path.go` | 2 | 迁移 |
| `modules/agent/plugins/core/workspace/internal/const.go` | 2 | 迁移 |
| `modules/agent/plugins/core/coding/register.go` | 2 | 新包 |
| `modules/agent/plugins/core/coding/internal/coding.go` | 2 | 迁移 |
| `modules/agent/plugins/core/coding/internal/shell.go` | 2 | 迁移 |
| `modules/agent/plugins/core/orchestration/register.go` | 2 | 新包 |
| `modules/agent/plugins/core/orchestration/internal/*.go` | 2 | 迁移 |
| `modules/agent/plugins/core/state/register.go` | 2 | 新包 |
| `modules/agent/plugins/core/state/internal/*.go` | 2 | 迁移 |

### 修改文件

| 文件 | Phase | 改动 |
| --- | --- | --- |
| `modules/agent/internal/runtime/runtime.go` | 3 | `handleLine` → Dispatcher |
| `modules/agent/internal/runtime/tool_execute.go` | 3 | `RunWithContext` → Registry.Handle |
| `modules/agent/internal/runtime/tool_batch.go` | 3 | `InvocationFromCall` + `executeTool` → Registry |
| `modules/agent/internal/provider/factory.go` | 4 | `providerFromOptions` → ProviderRegistry |
| `modules/agent/internal/tools/policy.go` | 4 | 删 `opsOnlyTools`，改 `EvaluateToolPolicy` |
| `modules/agent/internal/tools/runner.go` | 4 | 删 `ToolRunner` 字段 + dispatch |

### 删除文件

| 文件 | Phase |
| --- | --- |
| `modules/agent/internal/tools/dispatch_local.go` | 4 |
| `modules/agent/internal/tools/dispatch_orchestration.go` | 4 |
| `modules/agent/internal/tools/dispatch_state.go` | 4 |
| `modules/agent/internal/tools/dispatch_web.go` | 4 |
| `modules/agent/internal/tools/workspace_slash.go` | 4 |
| `modules/agent/internal/tools/registry.go`（旧） | 2（被新 registry 包替代） |

## 5. 测试里程碑

| 里程碑 | 验收条件 |
| --- | --- |
| M1（Phase 1 完成） | `registry/` + `hooks/` 新包测试通过 |
| M2（Phase 2 完成） | `plugins/core/*/` 全部注册后，工具定义列表与旧 `stableToolDefinitions()` 一致 |
| M3（Phase 3 完成） | 端到端 run（Gateway → Runtime → 工具 → 事件）与 v0.2.x 行为一致 |
| M4（Phase 4 完成） | `go test ./...` 全部通过，废弃文件全部删除 |

## 6. 风险点

| 风险 | 缓解 |
| --- | --- |
| 30 个工具迁移的机械替换出错 | 每迁移一个域就跑一次 `go test`，顺序锁定测试 `TestStableToolRegistryPreservesPublicOrder` 持续有效 |
| `ToolContext` 依赖注入链过长 | Phase 2 迁移时先保证 `WorkingDir`、`RunID`、`SessionID`、`Reply` 四个核心字段完整，其他字段（`Memory`、`Todo`）延到 Phase 4 |
| `executeToolBatch.go` 的并行/串行分流与 Registry 集成 | 分流逻辑不变，仅 `r.executeTool` → `r.registry.Handle` 的映射 |
| `requestPermission` 下沉为 HookPermissionCheck | 保留 `requestPermission` 函数体逻辑，改为 Hook handler 调用 |
| MCP 工具在 `AvailableTools` 快照后注册 | `InvocationFromCall` 的 MCP 前缀兜底逻辑移到 `Registry.Lookup` 中 |

