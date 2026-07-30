# v0.3.0 插件系统收敛开发计划

| 字段 | 值 |
| --- | --- |
| 目标版本 | `0.3.0` |
| 状态 | Active |
| 日期 | 2026-07-30 |
| 基础设计 | `docs/53-plugin-system-refactor-design.md` |
| 前序计划 | `docs/54-plugin-system-development-plan.md` |
| 适用基线 | `dev_v2@3e31693` |

## 1. 目的

本计划负责把当前“核心工具注册化、旧路径仍保留”的中间状态，收敛为设计文档定义的完整插件系统。完成后应具备以下能力：

1. Runtime 的工具、Provider、RPC 和生命周期扩展统一经过注册表与扩展总线。
2. MCP 工具与核心工具使用同一个注册、过滤和执行模型。
3. Gateway 请求分发完成注册化，允许 P1 服务插件注册方法。
4. P3 JavaScript 扩展可以注册工具、Hook 和命令，并支持受控热重载。
5. P4 skills、prompts、themes 和 system prompt 片段支持全局/项目分层发现。
6. 项目插件与资源受 Project Trust 控制，插件启停和状态可持久化。
7. 删除 v0.2.x 工具双轨实现，外部 HTTP、WS、JSON-RPC 和事件协议保持兼容。

本文是后续开发的执行基线。`docs/54-plugin-system-development-plan.md` 保留为第一轮注册化迁移记录，不再作为剩余工作的状态来源。

## 2. 当前基线

### 2.1 已完成

| 能力 | 当前实现 | 状态 |
| --- | --- | --- |
| Tool Registry | 支持来源覆盖栈、恢复、过滤、顺序、删除和超时 | Runtime 工具元数据与执行的唯一来源 |
| Runtime Dispatcher | 18 个 JSON-RPC 方法通过 Dispatcher 注册 | 已完成 |
| P1 核心工具 | workspace 10、coding 5、orchestration 11、state/web 8 | 已完成主路径迁移 |
| ExtensionBus | 12 个 Hook 名称、顺序、Block、Cancel、Transform、panic recover、来源卸载 | 基础契约完成，待接入生产生命周期 |
| ProviderRegistry | 显式默认、来源覆盖栈、恢复、Resolve、Names | 已接入 Runtime 与 per-run Provider 解析 |
| 回归状态 | Agent、Gateway 全量 Go 测试 | 当前通过 |

### 2.2 未完成或不符合设计

| 缺口 | 当前表现 | 目标 |
| --- | --- | --- |
| Hook 接入 | 生产代码没有 `Emit`/`Register` | 接入 Run、Provider、Tool、Permission、Session、Context、Message 生命周期 |
| Provider | 已统一通过 ProviderRegistry | Phase 1 已完成 |
| MCP | 定义临时拼接，执行依赖 `mcp__` 前缀兜底 | 每个 Run 注册到统一 Registry 视图 |
| Gateway | WebSocket 仍使用 method switch | Gateway Dispatcher + P1 服务注册 |
| 策略 | 工具过滤和 OpsOnly 已读取 ToolEntry，权限等待尚未接入 Hook | 权限通过 Hook 扩展 |
| 旧工具系统 | ToolRunner、stable registry 和四组 dispatch 已删除 | Phase 1 已完成 |
| P3 | 无 JS Host、loader、bridge 和 reload | JavaScript 单文件扩展可用 |
| P4 | 无资源分层发现和 Trust 闸 | 全局/项目资源可发现、可控制 |
| 配置与状态 | 无分层 settings、manifest、plugin_state | 配置合并、启停和状态持久化 |

### 2.3 实施进度

