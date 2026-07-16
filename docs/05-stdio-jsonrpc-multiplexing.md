# Gateway ↔ Runtime JSON-RPC 事件复用协议

> **Transport note (docs/41 W3-2, 2026-07):** the **default** wire is **IPC**
> (named pipes / Unix domain sockets) carrying the same newline-delimited
> JSON-RPC framing. **stdio** remains a **legacy escape hatch**
> (`RED_PANDA_RUNTIME_IPC=0` or an agent started without `RED_PANDA_IPC_ADDR`).
> Framing, methods, and event envelopes are transport-agnostic.

## 1. 问题定义

`red_panda` 的中间网关和 Agent Runtime 使用 newline-delimited JSON-RPC 2.0
（默认 IPC，兼容 legacy stdio）通信。Agent Runtime 内部会同时存在：

1. 入口 Run 输出文本、reasoning、工具事件。
2. 一个或多个 Worker Assignment 输出文本、reasoning、工具事件。
3. 工具 stdout/stderr 增量输出。
4. 权限请求、取消、失败、完成等控制事件。

这些事件最终都经过同一个 Agent Runtime 写出通道发给网关。协议必须保证：

1. 写出通道上每一行仍然是合法 JSON-RPC 消息。
2. 入口 Run 与 Worker 输出不会混在同一个文本流里。
3. 网关能把同一个 run 的入口与 Worker 事件转发到同一个 WebSocket 订阅通道。
4. 桌面端能按 Run、Worker Assignment、工具和内容流正确展示。
5. 事件可持久化、可断线续传、可重放。

## 2. 基本原则

1. Agent Runtime 写出通道只能写 JSON-RPC response 或 notification。
2. 任何 agent、Worker、tool 都不能直接写协议通道。
3. Runtime 内部必须有 Event Multiplexer，所有执行单元只向它投递事件。
4. Event Multiplexer 是协议写出通道的唯一写入者。
5. Event Multiplexer 为同一个 `run_id` 分配全局递增 `run_seq`（历史文档中的 `root_seq` 同义）。
6. 每个 Worker 维护自己的 `worker_seq`（历史 `agent_seq`）。
7. 每个流式内容维护自己的 `stream_seq`。
8. 网关存储和 WebSocket 转发时保留完整 EnvelopeV2，不丢失 Worker/Assignment 元数据。

## 3. JSON-RPC 外层

Runtime 向网关发送事件时，统一使用 JSON-RPC notification：

```json
{
  "jsonrpc": "2.0",
  "method": "agent.event",
  "params": {
    "protocol_version": "2026-07-09",
    "event_id": "evt_01J...",
    "root_run_id": "run_root",
    "run_id": "run_root",
    "root_seq": 1,
    "agent_seq": 1,
    "agent": {
      "agent_id": "root",
      "role": "root",
      "path": ["root"]
    },
    "type": "message_delta",
    "stream": {
      "stream_id": "stream_root_msg_1",
      "kind": "message",
      "seq": 1,
      "final": false
    },
    "payload": {
      "message_id": "msg_1",
      "delta": "hello"
    },
    "created_at": "2026-07-09T10:00:00Z"
  }
}
```

## 4. Event Envelope

`params` 是事件 envelope。

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `protocol_version` | 是 | 事件协议版本 |
| `event_id` | 是 | 全局事件 ID |
| `root_run_id` | 是 | root run ID，网关 WebSocket 订阅按它聚合 |
| `run_id` | 是 | 当前执行单元 run ID |
| `parent_run_id` | 否 | 子代理的父 run |
| `session_id` | 是 | root session ID |
| `root_seq` | 是 | root run 全局序号 |
| `agent_seq` | 是 | 当前 agent 内部序号 |
| `agent` | 是 | agent 身份 |
| `stream` | 否 | 流式内容元数据 |
| `type` | 是 | 事件类型 |
| `payload` | 是 | 事件 payload |
| `created_at` | 是 | Runtime 创建时间 |

`agent` 字段：

```json
{
  "agent_id": "subagent_123",
  "role": "subagent",
  "subagent_id": "subagent_123",
  "parent_agent_id": "root",
  "path": ["root", "subagent_123"],
  "name": "risk-reviewer"
}
```

