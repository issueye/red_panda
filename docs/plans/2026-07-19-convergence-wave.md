# Convergence Wave — Implementation Plan

> **For implementers:** Execute wave-by-wave. Each wave is independently shippable and reversible. Prefer behavior-preserving refactors with focused table-driven tests.

**Date:** 2026-07-19
**Goal:** 收敛当前三个真实痛点（文档失真、Session 服务耦合、超大文件），把项目带到"声明与代码一致、单文件可读、领域边界清晰"的状态。
**Architecture:** 三层（Desktop / Gateway / Runtime）保持现有协作契约；本计划只做 Gateway 内部 service 拆分、Runtime/Protocol 文件物理拆分、跨层文档/UI 同步——不改任何运行时行为。
**Tech Stack:** Go modules (redpanda/agent, redpanda/gateway, redpanda/protocol); React (Desktop); existing table-driven tests + Playwright.
**Parent docs:** [docs/47](../47-complexity-redundancy-development-plan.md)、[docs/48](../48-session-correctness-optimization-plan.md)、[docs/49](../49-session-hard-delete-jsonl-archive.md)

---

## 1. 背景与边界

探索代码后，原"痛点"清单需要修正：

| 原痛点 | 实际情况 | 处理 |
|---|---|---|
| MCP tools/call 未实现 | **代码已通**（`modules/agent/internal/mcp/tools.go` + `mcp_bridge.go` + `mcp_tools_integration_test.go`）；但 README:79 / docs/19:1-5 / UI 文案声明"未实现" | **Wave A：文档/UI 同步** |
| Session 服务耦合 | 真实存在：`session.go` 850 + `session_compact_summary.go` 585，假分离、跨 service 调用 | **Wave B：Session 拆分** |
| RunStateStore 不持久 | **by design**——事件已持久化在 Gateway 的 `run_events` 表；Runtime 字段含 `CancelFunc`/`chan` 不可序列化；强行持久化会双写不一致 | **不纳入**，仅补一行 known-limitation 注释（Wave A） |
| 超大文件 | runtimeclient 871 / registry 772 / methods 864 | **Wave C：物理拆分** |
| MCP 性能加固 | 进程复用 / crash budget / discovery 缓存——主链路已通 | **不纳入**，列入 Wave D 后续 backlog |

### 1.1 非目标（明确不做）

- 不改任何运行时行为、协议契约、DB schema
- 不持久化 Runtime 内存状态
- 不做 MCP 进程复用/crash budget（性能加固留待后续 wave）
- 不重构 worker/pool.go（734 行单文件已是高度内聚的并发原语，拆分收益低风险高）

---

## 2. Wave A — 文档与 UI 同步（低风险，先做）

**动机：** README/docs/UI 与代码严重失真，是当下最影响信任和上手的问题。零功能改动。

### Task A1: 修正 README MCP 声明
**Files:**
- Modify: `README.md`

**Steps:**
1. 修改 `README.md:79`，把"no MCP server process is started...tools/call...not implemented"改为现状：
   - MCP server 在 Runtime 侧按需 spawn stdio 子进程
   - 完成 initialize / tools/list / tools/call 全链路
   - 已集成 permission（默认 RiskHigh）、tool 事件、stderr 隔离、secret 脱敏、各阶段超时
   - **未实现**：进程复用（每次 call 新进程）、crash/restart budget、跨运行 discovery 缓存、协议级 cancellation notification
2. 修改 `README.md:233` 把"read-only discovery"措辞改为"discovery + provider-facing tool registration"
3. 修改 `README.md:239-241` 的 Next Focus：把"v0.1.2/v0.1.4/v0.1.5 视为已交付"改为"v0.1.2/v0.1.4/v0.1.5 + tools/call MVP 均已交付，剩余为性能加固"
4. 添加一行"Known limitation: RunStateStore is in-memory by design; event sourcing lives in Gateway `run_events`"到 Current Running Loop 段末尾

**Verify:** `grep -n -i "tools/call\|not implemented\|read-only discovery" README.md` 不再有过时声明。

---

### Task A2: 修正 docs/19 状态标记
**Files:**
- Modify: `docs/19-mcp-stdio-tools-design.md`

