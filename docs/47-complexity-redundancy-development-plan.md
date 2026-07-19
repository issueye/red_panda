# 复杂度与冗余收敛 — 开发计划

| Field | Value |
| --- | --- |
| **Date** | 2026-07-19 |
| **Status** | active (Wave A + B done; C–E planned) |
| **Branch** | `feat/goal-context-scratchpad` |
| **Related** | [35](35-redundancy-convergence-checklist.md)、[38](38-code-directness-optimization-plan.md)、[41](41-redundancy-overimpl-optimization-plan.md) |
| **Basis** | 2026-07-19 代码冗余度 / 功能实现复杂度审计 |
| **Task plan** | [plans/2026-07-19-complexity-redundancy-wave.md](plans/2026-07-19-complexity-redundancy-wave.md) |

---

## 1. 背景

docs/41 Wave 0–5 已完成：默认工具面收窄、`state.tool.execute` 统一入口、文件拆分、专家默认 3 个。审计结论：

| 维度 | 等级 | 主债 |
| --- | --- | --- |
| 冗余 | 中 (~5/10) | 四域 service 样板；`todo_write` / `segment_end` 兼容层；前端资源 lib 同构 |
| 复杂度 | 中高 (~7/10) | `runWithGoalLoop` 长协调器；`dispatchTool` 分支；Goal service 分支密度 |

本计划在 **不改变对外协议与默认行为** 的前提下，继续降复杂度与样板成本。

### 1.1 目标

1. Goal 外层循环可读为命名步骤：deadline → hydrate → segment → report → **decide continue**。
2. 段续跑 / 预算 / 工具轮次上限为 **可单测纯函数**，避免只在集成测试里覆盖。
3. Gateway 状态工具 JSON 输出共用 marshal 辅助；别名判断收敛到单点。
4. 为后续「兼容退役」与「App.jsx 削薄」留下明确后续波次，本迭代不强行 BREAKING。

### 1.2 非目标

- 合并 Memory/Todo/Goal/Context 数据表或 HTTP API。
- 删除 `todo_write` / 旧 4 个 `*.tool.execute` method（仅标注与单点化）。
- 改 Goal 专家数量、默认 allowlist、权限语义。
- Desktop 全站 TypeScript 或 UI 重设计。

### 1.3 原则

| # | 原则 |
| --- | --- |
| P1 | 行为零 diff：公开 HTTP/WS/JSON-RPC 字段与默认工具面不变 |
| P2 | 小步可合并：每波独立 `go test` 门禁 |
| P3 | 抽「命名领域步骤」，不上万能框架 |
| P4 | 正确性优先于删兼容层 |

---

## 2. 波次总览

```text
Wave A  Goal loop 纯决策拆分          ── 降复杂度（本迭代主交付）
Wave B  状态工具信封样板 + 别名单点   ── 降冗余（本迭代可并行）
Wave C  runner 分发表化 / 注册表驱动  ── 可选，行为不变
Wave D  Desktop App 协调器削薄        ── 前端，可独立会话
Wave E  兼容层 deprecate 窗口         ── 需产品拍板后另开
```

| Wave | 风险 | 预估 | 主要文件 |
| --- | --- | --- | --- |
| **A** | L | 0.5–1 天 | `runtime/goal_loop.go`、`goal_loop_decisions.go`、`goal_loop_test.go` |
| **B** | L | 0.5 天 | `service/runtime_tool_output.go`、memory/todo/goal output helpers、alias helper |
| **C** | M | 1–2 天 | `tools/runner.go`、`registry.go` |
| **D** | M | 2–3 天 | `App.jsx`、hooks |
| **E** | H | 另排期 | protocol methods、全栈别名 |

---

## 3. Wave A — Goal loop 纯决策拆分

### 3.1 问题

`runWithGoalLoop`（~143 行）混合：

- wall-clock deadline 配置
- 每段 Todo/Goal/notes 注入
- 段工具轮次预算
- Gateway `segment_end` 上报
- **是否继续下一段** 的分支树

后一类决策应是纯函数，便于单测与审阅。

### 3.2 设计

新增 `modules/agent/internal/runtime/goal_loop_decisions.go`：

