# Design: Scheduled Tasks（定时任务）

| Field | Value |
| --- | --- |
| **Title** | Scheduled Tasks / Cron-like Agent Runs |
| **Version** | v0.2.1 |
| **Date** | 2026-07-16 |
| **Status** | Draft (ready for implementation planning) |
| **Related** | `docs/02-functional-design.md`, `docs/03-gateway-mvc-design.md`, `docs/06-desktop-gateway-integration.md`, `docs/30-todo-feature-design.md`, `docs/37-goal-driven-execution-v2.md`, `docs/44-v0.2.1-development-plan.md` |

---

## 1. Overview

`red_panda` 目前只有**用户/客户端主动触发**的 run（WebSocket `run.start`、Goal continue 等）。用户无法表达：

- 「每天 09:00 汇总昨日工作区变更」
- 「每 30 分钟检查一次 CI / 服务状态并写入 memory」
- 「下周一 10:00 一次性提醒并生成计划」

本设计在 **Gateway 层**增加一等公民 **Scheduled Task（定时任务）**：

1. 任务定义持久化在 SQLite（Gateway 权威）。
2. Gateway 内置轻量 **Scheduler loop** 计算到期并触发 run。
3. 每次触发走现有 `RunService.Start` 路径，复用 Provider / 权限 / 工具 / 投影 / 恢复。
4. Desktop 提供管理 UI；可选 Runtime 工具让 Agent 代建/代改任务。
5. 不依赖 Desktop 在线即可**触发**（Desktop 仅影响权限确认与实时展示）。

一句话：

> **Scheduled Task = 可持久、可审计的「到点自动 `run.start`」配置；执行面仍是现有 Agent Runtime，状态权威仍在 Gateway。**

与现有概念边界：

| 概念 | 作用 | 与定时任务关系 |
| --- | --- | --- |
| **Todo** | 会话内执行中 checklist | 无关；定时任务不复用 `todo_items` |
| **Goal** | 长程目标反馈环 | 定时任务可选择 `run_kind=goal` 创建/继续目标（v0.2.1 可选薄支持） |
| **Memory** | 持久知识 | 定时 run 可正常读写 memory |
| **Worker / Assignment** | 单次 run 内协作 | 定时触发的是 root run，之后仍走 v0.2 Worker 模型 |
| **MCP tasks（外部）** | 第三方调度 schema 参考 | 仅借鉴字段语义，不实现 MCP 任务市场 |

---

## 2. Goals & Non-Goals

### 2.1 Goals

1. 用户可创建 **一次性**、**固定间隔**、**cron 表达式** 三类本地定时任务。
2. Gateway 进程存活期间，到期任务自动触发 Agent run，并写入完整 run 投影/时间线。
3. 任务配置、启用/暂停、下次执行时间、最近执行结果可查询、可恢复（Gateway 重启后不丢定义）。
4. 默认安全：自动 run 使用**收紧的工具/权限策略**，避免无人值守时任意 shell 写入。
5. Desktop 有一等 **定时任务** 管理面（列表 / 创建编辑 / 启停 / 执行历史 / 跳转到 run）。
6. 可选：Agent 通过 `schedule.*` 工具管理任务（高风险 + 权限门控）。
7. 与三层架构一致：**Gateway 调度与持久化，Runtime 执行，Desktop 展示**。

### 2.2 Non-Goals（v0.2.1 明确不做）

1. 操作系统级休眠唤醒 / Windows Task Scheduler 注册（不把 red_panda 做成系统服务安装器）。
2. 分布式多机调度、云同步、远程 Webhook 触发。
3. 完整 RFC 5545 RRULE / 复杂日历排除日。
4. 邮件 / 短信 / 系统托盘强提醒（v0.2.1 仅 App 内 WS 事件 + 历史；托盘可后置）。
5. 用定时任务替代 Goal 状态机或 Todo 清单。
6. MCP 远程任务市场或兼容外部 Grok `tasks` MCP 协议。
7. Desktop 关闭且 Gateway 未运行时仍执行（本地进程模型的固有限制；文档写清）。
8. 自动 catch-up 补跑所有错过的间隔（避免重启风暴）；见 §6.4。

---

## 3. Recommended product decisions

下列为 **v0.2.1 默认选型**（实现按此落地；若产品变更再改文档）。

