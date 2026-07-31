# Red Panda API 契约总表草案

> 版本：`v0.1-draft`
> 生成时间：2026-07-31 11:16 CST
> 状态：待评审，非正式发布版本
> 作用范围：覆盖 Desktop 客户端与 Gateway 之间的 HTTP/WebSocket 契约，以及 Gateway 与 Agent Runtime 之间的 JSON-RPC/NDJSON 协议契约。后续应作为协议变更评审基准。

---

## 1. 总览

- **HTTP Base**：`http://127.0.0.1:17888`
- **WebSocket Base**：`ws://127.0.0.1:17888`
- **API 版本前缀**：`/api/v1`
- **HTTP 响应信封**：
  - 成功：`{"ok": true, "data": ..., "error": null, "request_id": "..."}`
  - 失败：`{"ok": false, "data": null, "error": {"code": "...", "message": "..."}, "request_id": "..."}`
- **HTTP 分页约定**：当前实现常见 `offset/limit` 或 `after_seq/limit`；默认 `limit` 常见为 200，部分端点限制为 1-500。
- **权限/密钥脱敏**：provider profile、MCP server 的 `api_key`、`env` 敏感值在响应中必须脱敏；Raw key 只接受写入，不返回读取。
- **本地发现**：`GET /healthz`、`GET /readyz`、`GET /api/v1/app/bootstrap` 为 Desktop 首屏必用端点。
- **运行控制主通道**：Desktop 当前主要使用 WebSocket；文档中保留部分 HTTP fallback 端点，但当前 Gateway 路由表未见这些端点全部实现，需视为文档/实现差异项。

---

## 2. Gateway HTTP 契约

### 2.1 健康与启动

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/healthz` | 存活探测 | 无 | `{"status":"ok"}` | `modules/gateway/internal/gateway/controller/health.go` |
| GET | `/readyz` | 就绪探测 | 无 | `{"status":"ready"}` | `modules/gateway/internal/gateway/controller/health.go` |
| GET | `/api/v1/app/bootstrap` | Desktop 首屏状态 | 无 | 聚合对象，含 `gateway/agent_runtime/workspace/recent_workspaces/sessions/active_runs` | `modules/gateway/internal/gateway/controller/app.go` |

### 2.2 Session

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/sessions` | 列出会话 | `offset`、`limit` | `items`、`has_more`、`next_offset` | `modules/gateway/internal/gateway/controller/session.go` |
| POST | `/api/v1/sessions` | 创建会话 | `name`、`workspace_root`、`kind`、`parent_id?` | `id`、会话对象 | `modules/gateway/internal/gateway/controller/session.go` |
| DELETE | `/api/v1/sessions/:id` | 删除会话 | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/session.go` |
| GET | `/api/v1/sessions/:id/history` | 会话历史 | `after_seq`、`limit` | `items`、`has_more`、`next_after_seq` | `modules/gateway/internal/gateway/controller/session.go` |
| GET | `/api/v1/sessions/:id/bootstrap` | 会话聚合状态 | 无 | 聚合对象 | `modules/gateway/internal/gateway/controller/session.go` |
| POST | `/api/v1/sessions/:id/fork` | 会话 fork | `name?` | 新会话对象 | `modules/gateway/internal/gateway/controller/session.go` |
| GET | `/api/v1/sessions/:id/compact` | 压缩状态 | 无 | 压缩状态对象 | `modules/gateway/internal/gateway/controller/session.go` |
| GET | `/api/v1/sessions/:id/summaries` | 会话摘要 | 无 | summaries 数组 | `modules/gateway/internal/gateway/controller/session.go` |
| GET | `/api/v1/sessions/:id/context` | 上下文状态 | 无 | context 对象 | `modules/gateway/internal/gateway/controller/session.go` |
| POST | `/api/v1/sessions/:id/compact/preview` | 压缩预览 | 无 | preview 对象 | `modules/gateway/internal/gateway/controller/session.go` |
| POST | `/api/v1/sessions/:id/compact` | 执行压缩 | 无 | 压缩结果 | `modules/gateway/internal/gateway/controller/session.go` |

### 2.3 Run / Events / Tools / Permissions

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/runs/:id` | 运行详情 | 无 | run 对象 | `modules/gateway/internal/gateway/controller/run.go` |
| GET | `/api/v1/runs/:id/events` | 运行事件时间线 | `after_seq`、`limit` | 事件数组 | `modules/gateway/internal/gateway/controller/run.go` |
| GET | `/api/v1/sessions/:id/runs` | 会话下运行 | 无 | runs 数组 | `modules/gateway/internal/gateway/controller/run.go` |
| GET | `/api/v1/runs/:id/tools` | 运行工具调用 | `limit` 等 | tool_calls 数组 | `modules/gateway/internal/gateway/controller/tool.go` |
| GET | `/api/v1/sessions/:id/tools` | 会话工具调用 | `limit` 等 | tool_calls 数组 | `modules/gateway/internal/gateway/controller/tool.go` |
| GET | `/api/v1/permissions/pending` | 待处理权限 | 无 | permissions 数组 | `modules/gateway/internal/gateway/controller/permission.go` |
| GET | `/api/v1/permissions/:id` | 权限详情 | 无 | permission 对象 | `modules/gateway/internal/gateway/controller/permission.go` |
| GET | `/api/v1/runs/:id/permissions` | 运行权限 | 无 | permissions 数组 | `modules/gateway/internal/gateway/controller/permission.go` |
| GET | `/api/v1/sessions/:id/permissions` | 会话权限 | 无 | permissions 数组 | `modules/gateway/internal/gateway/controller/permission.go` |