**Steps:**
1. 把顶部 `Status: design only. MCP stdio tools are not implemented in v0.1.1.` 改为：
   ```
   Status: partially implemented (v0.2.0). config CRUD + discovery + tools/call MVP done.
   Remaining: process reuse, crash/restart budget, cross-run discovery cache, protocol-level cancel.
   See [docs/10-development-status.md](10-development-status.md) §MCP for current truth.
   ```
2. 在 "## 2. Scope" 后新增 "## 2.1 Implementation status (2026-07-19)" 小节，列出已实现/未实现两栏（与 README A1 内容一致）。

**Verify:** 文档顶部状态与 docs/10:25 一致。

---

### Task A3: 修正 docs/README 索引描述
**Files:**
- Modify: `docs/README.md`

**Steps:**
1. 把 `docs/README.md:40` 的 "MCP (config + discovery)" 改为 "MCP (config + discovery + tools/call MVP)"。

**Verify:** 索引描述与代码一致。

---

### Task A4: 修正 Desktop MCP 文案
**Files:**
- Modify: `modules/desktop/frontend/src/components/settings/shared.jsx`

**Steps:**
1. 修改 `shared.jsx:200-205` 的 `McpDiscoveryReadOnlyNotice`：
   - 函数重命名为 `McpDiscoveryCallReadyNotice`（或保留旧名仅改文案，避免触发多处引用；推荐改文案保留函数名以减小 diff）
   - 文案改为："发现：已列出服务器信息与可用工具清单。这些工具在对话中可被调用（默认高风险，受工具策略与权限模式约束）。性能加固（进程复用等）尚未完成。"
2. 修改 `shared.jsx:212, 222, 238, 239` 中"只读"措辞，改为"可用工具"。

**Verify:**
```bash
cd modules/desktop/frontend
npm test           # lib/* 单测仍通过
npm run build      # 构建无错
```

---

### Task A5: 在 RunStateStore 补已知限制注释
**Files:**
- Modify: `modules/agent/internal/runtime/run_state_store.go`

**Steps:**
1. 在 `RunStateStore` 类型注释（line 11 附近）追加一行：
   ```go
   // Known limitation: state is in-memory and process-local by design. Runtime
   // crash loses in-flight tool-call intermediates; Gateway's run_events table
   // is the source of truth for event-sourced recovery. Do NOT add persistence
   // here without solving the dual-write consistency problem (Runtime emit vs
   // Gateway ack). See docs/plans/2026-07-19-convergence-wave.md Wave A.
   ```

**Verify:** `go test ./modules/agent/internal/runtime/... -run RunStateStore -count=1` 通过。

---

### Wave A — Verification

```powershell
go test ./modules/agent/internal/runtime/... -count=1 -run RunStateStore
go build ./...
cd modules\desktop\frontend
npm test
npm run build
```

---

## 3. Wave B — Session 服务拆分（中风险，分 3 子任务）

**动机：** `SessionService` 850 行单 struct，含 12 个公共方法跨 5 个职责；`session_compact_summary.go` 585 行是"假分离"（文件分开但仍是 SessionService 方法）；`sessionLifecycle` 已被 WorkspaceService 跨 service 复用——结构已到拆分窗口期。

### 3.1 架构决策（ADR）

- **ADR-B1**：拆分方向是"提取 + 委托"，不破坏外部 API。`SessionService` 保留所有现有公共方法签名，内部委托给新提取的类型。Controller 与 Set 字段不动。
- **ADR-B2**：拆分顺序按依赖深度从浅到深：先 ContextPacker（被 RunService 跨 service 调用，最该独立）→ 再 Compactor（最内聚）→ 最后 PurgeService（已被共享）。
- **ADR-B3**：`compactGate` 从 SessionService 字段迁到 Compactor 字段——SessionService 副本问题随之消失。
- **ADR-B4**：测试文件原样迁移。`session_context_test.go` / `session_compact_summary_test.go` 已是包级函数测试，零改动成本。

### Task B1: 提取 SessionContextPacker
**动机：** `session_context.go` 的 `buildModelConversation` 被 `run_start.go:78` 跨 service 调用——它逻辑上属 session 但消费者是 run，是天然独立的纯函数模块。

**Files:**
- Create: `modules/gateway/internal/gateway/service/session_context_packer.go`（含 `SessionContextPacker` 类型 + 构造函数）
- Modify: `modules/gateway/internal/gateway/service/session_context.go`（函数搬走后保留 thin wrapper 或删除）
- Modify: `modules/gateway/internal/gateway/service/session.go`（`ContextState` 委托）
- Modify: `modules/gateway/internal/gateway/service/run_start.go`（改用 `SessionContextPacker`）
- Modify: `modules/gateway/internal/gateway/service/set.go`（注入 `SessionContextPacker`）
- Test: `modules/gateway/internal/gateway/service/session_context_test.go`（原样迁移，只改接收者）

