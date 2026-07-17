# 会话系统抽象与优化设计

| Field | Value |
| --- | --- |
| **Title** | Session System Abstraction & Optimization |
| **Version** | v0.3 |
| **Date** | 2026-07-17 |
| **Status** | In progress — PR1 done, PR2 done; lean; anti-overengineering |
| **Related** | `docs/13-session-fork-compact-design.md`, `docs/03-gateway-mvc-design.md`, `docs/06-desktop-gateway-integration.md`, `docs/40-centralization-abstraction-plan.md`, `docs/41-redundancy-overimpl-optimization-plan.md`, `docs/43-scheduled-task-design.md` |

---

## 1. 目标与约束

### 1.1 目标

收敛会话系统：**补生命周期、去冗余、理边界**。允许 **HTTP / WebSocket / DTO / 表字段 / 客户端** 做不兼容修改，但必须换来**更少代码、更少双路径**，而不是更大框架。

1. 会话创建 / 删除 / 定时创建在所有已连接 Desktop 上一致可见。  
2. 删除会话时不留下幽灵 run / 绑死的 fixed schedule。  
3. 去掉双名 DTO、死 API 面、无用 kind 残留，**不双写兼容层**。  
4. Run 与 Session 边界清晰：**能靠搬家函数解决的，不新建 service 族**。

### 1.2 兼容性立场（v0.3）

| 项 | 立场 |
| --- | --- |
| 协议 / API | **允许 BREAKING**；改完只保留一条路径 |
| 持久化 | **允许** 删列、改名、收紧语义；本地 SQLite 可迁移或清库开发 |
| Desktop | **与 Gateway 同版本绑定**；不做旧客户端适配 |
| 外部集成 | 无稳定第三方契约假设；protocol-compat 测试**改写为新契约** |

**BREAKING 的正当用途**：删除字段、合并 API、去掉别名、收紧删除语义。  
**BREAKING 的禁止用途**：借机引入通用框架、双 Service、事件总线中间件。

### 1.3 实现铁律

| # | 铁律 | 实践 |
| --- | --- | --- |
| R1 | **正确性 > 抽象** | 先修洞，再谈结构 |
| R2 | **抽出 > 新建** | 搬家 / 删冗余；禁止空接口 + 单实现 |
| R3 | **调用点 ≥ 2 才抽象** | 一处逻辑就写在调用处 |
| R4 | **不发明框架** | 禁止 Aggregate 框架、万能 Store、Service Locator |
| R5 | **BREAKING = 单路径** | 改字段就删旧字段；禁止「新+旧双写半年」 |
| R6 | **删除死代码** | 无 UI 的 fork 客户端代码、未用 DTO 别名、无引用 kind 分支一并删 |
| R7 | **PR 可测可回滚** | 每 PR 单主题；测试跟契约一起改 |

**默认姿态：能用 helper + 删代码解决的，不做 Domain 分层表演。**

### 1.4 非目标

- 跨工作区合并、云同步、多租户。  
- Redux / 完整 SessionStore 状态机。  
- Command/Query 双 Service、Notifier 接口族。  
- 为「对称」堆无消费者的事件类型。  
- 保留 fork 物理拷贝 / compact 原地摘要以外的第三种会话派生模型（除非产品明确要求）。

---

## 2. 现状摘要

**保留的不变量**

| 不变量 | 说明 |
| --- | --- |
| Session = 对话容器 | `workspace_root`、消息序、卫星数据 |
| Run ⊂ Session | 每会话串行 active run；全局并发上限 |
| Fork = 拷贝 | 新 session + messages + lineage（API 可保留，UI 已无） |
| Compact = 原地 | 不删消息；只改模型上下文；自动阈值 **80%** |
| Desktop 多会话投影 | `sessionRuntimes[id]` |

**主要问题**

| # | 问题 | 处理方向（允许 BREAKING） |
| --- | --- | --- |
| P0 | 仅 schedule 广播 `session.upserted` | create/delete 同 helper 广播 |
| P0 | 删除不取消 run / 不解 fixed schedule | Delete 内联清理 |
| P1 | DTO 双名 `name`/`title`、`workspace_root`/`working_dir` | **只留一套字段**，Desktop 删 normalize 分支 |
| P1 | Run 内嵌 `buildRunConversation` | 抽包级函数（非 interface） |
| P1 | App 会话编排散 | 事件入口收拢；**不**重写 Store |
| P2 | Fork UI 已删，客户端/测试死路径 | 删前端 fork 调用与 e2e；Gateway API 可留可砍 |
| P2 | `local-design` 占位 | **删除**；bootstrap 失败显式错误 |
| P2 | Compaction 表/模型无用字段（如 summary_message_id 若未用） | 迁移删除或停止写入 |

---

## 3. 目标契约（BREAKING 后的单路径）

### 3.1 Session DTO（HTTP）

**唯一字段集**（示例）：