| Phase | 状态 | 完成日期 | 说明 |
| --- | --- | --- | --- |
| Phase 0：修正基础契约 | Completed | 2026-07-30 | Registry 覆盖栈、HookOutcome、Dispatcher 来源卸载和统一标识校验已落地 |
| Phase 1：Runtime 单路径收敛 | Completed | 2026-07-30 | Provider、工具元数据和执行路径已收敛 |
| Phase 2：ExtensionBus 接入生命周期 | In Progress | - | 当前开发入口 |
| Phase 3：MCP 与 Gateway 注册化 | Completed | 2026-07-30 | RunScopedRegistry、Gateway Dispatcher 和 MCP manifest 已完成 |
| Phase 4：P3 JavaScript 扩展 | In Progress | - | JS manifest、独立 VM Host、Tool/Hook bridge 与全局启动加载已落地 |
| Phase 5：配置、Trust 与 P4 资源 | Pending | - | 依赖 Phase 4 |
| Phase 6：管理面与发布 | Pending | - | 最终收敛 |

## 3. 交付原则

1. **先收敛契约，再增加插件形态**：Registry 和 ExtensionBus 的语义必须稳定后，才能接入 JS 热重载。
2. **一个阶段只有一个生产路径**：迁移完成即删除该能力的旧入口，避免继续扩大双轨范围。
3. **外部协议兼容**：不修改现有 HTTP 路由、WS method、JSON-RPC method、EnvelopeV2 和 MCP 工具名。
4. **策略默认行为不变**：Hook 接入后，无第三方插件时，权限、工具风险和会话行为必须与当前版本一致。
5. **项目代码默认不受信任**：项目级 JS 和主动内容资源必须在 Trust 通过后加载。
6. **故障隔离**：单个 Hook、JS 扩展或 MCP server 失败，不得导致 Runtime 进程崩溃。
7. **可观测而非静默降级**：加载失败、覆盖、禁用、超时和恢复都必须包含来源信息并写入诊断日志。

## 4. 目标架构和依赖顺序

```text
Phase 0  契约修正
   |
   v
Phase 1  Runtime 单路径收敛
   |-------------------|
   v                   v
Phase 2 Hook 接入   Phase 3 MCP + Gateway 注册化
   |                   |
   |-------------------|
             v
Phase 4  P3 JavaScript Host
             |
             v
Phase 5  配置分层 + Project Trust + P4 资源
             |
             v
Phase 6  管理面、清理、端到端验收和发布
```

Phase 2 与 Phase 3 可以并行开发，但必须分别通过验收后才能开始 Phase 4。配置模型可以在 Phase 4 后半段提前设计，但项目插件不得绕过 Phase 5 的 Trust 闸正式启用。

## 5. Phase 0：修正基础契约

目标：解决当前 Registry 和 ExtensionBus 中会阻塞覆盖、热重载和 Transform 接入的问题。

状态：**Completed（2026-07-30）**

### P0-T1 Registry 覆盖栈

当前 `Register` 只记录被覆盖来源，`Clear("js:...")` 会把当前 entry 删除，不能恢复此前的内置 entry。

实施内容：

1. 将单 entry 存储改为按工具名保存覆盖栈，或保存可恢复的 previous entry 链。
2. 明确定义优先级：后注册覆盖前注册；卸载当前来源后恢复上一层。
3. `Remove(name, source)` 和 `ClearSource(prefix)` 只能移除指定来源。
4. 保证工具公开顺序在覆盖和恢复前后稳定。
5. 将字段拼写 `Overriden` 修正为 `Overridden`，不保留内部兼容别名。

验收：

- JS 覆盖 `workspace.read_file` 后卸载，内置工具自动恢复。
- 连续三层覆盖按 LIFO 顺序恢复。
- 并发 Lookup、Register、ClearSource 通过 `go test -race`。

### P0-T2 ExtensionBus 返回契约

当前总线只返回最后一个 `HookResult`，调用方无法可靠取得所有 handler 合并后的最终载荷。

实施内容：

1. 引入 `HookOutcome`，至少包含 `Event`、`Blocked`、`Cancelled`、`Reason` 和执行诊断。
2. 每个 Transform 合并到 `HookOutcome.Event`，后续 handler 接收更新后的只读快照。
3. 明确 Block、Cancel 和非阻塞 Hook 的语义矩阵。
4. Hook handler panic 记录 hook name、source 和 order 后继续；上下文取消则停止链路。
5. 增加按 source 注销 Hook 的接口，支持插件卸载。

