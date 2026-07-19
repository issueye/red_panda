# 会话删除：硬删除 + JSONL 归档

| Field | Value |
| --- | --- |
| **Date** | 2026-07-19 |
| **Status** | complete |
| **Related** | [45](45-session-system-abstraction-design.md)、[46](46-session-store-development-plan.md)、[48](48-session-correctness-optimization-plan.md) |
| **BREAKING** | 删除后 SQLite 中不再保留 soft-deleted session 行；审计改走磁盘 JSONL |

---

## 1. 背景与目标

### 1.1 现状问题

- 删除会话仅 **soft-delete**（`deleted_at` + `status=deleted`）。
- 消息 / compaction / lineage / run_events 等**默认仍留在库内**，库体积只增不减。
- 无 purge 运维路径；「已删会话」在 DB 中仍占空间。

### 1.2 目标

1. **用户删除会话后，活跃库中硬删除**该会话及其会话作用域卫星数据。  
2. **删除前**将快照 **持久化为 JSONL**，写入**固定目录**（可配置），便于审计与人工恢复。  
3. 删除失败策略：**归档失败则不删库**；归档成功、删库失败则保留 JSONL（可重试删除）。  
4. 保持 Desktop 行为：`session.deleted` 广播不变；列表不再出现该会话。

### 1.3 非目标

- 产品内「从归档恢复会话」UI（可后续加）。  
- 删除跨会话 **Memory** 记录（长期记忆保留；仅在 JSONL 中附带 `session_id` 匹配的 memory 快照可选）。  
- 迁移历史 soft-deleted 行的一键 purge（可另开脚本）。  
- 改 Fork / Compact 语义。

---

## 2. 设计

### 2.1 删除流水线

```text
lifecycle.delete(sessionIDs, reason)
  1. Runtime Cancel active runs（现有）
  2. 对每个 session_id：
       archiveSessionJSONL(archiveDir, session_id, reason)  // 失败 → 整批中止该会话
  3. 事务 hardDeleteSessionCascade(session_ids)
  4. broadcast session.deleted（现有）
```

工作区级联删除仍走同一 `lifecycle.delete`。

### 2.2 JSONL 归档

| 项 | 约定 |
| --- | --- |
| **目录** | `RED_PANDA_SESSION_ARCHIVE_DIR`；默认 `{dirname(database)}/session_archives` |
| **文件名** | `{session_id}_{utc_compact}.jsonl` 例如 `session_123_20260719T120000Z.jsonl` |
| **格式** | 每行一个 JSON 对象；UTF-8；首行 header |

**行类型（`type` 字段）：**

| type | 内容 |
| --- | --- |
| `archive_header` | `v`, `archived_at`, `reason`, `session_id`, `gateway_version?` |
| `session` | sessions 表行 |
| `message` | messages |
| `run` | run_records |
| `run_event` | run_events（按 run 导出） |
| `tool_call` | tool_calls |
| `permission` | permission_requests |
| `todo` | todo_items |
| `goal` / `goal_action` / `goal_event` / `goal_segment` / `goal_note` | Goal 族 |
| `compaction` | session_compactions（source 或 target 命中） |
| `lineage` | session_lineages（source 或 target 命中） |
| `schedule_run` | scheduled_task_runs（可选审计） |
| `memory_ref` | 仅 `session_id` 匹配的 memory **快照**（**不**从库删除） |

每行形状：

```json
{"v":1,"type":"message","data":{...row fields...}}
```

### 2.3 硬删除范围（SQLite）

| 删除 | 说明 |
| --- | --- |
| messages | by session_id |
| todos | by session_id |
| tool_calls / permissions | by session_id |
| run_events | by run_id ∈ session runs |
| run_records | by session_id |
| goal_notes / actions / events / segments | by session_id 或 goal_id |
| goals | by session_id |
| session_compactions | source_session_id 或 target_session_id |
| session_lineages | source 或 target |
| scheduled_task_runs | by session_id（审计行） |
| sessions | **HardDelete** 行 |
| fixed schedules | 仍 **Disable**，不删 schedule 定义 |

**不删：** memory_records（跨会话知识）、provider/mcp 配置、workspace 行。

### 2.4 配置

| 来源 | 键 |
| --- | --- |
| Env | `RED_PANDA_SESSION_ARCHIVE_DIR` |
| 默认 | 与 DB 同目录下 `session_archives/` |
| `app.Config` | `SessionArchiveDir` |

---

## 3. 实现切片

| ID | 内容 | 文件 |
| --- | --- | --- |
| **W1** | 本文档 + docs/README 索引 | docs/49… |
| **W2** | `repository` 硬删/按 session 批量删 | `session.go` + 各 repo 或 `session_purge.go` |
| **W3** | `session_archive.go` JSONL 写出 | service |
| **W4** | `deleteSessions`：先归档再 hard purge | session_store / lifecycle |
| **W5** | 配置透传 Options / app.Config / main env | set.go, app.go, main.go |
| **W6** | 单测：删后 DB 无行 + 文件存在可解析 | service tests |

---

## 4. 验收

```text
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Delete|Archive|Hard"
go test ./modules/gateway/internal/gateway/repository/ -count=1
```

- 删除会话后 `Sessions.Get` → not found；`Messages.ListAll` 为空。  
- `session_archives/` 下存在对应 `.jsonl`，含 `archive_header` 与至少 session/message 行。  
- 归档目录不可写时删除失败且会话仍在。  
- Desktop：删除后仍收到 `session.deleted`（行为不变）。

---

## 5. 风险

| 风险 | 缓解 |
| --- | --- |
| 大会话归档 IO 慢 | 流式按表写 JSONL；删除仍同步（可后续异步） |
| 磁盘满 | 归档 error 阻断删除 |
| 旧 soft-deleted 行 | 不自动清；可后续 `purge-soft-deleted` 脚本 |
| 恢复 | 仅文件级；不做自动 reimport |

---

## 6. 进度

| ID | 状态 |
| --- | --- |
| W1 文档 | `done` |
| W2 repository purge | `done` |
| W3 JSONL archive | `done` |
| W4 deleteSessions 接线 | `done` |
| W5 配置 env/Options | `done` |
| W6 单测 | `done` |

## 7. 执行记录

| Date | Note |
| --- | --- |
| 2026-07-19 | 创建计划；启动实现 |
| 2026-07-19 | 硬删 + JSONL 归档落地；Workspace 删除测更新为 hard-delete 期望 |