> 契约差异：文档列出 `POST /api/v1/runs`、`POST /api/v1/runs/:run_id/cancel`、`POST /api/v1/permissions/:id/approve`、`POST /api/v1/permissions/:id/deny`；当前实现未在路由表中发现这些端点，Desktop 也主要走 WebSocket。

### 2.4 Workspace

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| POST | `/api/v1/workspaces/open` | 打开工作区 | `root` | workspace 对象 | `modules/gateway/internal/gateway/controller/workspace.go` |
| GET | `/api/v1/workspaces/current` | 当前工作区 | 无 | workspace 对象 | `modules/gateway/internal/gateway/controller/workspace.go` |
| GET | `/api/v1/workspaces/recent` | 最近工作区 | 无 | workspaces 数组 | `modules/gateway/internal/gateway/controller/workspace.go` |
| DELETE | `/api/v1/workspaces/:id` | 删除工作区 | `delete_sessions?` | 空或 `{}` | `modules/gateway/internal/gateway/controller/workspace.go` |
| GET | `/api/v1/workspaces/tree` | 文件树 | `path`、`max_depth` | tree 结构 | `modules/gateway/internal/gateway/controller/workspace.go` |
| GET | `/api/v1/workspaces/file` | 读取文件 | `path` | file 对象 | `modules/gateway/internal/gateway/controller/workspace.go` |
| GET | `/api/v1/workspaces/diff` | 工作区 diff | `path?` | diff 对象 | `modules/gateway/internal/gateway/controller/workspace.go` |

### 2.5 Attachment

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/sessions/:id/attachments` | 会话附件列表 | 无 | attachments 数组 | `modules/gateway/internal/gateway/controller/attachment.go` |
| POST | `/api/v1/sessions/:id/attachments` | 上传附件 | multipart | attachment 对象 | `modules/gateway/internal/gateway/controller/attachment.go` |
| POST | `/api/v1/sessions/:id/attachments/from-workspace` | 从工作区关联附件 | `path` 等 | attachment 对象 | `modules/gateway/internal/gateway/controller/attachment.go` |
| GET | `/api/v1/attachments/:id` | 下载附件字节 | 无 | binary | `modules/gateway/internal/gateway/controller/attachment.go` |
| GET | `/api/v1/attachments/:id/meta` | 附件元信息 | 无 | attachment meta | `modules/gateway/internal/gateway/controller/attachment.go` |
| DELETE | `/api/v1/attachments/:id` | 删除附件 | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/attachment.go` |