验收：

- 三个 Transform 的最终结果可由调用方直接取得。
- Block 短路、Cancel、panic recover、context cancel 和 source unload 均有单测。

### P0-T3 标识与错误规范

1. Source 统一为 `builtin:<domain>`、`js:<plugin-id>`、`mcp:<server-id>`。
2. 插件 ID、工具名、RPC method 和命令名增加集中校验。
3. 注册覆盖、加载失败和未知来源使用可识别的错误类型。
4. Dispatcher 增加 `Unregister`、`List` 和来源元数据，避免只能注册不能卸载。

退出条件：Registry、ExtensionBus 和 Dispatcher 的契约冻结，相关包单测和 race test 通过。

实施结果：

- Tool Registry 使用来源覆盖栈；`ClearSource`/`Remove(name, source)` 可恢复被覆盖 entry。
- `HookOutcome` 返回最终 Event、Block、Cancel、Reason 和 panic diagnostics；支持按来源卸载。
- Dispatcher 支持来源覆盖、`Unregister`、`ClearSource` 和确定性 `List`。
- 新增共享 `pluginmeta` 校验，统一 tool、hook、method 和 source 标识规则。
- P1 来源统一为 `builtin:<domain>`，并通过 `MustRegister` 防止内置注册错误被静默忽略。
- 已通过 `go test -race ./internal/pluginmeta ./internal/runtime/registry ./internal/runtime/hooks` 和 Agent `go test ./...`。

## 6. Phase 1：Runtime 单路径收敛

目标：完成前序计划 Phase 4，移除 Provider、策略和工具执行双轨。

状态：**Completed（2026-07-30）**

### P1-T1 ProviderRegistry 接入

1. Runtime 持有 ProviderRegistry，并在构造阶段注册三个内置 Provider 工厂。
2. 用显式 `defaultProvider` 替代 map 随机选择。
3. `NewFromEnv` 和 per-run profile 都调用同一个 `Resolve`。
4. 删除 `providerFromOptions` switch。
5. Provider 注册支持 source、覆盖和卸载，为 P3 Provider 扩展保留边界；v0.3.0 不要求 JS 实现自定义流式 Provider。

### P1-T2 ToolEntry 成为工具元数据唯一来源

1. `AvailableToolsForOptions` 改为读取 Registry 快照。
2. `opsOnlyTools` 删除，统一读取 `ToolEntry.OpsOnly`。
3. 工具超时、风险、来源和过滤全部从 ToolEntry 获取。
4. `resolveToolCall` 不再调用旧 ToolRunner 接口。
5. 增加 Registry 快照测试，锁定当前 34 个核心工具的名称、顺序、schema 和标记。

### P1-T3 删除旧工具执行路径

删除或拆分以下旧实现：

- `internal/tools/dispatch_local.go`
- `internal/tools/dispatch_orchestration.go`
- `internal/tools/dispatch_state.go`
- `internal/tools/dispatch_web.go`
- `stableToolDefinitions`、`stableToolRegistry`、`stableToolTimeoutByName`
- ToolRunner 的 executor 字段、`RunWithContext` 和 `dispatchTool`

若 slash command 的 Parse 能力仍被使用，应迁移到独立 `commands` 包，不为保留 Parse 而继续保留 ToolRunner。

退出条件：Runtime 生产代码和测试都不再引用旧 dispatch、stable registry 或 ToolRunner 执行接口；Agent 全量测试通过。

实施结果：

- Provider Registry 下沉到 `internal/provider`，Runtime facade 保留类型入口；内置 Provider 使用显式默认值，并支持来源覆盖、卸载和恢复。
- Runtime 普通请求与最终重试均绑定同一个 Provider Registry；删除 `providerFromOptions` switch。
- Tool Registry 统一承担 allowlist、denylist、debug、OpsOnly、风险、超时和来源元数据；新增 34 个核心工具快照测试。
- 删除旧 ToolRunner、stable registry、四组 dispatch 及重复的 workspace、coding、web 实现；保留的测试迁移到对应核心插件包。
- 已通过 Protocol 的 `go test -race -count=1 ./pluginmeta`，以及 Agent 的 `go test -race -count=1 ./internal/provider ./internal/runtime/registry ./internal/runtime/hooks`。
- 已通过 Agent module 的 `go test -count=1 ./...`，并用 `rg` 确认生产代码不再引用旧执行符号。