| 议题 | 推荐 | 理由 |
| --- | --- | --- |
| 调度归属 | **Gateway 进程内 loop** | Desktop 可关；Gateway 已是状态权威与 run 入口 |
| 表达式 | **one_shot / interval / cron(5-field)** | 覆盖 90% 场景；避免 RRULE 复杂度 |
| 时区 | **IANA 时区字符串**，默认本机时区 | cron 语义清晰 |
| 触发动作 | **`run.start` 等价内部调用** | 零新执行引擎 |
| 会话策略 | 默认 **`session_mode=new_each_run`** | 隔离上下文；可选 `fixed_session` |
| 执行种类 | 默认 **`run_kind=chat`** | Goal 可选 `run_kind=goal`（薄支持） |
| 重叠策略 | 默认 **`overlap=skip`** | 防止堆积；可选 `queue` 后置 |
| 错过策略 | **skip-missed**，只算 next | 防重启补跑风暴 |
| 无人值守权限 | 默认 **`permission_mode=deny`** + 可配置 allowlist | 安全优先 |
| Agent 工具 | **有，但高风险** | 与 memory/todo 模式一致，Gateway RPC 持久化 |

---

## 4. Architecture

### 4.1 Layering

```text
┌─────────────────────────────────────────────────────────────┐
│ Desktop                                                      │
│  SchedulesPanel：CRUD / 启停 / 历史 / 跳转 run               │
│  WS 订阅 schedule.* 与 run.event                            │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTP + WebSocket
┌───────────────────────────▼─────────────────────────────────┐
│ Gateway（状态权威）                                          │
│  ScheduleService  CRUD + 校验 + next_run_at 计算             │
│  SchedulerLoop    每秒/可配置 tick，捞取 due 任务             │
│  RunService.Start  已有路径：admit → prepare → dispatch      │
│  SQLite           scheduled_tasks / scheduled_task_runs      │
│  EventHub         schedule.updated / schedule.fired / …     │
└───────────────────────────┬─────────────────────────────────┘
                            │ IPC/JSON-RPC
┌───────────────────────────▼─────────────────────────────────┐
│ Agent Runtime                                                │
│  正常 reply / tool loop / permission / worker               │
│  可选 schedule.* 工具 → schedule.tool.execute → Gateway     │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 Sequence：到期触发

```mermaid
sequenceDiagram
  participant Loop as SchedulerLoop
  participant Svc as ScheduleService
  participant DB as SQLite
  participant Run as RunService
  participant RT as Agent Runtime
  participant Hub as EventHub
  participant UI as Desktop

  Loop->>Svc: tick: claim due tasks
  Svc->>DB: SELECT ... WHERE enabled AND next_run_at <= now FOR UPDATE-ish
  Svc->>DB: insert scheduled_task_runs (status=starting)
  Svc->>Hub: schedule.fired
  Svc->>Run: Start(internal payload from task)
  Run->>RT: run.execute
  RT-->>Hub: run.event...
  Run-->>Svc: on finish callback / poll status
  Svc->>DB: update run row + task last_* + next_run_at
  Svc->>Hub: schedule.updated
  Hub-->>UI: WS fanout