| 符号 | 职责 |
| --- | --- |
| `effectiveSegmentToolLimit(goal)` | `MaxToolTurnsSeg` 与总预算剩余取紧 |
| `goalLoopAfterSegment` 输入/输出 | 根据 `state`、`last.Reason`、`seg`、`maxSeg` 决定 stop / continue / 新 `maxSeg` |
| （可选）`initialGoalMaxSegments(state)` | 绑定且非终态时用 `goalRunSegmentLimit`，否则 1 |

`runWithGoalLoop` 只编排：seed → deadline → for { hydrate; limit; segment; report; **decision** }。

### 3.3 验收

```text
go test ./modules/agent/internal/runtime/ -count=1 -run "Goal|Segment|Loop"
go test ./modules/agent/internal/runtime/ -count=1 -skip "Web|Duck|Tavily"
```

- 既有 `TestRunWithGoalLoop*` 行为不变。
- 新增纯函数表驱动测：未绑定单段、预算耗尽停、terminal 停、soft boundary 续跑、maxSeg 顶住。

### 3.4 不做

- 不改 `segment_end` 协议名（属 Wave E）。
- 不改 `MaxSegmentsPerRun` 语义。

---

## 4. Wave B — 状态工具样板与别名单点

### 4.1 问题

- `runtimeMemoryOutput` / `runtimeTodoOutput` / `runtimeGoalOutput` 各自 `json.Marshal` + fallback 字符串。
- `todo_write` 在 runner / policy / gateway / frontend / worker 多处字符串比较。

### 4.2 设计

1. `service/runtime_tool_output.go`：`marshalRuntimeToolJSON(payload map[string]any, fallback string) string`。
2. 三域 output helper 改为调用该函数（payload 形状 **不变**）。
3. Agent 侧：`tools/aliases.go` 或 `helpers.go` 增加 `NormalizeToolName(name string) string`（`todo_write` → `todo.write`），runner 入口归一化一次；Gateway todo 分支可保留兼容 case 一版。

### 4.3 验收

```text
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Todo|Memory|Goal|StateTool"
go test ./modules/agent/internal/tools/ -count=1
```

---

## 5. Wave C — runner 分发（后续）

将 `dispatchTool` 大 switch 拆为：

- workspace / git / shell 组函数
- state 工具组（已部分有 `runTodoTool` 等）
- worker / skill 组

或从 registry 挂 `Handler` 字段。**禁止**改变 schema / risk / 超时类。

---

## 6. Wave D — Desktop 协调器（后续）

从 `App.jsx`（~1135 行）迁出：

- permission 流
- activity 加载
- right-panel 选择

保持 `useSessionActions` facade；单测 `npm test`。

---

## 7. Wave E — 兼容退役（需拍板）

| 项 | 说明 |
| --- | --- |
| `todo_write` | 单协议边界 deprecate → 删除 |
| `segment_end` | 改为私有 Gateway method 或内部字段 |
| 旧 4 method | 仅保留 `state.tool.execute` |
| `subagent` 文案 | 代码/测试统一 Worker |

---

## 8. 进度

| Wave | 条目 | 状态 |
| --- | --- | --- |
| A | A1 `goal_loop_decisions.go` 纯函数 | `done` |
| A | A2 `runWithGoalLoop` 改用决策 | `done` |
| A | A3 纯函数单测 + 既有 Goal 测 | `done` |
| B | B1 marshal 共享 | `done` |
| B | B2 tool name 归一化 | `done` |
| C–E | 见上文 | `todo` |

---

## 9. 门禁（每波）

**最小：**

```text
go test ./modules/agent/internal/runtime/ -count=1 -skip "Web|Duck|Tavily"
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Todo|Memory|Goal|State|Run"
```

**合并前：**

```text
go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...
```

---

## 10. 执行记录

| Date | ID | Note |
| --- | --- | --- |
| 2026-07-19 | plan | 创建本文档 + plans 任务切片；启动 Wave A |
| 2026-07-19 | A1–A3 | `goal_loop_decisions.go` + `runWithGoalLoop` 接线 + 表驱动测 |
| 2026-07-19 | B1–B2 | `runtime_tool_output.go`；`CanonicalToolName` 于 runner 入口 |