## 7. Phase 2：ExtensionBus 接入生命周期

目标：让 Hook 从可测试的数据结构变成真实扩展面。

### P2-T1 Tool 和 Permission Hook

状态：**Completed（2026-07-30）**

执行顺序固定为：

```text
resolve entry
  -> tool.call Hook（可改写 args / Block）
  -> policy evaluation
  -> permission.check Hook
  -> handler
  -> tool.result Hook（可改写 result）
  -> event emission
```

内置权限行为注册为系统级 P1 Hook。没有自定义 Hook 时，用户看到的权限请求和结果保持不变。

实施结果：

- `tool.call` 已在策略计算前接入，支持参数改写、Block 和 Cancel；无效参数改写按拒绝处理。
- `permission.check` 已注册 `builtin:permission` 系统 Hook，并允许插件进一步收紧决策或显式处理权限请求。
- `tool.result` 已在事件发送前接入，改写后的状态、输出、错误、退出码和耗时同时进入返回值与事件流。
- 已覆盖参数改写、结果改写、工具阻断、权限拒绝和权限显式批准测试。

### P2-T2 Provider Hook

状态：**Completed（2026-07-30）**

1. 每次模型调用前触发 `provider.request_before`，允许修改 messages、tools 和安全 headers 白名单。
2. 每次模型响应后触发 `provider.response_after`，供审计和指标使用。
3. 不允许 Hook 读取 Provider API key；敏感配置不得进入通用 event map。
4. 流式响应只在完整响应结束后触发 after Hook，避免逐 token Hook 开销。

实施结果：

- Runtime 的普通模型请求和最终答案重试统一经过 Provider Hook 包装器。
- `provider.request_before` 支持改写 messages、tools 和安全 header 白名单；Hook 载荷不包含 API key、代理或 Provider 私密配置。
- Provider adapters 仅接受 `Anthropic-Beta`、`OpenAI-Organization`、`OpenAI-Project`、`User-Agent` 和 `X-Request-ID`，认证与 Cookie header 无法被 Hook 覆盖。
- `provider.response_after` 在完整流式响应结束或失败后触发，提供 chunk、文本字节数、工具调用数和错误摘要。
- 已覆盖请求改写、敏感字段隔离、header 过滤、请求阻断和失败响应通知测试。

### P2-T3 Run、Session、Context 和 Message Hook

状态：**In Progress**

在明确的单一位置接入：

| Hook | 挂载点 |
| --- | --- |
| `run.start` / `run.end` | `run.execute` 生命周期最外层 |
| `session.before_compact` / `session.after_compact` | compact 临界区内 |
| `context.compose` | Provider Request 组装完成、发送之前 |
| `message.end` | 最终 assistant message 持久化和发送之前 |
| `ext.project_trust` | 项目级扩展发现后、加载前 |

当前实施：

- `run.start` / `run.end` 已挂载在 Agent 实际执行生命周期最外层；start 可改写输入或阻断，end 在取消场景使用脱离取消的 context 保证通知。
- `context.compose` 已挂载在 Provider Request 完成组装后、`provider.request_before` 之前，并复用相同的安全请求载荷与校验。
- `session.before_compact` / `session.after_compact` 和可改写的 `message.end` 尚未完成。compact 临界区与 assistant message 持久化均由 Gateway 所有，必须在 Phase 3 Gateway 扩展边界落地，不能在 Agent 侧伪造通知点。
- `ext.project_trust` 按计划保留到 Phase 5 的项目扩展发现链路。

### P2-T4 Hook 集成测试

