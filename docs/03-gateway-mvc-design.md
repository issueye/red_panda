# 中间网关 MVC 设计

## 1. 技术栈

中间网关服务固定采用：

- Go
- Gin
- SQLite no-cgo
- GORM
- MVC + Service + Repository

SQLite 驱动要求：

- 禁止使用依赖 cgo 的 `mattn/go-sqlite3`。
- GORM 适配 SQLite 时使用 pure-Go driver，建议选 `github.com/glebarez/sqlite`。
- 网关必须能在 `CGO_ENABLED=0` 下完成构建。

初始依赖建议：

```go
require (
    github.com/gin-gonic/gin v1
    gorm.io/gorm v1
    github.com/glebarez/sqlite v1
)
```

## 2. 目录结构

```text
internal/gateway/
  app/
    app.go                 # 创建 Gin engine、注册中间件和路由
    router.go              # 路由分组
    middleware/
      auth.go
      cors.go
      request_id.go
      recovery.go
  controller/
    health_controller.go
    agent_controller.go
    run_controller.go
    session_controller.go
    workspace_controller.go
    permission_controller.go
    tool_controller.go
    skill_controller.go
    subagent_controller.go
  service/
    agent_service.go
    run_service.go
    session_service.go
    workspace_service.go
    permission_service.go
    tool_service.go
    skill_service.go
    subagent_service.go
  repository/
    session_repository.go
    message_repository.go
    run_event_repository.go
    workspace_repository.go
    setting_repository.go
    permission_repository.go
  model/
    session.go
    message.go
    run_event.go
    workspace.go
    setting.go
    permission.go
  dto/
    common.go
    agent.go
    run.go
    session.go
    workspace.go
    permission.go
  infra/
    database/
      database.go
      migrate.go
      sqlite.go
    eventhub/
      hub.go
      subscriber.go
      websocket.go
  runtime/
    client.go              # Agent Runtime JSON-RPC client
    process.go             # Agent 子进程管理
    protocol_adapter.go
  config/
    config.go
```

## 3. 分层规则

### 3.1 Controller

Controller 只负责：

1. Gin 参数绑定。
2. 请求上下文和用户认证信息读取。
3. 调用 Service。
4. 返回统一响应。

Controller 不负责：

1. 数据库查询。
2. Agent Runtime JSON-RPC 细节。
3. 工作区路径解析。
4. 权限业务判断。
5. 会话事件持久化。

### 3.2 Service

Service 负责业务编排：

1. Run Service 创建 run、调用 Agent Runtime、连接 Event Hub。
2. Session Service 管理会话生命周期。
3. Workspace Service 管理工作区、文件树、文件预览、Git diff。
4. Permission Service 管理 pending permission，并把用户决策回传 Agent。
5. Agent Service 管理 Agent Runtime 状态、初始化、重启。

Service 可以调用多个 Repository，也可以调用 Runtime Client。

### 3.3 Repository

Repository 只负责 GORM 数据访问：

1. 查询、写入、事务。
2. GORM model 到领域对象或 DTO 的基础映射。
3. 所有方法接收 `context.Context`。

Repository 不负责 HTTP 状态码和响应结构。

### 3.4 Model 和 DTO

Model 是数据库结构，DTO 是 API 结构，两者必须分离。

原因：

1. 避免数据库字段直接泄露给桌面端。
2. 避免 API 兼容性被数据库迁移影响。
3. 方便隐藏敏感字段，如 API key、内部路径和错误堆栈。

## 4. 数据库初始化

初始化建议：

```go
package database

import (
    "gorm.io/gorm"
    "github.com/glebarez/sqlite"
)

func Open(path string) (*gorm.DB, error) {
    return gorm.Open(sqlite.Open(path), &gorm.Config{})
}
```

启动后必须设置 SQLite pragma：

```sql
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
```

写入策略：

- MVP 可以先依赖 SQLite WAL + busy timeout。
- 如果后续并发写入变多，在 Service 层为 run event append 引入单写队列。
- 长事务禁止覆盖 Agent run 全生命周期，只包住最小数据库写入。

## 5. 核心数据模型

### 5.1 Session

字段：

- `id`
- `name`
- `workspace_root`
- `status`
- `parent_id`
- `created_at`
- `updated_at`
- `deleted_at`

### 5.2 Message

字段：

- `id`
- `session_id`
- `role`
- `content_json`
- `seq`
- `run_id`
- `created_at`

### 5.3 RunEvent

字段：

- `id`
- `run_id`
- `seq`
- `type`
- `payload_json`
- `created_at`

索引：

- unique index: `run_id + seq`
- index: `run_id`

### 5.4 Workspace

字段：

- `id`
- `root`
- `name`
- `last_opened_at`
- `created_at`
- `updated_at`

### 5.5 PermissionRequest

字段：

- `id`
- `run_id`
- `tool_call_id`
- `tool_name`
- `risk`
- `summary`
- `arguments_json`
- `status`
- `decision`
- `reason`
- `created_at`
- `resolved_at`

## 6. 路由分组

```text
/healthz
/readyz

/api/v1/agent
  GET  /status
  POST /restart

/api/v1/runs
  POST /                     # start run
  POST /:run_id/cancel

/api/v1/ws
  GET  /                     # WebSocket command/event channel

/api/v1/sessions
  GET  /
  POST /
  GET  /:id
  GET  /:id/history
  POST /:id/fork
  POST /:id/compact
  DELETE /:id

/api/v1/workspaces
  POST /open
  GET  /current
  GET  /recent
  GET  /tree
  GET  /file
  GET  /diff

/api/v1/permissions
  POST /:id/approve
  POST /:id/deny

/api/v1/tools
  GET /

/api/v1/skills
  GET /
  GET /:name

/api/v1/subagents
  GET /
  POST /:id/cancel
```

## 7. 统一响应

普通 JSON API：

```json
{
  "ok": true,
  "data": {},
  "error": null,
  "request_id": "req_..."
}
```

错误响应：

```json
{
  "ok": false,
  "data": null,
  "error": {
    "code": "session_not_found",
    "message": "session not found"
  },
  "request_id": "req_..."
}
```

WebSocket 不使用普通 HTTP envelope，采用独立消息 envelope，支持 request/response、event、ack、error。事件恢复依赖 `root_seq` 和客户端 `resume` 消息。

## 8. 构建约束

网关构建命令必须长期可用：

```sh
CGO_ENABLED=0 go build ./cmd/red-panda-gateway
```

Windows PowerShell 对应：

```powershell
$env:CGO_ENABLED="0"; go build ./cmd/red-panda-gateway
```

CI 至少应包含：

1. `go test ./internal/gateway/...`
2. `CGO_ENABLED=0 go test ./internal/gateway/...`
3. `CGO_ENABLED=0 go build ./cmd/red-panda-gateway`
