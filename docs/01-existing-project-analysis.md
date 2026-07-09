# 现有项目分析

分析对象：

- `D:\codes\issueye\ai_agents\DeepSeek-Reasonix`
- `D:\codes\issueye\ai_agents\night24`

目标是为 `red_panda` 提炼可复用能力和应避免的问题，并最终落到 Go 三层架构：桌面端、中间网关服务、Agent Runtime。

## 1. DeepSeek-Reasonix

### 1.1 项目定位

`DeepSeek-Reasonix` 是一个 Go 实现的 coding agent。它的核心思想是：用一个 transport-agnostic 的 Go 控制器承载 agent 会话生命周期，CLI、HTTP/SSE 服务和 Wails 桌面端都复用同一个 `internal/control.Controller`。

关键证据：

- `cmd/reasonix/main.go` 只负责 CLI 入口，并通过 blank import 注册 provider 和内置工具。
- `internal/control/controller.go` 明确描述 Controller 是无前端绑定的 session driver。
- `internal/serve/serve.go` 把 Controller 暴露为 HTTP/SSE。
- `desktop/README.md` 说明桌面端通过 Wails 直接绑定 Go 方法，不走 HTTP hop。
- `REASONIX.md` 强调“一个 transport-agnostic Controller 位于所有前端之后”。

### 1.2 核心模块

| 模块 | 职责 |
| --- | --- |
| `internal/agent` | 模型循环、工具调用、子 agent 上下文、上下文压缩 |
| `internal/control` | 会话生命周期、取消、审批、计划模式、checkpoint、事件输出 |
| `internal/provider` | Provider 抽象和注册表，OpenAI/Anthropic 等实现自注册 |
| `internal/tool` | 工具接口、工具注册表、内置工具和 schema 合约 |
| `internal/plugin` | MCP 客户端，支持 stdio 和 HTTP/streamable HTTP |
| `internal/serve` | HTTP/SSE 前端，复用 Controller |
| `desktop` | Wails 桌面壳，React 前端直接调用 Go 绑定方法 |
| `internal/config` | TOML 配置、provider/model、权限、插件、路径 |
| `internal/memory` / `internal/history` | 长期记忆、历史检索和压缩归档 |
| `internal/checkpoint` | 文件快照和 rewind |
| `internal/hook` / `internal/skill` | 生命周期 hook 和 skill 扩展 |

### 1.3 可借鉴点

1. Go 内核成熟，适合 `red_panda` 继续选择 Go。
2. Provider 和 Tool 都是接口 + registry，扩展能力清晰。
3. 工具 schema 有文档和测试保护，能减少 provider-visible contract 漂移。
4. 权限、计划模式、checkpoint、memory、history、hooks、skills 形成完整 agent 能力。
5. MCP 插件适配做得比较完整，内置工具和插件工具对 agent 统一呈现。
6. 上下文缓存意识强，避免频繁改动 system prompt 前缀。
7. 事件流是 typed event stream，适合多前端统一消费。

### 1.4 不直接照搬点

1. 桌面端直连 Go Controller，虽然低延迟，但会弱化三层边界；`red_panda` 明确要保留中间网关服务。
2. Controller 职责非常重，包含会话、权限、checkpoint、MCP、memory、goals、jobs 等，后期维护复杂。
3. 单个 Go 进程内聚合过多能力，Agent 崩溃和 UI/API 网关隔离不足。
4. 桌面、serve、CLI 共用一个控制器是优点，但 `red_panda` 更应把“网关协议”和“agent 协议”固定下来，避免桌面端绕过网关。

## 2. night24

### 2.1 项目定位

`night24` 是一个 Rust/Tauri 实现的三层 AI Agent 原型。它的核心思想是：Tauri 桌面端通过 HTTP/SSE 访问本地 `night24-server`，server 再通过 stdio JSON-RPC 管理 `night24-agent-core`。

关键证据：

- `README.md` 说明 `night24-server` 提供 HTTP API、SSE、Session、工具、权限和 OpenAPI。
- `docs/server-definition.md` 定义 server 是桌面端和 Agent Core 之间的本地桥梁。
- `docs/protocol-server-agent-core-json-rpc.md` 定义 server 与 agent core 使用 newline-delimited JSON-RPC 2.0 over stdio。
- `crates/night24-server/src/main.rs` 暴露 `/reply`、`/runs/{run_id}/events`、`/sessions`、`/workspace/*`、`/permissions/*`、`/agent/core/restart` 等路由。
- `crates/night24-server/src/agent_runner.rs` 支持 `single_core`、`per_run_process`、`process_pool` 三种 runner 模式，其中 process pool 还未实现。
- `crates/night24-agent-core/src/main.rs` 是 stdio JSON-RPC 进程入口。

### 2.2 核心模块