必须覆盖 args 改写、结果改写、Block、权限批准/拒绝、Provider request 改写、compact Cancel、最终消息改写和 Hook panic。测试应验证事件顺序与持久化内容一致，避免 UI 显示值和会话存储值分叉。

退出条件：12 个 Hook 均有生产挂载点或明确标注为通知型；核心权限策略通过系统 Hook 工作；无 Hook 时回归行为一致。

## 8. Phase 3：MCP 与 Gateway 注册化

### P3-T1 RunScopedRegistry

状态：**Completed（2026-07-30）**

MCP 工具是 per-run 动态集合，不应直接污染全局核心 Registry。新增只读组合视图：

```text
RunScopedRegistry = GlobalRegistry snapshot + MCP run entries + JS enabled entries
```

1. `PrepareToolsForRun` 将 MCP discovery 转为 `ToolEntry{Source: "mcp:<server>"}`。
2. Provider 工具定义和工具执行读取同一个 RunScopedRegistry。
3. 删除 `toolsForReply` 的手工 append 和执行时 `mcp__` 前缀兜底。
4. Run 结束后清理动态 entry；MCP Manager 的进程池生命周期保持独立。
5. MCP server 同名工具冲突必须记录覆盖来源，并遵循确定性加载顺序。

实施结果：

- 新增 `Registry.Clone` 和 `RunScopedRegistry`，每个 run 固定全局工具快照，动态 entry 不污染 Runtime 全局 Registry。
- MCP discovery 直接注册 `ToolEntry{Source: "mcp:<server-id>", TimeoutClass: SelfManagedToolTimeout}`，handler 通过 Manager 的 run binding 执行。
- Provider definitions、allow/deny/OpsOnly 过滤、调用元数据补全和工具执行统一读取同一个 run registry。
- 删除 `toolsForReply` 手工拼接、`mcp__` 前缀执行兜底、`executeMCPToolRegistry`、`IsMCPToolName` 和无调用的 `DefinitionsForRun`。
- 优先级固定为全局快照、MCP、active JS；同一规范化 MCP server id 首个配置生效，后续重复配置产生诊断并跳过。
- run 注销时同时清除 scoped registry 与 MCP bindings，MCP 进程池和 discovery cache 仍由 Manager 生命周期管理。

### P3-T2 Gateway Dispatcher

状态：**Completed（2026-07-30）**

1. 在 Gateway 建立独立 Dispatcher，不直接依赖 Agent internal 包。
2. 将 WebSocket 的现有方法注册为内置 handler。
3. 增加 Gateway P1 插件注册入口和 source unload。
4. 保持错误码和响应 payload 与当前协议一致。
5. HTTP 路由本期不开放任意插件注入，只为 `/api/v1/plugins` 管理面预留固定核心路由。

实施结果：

- Gateway 新增独立 `WSDispatcher`，WebSocket 8 个现有 request method 全部注册为 `builtin:gateway`，删除 controller method switch。
- Dispatcher 支持来源覆盖栈、同来源替换、单方法卸载、`ClearSource` 恢复和确定性 `List`。
- 暴露 `RegisterFrom` 作为 Gateway P1 编译期服务插件入口；未知方法仍返回原 `method_not_implemented` envelope。
- 共享标识契约从 Agent internal 提升到 `protocol/pluginmeta`，Agent 与 Gateway 使用同一 method/source 校验规则。
- Gateway 全量测试及 Dispatcher race test 已通过。

### P3-T3 MCP 清单

状态：**Completed（2026-07-30）**

1. 定义并校验 `redpanda-plugin.json` schema。
2. 为仓库内 MCP server 补充 id、version、entry、capabilities 和默认启停状态。
3. 清单只描述启动方式和能力，不允许声明任意宿主文件权限。

实施结果：