**Steps:**
1. **Step 1**: 新建 `session_context_packer.go`，定义：
   ```go
   type SessionContextPacker struct {
       repos repository.Set
   }
   func NewSessionContextPacker(repos repository.Set) *SessionContextPacker { ... }
   func (p *SessionContextPacker) BuildModelConversation(ctx context.Context, sessionID string, opts ...) (...) { ... }
   func (p *SessionContextPacker) LoadModelContextForAssembly(...) { ... }
   func (p *SessionContextPacker) AssembleModelConversation(...) { ... }
   func (p *SessionContextPacker) SelectModelContextMessages(...) { ... }  // 纯函数，可保持包级
   ```
2. **Step 2**: 把 `session_context.go` 的 4 个函数体搬到 `SessionContextPacker` 方法；保留 `selectModelContextMessages` 为包级纯函数（无状态依赖）。
3. **Step 3**: `SessionService` 加字段 `packer *SessionContextPacker`；`ContextState` 方法体改为 `return s.packer.BuildModelConversation(...)`。
4. **Step 4**: `run_start.go:78` 改为 `s.packer.BuildModelConversation(...)`（RunService 也加 `packer` 字段，由 `Set` 注入同一个实例）。
5. **Step 5**: `set.go` 在 `NewSet` 时构造单一 `SessionContextPacker` 实例，同时注入 `Session` 和 `Run`。

**Verify:**
```powershell
go test ./modules/gateway/internal/gateway/service/... -count=1 -run "Context|SessionContext|RunStart"
go test ./modules/gateway/internal/gateway/service/... -count=1 -run Session
```

---

### Task B2: 提取 SessionCompactor
**动机：** 最干净的边界。`session.go:454-606` 的 4 个公共方法 + `session_compact_summary.go` 整文件 585 行 + `compactGate` + pause/resume helpers 形成高度内聚的"压缩子系统"。

**Files:**
- Create: `modules/gateway/internal/gateway/service/session_compactor.go`（`SessionCompactor` 类型）
- Delete/Empty: `modules/gateway/internal/gateway/service/session_compact_summary.go`（内容整体迁入 compactor）
- Modify: `modules/gateway/internal/gateway/service/session.go`（CompactPreview/Compact/CompactionState/Summaries 委托；删除 `compactGate` 字段、`beginSessionCompact`、`pauseSessionForCompact`、`resumeRunsAfterCompact`、`resumeSessionAfterCompact`）
- Modify: `modules/gateway/internal/gateway/service/set.go`（注入 `SessionCompactor`）
- Test: `modules/gateway/internal/gateway/service/session_compact_summary_test.go`（原样迁移）
- Test: `modules/gateway/internal/gateway/service/session_test.go`（Compact 相关用例不变，因外部 API 不动）

**Steps:**
1. **Step 1**: 新建 `session_compactor.go`：
   ```go
   type SessionCompactor struct {
       repos     repository.Set
       store     sessionStore         // 共享同一个实例
       runtime   *runtimeclient.Client
       hub       *eventhub.Hub
       gate      *sessionCompactGate  // 从 SessionService 迁入
       profiles  *ProviderProfileService  // resolveCompactProvider 用
   }
   func NewSessionCompactor(...) *SessionCompactor { ... }
   func (c *SessionCompactor) Preview(ctx, sessionID, req) (..., error)
   func (c *SessionCompactor) Apply(ctx, sessionID, req) (..., error)
   func (c *SessionCompactor) State(ctx, sessionID) (..., error)
   func (c *SessionCompactor) Summaries(ctx, sessionID) (..., error)
   ```
2. **Step 2**: 把 `session_compact_summary.go` 整文件内容（plan / build / LLM / local / parse 函数）迁入 `session_compactor.go` 或拆为 `session_compactor_planner.go` + `session_compactor_summary.go`（后者保留命名以减少 diff）。
3. **Step 3**: 把 `session.go` 的 `compactGate` 字段、`beginSessionCompact`、`pauseSessionForCompact`、`resumeRunsAfterCompact`、`resumeSessionAfterCompact`、`ensureOpenTasks` 搬到 compactor。
4. **Step 4**: `SessionService` 加字段 `compactor *SessionCompactor`；`CompactPreview/Compact/CompactionState/Summaries` 方法体改为单行委托：
   ```go
   func (s SessionService) Compact(ctx, id, req) (..., error) { return s.compactor.Apply(ctx, id, req) }
   ```