| 模块 | 职责 |
| --- | --- |
| `tauri-app` | 桌面 UI，React 组件、hooks、设置、会话、工作区、SSE 消费 |
| `crates/night24-server` | HTTP/SSE API、auth、session、workspace、run event store、agent-core 进程管理 |
| `crates/night24-agent-core` | stdio JSON-RPC Agent Core，处理 `agent.reply`、`agent.cancel`、权限、skills、subagents |
| `crates/night24-protocol` | server/core 共享协议类型、事件、权限、方法参数 |
| `crates/night24-core` | provider、agent loop、tool executor、session、security、context compaction |
| `crates/night24-mcp` | MCP 能力 |
| `crates/night24-gts` | 自定义脚本/插件运行时方向，较重 |

### 2.3 可借鉴点

1. 三层边界清晰，正好符合 `red_panda` 目标。
2. 桌面端只依赖 HTTP/SSE，不直接绑定 Agent Runtime。
3. Server/Core 协议用 JSON-RPC 2.0，方法、事件、错误码、capability 都有文档化设计。
4. Agent Core stdout 只输出协议消息，stderr 只输出日志，便于进程隔离和故障诊断。
5. `agent.reply` 采用 accepted + event notification 模式，适合长任务流式执行。
6. Server 负责 session、workspace、permission coordinator 和 event replay，Agent Core 专注执行。
7. 支持单 Core、每次运行一个 Core、进程池的演进方向。
8. 子 agent 设计比较直观：spawn/status/message/wait/cancel，加 mailbox 和 pool 状态。

### 2.4 不直接照搬点

1. 语言栈分散：Rust server/core + Tauri + JS 前端，`red_panda` 应统一 Go 后端和 runtime。
2. `night24-core` 的 agent loop 和工具体系较基础，Reasonix 在工具、MCP、记忆、checkpoint、上下文维护上更成熟。
3. 当前 Tauri shell 中存在固定 `http://localhost:17787` 访问方式，`red_panda` 需要设计端口发现、健康检查和网关生命周期管理。
4. `process_pool` 还未落地，`red_panda` 设计时应把进程池作为第二阶段，不阻塞 MVP。
5. 文档和代码中部分 server 职责仍在演进，`red_panda` 需要在一开始明确哪些状态归网关，哪些状态归 Agent Runtime。

## 3. 两者对比

| 维度 | Reasonix | night24 | red_panda 取舍 |
| --- | --- | --- | --- |
| 语言 | Go | Rust + Tauri + JS | Go 为后端和 runtime 主语言 |
| 桌面通信 | Wails 直接绑定 Go Controller | HTTP/SSE 到 server | 桌面端只访问本地网关 |
| 中间网关 | 可选 serve 前端 | 核心层之一 | 作为强制层 |
| Agent 隔离 | 多数能力在同一 Go 进程 | Agent Core 子进程 | Agent Runtime 独立进程 |
| 协议 | 内部 typed events + HTTP/SSE | HTTP/SSE + stdio JSON-RPC | 固定双协议：桌面 HTTP/WebSocket，网关 JSON-RPC |
| Provider | Registry 成熟 | Provider registry 较简单 | 借鉴 Reasonix registry |
| Tools | 内置工具 + MCP 成熟 | 基础工具 + security | 借鉴 Reasonix 工具合约和 MCP |
| Session | Controller 管理 JSONL/session | Server 管理 SQLite/内存 | 网关持久化，Agent 接收快照 |
| Permission | Controller gate | Server 协调，Core 等待 resolve | 网关协调，Agent 发 permission_required |
| Sub-agent | task/parallel_tasks 等较成熟 | pool + mailbox 设计清晰 | Agent 内建 pool，网关展示和控制 |

## 4. red_panda 设计结论

`red_panda` 应采用“Night24 的三层边界 + Reasonix 的 Go agent 能力”：

1. 桌面端必须走中间网关，不直接调用 Agent Runtime。
2. 中间网关是唯一 API 入口，管理 session、workspace、权限、事件、Agent 进程。
3. Agent Runtime 是可重启、可替换、可多实例的 Go 子进程。
4. Server/Agent 之间用 newline-delimited JSON-RPC 2.0 over stdio。
5. Desktop/Gateway 之间用 HTTP JSON API + WebSocket event/command channel；不采用 SSE，便于未来外部客户端复用同一交互协议。
6. Provider、Tool、MCP、Memory、Skill、Sub-agent 都在 Agent Runtime 内实现接口化，网关只做编排和展示。
7. 网关持久化会话和事件，Agent Runtime 每次 `agent.reply` 接收 session snapshot，返回事件增量。
8. 权限必须由 Agent Runtime 发起、网关协调、桌面端确认、再由网关 resolve 回 Agent。