- Protocol 新增严格类型的 `pluginmanifest` 包、发现器和 Draft 2020-12 JSON Schema；未知字段、重复 capability、无效 id/version/entry/risk 和超过 1 MiB 的清单会被拒绝。
- 首版 schema 明确只接受 `type: "mcp"`；JS entry 结构留到 Phase 4 与实际 loader 同步定义，避免提前形成错误兼容承诺。
- `mcps/fetch`、`robotgo-flow`、`sequential-thinking` 和 `tasks` 均已补充 id、version、entry、capabilities、默认启停和默认风险。
- 四份内置清单默认禁用；缺少仓库内 executable 的 tasks 使用 `command_env`，不会伪造或自动执行不存在的二进制。
- repository manifest discovery 测试锁定四份清单及确定性顺序。

退出条件：所有工具都通过 Registry 查找和执行；Gateway WebSocket 不再含 method switch；MCP 原有集成测试全部通过。

Phase 3 状态：**Completed（2026-07-30）**。

## 9. Phase 4：P3 JavaScript 扩展

目标：提供单文件、无需重新编译的 Runtime 扩展能力。

### P4-T1 JS Host 与加载器

状态：**In Progress**

1. 使用维护活跃、纯 Go 的 goja 包；落地前通过最小 spike 确认准确模块路径和 Windows 支持。
2. 每个插件使用独立 VM，记录 plugin id、source path、load generation 和已注册资源。
3. 扫描全局 `~/.redpanda/plugins/*.js`；项目目录加载由 Phase 5 Trust 控制。
4. 加载顺序确定化：全局按规范化路径排序，项目级后加载并允许覆盖。

当前实施：

- 已引入纯 Go `goja`，每个插件独立 VM，并记录 plugin id、source path 与 generation。
- 已支持确定性扫描 `~/.redpanda/plugins/*.js`；`RED_PANDA_HOME` 可重定向全局目录，项目目录仍留待 Phase 5 Trust。
- Runtime 构造时加载全局 JS；单插件失败只产生诊断，不阻止其他插件和 Runtime 启动。
- manifest 已改为按 `type` 判别：MCP 使用 `command/command_env`，JS 使用规范化相对路径 `entry.script`。

### P4-T2 Host API

状态：**In Progress**

首版只开放最小接口：

```javascript
rp.registerTool(definition, handler)
rp.on(hookName, handler, options)
rp.registerCommand(definition, handler)
rp.log(level, message, fields)
rp.getState(key)
rp.setState(key, value)
```

文件、网络、进程能力不直接暴露。需要系统访问的扩展应注册工具并经过现有风险和权限链。

当前实施：

- `rp.registerTool`、`rp.on` 与 `rp.log` 已接入 Tool Registry、ExtensionBus 和 Runtime 日志。
- 工具定义支持 name、displayName、description、risk、parameters 与 opsOnly；执行仍经过既有策略、权限和结果 Hook。
- Hook 支持 Block、Cancel、Reason 与 Transform 返回契约。
- `rp.registerCommand`、`rp.getState` 和 `rp.setState` 尚未实现，依赖 CommandRegistry 与 Phase 5 状态存储。

### P4-T3 执行限制和故障隔离

状态：**In Progress**

1. 每次 JS 调用设置 context deadline 和 goja Interrupt。
2. 捕获 JS exception 和 Go panic，转换为带 plugin id 的结构化错误。
3. 限制 state value 和工具结果大小。
4. 连续失败达到阈值后禁用当前 generation，不影响其他插件。

当前实施：

- 每次 JS 工具和 Hook 调用均绑定 context deadline，并使用 goja Interrupt 中止死循环。
- VM 调用按插件串行化；中断 watcher 在调用返回前同步退出，避免跨调用污染。
- JS exception、加载错误与 Hook panic 均带 plugin id 隔离；源码和工具结果上限为 1 MiB。
- 失败预算和 generation 自动禁用尚未实现。

### P4-T4 热重载

1. 提供 `extension.reload` Runtime RPC。
2. reload 采用“加载新 generation 成功后原子切换”，加载失败保留旧 generation。
3. 卸载时按 source 清理工具、Hook、命令和 VM。
4. 正在执行的调用持有旧 generation 引用，完成后再释放，禁止中途销毁 VM。