5. **Step 5**: `set.go` 构造唯一 `SessionCompactor`，注入 `Session`。SessionService 副本问题彻底消失（compactGate 现在只属于 compactor 一个实例）。

**Verify:**
```powershell
go test ./modules/gateway/internal/gateway/service/... -count=1 -run "Compact|Compaction|Summary"
go test ./modules/gateway/internal/gateway/service/... -count=1 -race -run "Compact"   # 并发契约
```

预期：`TestSessionServiceCompactResumesWorkersWhenSummaryFails`、`TestSessionServiceCompactReportsResumeFailure`、`TestSessionServiceCompactRejectsConcurrent` 全部通过。

---

### Task B3: 升级 sessionLifecycle → PurgeService
**动机：** `sessionLifecycle` 已被 `WorkspaceService.Remove`（workspace.go:17）跨 service 复用做级联删除——它本就不是 SessionService 私有。把它显式化为独立 service，命名清晰化。

**Files:**
- Rename/Modify: `modules/gateway/internal/gateway/service/session_lifecycle.go` → `session_purge_service.go`
- Modify: `modules/gateway/internal/gateway/service/session.go`（Delete 委托）
- Modify: `modules/gateway/internal/gateway/service/workspace.go`（直接依赖 PurgeService，不再借 SessionService 的 lifecycle 字段）
- Modify: `modules/gateway/internal/gateway/service/set.go`（注入 PurgeService 到 Session + Workspace）
- Test: `modules/gateway/internal/gateway/service/session_test.go`（Delete/Archive 用例不变）

**Steps:**
1. **Step 1**: 把 `sessionLifecycle` 重命名为 `PurgeService`（type 改名、构造函数改名），方法 `delete` 改为 `PurgeSessions(ids, reason)`。文件改名 `session_lifecycle.go` → `session_purge_service.go`。
2. **Step 2**: `SessionService.Delete` 委托 `s.purge.PurgeSessions([]string{id}, "user_delete")`。
3. **Step 3**: `WorkspaceService.Remove` 改为依赖 `PurgeService` 直接调用，不再借道 SessionService 的私有字段。
4. **Step 4**: `set.go` 构造唯一 `PurgeService`，注入 Session 和 Workspace。

**Verify:**
```powershell
go test ./modules/gateway/internal/gateway/service/... -count=1 -run "Session|Workspace|Purge|Delete"
go test ./modules/gateway/internal/gateway/service/... -count=1 -run "JSONL|Archive|HardDelete"
```

---

### Task B4: 文档同步
**Files:**
- Create: `docs/50-session-service-decomposition.md`（design doc，记录拆分边界与新 service 关系图）

**Steps:**
1. 用 docs/49 的 front matter 表格格式（Date/Status/Related/BREAKING: none）。
2. 章节：1. 背景与目标 2. 拆分边界（ContextPacker/Compactor/PurgeService）3. Set 装配关系图 4. 验收 5. 风险（"假分离"消失、controller API 不变）6. 进度。

---

### Wave B — Verification matrix

| After | Command | Expected |
|---|---|---|
| B1 | `go test ./modules/gateway/internal/gateway/service/... -run Context` | pass |
| B2 | `go test ./modules/gateway/internal/gateway/service/... -race -run Compact` | pass, no race |
| B3 | `go test ./modules/gateway/internal/gateway/service/... -run "Session\|Workspace"` | pass |
| All B | `go test ./modules/gateway/internal/gateway/... -count=1` | pass |
| All B | `powershell -File scripts\protocol-compat.ps1` | pass (外部协议契约不变) |

**完成定义：**
- `session.go` 从 850 行降到 ≤ 400 行（保留 CRUD/Fork/History/ContextState 委托）
- `session_compact_summary.go` 不再以"SessionService 方法"形式存在
- `SessionService` 不再持有 `compactGate` 字段
- 所有现有测试零修改通过

---

## 4. Wave C — 超大文件物理拆分（中风险，纯重构）