```

### 4.3 为何不放在 Runtime / Desktop

| 放在 | 问题 |
| --- | --- |
| Desktop | 关 UI 即停；违背「本地常驻 Agent」预期 |
| Agent Runtime | Runtime 可 per-run 起停；不持有跨 run 配置权威 |
| 系统 cron 调 CLI | 跨平台差、难审计、难与权限/会话投影统一 |
| **Gateway** | 已管 session/run/projection；最自然 |

---

## 5. Domain model

### 5.1 `scheduled_tasks`

| Column | Type | Notes |
| --- | --- | --- |
| `id` | string PK | `sched_...` |
| `name` | string | 短名，唯一（workspace 或全局作用域内） |
| `description` | text | 可选 |
| `enabled` | bool | 暂停不删 |
| `schedule_kind` | string | `one_shot` \| `interval` \| `cron` |
| `cron_expr` | string | 5-field cron；仅 cron |
| `interval_sec` | int | 仅 interval；最小 60 |
| `run_at` | datetime | 仅 one_shot 的计划时刻（存 UTC） |
| `timezone` | string | IANA，如 `Asia/Shanghai` |
| `prompt` | text | 触发时作为 user input |
| `run_kind` | string | `chat`（默认）\| `goal` |
| `session_mode` | string | `new_each_run`（默认）\| `fixed_session` |
| `session_id` | string | fixed 时必填；new 时可选「模板会话」只继承 workspace/options |
| `workspace_root` | string | 必填或可从 session 推导 |
| `provider_profile_id` | string | 可选 |
| `run_options_json` | text | 序列化 run.start options 子集 |
| `tool_policy` | string | 默认 `allowlist` 或 `default` |
| `tool_allowlist_json` | text | 默认收紧集合 |
| `tool_denylist_json` | text | |
| `permission_mode` | string | 默认 `deny` |
| `overlap_policy` | string | `skip`（默认）\| `queue`（v0.2.1 可只实现 skip） |
| `missed_policy` | string | `skip_missed`（默认） |
| `max_runs` | int | 0=无限；one_shot 强制 1 |
| `run_count` | int | 已成功启动次数 |
| `last_run_id` | string | |
| `last_status` | string | idle/running/succeeded/failed/skipped/… |
| `last_error` | text | |
| `last_fired_at` | datetime | |
| `next_run_at` | datetime index | Scheduler 主索引 |
| `created_at` / `updated_at` | datetime | |
| `deleted_at` | datetime nullable | soft delete |

**唯一性建议：** `(name)` 在未删除记录中唯一，或 `(workspace_root, name)` 唯一。v0.2.1 推荐 **全局 name 唯一**（实现简单）；若冲突返回 409。

### 5.2 `scheduled_task_runs`

每次触发一条审计记录（独立于 `run_records`，但关联 `run_id`）。

| Column | Type | Notes |
| --- | --- | --- |
| `id` | string PK | `srun_...` |
| `schedule_id` | string index | |
| `run_id` | string index | 成功 dispatch 后填入 |
| `session_id` | string | 实际使用的 session |
| `status` | string | `starting` \| `running` \| `succeeded` \| `failed` \| `skipped` \| `cancelled` |
| `skip_reason` | string | overlap / disabled / max_runs / … |
| `scheduled_for` | datetime | 原计划点 |
| `started_at` / `finished_at` | datetime | |
| `error` | text | |
| `created_at` | datetime | |

### 5.3 Schedule expression 语义

#### `one_shot`

- 字段：`run_at` + `timezone`（展示用；存储建议 UTC 瞬时）。
- 触发一次后：`enabled=false`，`next_run_at=null`，`run_count=1`。

#### `interval`

- 字段：`interval_sec >= 60`。
- `next_run_at = last_fired_at + interval`（首次可用 `created_at` 或显式 `start_after`）。
- v0.2.1 增加可选 `start_after`（datetime）避免创建即立刻连跑。

#### `cron`

- 5 字段：`minute hour day-of-month month day-of-week`。
- 使用成熟库（推荐 Go：`github.com/robfig/cron/v3` 的 **带时区** Parser，秒级关闭）。
- 禁止过密：若解析出未来 1 小时内触发次数 > `N`（建议 N=60），创建/更新拒绝。

### 5.4 `run_options` 白名单

从 `run.start` options 允许复制的子集（避免任意注入内部字段）：

```text
model, provider_profile_id, runtime_mode,
tool_policy, tool_allowlist, tool_denylist, permission_mode,
worker_pool_size, web_search_max_results, web_fetch_max_bytes
```

**禁止** 由 schedule 直接写入的内部字段：`goal_id` 除非 `run_kind=goal` 且走 Goal 入口；禁止伪造 `root_run_id`。

### 5.5 默认安全策略（新建任务）

| 项 | 默认值 |
| --- | --- |
| `permission_mode` | `deny` |
| `tool_policy` | `allowlist` |
| `tool_allowlist` | `workspace.read_file`, `workspace.list`, `workspace.grep`, `workspace.diff_file`, `memory.list`, `memory.create`, `web.search`, `web.fetch` |
| 高风险默认排除 | `shell.exec`, `workspace.write_file`, `workspace.edit_file`, `workspace.apply_patch`, `skill.*`, MCP 工具 |

用户可在 UI 显式放宽；Agent 通过工具放宽必须 **高风险权限批准**。

---

## 6. Scheduler runtime behavior

### 6.1 Loop

- Gateway `app` 启动后启动 `SchedulerLoop` goroutine。
- Tick 默认 **1s**（可 env：`RED_PANDA_SCHEDULER_TICK_MS`，最小 200ms）。
- 每 tick：
  1. 查询 `enabled=1 AND deleted_at IS NULL AND next_run_at <= now`，按 `next_run_at` 排序，**limit**（默认 10）。
  2. 对每条任务 `ClaimAndFire`（事务内：再读、重叠检查、写 run 行、推进/清空 next）。
  3. 事务外调用 `RunService.Start`；失败回写 run 行与 `last_error`。

### 6.2 Claim 语义（防双触发）

单进程 Gateway 假设：用 DB 事务 + `updated_at` 乐观锁，或「先把 `next_run_at` 推到未来 / 置 running 标记」再执行。

v0.2.1 单实例足够：

```text
BEGIN
  reload task
  if !enabled or next_run_at > now → abort
  if overlap=skip and has open scheduled_task_run → status=skipped, recompute next, COMMIT, return
  insert scheduled_task_run(status=starting)
  set last_status=running, last_fired_at=now
  recompute next_run_at (or null if one_shot / max_runs)