```json
{
  "id": "session_...",
  "name": "定时 · 汇总",
  "workspace_root": "E:/code/...",
  "kind": "normal",
  "parent_id": "",
  "status": "active",
  "created_at": "...",
  "updated_at": "..."
}
```

| 删除 | 原因 |
| --- | --- |
| `title` | 与 `name` 重复 |
| `working_dir` | 与 `workspace_root` 重复 |
| 无用 fork 展示字段若前端不用 | 减噪；lineage 仍走专用 API |

Desktop：`normalizeSession` 只映射上述字段，删掉 fallback 链。

### 3.2 WebSocket 会话事件

| Method | Payload | 何时 |
| --- | --- | --- |
| `session.upserted` | `{ "session": SessionDTO, "reason": "...", "run_id"?: "...", "schedule_id"?: "..." }` | create / schedule fire / fork（若保留） |
| `session.deleted` | `{ "id": "...", "reason": "user_delete" }` | soft-delete 成功后 |

`reason` 用短字符串即可，**不必**枚举类型生成器。

不新增 `session.compacted`，除非出现第二个实时消费者。

### 3.3 删除语义（BREAKING：更强）

`DELETE /sessions/:id`：

1. Cancel 该 session 全部 active runs。  
2. Disable 所有 `session_mode=fixed_session && session_id=id` 的 schedule。  
3. Soft-delete session + 删 todos（现有）。  
4. Broadcast `session.deleted`。  

Goals：尽力 cancel；失败记日志不阻断删除。  
Messages / compaction / lineage：**保留**（审计，零成本）。

### 3.4 模型上下文

```text
// 包级函数即可，同一 service 包
func buildModelConversation(repos, sessionID) ([]msg, error)
```

从 `run_start.go` 挪出；**不**新建 `SessionContextAssembler` 类型。

### 3.5 Desktop

- 处理 `session.upserted` / `session.deleted`（已有 upsert 则扩展 delete）。  
- 去掉 `local-design` 假会话。  
- 去掉未接线的 fork 客户端路径（`forkSession` 若无入口可删 hook 导出）。  
- Gateway fork/compact HTTP **可保留**供将来或脚本；无 UI 不算死 API，但 e2e 不再依赖侧栏按钮。

---

## 4. 落地 PR（允许不兼容，仍禁止镀金）

### PR1 — 契约收紧 + 生命周期（BREAKING） — **DONE**

- SessionDTO 去掉 `title` / `working_dir`
- `broadcastSessionUpserted` / `broadcastSessionDeleted`；create / delete / fork / schedule 共用
- Delete：cancel runs + disable fixed schedules + broadcast
- Desktop：normalize 单路径、`session.deleted`、去掉 `local-design`

### PR2 — 上下文组装搬家 — **DONE**

- `buildModelConversation(repos, sessionID)` 于 `session_context.go`
- `run_start.prepareRun` 一行调用；测试改调新函数

### PR3 — 文档对齐（轻量）

1. 本文状态与实现一致即可；不删 DB 列除非确认无引用。  
2. 不扩大范围到 SummaryMessageID 迁移（YAGNI）。

---

## 5. Key Decisions

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 兼容性 | **允许 BREAKING** | 用户明确授权；换单路径 |
| 双名 DTO | **直接删旧字段** | 禁止双写兼容层 |
| 抽象厚度 | Helper + 现有 SessionService | 反过度实现 |
| 事件 | upserted + deleted only | 有消费者 |
| 数据模型核心 | fork-copy / compact-in-place **保持** | 正确且已落地 |
| 假会话 | **删除 local-design** | 减少特殊分支 |
| 自动摘要 | 80%（已实现） | 不改除非产品再调 |

---

## 6. 风险

| 风险 | 缓解 |
| --- | --- |
| 本地旧 DB 读挂 | 开发期 migrate 删列或文档要求清库；无生产多版本承诺 |
| protocol-compat 大面积失败 | PR1 内同步改脚本，不当「以后再说」 |
| BREAKING 范围蔓延 | PR 描述写清 breaking list；超列表项新开 PR |
| 借 BREAKING 堆架构 | 评审卡 R1–R7 |

---

## 7. 验收

- [ ] DTO / 前端 normalize **无** `title`/`working_dir` fallback。  
- [ ] create + schedule → 侧栏一致；delete → 全端消失 + run 取消。  
- [ ] 无 `local-design` 特殊分支。  
- [ ] 无「新旧字段双写」。  
- [ ] 净效果：重复代码减少，而非仅文件数增加。  
- [ ] Gateway session/schedule 测试 + Desktop 会话相关测试通过。

---

## 8. 小结

在**允许不兼容**的前提下，会话优化的正确姿势是：

1. **用 BREAKING 删冗余**（双名、假会话、死路径），不是用 BREAKING 堆新层。  
2. **生命周期补洞**（广播 + 删除清理）用已有 hub/helper。  
3. **上下文组装**用包级函数搬家即可。

**一句话：可以砍契约，但不许镀金；砍了就要更简单。**