**动机：** 三个文件超 700 行且内部结构清晰，按职责拆分能显著降低阅读门槛。**不改任何符号签名、不改任何运行时行为。**

### Task C1: 拆分 runtimeclient/client.go (871 → 4 文件)
**Files:**
- Modify: `modules/gateway/internal/gateway/infra/runtimeclient/client.go`（保留 Client struct + New + 基础设施 ~250 行）
- Create: `modules/gateway/internal/gateway/infra/runtimeclient/client_run.go`（Execute/Cancel/Pause/Resume + per-run 子进程管理 ~200 行）
- Create: `modules/gateway/internal/gateway/infra/runtimeclient/client_worker.go`（Workers/CancelAssignment/Send/Receive/PoolStatus ~120 行）
- Create: `modules/gateway/internal/gateway/infra/runtimeclient/client_mgmt.go`（Initialize/DiscoverMCP/Skills*/Shutdown/Status ~180 行）
- Create: `modules/gateway/internal/gateway/infra/runtimeclient/client_transport.go`（ensureStarted/ensureStartedStdio/ensureStartedIPC/runtimeTransport/buildChildEnv ~150 行）

**Steps:**
1. **Step 1**: 同包同 struct method 物理搬家——Go 允许同一 struct 的方法分布在不同文件。每个方法签名不动。
2. **Step 2**: 公共 RPC 方法按域分文件（run/worker/mgmt）；transport 私有方法单独成文件；`call` / `nextRequestID` / `removePending` 等 RPC 基础设施留在 `client.go`。
3. **Step 3**: 文件头注释每个文件的职责范围。
4. **Step 4**: `client.go` 顶部加文件清单注释（类似 `runtime.go:4` 的风格）。

**Verify:** `go test ./modules/gateway/internal/gateway/infra/runtimeclient/... -count=1`。

---

### Task C2: 拆分 tools/registry.go (772 → 按 tool 域多文件)
**动机：** `registry.go:39-715` 是 676 行的字面量 tool schema 表，是纯数据，按工具域拆零风险。

**Files:**
- Modify: `modules/agent/internal/tools/registry.go`（保留 type/struct/小函数 ~80 行）
- Create: `modules/agent/internal/tools/defs_workspace.go`（workspace.* 定义）
- Create: `modules/agent/internal/tools/defs_coding.go`（edit_file/apply_patch/write_file/diff_file）
- Create: `modules/agent/internal/tools/defs_web.go`（web_*）
- Create: `modules/agent/internal/tools/defs_orchestration.go`（skill.*/worker.*）
- Create: `modules/agent/internal/tools/defs_state.go`（memory.*/todo.*/goal.*/context.*）

**Steps:**
1. **Step 1**: 把 `stableToolDefinitions` 函数体改为 aggregator：
   ```go
   func stableToolDefinitions() []ptools.Definition {
       return append(append(append(append(append(
           nil,
           workspaceToolDefinitions()...,
       ), codingToolDefinitions()...), webToolDefinitions()...),
           orchestrationToolDefinitions()...), stateToolDefinitions()...)
   }
   ```
2. **Step 2**: 每个域文件定义独立 `xxxToolDefinitions() []ptools.Definition`，函数体就是原来对应的字面量片段。
3. **Step 3**: 顺序保持与原 `stableToolDefinitions` 完全一致（避免 diff 噪音 + 保持 provider 看到的工具顺序稳定）。

**Verify:** `go test ./modules/agent/internal/tools/... -count=1`；特别注意 `TestStableToolDefinitions*`（如有顺序断言）。

---

### Task C3: 拆分 protocol/methods.go (864 → 按域多文件)
**Files:**
- Modify: `modules/protocol/methods/methods.go`（保留 const method 名 + 公共 helpers ~200 行）
- Create: `modules/protocol/methods/methods_worker.go`（Worker* / Assignment / Message 类型）
- Create: `modules/protocol/methods/methods_run.go`（Run* / Pause / Resume / Cancel）
- Create: `modules/protocol/methods/methods_state_tool.go`（StateToolExecute + AsXxxParams + Resolve / Requires）
- Create: `modules/protocol/methods/methods_goal.go`（Goal* / GoalToolExecute*）
- Create: `modules/protocol/methods/methods_skill.go`（SkillSummary/Detail/Mutate/Delete）
- Create: `modules/protocol/methods/methods_mcp.go`（MCPDiscover*）
- Create: `modules/protocol/methods/methods_initialize.go`（Initialize/Ping/Capability/Environment）