COMMIT
dispatch RunService.Start
```

### 6.3 与 Run 生命周期衔接

- `RunService.Start` 成功：`scheduled_task_runs.run_id` 赋值，`status=running`。
- 监听既有 run 终态（投影更新或 EventHub finish）：映射到 schedule run：
  - run completed → schedule run `succeeded`
  - run failed/denied → `failed`
  - run cancelled → `cancelled`
- 更新 `scheduled_tasks.last_run_id/last_status/last_error`。

实现偏好：**在 RunService 终态投影处挂一个可选 hook**（`OnRunTerminal`），比 Runtime 回传新 RPC 更简单。

### 6.4 Missed runs（Gateway 重启）

- 不补跑中间所有 tick。
- 启动时：对所有 enabled 任务，若 `next_run_at` 已过：
  - **立即 fire 一次**（可选）或 **跳到下一个未来点**。
- **推荐默认：`catch_up=one`** —— 最多补一次 due 任务，然后按正常规则算 next。  
  若担心启动风暴：`catch_up=none`（仅重算 next 到未来，不立刻 fire）。  
  **v0.2.1 默认 `catch_up=one`，并加全局限流：启动 30s 内最多并发 schedule fire = 2。**

### 6.5 Gateway 未运行

- 任务不执行；`next_run_at` 保持不变。
- Desktop 文案明确：「定时任务由 Gateway 执行，请保持 Gateway 运行」。

### 6.6 权限等待与无人值守

- 若 run 进入 permission wait 且 Desktop 不在线：run 保持 pending permission（现有行为）。
- 定时任务配置默认 `permission_mode=deny`，高风险工具直接失败，避免永久挂起。
- UI 对 `last_status=failed` 且错误含 permission/deny 给出引导。

---

## 7. API & Protocol

### 7.1 HTTP（Desktop / 外部客户端）

Base：`/api/v1/schedules`

| Method | Path | 说明 |
| --- | --- | --- |
| `GET` | `/schedules` | 列表；query: `enabled`, `limit`, `offset` |
| `POST` | `/schedules` | 创建 |
| `GET` | `/schedules/:id` | 详情（含 next/last） |
| `PUT` | `/schedules/:id` | 更新（重算 next） |
| `DELETE` | `/schedules/:id` | soft delete + disable |
| `POST` | `/schedules/:id/enable` | enabled=true，重算 next |
| `POST` | `/schedules/:id/disable` | enabled=false |
| `POST` | `/schedules/:id/trigger` | 立即手动触发一次（不改变 cron 相位或可选 shift） |
| `GET` | `/schedules/:id/runs` | 执行历史 |

响应信封与现有 Gateway 一致：`{ ok, data, error? }`。

### 7.2 WebSocket 事件（建议）

在现有 WS 通道上广播（不必新端口）：

| Event type | 何时 |
| --- | --- |
| `schedule.updated` | CRUD / enable / next 变化 |
| `schedule.fired` | claim 成功即将/已经 start |
| `schedule.run_finished` | 关联 run 终态 |

客户端也可仅靠 HTTP 轮询 + 既有 `run.event`；事件为增强。

### 7.3 内部：从 Schedule 构造 `RunStartPayload`

伪字段映射：

```text
session_id   = resolveSession(task)  // new or fixed
input        = task.prompt
options      = merge(defaults, task.run_options, safety)
source       = "schedule"
schedule_id  = task.id               // 写入 run metadata / input metadata
```

`source=schedule` 应进入 `run_records` 可查询字段或 `Input` 前缀元数据，便于 Activity 过滤「定时触发」。

**建议在 `RunRecord` 增加可选列 `TriggerSource` / `TriggerRef`（`schedule:<id>`）**；若不想改 schema，可先把 metadata 放进 `Input` 旁路 JSON——更推荐正式列，便于索引。

### 7.4 Runtime 工具（可选但建议同版本做 MVP）

| Tool | Risk | 说明 |
| --- | --- | --- |
| `schedule.list` | low | 列表摘要 |
| `schedule.create` | high | 创建 |
| `schedule.update` | high | 更新 |
| `schedule.enable` / `schedule.disable` | high | 启停 |
| `schedule.delete` | high | soft delete |
| `schedule.trigger` | high | 立即跑一次 |

执行路径对齐 memory/todo：

```text
Provider tool_call
  → Runtime policy/permission
  → JSON-RPC schedule.tool.execute
  → Gateway ScheduleService
  → 标准 tool_* 事件