`stream` 字段：

```json
{
  "stream_id": "stream_subagent_123_msg_1",
  "kind": "message",
  "seq": 3,
  "final": false
}
```

`stream.kind` 可选值：

- `message`
- `reasoning`
- `tool_stdout`
- `tool_stderr`
- `tool_result_preview`

## 5. 序号规则

### 5.1 root_seq

`root_seq` 是同一个 root run 内的全局事件顺序。

规则：

1. 由 Event Multiplexer 分配。
2. 从 1 开始递增。
3. 主代理和所有子代理共享同一组 `root_seq`。
4. 网关持久化事件时以 `(root_run_id, root_seq)` 做唯一索引。
5. WebSocket 断线续传使用 `resume.after_seq`，对应 `root_seq`。

### 5.2 agent_seq

`agent_seq` 是当前 agent 自己的事件顺序。

规则：

1. 主代理和每个子代理各自从 1 开始。
2. 用于 UI 排查某个子代理内部是否丢事件。
3. 不用于跨 agent 排序。

### 5.3 stream_seq

`stream.seq` 是单个内容流的 delta 顺序。

规则：

1. 每个 `stream_id` 独立从 1 开始。
2. UI 拼接文本必须按同一个 `stream_id` 的 `stream.seq` 拼接。
3. 不同 `stream_id` 的 delta 即使 `root_seq` 相邻，也不能拼接到一起。
4. `stream.final=true` 表示该内容流结束。

## 6. 主代理与子代理同时输出示例

主代理输出：

```json
{
  "jsonrpc": "2.0",
  "method": "agent.event",
  "params": {
    "event_id": "evt_1",
    "root_run_id": "run_1",
    "run_id": "run_1",
    "session_id": "sess_1",
    "root_seq": 10,
    "agent_seq": 5,
    "agent": {
      "agent_id": "root",
      "role": "root",
      "path": ["root"]
    },
    "type": "message_delta",
    "stream": {
      "stream_id": "stream_root_msg_a",
      "kind": "message",
      "seq": 5,
      "final": false
    },
    "payload": {
      "message_id": "msg_root_a",
      "delta": "我先检查入口。"
    },
    "created_at": "2026-07-09T10:00:01Z"
  }
}
```

子代理同时输出：

```json
{
  "jsonrpc": "2.0",
  "method": "agent.event",
  "params": {
    "event_id": "evt_2",
    "root_run_id": "run_1",
    "run_id": "run_1:subagent:subagent_7",
    "parent_run_id": "run_1",
    "session_id": "sess_1",
    "root_seq": 11,
    "agent_seq": 1,
    "agent": {
      "agent_id": "subagent_7",
      "role": "subagent",
      "subagent_id": "subagent_7",
      "parent_agent_id": "root",
      "path": ["root", "subagent_7"],
      "name": "risk-reviewer"
    },
    "type": "message_delta",
    "stream": {
      "stream_id": "stream_subagent_7_msg_a",
      "kind": "message",
      "seq": 1,
      "final": false
    },
    "payload": {
      "message_id": "msg_subagent_7_a",
      "delta": "我发现权限检查路径需要重点看。"
    },
    "created_at": "2026-07-09T10:00:01Z"
  }
}
```

这两个事件都进入同一个网关 WebSocket 连接上的 `run.event` 消息流，但桌面端通过 `agent.role`、`agent.subagent_id` 和 `stream.stream_id` 分开展示。

## 7. 工具输出复用

工具 stdout/stderr 也走同一个事件 envelope。

```json
{
  "root_run_id": "run_1",
  "run_id": "run_1:subagent:subagent_7",
  "root_seq": 18,
  "agent_seq": 8,
  "agent": {
    "agent_id": "subagent_7",
    "role": "subagent",
    "subagent_id": "subagent_7",
    "path": ["root", "subagent_7"]
  },
  "type": "tool_output",
  "stream": {
    "stream_id": "tool_call_9_stdout",
    "kind": "tool_stdout",
    "seq": 2,
    "final": false
  },
  "payload": {
    "tool_call_id": "tool_call_9",
    "tool_name": "shell",
    "text": "running tests...\n"
  }
}
```