> 契约约束：multipart 内存阈值约为 4 MiB；超限走临时文件。

### 2.6 Memory

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/memory` | 记忆列表 | `scope`、`workspace_root`、`session_id`、`status`、`limit` | memory 数组 | `modules/gateway/internal/gateway/controller/memory.go` |
| POST | `/api/v1/memory` | 创建记忆 | `scope`、`kind`、`content`、`confidence?` 等 | memory 对象 | `modules/gateway/internal/gateway/controller/memory.go` |
| PUT | `/api/v1/memory/:id` | 更新记忆 | `content`、`status?`、`confidence?` 等 | memory 对象 | `modules/gateway/internal/gateway/controller/memory.go` |
| DELETE | `/api/v1/memory/:id` | 删除记忆 | `hard?` | 空或 `{}` | `modules/gateway/internal/gateway/controller/memory.go` |
| POST | `/api/v1/memory/preview-run` | 预览运行记忆选择 | `run_id`、`session_id`、`workspace_root` | 预览结果 | `modules/gateway/internal/gateway/controller/memory.go` |

### 2.7 Provider Profiles

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/provider-profiles` | 列出模型配置 | 无 | profiles 数组 | `modules/gateway/internal/gateway/controller/provider_profile.go` |
| POST | `/api/v1/provider-profiles` | 创建模型配置 | `name`、`provider`、`base_url`、`model`、`default?`、`active?`、`api_key?` | profile 对象 | `modules/gateway/internal/gateway/controller/provider_profile.go` |
| GET | `/api/v1/provider-profiles/:id` | 模型配置详情 | 无 | profile 对象 | `modules/gateway/internal/gateway/controller/provider_profile.go` |
| PUT | `/api/v1/provider-profiles/:id` | 更新模型配置 | 同创建 | profile 对象 | `modules/gateway/internal/gateway/controller/provider_profile.go` |
| DELETE | `/api/v1/provider-profiles/:id` | 删除模型配置 | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/provider_profile.go` |

> 契约约束：`POST` / `PUT` 可传 `api_key`；响应不得返回原始密钥，只返回 `api_key_set` 与脱敏值。`run.start` 可携带 `provider_profile_id`，Gateway 会解析为 per-run provider override。

### 2.8 MCP Servers

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/mcp/servers` | 列出 MCP 配置 | 无 | servers 数组 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| POST | `/api/v1/mcp/servers` | 创建 MCP 配置 | `name`、`command`、`args`、`env`、`cwd`、`enabled`、`timeouts`、`raw_tool_allowlist?`、`risk_overrides?` | server 对象 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| GET | `/api/v1/mcp/servers/:id` | MCP 配置详情 | 无 | server 对象 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| PUT | `/api/v1/mcp/servers/:id` | 更新 MCP 配置 | 部分字段 | server 对象 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| DELETE | `/api/v1/mcp/servers/:id` | 删除 MCP 配置 | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| POST | `/api/v1/mcp/servers/:id/discover` | 发现 MCP 工具 | 无 | discovery 结果 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |
| POST | `/api/v1/mcp/servers/:id/call` | 管理路径 try-call | `tool_name`、`arguments` | call 结果 | `modules/gateway/internal/gateway/controller/mcp_server_config.go` |

> 契约约束：配置校验不执行命令；环境敏感值脱敏；名称冲突返回 409；timeouts 有默认归一化；启用态、raw tool allowlist、risk overrides 需持久化。

### 2.9 Skills

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/skills` | 列出 managed skills | 无 | skills 数组 | `modules/gateway/internal/gateway/controller/skill.go` |
| POST | `/api/v1/skills` | 创建 managed skill | `name`、`source`、`meta?` | skill 对象 | `modules/gateway/internal/gateway/controller/skill.go` |
| GET | `/api/v1/skills/:name` | skill 详情 | 无 | skill 对象 | `modules/gateway/internal/gateway/controller/skill.go` |
| PUT | `/api/v1/skills/:name` | 更新 skill | 部分字段 | skill 对象 | `modules/gateway/internal/gateway/controller/skill.go` |
| DELETE | `/api/v1/skills/:name` | 删除 skill | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/skill.go` |