```

`schedule.*` 进入默认 **高风险 / denylist for specialists**（根 run 可调，Worker 默认不可改系统调度）。

### 7.5 Protocol DTO（`modules/protocol`）

新增例如：

- `ScheduleDTO`, `ScheduleRunDTO`
- `ScheduleToolExecuteParams` / `Result`
- 可选并入 `StateTool` domain=`schedule`（若继续中心化 state-tool 管道）

---

## 8. Desktop UX

### 8.1 信息架构

- 左侧 Sidebar 或 Settings 增加 **「定时」** 入口（推荐 Sidebar 一级，与会话并列，因属常驻能力）。
- 主区：`SchedulesPanel`
  - 顶部：新建按钮 + 过滤（启用/全部）
  - 列表行：名称、表达式摘要、下次时间、上次状态、启用开关
  - 详情抽屉/页：prompt、workspace、策略、历史 runs
  - 历史 run 点击 → 切换到对应 session 并打开 Activity/对话

### 8.2 创建表单（中文标签）

- 名称、描述
- 类型：一次性 / 间隔 / Cron
- 时间与时区
- 提示词（多行）
- 工作区路径（默认当前 workspace）
- 会话模式：每次新建 / 固定会话
- Provider 配置
- 安全：权限模式、工具白名单（展示默认收紧说明）
- 高级：max_runs、立即触发

### 8.3 文案要点

- 「任务由 Gateway 调度，关闭 Desktop 不影响触发（需 Gateway 运行）」。
- 「默认识别为无人值守：拒绝需批准的高风险工具」。
- 空状态：引导创建第一个每日摘要任务。

### 8.4 与 Chat 的关系

- 定时 run 产生的消息落在对应 session；用户可在会话列表看到 `定时 · <name>` 自动命名的 session（`new_each_run` 时）。
- 不在 Composer 默认塞 schedule 控件；命令面板可加 `/schedule` 快捷打开面板（可选）。

---

## 9. Gateway MVC 落点

| 层 | 文件建议 |
| --- | --- |
| model | `model/models.go` → `ScheduledTask`, `ScheduledTaskRun` |
| migrate | `infra/database/migrate.go` |
| repository | `repository/schedule.go` |
| service | `service/schedule.go`, `service/scheduler_loop.go` |
| controller | `controller/schedule.go` |
| routes | `app` 注册 `/api/v1/schedules` |
| run hook | `service/run.go` 终态 → schedule 回调 |
| runtime client | 若有 `schedule.tool.execute` handler 注册 |

Agent：

| 层 | 文件建议 |
| --- | --- |
| tools | `internal/tools` 或 runtime 注册 `schedule.*` |
| gateway RPC | 扩展 `gateway_rpc.go` / state_tool 管道 |

Desktop：

| 层 | 文件建议 |
| --- | --- |
| lib | `lib/schedules.js` normalize/payload |
| hooks | `hooks/useSchedules.js` |
| components | `components/SchedulesPanel.jsx` |
| App | 路由/侧栏接入 |

依赖：

- 新增 Go module 依赖 `robfig/cron/v3`（仅 Gateway）。

---

## 10. Error handling & observability

| 场景 | 行为 |
| --- | --- |
| cron 非法 | HTTP 400，不落库 |
| interval < 60s | 400 |
| workspace 不存在 | 400 或 fire 时 failed |
| fixed_session 已删 | fire → skipped/failed，disable 或标记 error |
| Runtime 未启动 | Start 失败，schedule run failed，next 仍按策略推进 |
| 重叠 skip | run 行 `skipped` + skip_reason=overlap |
| 达到 max_runs | disable + next=null |
| DB 锁失败 | 本 tick 跳过，下 tick 重试 |

日志：Gateway 结构化日志 `schedule_id`, `run_id`, `scheduled_for`, `result`。

Activity：可用 `TriggerSource=schedule` 过滤。

---

## 11. Testing strategy

### 11.1 Unit

- next_run_at 计算：one_shot / interval / cron + timezone
- claim 重叠 skip
- max_runs / disable
- options 白名单合并与默认安全策略
- 重启 catch_up=one 限流

### 11.2 Service / Repository

- CRUD soft delete
- enable/disable 重算 next
- fire 写入 scheduled_task_runs + 调用 RunService（mock runtime）

### 11.3 Protocol-compat

- HTTP CRUD 冒烟
- `POST trigger` 产生 run_record 与 timeline
- 禁用后不再 fire（用可注入 clock 的测试 hook）

### 11.4 Desktop

- 单元：DTO / 表单 payload
- Playwright fixture：列表面板
- 可选 gateway-backed：创建 interval 任务 + 手动 trigger 看到消息

### 11.5 时间可测性

- `ScheduleService` 注入 `Clock` 接口（`Now() time.Time`）
- 测试中推进时间，避免 `time.Sleep` 依赖

---

## 12. Security & abuse

1. 默认拒绝高风险工具与 permission prompt 悬挂。
2. 最小 interval 60s；cron 密度校验。
3. 全局限流：同时 running 的 schedule 触发 ≤ `RED_PANDA_SCHEDULE_MAX_INFLIGHT`（默认 3）。
4. prompt / name 长度限制（name ≤ 64，prompt ≤ 32KiB）。
5. soft delete，不硬毁审计 runs。
6. schedule 工具高风险，操作进 tool 投影。

---

## 13. Rollout / compatibility

- **非 breaking**：仅新增表与 API；旧客户端忽略即可。
- 不改现有 `run.start` 必填语义；仅增加可选 metadata。
- 文档：README 增加「定时任务需 Gateway 常驻」说明。

---

## 14. Future (out of v0.2.1)

- `queue` 重叠策略与优先级
- 系统托盘 / OS 通知
- 将 Gateway 注册为 Windows 服务
- RRULE / 节假日
- 定时触发 Goal continue 的完整策略面板
- 多 Gateway 实例租约（分布式）
- 日历视图 UI

---

## 15. Acceptance criteria（设计验收）

1. 可创建 one_shot / interval / cron 任务并持久化。
2. Gateway 运行时到期自动产生 run，Desktop 可见消息/Activity。
3. Gateway 重启不丢任务定义；missed 行为符合 catch_up 策略且无风暴。
4. 默认安全策略阻止无人值守 shell 写入。
5. 启停、删除、手动 trigger、历史查询可用。
6. 测试：Gateway 单测 + protocol-compat 关键路径通过。

---

## 16. Open questions（实现前可默认关闭）

| # | 问题 | 默认关闭方式 |
| --- | --- | --- |
| Q1 | Sidebar 一级 vs Settings 子页？ | **Sidebar 一级「定时」** |
| Q2 | `run_kind=goal` 是否同版本？ | **做薄支持：create_goal + objective=prompt；复杂 Goal UI 后置** |
| Q3 | Agent `schedule.*` 工具是否同版本？ | **做 list/create/enable/disable；update/delete/trigger 可同做** |
| Q4 | catch_up 默认 one 还是 none？ | **one + 启动限流** |
| Q5 | 是否写 Windows 服务文档？ | **仅 README 提示可选 nssm/service，不进代码** |