退出条件：示例 JS 插件可以注册工具、改写 Hook、注册命令；覆盖内置工具后卸载可恢复；死循环和异常不会终止 Runtime。

## 10. Phase 5：配置、Project Trust 与 P4 资源

### P5-T1 分层配置

实现固定优先级：

```text
环境变量 > 项目 .redpanda/settings.json > 全局 ~/.redpanda/settings.json > DB > 默认值
```

配置必须使用带来源的 typed merge，禁止以通用 map 静默覆盖。未知字段产生诊断警告，敏感值不得写入日志。

### P5-T2 插件状态存储

1. Gateway 增加 `plugin_state` KV migration 和 repository/service。
2. 持久化插件启停、失败计数、最后加载错误、配置和小型插件状态。
3. 插件状态按 plugin id 和 scope 隔离；项目级状态附带规范化 workspace root。

### P5-T3 Project Trust

1. 路径使用绝对规范化路径，并处理 Windows 大小写和符号链接边界。
2. 未信任项目不得执行 `.redpanda/plugins/*.js`。
3. 项目 prompt/system 片段按“主动内容”处理，也必须受 Trust 控制；纯展示 theme 可单独配置。
4. Trust 决策通过现有 Gateway permission UI 呈现，并支持仅本次、记住允许、拒绝。
5. CI 提供显式环境变量策略，默认仍为不信任。

### P5-T4 P4 资源发现

1. 发现 `skills/`、`prompts/`、`themes/` 和 `SYSTEM.md`。
2. 每类资源定义 schema、最大大小、确定性排序和同名覆盖规则。
3. `.codex/skills` 在 v0.3.0 只读兼容，日志提示迁移到 `.redpanda/skills`；不允许两个目录产生不确定覆盖。
4. Prompt 注册到 CommandRegistry；system 片段通过 `context.compose` 注入。
5. Theme 仅进入 Desktop 可消费的资源目录，不在 Runtime 执行代码。

退出条件：配置来源可解释；未信任项目资源不会加载；信任后 reload 生效；重启后启停和信任状态保持。

## 11. Phase 6：管理面、清理与发布

### P6-T1 插件管理 API

提供固定核心 API：

- `GET /api/v1/plugins`
- `POST /api/v1/plugins/:id/enable`
- `POST /api/v1/plugins/:id/disable`
- `POST /api/v1/plugins/reload`
- `GET /api/v1/plugins/:id/diagnostics`

API 返回来源、类型、版本、状态、已注册能力、覆盖关系和最后错误。API 不负责联网安装插件。

### P6-T2 Desktop 管理界面

在 Settings 中增加插件列表、启停、reload、错误详情和项目 Trust 管理。前端插件组件注入仍属于非目标。

### P6-T3 最终清理

1. 删除全部旧 registry、dispatch、Provider switch 和冗余测试 fixture。
2. 删除无调用的兼容 helper 和过期注释。
3. 更新 `docs/10-development-status.md`、README、插件开发指南和示例。
4. 将 `docs/54` 状态改为 Superseded/Completed History，并指向本文。

### P6-T4 发布门禁

退出条件：

- 所有 Go module 的 `go test ./...` 通过。
- Agent、Gateway 关键包通过 `go test -race ./...`。
- Desktop 前端 lint、typecheck、test 和 build 通过。
- Gateway -> Runtime -> core/MCP/JS tool -> WS event 端到端测试通过。
- Trust、disable、reload、崩溃恢复和覆盖恢复测试通过。
- `rg` 确认不存在旧 dispatch、`providerFromOptions`、`opsOnlyTools` 和生产 ToolRunner。
- Windows 桌面端完成一次手工 smoke test。

## 12. 测试矩阵