### 2.10 Worker Profiles

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/worker-profiles` | 列出 worker profile | 无 | profiles 数组 | `modules/gateway/internal/gateway/controller/worker_profile.go` |
| GET | `/api/v1/worker-profiles/enabled` | 列出启用 profile | 无 | profiles 数组 | `modules/gateway/internal/gateway/controller/worker_profile.go` |
| POST | `/api/v1/worker-profiles` | 创建 profile | 配置字段 | profile 对象 | `modules/gateway/internal/gateway/controller/worker_profile.go` |
| GET | `/api/v1/worker-profiles/:id` | profile 详情 | 无 | profile 对象 | `modules/gateway/internal/gateway/controller/worker_profile.go` |
| PUT | `/api/v1/worker-profiles/:id` | 更新 profile | 部分字段 | profile 对象 | `modules/gateway/internal/gateway/controller/worker_profile.go` |
| DELETE | `/api/v1/worker-profiles/:id` | 删除 profile | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/worker_profile.go` |

### 2.11 Schedules

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/schedules` | 列出定时任务 | 无 | schedules 数组 | `modules/gateway/internal/gateway/controller/schedule.go` |
| POST | `/api/v1/schedules` | 创建定时任务 | cron / prompt / session / enabled 等 | schedule 对象 | `modules/gateway/internal/gateway/controller/schedule.go` |
| GET | `/api/v1/schedules/:id` | 定时任务详情 | 无 | schedule 对象 | `modules/gateway/internal/gateway/controller/schedule.go` |
| PUT | `/api/v1/schedules/:id` | 更新定时任务 | 部分字段 | schedule 对象 | `modules/gateway/internal/gateway/controller/schedule.go` |
| DELETE | `/api/v1/schedules/:id` | 删除定时任务 | 无 | 空或 `{}` | `modules/gateway/internal/gateway/controller/schedule.go` |
| POST | `/api/v1/schedules/:id/enable` | 启用任务 | 无 | schedule 对象 | `modules/gateway/internal/gateway/controller/schedule.go` |
| POST | `/api/v1/schedules/:id/disable` | 禁用任务 | 无 | schedule 对象 | `modules/gateway/internal/gateway/controller/schedule.go` |
| POST | `/api/v1/schedules/:id/trigger` | 手动触发任务 | 无 | 触发结果 | `modules/gateway/internal/gateway/controller/schedule.go` |
| GET | `/api/v1/schedules/:id/runs` | 任务运行记录 | `limit?` | runs 数组 | `modules/gateway/internal/gateway/controller/schedule.go` |

### 2.12 Agent

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/agent/status` | Runtime 状态 | 无 | RuntimeStatus | `modules/gateway/internal/gateway/controller/agent.go` |

### 2.13 Todo

| 方法 | 路径 | 用途 | 关键入参 | 关键出参 | 来源 |
|------|------|------|----------|----------|------|
| GET | `/api/v1/sessions/:id/todos` | 会话 todo | 无 | todos 数组 | `modules/gateway/internal/gateway/controller/todo.go` |

---

## 3. WebSocket 契约

### 3.1 连接与基础约定

- **实现路径**：`GET /ws`
- **文档/Desktop 路径**：`/api/v1/ws`
- **本地地址**：`ws://127.0.0.1:17888`
- **消息类型**：`ping`、`pong`、`auth`、`request`、`response`、`event`、`error`
- **请求必填字段**：`id`、`type`、`method`
- **响应字段**：`id`、`type`、`ok`、`payload`、`error`、`meta`
- **错误字段**：`error.code`、`error.message`、`error.recoverable`
- **心跳**：Gateway 每 30 秒主动 ping；Desktop 应回 pong。