**Steps:**
1. **Step 1**: const 常量块（method 名）保留在 `methods.go`，因为它们是协议面入口，集中可读。
2. **Step 2**: 按 domain 把 type 块搬到对应文件，签名不动。
3. **Step 3**: StateToolExecute 相关的 helper 函数（`ResolveStateToolDomain` / `StateToolRequiresSession` / `As*Params` / `NewStateToolParams`）整体搬到 `methods_state_tool.go`——它们是 state-tool 子系统专用的。

**Verify:** `go test ./modules/protocol/... -count=1`（含 state_tool_test.go / legacy_test.go）。

---

### Wave C — Verification matrix

| After | Command | Expected |
|---|---|---|
| C1 | `go test ./modules/gateway/internal/gateway/infra/runtimeclient/... -count=1` | pass |
| C2 | `go test ./modules/agent/internal/tools/... -count=1` | pass |
| C3 | `go test ./modules/protocol/... -count=1` | pass |
| All C | `go build ./...` | pass |
| All C | `go vet ./...` | pass |

**完成定义：**
- 没有任何文件超过 500 行（registry.go 主文件、methods.go 主文件、client.go 主文件）
- 所有测试零修改通过
- 没有任何 import 循环

---

## 5. Wave D — MCP 性能加固 backlog（不执行，仅记录）

明确不纳入本次计划，作为后续独立 wave 的输入。每个 backlog 项附现有代码位置：

| 项 | 现有位置 | 收益 |
|---|---|---|
| MCP 进程复用 / 长生命周期 session | `modules/agent/internal/mcp/tools.go:121` 注释"进程复用延后至 D3"；当前每次 call spawn 新进程 | 性能：避免每次 call 的 spawn+initialize 开销 |
| Crash/restart budget（3 次/60 秒禁用） | `modules/agent/internal/mcp/manager.go` 无相关字段 | 健壮性：避免坏 server 拖垮 run |
| 跨运行 discovery 缓存 | `modules/agent/internal/mcp/manager.go`、`mcp_bridge.go:31-47` 每次运行重新 discover | 性能：同 server 配置不变时复用 tools/list |
| 协议级 cancellation notification | `modules/mcpkit/session.go` 无 Cancel 方法 | 健壮性：当前仅靠 ctx cancel 让 call 立即结束，无 MCP 协议层通知 |
| 协议违规专门状态码 | `modules/mcpkit/session.go:127-137` 靠 mcp-go 静默吞非 JSON 行 + initialize timeout 兜底 | 容错：能区分"server 卡住"与"server 输出垃圾" |
| 试运行 tools/call 按钮 | Desktop `McpTab.jsx` 无 call UI | UX：发现工具后能手动测试 |

---

## 6. 风险与回滚

**Wave A 风险**：极低。纯文档/注释/文案。回滚 = git revert 单 commit。

**Wave B 风险**：
- 中等。拆分是行为保持的，但 Set 装配改动多。每个 Task 后跑完整 gateway 测试 + protocol-compat.ps1。
- 回滚点：每个 Task 是独立 commit，可单 Task revert。Task B1/B2/B3 互相独立，B1 失败不影响 B2 决策（B2 不依赖 B1）。

**Wave C 风险**：
- 低。纯文件物理拆分，符号签名不变。Go 同包同 struct 方法跨文件是原生支持。
- 回滚点：每个 Task 独立 commit。

**全局不变量：**
- Wave A/B/C 之间无强依赖，可任意顺序执行，但推荐 A→B→C（先消除文档噪音，再拆核心服务，最后清理文件）
- 每个 Task 完成后必须 `go build ./... && go vet ./...` 通过
- Wave B 完成后必须跑 `scripts/protocol-compat.ps1` 验证外部协议契约未破

---

## 7. 执行顺序总览

```
Wave A (文档/UI 同步, ~0.5 天)
  A1 → A2 → A3 → A4 → A5
    ↓
Wave B (Session 拆分, ~2 天)
  B1 (ContextPacker) → B2 (Compactor) → B3 (PurgeService) → B4 (docs/50)
    ↓
Wave C (超大文件拆分, ~1 天)
  C1 (runtimeclient) → C2 (registry) → C3 (methods)
    ↓
[Wave D backlog 记录在案, 不执行]
```

总工作量：~3.5 天，零运行时行为变更，全部可独立 revert。