| 层级 | 必测内容 |
| --- | --- |
| Registry 单测 | 覆盖栈、恢复、顺序、来源清理、并发、超时 |
| Hook 单测 | Transform 聚合、Block、Cancel、panic、取消、卸载 |
| Provider 单测 | 显式默认、覆盖、未知 Provider、环境与 profile 合并 |
| MCP 集成 | discovery、动态注册、冲突、run 清理、crash budget |
| JS Host 单测 | API bridge、异常、死循环、大小限制、generation 切换 |
| Trust 单测 | 路径规范化、允许/拒绝、记忆、CI override |
| Gateway 集成 | Dispatcher 兼容、管理 API、状态持久化 |
| 端到端 | 核心工具、MCP、JS 工具、Hook 改写、reload、重启恢复 |

建议每个 Phase 至少执行：

```powershell
go test ./...
```

命令应分别在受影响的 Go module 目录执行。涉及 Registry、Hook、JS Host 和 Gateway 状态时，再执行对应包的 `go test -race`。仓库根目录是 Go workspace，但不能直接用根目录的 `go test ./...` 代替逐 module 验证。

## 13. 任务拆分和提交边界

每个任务应形成可独立审查、可回滚且测试通过的提交。建议提交顺序：

| 顺序 | 任务组 | 允许并行 |
| --- | --- | --- |
| 1 | P0-T1 Registry 覆盖栈 | 与 P0-T2 并行 |
| 2 | P0-T2 HookOutcome | 与 P0-T1 并行 |
| 3 | P0-T3 标识与 Dispatcher 元数据 | 否 |
| 4 | P1-T1 Provider 接入 | 与 P1-T2 并行 |
| 5 | P1-T2/T3 工具元数据收敛和旧路径删除 | 否 |
| 6 | P2 Tool/Permission Hook | 否 |
| 7 | P2 Provider 与生命周期 Hook | 可按挂载点并行 |
| 8 | P3 MCP Registry / Gateway Dispatcher | 两项并行 |
| 9 | P4 JS Host、bridge、reload | loader 与 bridge 可并行，reload 后置 |
| 10 | P5 配置/状态/Trust/资源 | 配置与 DB migration 可并行，Trust 后置 |
| 11 | P6 API/UI/清理/发布 | API 先于 UI，清理最后执行 |

禁止在同一个提交中同时引入新执行路径和删除尚未被测试覆盖的旧路径。

## 14. 风险和控制

| 风险 | 影响 | 控制措施 |
| --- | --- | --- |
| Hook 改写导致持久化与 UI 事件不一致 | 高 | 固定改写发生点，端到端断言 provider/history/event 三方一致 |
| JS reload 与在途调用竞争 | 高 | generation 引用计数、成功后原子切换、延迟释放旧 VM |
| 插件覆盖后无法恢复内置能力 | 高 | Phase 0 先实现覆盖栈和 source unload |
| Project Trust 路径绕过 | 高 | 规范化绝对路径、符号链接测试、默认拒绝 |
| MCP 动态工具污染其他 Run | 高 | RunScopedRegistry，不向全局 Registry 写 per-run entry |
| Provider 默认值不确定 | 中 | 显式 defaultProvider，禁止依赖 map 顺序 |
| 配置来源难以排查 | 中 | typed merge 并保留每个字段的来源诊断 |
| 双轨代码长期保留 | 中 | Phase 1 设置删除门禁，新增功能不得调用 ToolRunner |
| 插件错误刷屏或拖慢主循环 | 中 | 限流日志、调用 deadline、失败预算和自动禁用 |

## 15. 完成定义

只有同时满足以下条件，插件系统才能标记为 `Implemented`：

1. P1、P2、P3、P4 四种插件形态均有生产入口和端到端测试。
2. 所有工具、Provider 和 RPC 都通过各自注册表解析，不存在硬编码分发 switch。
3. 12 个 Hook 均已挂载，并有清晰的输入、输出和阻断语义。
4. JS 插件支持加载、启停、覆盖、卸载和原子 reload。
5. Project Trust 默认拒绝未信任项目代码，状态可持久化。
6. 旧工具执行链和旧 Provider 工厂完全删除。
7. 外部协议兼容测试、全量测试、race test 和桌面 smoke test 全部通过。
8. 用户文档包含插件目录、manifest、JS API、配置优先级、Trust 和故障排查说明。