> 契约差异：文档中 WebSocket 路径写为 `/api/v1/ws`，但 Gateway 实际注册在 `/ws`。
> 契约缺口：文档描述认证有两种方案；当前实现仅支持首条 `auth` 消息，且不校验 token，不支持 `Sec-WebSocket-Protocol`。

### 3.2 WebSocket 命令

| WS Method | 用途 | 关键入参 | 关键出参 | 来源 |
|-----------|------|----------|----------|------|
| `agent.status` | 查询 Runtime 状态 | 无 | RuntimeStatus | `modules/gateway/internal/gateway/controller/ws_dispatcher.go` |
| `run.start` | 启动 run | `session_id`、`prompt`、`provider_profile_id?`、`subscribe?` | `run_id`、`runtime_mode`、`accepted`、`session_id`、`subscribed`、`from_seq` | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run.subscribe` | 订阅已有 run | `run_id`、`after_seq?` | 订阅结果 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run.resume` | 恢复活跃 run | `last_seen: { run_id: run_seq }` | 恢复结果 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run.cancel` | 取消 run | `run_id` | `accepted`、`cancelled` | `modules/gateway/internal/gateway/controller/websocket.go` |
| `worker.list` | 列出 worker | `run_id?`、`worker_id?`、`assignment_id?` | workers / assignments | `modules/gateway/internal/gateway/controller/websocket.go` |
| `worker.assignment.cancel` | 取消 worker assignment | `run_id`、`assignment_id` | `accepted`、`cancelled` | `modules/gateway/internal/gateway/controller/websocket.go` |
| `permission.resolve` | 提交权限决策 | `permission_id`、`run_id`、`decision`、`scope?`、`reason?` | `accepted` | `modules/gateway/internal/gateway/controller/websocket.go` |

> 契约差异：文档列出 `subagent.cancel`；当前 Gateway 实现中未见该方法，仅见 `worker.assignment.cancel`。
> 契约差异：文档列出 HTTP fallback `POST /api/v1/runs`、`POST /api/v1/runs/:run_id/cancel`、`POST /api/v1/permissions/:id/approve`、`POST /api/v1/permissions/:id/deny`；当前实现路由表未覆盖。

### 3.3 WebSocket 事件

| WS 类型 | 用途 | 关键字段 | 来源 |
|---------|------|----------|------|
| `event` | 运行时事件推送 | `meta.run_id`、`meta.run_seq`、`payload` | `modules/gateway/internal/gateway/controller/websocket.go` |
| `error` | 命令失败 | `error.code`、`error.message`、`error.recoverable` | `modules/gateway/internal/gateway/controller/websocket.go` |
| `ack` | 订阅/恢复确认 | `meta` | 文档定义 |
| `runtime.status` | Runtime 状态广播 | 状态对象 | 文档定义 |

### 3.4 WebSocket 错误码

| 错误码 | 含义 | 来源 |
|--------|------|------|
| `invalid_json` | JSON 解析失败 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `unsupported_message_type` | 不支持的消息类型 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `method_not_implemented` | 未实现方法 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `invalid_payload` | 请求体非法 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run_start_failed` | run 启动失败 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run_cancel_failed` | run 取消失败 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `permission_resolve_failed` | 权限决策失败 | `modules/gateway/internal/gateway/controller/websocket.go` |
| `run_replay_failed` | 回放失败 | `modules/gateway/internal/gateway/controller/websocket.go` |

> 契约缺口：文档定义了 `unauthorized`、`replay_unavailable`、`permission_expired`、`agent_unavailable`、`backpressure`；当前实现未完全落地。

### 3.5 Desktop 强依赖字段

- **bootstrap**：`gateway`、`agent_runtime`、`workspace`、`recent_workspaces`、`sessions`、`active_runs`
- **session list**：`id`、`name`、`status`、`workspace_root`、`kind`、`parent_id`
- **session history**：`id`、`seq`、`role`、`run_id`、`run_seq`、`assignment_id`、`worker_id`、`profile_key`、`visibility`、`created_at`、`content[].text`、`attachments`
- **run object**：`id`、`session_id`、`workspace_root`、`runtime_mode`、`status`、`last_event_type`、`last_run_seq`、`message_count`、`tool_count`、`error`、`started_at`、`finished_at`、`updated_at`
- **tool object**：`id`、`tool_name`、`display_name`、`risk`、`arguments`、`status`、`output`、`error`、`duration_ms`、`run_seq`、`started_at`
- **permission object**：`id`、`run_id`、`assignment_id`、`worker_id`、`profile_key`、`status`、`decision`、`summary`、`detail`、`tool_name`、`arguments`、`risk`、`run_seq`、`created_at`
- **run event**：`protocol_version`、`run_id`、`session_id`、`assignment_id`、`worker.id`、`run_seq`、`worker_seq`、`event_id`、`type`、`payload`、`created_at`
- **event type**：`message_delta`、`finish`、`error`、`permission_required`、`tool_started`、`tool_output`、`tool_finished`、`tool_failed`、`worker_assignment_updated`、`todo_updated`

> 契约差异：Desktop 的事件归一化依赖顶层 `protocol_version / run_id / session_id / assignment_id / worker.id / run_seq / worker_seq / event_id`，但部分文档示例仍偏 `agent/stream/payload` 旧式 envelope。

---

## 4. Gateway ↔ Runtime JSON-RPC/NDJSON 契约

### 4.1 传输层约定

- **协议**：JSON-RPC 2.0 over newline-delimited JSON
- **传输介质**：默认 IPC；也可通过 `RED_PANDA_RUNTIME_IPC` 切换为 stdio/stdout-stdin
- **单行上限**：Runtime → Gateway 单行最大 `4MB`
- **请求/响应路由**：Gateway 侧按 `id` 匹配 response；Runtime 侧只允许 Event Multiplexer 单一写出

### 4.2 Gateway → Runtime 方法

| 方法 | 用途 | 入参 | 出参 | 来源 |
|------|------|------|------|------|
| `core.initialize` | 协议握手 | `InitializeParams`，必填 `protocol_version` | `InitializeResult`、`Capabilities` | `modules/protocol/methods/methods.go` |
| `core.ping` | 健康检查 | `PingParams` | `PingResult{status:"ok"}` | `modules/protocol/methods/methods.go` |
| `core.shutdown` | 关闭 Runtime | 无 | `{accepted:true}` | `modules/agent/internal/runtime/register_rpc.go` |
| `run.execute` | 执行 run | `RunExecuteParams` | `RunExecuteResult{accepted,run_id,assignment_id,worker_id}` | `modules/protocol/methods/methods.go` |
| `run.cancel` | 取消 run | `RunCancelParams{run_id,reason}` | `RunCancelResult{accepted,cancelled}` | `modules/protocol/methods/methods.go` |
| `run.pause` | 暂停 run | `RunPauseParams{run_id,delegated_only}` | `RunPauseResult{accepted,paused}` | `modules/protocol/methods/methods.go` |
| `run.resume_execution` | 恢复 run | `RunResumeParams{run_id,delegated_only}` | `RunResumeResult{accepted,resumed}` | `modules/protocol/methods/methods.go` |
| `worker.list` | 列出 worker | `WorkerListParams{run_id,worker_id,assignment_id}` | `WorkerListResult{workers,assignments}` | `modules/protocol/methods/methods.go` |
| `worker.assignment.cancel` | 取消 assignment | `WorkerAssignmentCancelParams{run_id,assignment_id,reason}` | `WorkerAssignmentCancelResult{accepted,cancelled}` | `modules/protocol/methods/methods.go` |
| `worker.message.send` | 向 worker 发消息 | `WorkerMessageSendParams{to_worker_id,to_assignment_id,kind,correlation_id,reply_to,payload}` | `WorkerMessageSendResult{accepted,message}` | `modules/protocol/methods/methods.go` |
| `worker.message.receive` | 从 worker 收消息 | `WorkerMessageReceiveParams{timeout_ms}` | `WorkerMessageReceiveResult{found,message}` | `modules/protocol/methods/methods.go` |
| `worker.pool.status` | worker pool 状态 | 无 | `WorkerPoolStatusResult{pool}` | `modules/protocol/methods/methods.go` |
| `permission.resolve` | 权限回传 | `ResolveParams{permission_id,run_id,decision,scope,reason}` | `ResolveResult{accepted}` | `modules/protocol/permission/permission.go` |
| `mcp.discover` | MCP 发现 | `MCPDiscoverParams{workspace_root,servers}` | `MCPDiscoveryResult` | `modules/protocol/methods/methods.go` |
| `mcp.call` | MCP 工具调用 | `MCPCallParams{workspace_root,server,tool_name,arguments}` | `MCPCallResult{output,ok,error,duration_ms,stderr_summary}` | `modules/protocol/methods/methods.go` |
| `agent.skills` | 列出 skills | `SkillsContext` | `SkillSummary[]` | `modules/protocol/methods/methods.go` |
| `agent.skill.load` | 加载 skill 详情 | `SkillSummary` | skill detail | `modules/protocol/methods/methods.go` |
| `agent.skill.create` | 创建 skill | skill create DTO | skill object | `modules/protocol/methods/methods.go` |
| `agent.skill.update` | 更新 skill | skill update DTO | skill object | `modules/protocol/methods/methods.go` |
| `agent.skill.delete` | 删除 skill | skill delete DTO | 空或确认 | `modules/protocol/methods/methods.go` |

### 4.3 Runtime → Gateway 方法/Notification

| 方法/类型 | 用途 | 入参 | 出参 | 来源 |
|-----------|------|------|------|------|
| `run.event` | 事件通知 | `events.EnvelopeV2` | notification 无业务出参 | `modules/protocol/methods/methods.go` |
| `state.tool.execute` | 状态工具反向 RPC | `StateToolExecuteParams{domain?,run_id,session_id,workspace_root,tool_call_id,tool_name,arguments}` | state tool result | `modules/protocol/methods/methods.go` |

### 4.4 事件 EnvelopeV2 关键字段

| 字段 | 类型 | 必填 | 说明 | 来源 |
|------|------|------|------|------|
| `protocol_version` | string | 是 | 协议版本，当前为 `2026-07-13` | `modules/protocol/events/events.go` |
| `event_id` | string | 是 | 事件 ID | `modules/protocol/events/events.go` |
| `run_id` | string | 是 | 当前 run ID | `modules/protocol/events/events.go` |
| `session_id` | string | 是 | 会话 ID | `modules/protocol/events/events.go` |
| `run_seq` | uint64 | 是 | run 级序号 | `modules/protocol/events/events.go` |
| `worker_seq` | uint64 | 否 | worker 级序号 | `modules/protocol/events/events.go` |
| `assignment_id` | string | 否 | assignment ID | `modules/protocol/events/events.go` |
| `worker` | object | 否 | `worker_id`、`role`、`path` | `modules/protocol/events/events.go` |
| `stream` | object | 否 | `stream_id`、`kind`、`seq`、`final` | `modules/protocol/events/events.go` |
| `type` | string | 是 | 事件类型 | `modules/protocol/events/events.go` |
| `payload` | object | 是 | 事件负载 | `modules/protocol/events/events.go` |
| `created_at` | string | 是 | 创建时间 | `modules/protocol/events/events.go` |

### 4.5 错误码约定

| 错误码 | 含义 | 来源 |
|--------|------|------|
| `-32700` | 解析错误 | `modules/protocol/jsonrpc/jsonrpc.go` |
| `-32600` | 无效请求 | `modules/protocol/jsonrpc/jsonrpc.go` |
| `-32601` | 方法未找到 | `modules/protocol/jsonrpc/jsonrpc.go` |
| `-32602` | 无效参数 | `modules/protocol/jsonrpc/jsonrpc.go` |
| `-32603` | 内部错误 | `modules/protocol/jsonrpc/jsonrpc.go` |
| `-32001` | 协议版本不匹配 | `modules/protocol/methods/methods.go` |
| `-32004` | 权限未找到 | `modules/protocol/permission/permission.go` |
| `-32009` | 重复 run_id | `modules/protocol/methods/methods.go` |
| `-32010` | 运行时错误 | `modules/protocol/methods/methods.go` |
| `-32050` | 运行时错误 | `modules/protocol/methods/methods.go` |

---

## 5. 风险 / 权限 / 工具契约

### 5.1 工具风险等级

| 风险等级 | 说明 | 来源 |
|----------|------|------|
| `low` | 低风险 | `modules/protocol/tools/tools.go` |
| `medium` | 中风险 | `modules/protocol/tools/tools.go` |
| `high` | 高风险 | `modules/protocol/tools/tools.go` |

### 5.2 工具状态

| 状态 | 说明 | 来源 |
|------|------|------|
| `pending` | 等待中 | `modules/protocol/tools/tools.go` |
| `running` | 运行中 | `modules/protocol/tools/tools.go` |
| `completed` | 已完成 | `modules/protocol/tools/tools.go` |
| `failed` | 失败 | `modules/protocol/tools/tools.go` |
| `denied` | 被拒绝 | `modules/protocol/tools/tools.go` |

### 5.3 权限与钩子

- `run.execute` 携带 `require_permission` 时，Runtime 会在 provider 前发起 checkpoint approval；超时默认 5 分钟。
- MCP 工具默认 `RiskHigh`，受 `tool_policy / tool_allowlist / tool_denylist / permission_mode` 约束。
- Managed skill 的 `create / update / delete` 视为高风险写操作，需要路径/大小/目录边界/symlink escape 校验。
- Runtime 内置 `builtin:permission` hook，可在 `HookPermissionCheck` 阶段 block/deny。

---

## 6. 补充契约项

### 6.1 统一字段约定

| 类别 | 字段 | 说明 |
|------|------|------|
| 通用 | `request_id` | HTTP 响应可选字段；Gateway 未提供时自动生成 |
| 通用 | `ok` | HTTP 响应成功标记 |
| 通用 | `error.code` | HTTP/WS 错误码 |
| 通用 | `error.message` | 人类可读错误信息 |
| 通用 | `error.recoverable` | 是否可恢复 |
| 时间 | `created_at / started_at / finished_at / updated_at` | ISO8601 / RFC3339 |
| 分页 | `offset / limit / after_seq / has_more / next_offset / next_after_seq` | 游标/偏移分页 |
| 脱敏 | `api_key_set`、masked key、masked env | 敏感字段只返回布尔/掩码 |

### 6.2 已知契约缺口与待澄清项

| 编号 | 缺口/差异 | 建议后续动作 |
|------|-----------|-------------|
| G1 | WebSocket 路径文档为 `/api/v1/ws`，实现为 `/ws` | 统一路径或补文档说明 |
| G2 | WS 认证未落地 | 明确 token 校验、connection_id、过期策略 |
| G3 | 文档定义 `replay_unavailable / backpressure`，实现未完整落地 | 明确是否保留为 backlog |
| G4 | 文档列有 HTTP run/permission fallback 端点，但当前未实现 | 明确保留或移除 |
| G5 | Desktop 事件归一化依赖顶层 run_seq/worker_seq，文档示例偏旧 envelope | 更新官方事件示例 |
| G6 | WorkerMessage.payload 无公共 schema | 补 payload schema 示例 |
| G7 | 版本演进规则缺失 | 补版本兼容矩阵与 breaking change 规则 |
| G8 | 错误码字典不集中 | 公开统一 error.code 字典 |

---

## 7. 使用建议

- 本草案优先作为 **客户端集成、插件开发、协议评审** 的对照基线。
- 发现实现与文档不一致时，以 **源码实现为准**，并将差异回写到本草案。
- 后续若要转成正式契约，建议补充：
  - 每个端点的 request/response JSON Schema；
  - WebSocket 完整示例序列；
  - JSON-RPC 错误码字典；
  - 认证与安全策略手册；
  - 版本兼容矩阵。
