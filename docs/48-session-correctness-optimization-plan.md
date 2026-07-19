# 会话系统正确性优化计划

| Field | Value |
| --- | --- |
| **Date** | 2026-07-19 |
| **Status** | active (Wave A–C done; D planned) |
| **Related** | [45](45-session-system-abstraction-design.md)、[46](46-session-store-development-plan.md)、会话系统问题审计 |
| **Task plan** | [plans/2026-07-19-session-correctness-p0.md](plans/2026-07-19-session-correctness-p0.md) |

---

## 1. 背景

会话主路径（容器、分页、删除级联、compact 双投影、Desktop 多会话 runtime）已可用。审计剩余风险集中在：

| ID | 问题 | 影响 |
| --- | --- | --- |
| **C1** | 同 session 并发 Compact/Preview 无互斥 | 摘要互相覆盖、pause/resume 交错 |
| **C2** | Compact 与 root run 契约未文档化/未收紧 | 区间与 transcript 可能短暂不一致 |
| **C3** | `preserveLive` hydrate **丢弃** history 覆盖 | 切回仍在跑的会话可能丢消息 |
| **C4** | 快速切换会话时 hydrate 响应乱序 | 旧请求覆盖新会话投影 |
| **C5** | 模型上下文硬编码 200 条 | 长会话截断与 token 预算脱节 |

本计划优先 **C1–C4（P0）**，C5 与 bootstrap API 放在 P1。

### 1.1 目标

1. 同一 session 同时最多一个 compact 临界区（Preview 或 Apply）。  
2. 写清 compact 与 run 的并发契约；Apply 始终以 pause 后服务端 re-plan 为准。  
3. `preserveLive` 路径 **merge** 权威 history，保留未落库的 live 流式行。  
4. hydrate 带 generation，丢弃过期响应。  

### 1.2 非目标

- 重写 Session 状态机 / 引入 Store 框架。  
- 强制 compact 时取消 root run（会破坏 auto-compact 产品行为）。  
- 本迭代做 `session.compacted` 事件或软删 GC。  
- Fork 产品化或删除。

### 1.3 原则

| # | 原则 |
| --- | --- |
| P1 | 正确性 > 性能 > 抽象 |
| P2 | 小步可测：每波独立单测 |
| P3 | 复用已有 `mergeSessionHistoryMessages`，不重造 merge |
| P4 | Compact 仍 **DelegatedOnly** pause（与现网 auto-compact 一致） |

---

## 2. 波次

```text
Wave A  Compact 会话互斥 + 契约注释     Gateway
Wave B  preserveLive merge + hydrate gen Desktop
Wave C  模型上下文 token 裁剪（P1）      Gateway
Wave D  bootstrap 聚合 / 虚拟列表（P1）  后续
```

| Wave | 风险 | 预估 | 主要文件 |
| --- | --- | --- | --- |
| **A** | L–M | 0.5 天 | `session.go`、`session_test.go` |
| **B** | L–M | 0.5 天 | `useSessionBootstrap.js`、`sessionHistory.js`、相关 test |
| **C** | M | 1 天 | `session_context.go`、ContextState、token 估计 |
| **D** | M | 1–2 天 | controller + Desktop |

---

## 3. Wave A — Compact 互斥与契约

### 3.1 契约（写入代码注释 + 本文）

```text
1. 同一 session_id：CompactPreview 与 Compact 共享互斥锁；拿不到锁 → 明确错误。
2. Root run 允许保持 active；pause 仅 DelegatedOnly（Worker 委托）。
3. Apply 在 pause 之后必须重新 planCompaction（已有）；客户端 SourceRange 仅作提示，
   EndSeq 超过最新消息时由 plan 夹紧（已有逻辑则保持）。
4. 不在本波取消 root run。
```

### 3.2 实现

- `SessionService` 增加 `compactMu sync.Mutex` + `compacting map[string]struct{}`（或 `sync.Map`）。  
- `beginSessionCompact(sessionID) (unlock func(), error)`  
- `Compact` / `CompactPreview` 入口 `begin`，`defer unlock`。  
- 错误文案：`session compact already in progress`。

### 3.3 验收

```text
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Compact|Session"
```

- 单测：同 session 并发第二次 Compact 失败；结束后第三次成功。

---

## 4. Wave B — preserveLive merge + hydrate generation

### 4.1 preserveLive

当 `preserveLive && prev.hydrated && prev.running`：

| 字段 | 行为 |
| --- | --- |
| messages | `mergeSessionHistoryMessages(prev.messages, normalizedHistory)` |
| tools / permissions | 仍可用服务端全量替换（或 merge by id）；本波 **tools/permissions 用服务端列表** |
| runs / todos / goals / context | 保持现有更新逻辑 |
| live streaming 行 | merge 助手保留未持久化 suffix |

### 4.2 hydrate generation

```text
per-session gen++
await fetch...
if gen !== current[sessionId] → discard patch
```

避免快速切换 A→B 时 A 的慢请求写回 B（或写回过期的 A）。

### 4.3 可选小改进

- `loadAllSessionHistory(sessionId, { afterSeq })` 支持增量；preserveLive 时用 `max(messageSeq)` 作 afterSeq 再 merge（全量仍可作 fallback）。

### 4.4 验收

```text
cd modules/desktop/frontend && node --test src/lib/sessionMessageMerge.test.js
# + bootstrap 相关单测若有
```

- 单测：preserveLive merge 路径保留 streaming assistant 后缀。  
- 单测：generation 丢弃过期（纯函数或 mock）。

---

## 5. Wave C — 模型上下文 token 裁剪（P1，本迭代可后置）

- `buildModelConversation` 不再固定 200 条；按估算 token 从 tail 向前装。  
- ContextState 返回 `model_message_count` / `estimated_tokens`（已有字段则对齐实现）。  
- 与 Desktop token ring 同源口径文档化。

---

## 6. Wave D — 后续

- `GET /sessions/:id/bootstrap` 聚合 hydrate。  
- 非当前会话 runtime LRU。  
- 软删 GC。  
- Fork 产品决策。

---

## 7. 进度

| Wave | 条目 | 状态 |
| --- | --- | --- |
| A | A1 compact 互斥 | `done` |
| A | A2 并发单测 + 契约注释 | `done` |
| B | B1 preserveLive merge | `done` |
| B | B2 hydrate generation | `done` |
| B | B3 单测 | `done`（复用 merge 既有测 + compact 并发测） |
| C | C1 token 裁剪 model context | `done` |
| D | bootstrap / LRU | `todo` |

---

## 8. 执行记录

| Date | ID | Note |
| --- | --- | --- |
| 2026-07-19 | plan | 创建本文档；启动 Wave A/B |
| 2026-07-19 | A1–A2 | `sessionCompactGate` + Compact/Preview 互斥；`TestSessionServiceRejectsConcurrentCompact` |
| 2026-07-19 | B1–B2 | preserveLive 用 `mergeSessionHistoryMessages`；per-session hydrate gen |
| 2026-07-19 | C1 | `selectModelContextMessages` + fetch 800 / budget 32k；ContextState 同源 |
