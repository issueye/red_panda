# 会话存储与上下文系统开发计划

| Field | Value |
| --- | --- |
| Version | v0.1 |
| Date | 2026-07-18 |
| Status | Complete |
| Related | `docs/13-session-fork-compact-design.md`, `docs/45-session-system-abstraction-design.md` |

## 1. 目标

在不引入通用 Store 框架的前提下，为会话建立一个薄的存储投影边界，统一管理：

- 会话元数据与生命周期；
- 完整可见历史与分页读取；
- 模型上下文窗口；
- 版本化会话摘要；
- Goal scratchpad 上下文的关联读取；
- fork、compact 和级联删除的数据一致性。

原始消息始终是审计事实。摘要、上下文条目和模型窗口都是派生投影，不能覆盖或删除原始消息。

## 2. 设计边界

```text
SessionService / WorkspaceService
        |
        +-- sessionStore       SQLite 持久化投影、分页、摘要读取
        +-- sessionLifecycle   Run 取消、数据库清理、事件广播
        +-- RuntimeClient      进程级暂停/取消
        +-- EventHub           Desktop 实时同步
```

`sessionStore` 使用具体类型而不是单实现接口。底层继续复用现有 Repository，避免重复 ORM 封装。

### 2.1 数据语义

| 数据 | 语义 |
| --- | --- |
| Message | 不可变可见历史；按 `(session_id, seq)` 唯一排序 |
| SessionSummary | `session_compactions` 中的版本化摘要；同一会话只有一个 active/applied 快照 |
| ModelContext | active summary + `source_end_seq` 之后的 root conversation tail |
| ContextItem | Goal scratchpad；不伪装成聊天消息，不重复注入 |
| Memory | 长期跨会话数据；保持独立 MemoryService，仅在运行时按策略注入 |

## 3. 开发阶段

### Phase 1 - 存储投影与长历史

- 新增薄 `sessionStore`，集中完整历史、分页历史、摘要和模型上下文读取。
- `GET /sessions/:id/history` 改为游标分页契约。
- Desktop 顺序加载全部分页，保持完整可见历史。
- 压缩规划使用不截断的完整历史查询。

验收：超过 200 条消息后，UI 恢复完整；压缩保留真正最近轮次。

### Phase 2 - 生命周期统一

- 新增共享 `sessionLifecycle`。
- 单会话删除和工作区级联删除走同一数据清理路径。
- 取消 active run、取消 active goal、禁用 fixed schedule、删除 todo、软删除 session。
- 提交后逐会话广播 `session.deleted`。

验收：任何删除入口都不留下活动 Run、Goal 或持续失败的固定定时任务。

### Phase 3 - 数据库不变量

- 为 `(session_id, seq)` 添加唯一索引。
- 消息追加在事务中分配序号，唯一冲突时有限重试。
- 为摘要 active 状态和常用范围查询补索引。

验收：并发追加不能产生重复序号；旧数据库迁移可重复执行。

### Phase 4 - 会话上下文查询

- 提供会话上下文状态 DTO：摘要边界、tail 数量、估算使用量、关联 Goal context。
- 保持 Goal scratchpad 独立写模型，由会话上下文读取投影关联。
- Desktop 使用服务端上下文边界，不再推测固定保留轮次。

验收：Desktop token ring 与 Gateway 实际模型上下文使用同一边界。

### Phase 5 - 容量与运维

- Session 列表改为分页，不再固定 100 条。
- 增加摘要历史查询与诊断信息。
- 增加长会话、迁移、级联删除和多 Desktop 事件回归测试。

验收：超过 100 个会话仍可完整访问，协议兼容脚本覆盖分页与删除。

## 4. 测试门禁

每个阶段至少通过：

1. `modules/gateway`: `go test ./...`
2. `modules/agent`: `go test ./...`
3. `modules/protocol`: `go test ./...`
4. `modules/desktop/frontend`: `npm test -- --run`
5. 会话相关协议脚本与 Desktop Gateway-backed 测试

## 5. 完成定义

- [x] 所有会话删除入口共享生命周期语义。
- [x] 历史和 Session 列表没有不可见的固定截断。
- [x] 摘要不改变原始历史，模型上下文边界可审计。
- [x] 消息序号由数据库约束保证唯一。
- [x] 上下文、摘要、Memory 的职责没有重叠写路径。
- [x] 新增长会话、级联删除、迁移、race 和分页测试全部通过。

## 6. 实施结果

- `sessionStore` 统一完整历史、分页历史、active summary 和模型上下文读取。
- `sessionLifecycle` 被单会话删除和工作区级联删除共同使用。
- 历史 API 使用 `after_seq`，Session 列表使用 `offset`；Desktop 会加载全部分页。
- `GET /sessions/:id/context` 返回 active summary、摘要边界、tail、Goal scratchpad 和估算用量。
- `GET /sessions/:id/summaries` 返回版本化摘要历史。
- 数据迁移会修复旧重复消息序号和重复 active summary，再创建唯一索引。
- 协议脚本已移除 v0.1 `root_seq/root_run_id/subagent_update` 路径，只验证 v0.2 Worker 契约。