工具事件类型建议：

- `tool_started`
- `tool_output`
- `tool_finished`
- `tool_failed`

## 8. 网关处理规则

网关收到 `agent.event` 后：

1. 校验 `root_run_id`、`root_seq`、`event_id`。
2. 按 `(root_run_id, root_seq)` 幂等写入 SQLite。
3. 发布到 Event Hub 的 `root_run_id` topic。
4. WebSocket 连接通过 `run.subscribe` 订阅该 topic。
5. 如果事件属于子代理，同时更新 subagent projection。
6. 如果事件是 terminal，只有 root agent 的 `finish/error/cancelled` 才结束 root run 订阅；子代理 terminal 不关闭 WebSocket 连接。

网关不得：

1. 按接收时间重排同一个 root run 的事件。
2. 把不同 `stream_id` 的 delta 合并。
3. 丢弃子代理 envelope 后只保留文本。

## 9. 桌面端展示规则

桌面端展示时：

1. 主代理消息显示在主对话流。
2. 子代理消息显示在子代理面板或可折叠嵌套块。
3. 同一个 `stream_id` 内按 `stream.seq` 拼接 delta。
4. 不同 agent 的消息不能因为 `root_seq` 相邻而拼到同一个气泡。
5. `agent.path` 用于展示嵌套子代理层级。
6. 工具输出按 `tool_call_id` 聚合，且保留所属 agent。

## 10. 多进程场景

### 10.1 single_core

一个 Runtime stdout 对应一个网关 JSON-RPC client。

Runtime 内部 Event Multiplexer 分配 `root_seq`。

### 10.2 per_run_process

每个 root run 一个 Runtime 进程。

网关仍然按 `root_run_id` 管理 WebSocket 订阅。由于一个进程只服务一个 root run，`root_seq` 可由 Runtime 分配。

### 10.3 process_pool

多个 Runtime worker 同时存在。

规则：

1. root run 绑定到一个 worker，root run 生命周期内不迁移。
2. `root_seq` 仍由该 worker 的 Event Multiplexer 分配。
3. 如果子代理被调度到另一个 worker，该 worker 不直接向网关 root WebSocket 订阅输出；它把事件回传给 root worker 或网关聚合器。
4. 推荐第二阶段先实现 root worker 聚合，避免网关同时合并多个 worker 的同一个 root run 序号。

## 11. 子代理独立进程场景

当 `subagent.backend=runtime_process` 时：

```text
Gateway
  |
  | stdio JSON-RPC
  v
Root Agent Runtime
  |
  | stdio JSON-RPC or internal pipe
  v
SubAgent Runtime Process
```

规则：

1. 子代理进程不能直接写网关 stdout。
2. 子代理进程事件先发给 Root Agent Runtime。
3. Root Agent Runtime 重新包装或补齐 envelope。
4. Root Agent Runtime Event Multiplexer 分配最终 `root_seq`。
5. 网关只看到一个 root run 的有序事件流。

## 12. 错误处理

协议错误：

- 缺少 `root_run_id`：网关拒绝事件，标记 Runtime protocol violation。
- `root_seq` 重复但 payload 不一致：网关标记 run failed。
- `stream.seq` 缺口：网关仍持久化事件，桌面端显示流缺口警告。
- 子代理事件缺少 `parent_run_id`：网关接受但标记 projection degraded。

Runtime 崩溃：

1. 网关关闭该 Runtime 对应的 active runs。
2. 对每个 root run 追加 `error(recoverable=true/false)` 事件。
3. WebSocket 正常发送 terminal error 后结束对应 run 订阅，连接保持可用。

## 13. 最小实现建议

MVP 必须实现：

1. Event Multiplexer 单写 stdout。
2. `root_seq`、`agent_seq`、`stream.seq`。
3. 主代理和 `in_process` 子代理共用 root run 的 WebSocket 事件订阅。
4. 网关按 `(root_run_id, root_seq)` 持久化。
5. 桌面端按 `stream_id` 拼接 delta。

暂缓实现：

1. 跨 worker 子代理事件合并。
2. 子代理独立进程直接调度。
3. 复杂事件重排和缺口自动恢复。
