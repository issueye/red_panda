# 冗余收敛与过度实现优化方案

| Field | Value |
| --- | --- |
| **Date** | 2026-07-16 |
| **Status** | complete for planned waves (W4-A + W5-5 landed; remaining optional polish only) |
| **Branch context** | `feat/goal-context-scratchpad` 及后续主干 |
| **Related** | [35](35-redundancy-convergence-checklist.md)、[36](36-optimization-plan.md)、[38](38-code-directness-optimization-plan.md)、[40](40-centralization-abstraction-plan.md) |
| **Basis** | 全项目冗余 / 过度实现审计（2026-07-16） |

---

## 1. 背景与目标

### 1.1 现状摘要

`red_panda` 已具备可运行的三层本地 Agent，并完成 Wave 0–5 大部分收敛（docs/35）与 v0.2 WorkerPool 切换。当前主要问题从「死代码」转为：

| 问题类 | 表现 | 成本 |
| --- | --- | --- |
| **结构冗余** | memory/todo/goal/context 四套平行全栈管道；工具名列表多处复制 | 新域成本 ×N；字段易漂移 |
| **语义重叠** | Todo checklist vs Goal actions；Memory vs Context notes vs assessment evidence | 模型选错工具；prompt 靠文案硬撑 |
| **演进残留** | `segment_end` 工具假名、`todo_write` 别名、`phase` 字段、Worker/subagent 混称 | 阅读与测试分支膨胀 |
| **过度实现** | 默认工具面 ~40；Goal 5 专家；stdio+IPC 双传输；process_pool 完整产品化 | 选择税、运维复杂度 |

### 1.2 优化目标（6 周视野）

1. **默认路径更短**：普通对话与 Goal 模式的模型可见工具显著收窄，高级能力保留但默认不可见。  
2. **平行管道减样板**：状态工具共享执行信封与校验；不再为第 5 个域复制 4 层代码。  
3. **叙事单一**：协议 / UI / 文档只保留 Worker + Goal V2 反馈控制器说法。  
4. **行为兼容优先**：公开 HTTP/WS/JSON-RPC 字段名不随意 BREAKING；收窄用 allowlist / ops-only / 默认隐藏。  
5. **可验证**：每波有针对性 `go test` + 必要前端单测；大波次跑 protocol-compat。

### 1.3 非目标

- 合并 Memory/Todo/Goal/Notes 数据表（生命周期不同，见 docs/35 X1）  
- 删除事件投影或权限流（审计必要，见 X3）  
- 删除 `per_run_process`（隔离价值高，见 X2）  
- 一次性重写 Desktop 为 TypeScript  
- 本方案周期内做 MCP 大工程或 CLI 产品化  

### 1.4 与既有文档分工

| 文档 | 本方案关系 |
| --- | --- |
| docs/35 | 历史收敛清单；未完成项 O2b/O5b/S1–S3 由本方案吸收推进 |
| docs/36 | 系统四轨道；**正确性轨道 A 仍优先于任何大行为变更** |
| docs/38 | 协调器可读性；与本方案 Wave 结构项互补，不重复开工 |
| docs/40 | Provider 中立 / RunStateStore；已完成部分不重做 |
| **docs/41（本文）** | **冗余 + 过度实现** 的执行计划与验收 |

原则：**正确性 > 收窄默认面 > 去样板 > 大拆包**。

---

## 2. 原则

| # | 原则 | 实践 |
| --- | --- | --- |
| P1 | 小步可合并 | 每 PR 单主题；可独立测、可回滚 |
| P2 | 协议兼容默认 | 隐藏/收紧 > 删 RPC；BREAKING 单独版本说明 |
| P3 | Gateway 仍是状态权威 | 持久化与终态只在 Gateway |
| P4 | 默认收窄，高级显式打开 | allowlist / ops-only / Settings 高级区 |
| P5 | 抽象不过度 | 抽「执行信封 + 校验」，不做万能 CRUD 框架 |
| P6 | 产品决策单列 | 专家数量（5→3）等需拍板后再改行为 |

---

## 3. 优化全景

```text
Wave 0  止血与小清理          ── 本迭代立即做
Wave 1  默认工具面收窄        ── 降选择税
Wave 2  状态工具管道平台化    ── 去样板（兼容层）
Wave 3  叙事与进程矩阵收敛    ── 降认知负载
Wave 4  Goal 编制瘦身         ── 产品决策后
Wave 5  结构拆分（大文件）    ── 不改行为
```

轨道可与 docs/36 A（Goal 正确性）并行，但 **禁止** 与 Goal 状态机大改同一 PR。

---

## 4. Wave 0 — 止血与小清理

**目标**：消灭明显死分支、超时分类错误、无行为争议的冗余。  
**风险**：L  
**预估**：0.5–1 天

| ID | 项 | 改动要点 | 验收 |
| --- | --- | --- | --- |
| **W0-1** | 简化 `goalExecutor` 分支 | `goal.list` 跳过 snapshot；其余（含 `segment_end`）一律 `applyGoalToolResult` | Runtime 单测绿 |
| **W0-2** | `context.*` 超时归 Gateway 类 | `timeoutClassForStableTool` 增加 `context.` 前缀 | registry 超时测绿；context 工具 30s 界 |
| **W0-3** | Goal 模式默认隐藏 `todo.*` | 从 `goalModeDefaultAllowlist` 移除 `todo.write`/`todo.list` | policy 单测：Goal 无 todo；非 Goal 仍有 |
| **W0-4** | 文档入口 | 本文件 + docs/README 索引 | 索引可见 |

**非本波**：删 `todo_write` 别名（仍有 Gateway/前端兼容）、删 `segment_end` 协议名（属 Wave 2/3）。

---

## 5. Wave 1 — 默认工具面收窄

**目标**：模型默认少选错；能力仍可通过 debug/allowlist/HTTP 管理保留。  
**风险**：M（默认 schema 变化）  
**预估**：2–3 天

| ID | 项 | 改动要点 | 验收 |
| --- | --- | --- | --- |
| **W1-1** | 主写路径收敛（Goal 默认） | Goal allowlist 仅保留 `workspace.write_file` + `diff_file`；`edit_file`/`apply_patch` 需显式 allowlist 或 debug | Goal 默认 schema 无 edit/patch；非 Goal 不变 |
| **W1-2** | Worker 消息工具 ops-only | `worker.send`/`worker.receive` 进 `opsOnlyTools`；Goal allowlist 同步移除 | 默认不可见；debug 可开 |
| **W1-3** | 非 Goal 隐藏完整 context CRUD？ | **不做**：context 仅 Goal 模式有意义，已由 Goal allowlist 约束 | — |
| **W1-4** | skill.run 在 Goal 中的定位 | 保留；文档注明与 worker.delegate 分工（skill=配方，worker=编制角色） | 描述更新 |

门禁：

```text
go test ./modules/agent/internal/tools/ -count=1
go test ./modules/agent/internal/runtime/ -count=1 -skip "Web|Duck|Tavily"
```

---

## 6. Wave 2 — 状态工具管道平台化

**目标**：下个状态域不再复制 4 层。  
**风险**：M–H（协议可兼容）  
**预估**：1 周

| ID | 项 | 改动要点 | 验收 |
| --- | --- | --- | --- |
| **W2-1** | Runtime 泛化 Gateway 工具调用 | 在 `callGatewayResult` 之上提供 typed helper；Todo/Goal 本地 cache 仍显式 | 行为不变 |
| **W2-2** | Gateway `StateTool` 校验+结果包装**共享** | 扩展 `runtime_tool_meta`；统一 output envelope 辅助 | 四域测绿 |
| **W2-3** | 协议：`state.tool.execute` 适配层 | 新 method + domain 字段；旧 4 method 薄转发 1–2 版本 | 双栈兼容测；再谈删旧 |
| **W2-4** | Desktop normalize envelope | 共享 `{ok,data}` / list items 解析，减少 memory/todos/goals 复制 | 前端 unit 绿 |

对应 docs/35 **S3**。  
**不做**：反射万能 Service、合并 repository。

---

## 7. Wave 3 — 叙事与进程矩阵

**目标**：一套 Worker 说法；高级隔离退出默认产品路径。  
**风险**：M  
**预估**：3–5 天

| ID | 项 | 改动要点 | 验收 |
| --- | --- | --- | --- |
| **W3-1** | O2b：in-process planner 路径 | 标注 deprecated 或并入 `worker.delegate`；去掉双叙事测试文案 | 无两套 subagent 用户文案 |
| **W3-2** | stdio 传输 | 文档与日志标明 legacy；默认 IPC；`RED_PANDA_RUNTIME_IPC=0` 逃生舱保留 | 默认 IPC 冒烟绿 |
| **W3-3** | process_pool | 保持 API；Settings 仅高级；README 不作为推荐默认 | 默认 runtime_process |
| **W3-4** | `phase` 字段 | WorkerProfile/Note：文档标 capability 别名；新代码停写 phase 语义 | 无新 phase 依赖 |

---

## 8. Wave 4 — Goal 编制瘦身（需产品拍板）

**目标**：专家与预算与 V2「无固定 phase」一致。  
**风险**：H  
**预估**：拍板后 3–5 天

| ID | 选项 | 说明 |
| --- | --- | --- |
| **W4-A（推荐）** | 3 专家：analyst / implementer / verifier | planner 并入 analyst 或 root `goal.plan`；evaluator 并入 verifier |
| **W4-B** | 2 专家：explorer / executor | 最简；root 承担 assess |
| **W4-C** | 保持 5 | 仅改 prompt 与 cap，不删 profile |

预算：继续隐藏段级高级旋钮（已有 O5a）；Runtime 字段可保留。  
对应 docs/35 **O5b**。

**阻塞**：未书面选定 W4-A/B/C 前不改 builtin specialist map。

---

## 9. Wave 5 — 结构拆分（行为不变）

**目标**：热点文件可并行修改。  
**风险**：M（纯搬迁）  
**预估**：按文件拆分多 PR

| ID | 文件/包 | 动作 |
| --- | --- | --- |
| **W5-1** | `tools/workspace.go` | read/list/grep vs write/edit/patch 分文件 |
| **W5-2** | `tools/web.go` | search provider vs fetch 分文件 |
| **W5-3** | `service/run.go` | 延续 docs/38：admission → prepare → dispatch |
| **W5-4** | `service/goal.go` + `goal_v2.go` | 可合并为 `goal/` 子包或按 command 文件切，避免「V1/V2」文件名误导 |
| **W5-5** | Runtime S1 | docs/35：loop / tools / worker / state 边界清晰 |

门禁：全量 `go test ./modules/agent/... ./modules/gateway/...` 行为零 diff。

---

## 10. 明确暂缓 / 不做

| ID | 项 | 原因 |
| --- | --- | --- |
| X1 | 合并四张状态表 | 领域生命周期不同 |
| X2 | 删 per_run_process | 隔离价值 |
| X3 | 删 run/tool/permission 投影 | 审计与恢复 |
| X4 | 本周期 MCP 大工程 | 与收敛抢焦点 |
| X5 | 全站 TypeScript | 另立项 |
| X6 | 立即删除 `todo_write` 别名 | 兼容面；Wave 2 后再标 deprecated |
| X7 | 立即删除 `segment_end` 工具名 | 内部预算路径；先私有 API 再改名 |

---

## 11. 推荐落地顺序

```text
W0-1..4  →  W1-1, W1-2  →  W2-1, W2-2  →  W2-3  →  W3-*  →  (拍板) W4  →  W5-*
         ↘  docs/36 轨道 A（Goal 正确性）可并行，不同 PR
```

| 阶段 | 产出 | 预期收益 |
| --- | --- | --- |
| Wave 0 | 死分支/超时/Goal-todo 互斥 | 可信小清理，立即可合 |
| Wave 1 | 默认工具面更短 | 模型更稳、Goal 更省 turn |
| Wave 2 | 状态管道平台化 | 维护成本下降 |
| Wave 3 | 单一叙事 | 用户/开发者心智降载 |
| Wave 4 | 专家编制对齐 V2 | 长任务编制更简单 |
| Wave 5 | 文件可演进 | 并行开发降碰撞 |

---

## 12. 进度追踪

| Wave | 条目 | 状态 |
| --- | --- | --- |
| 0 | W0-1 goalExecutor | `done` |
| 0 | W0-2 context timeout | `done` |
| 0 | W0-3 Goal hide todo | `done` |
| 0 | W0-4 本文档 + README | `done` |
| 1 | W1-1 Goal 主写路径 / W1-2 mailbox ops-only | `done`（与 Wave 0 同 PR） |
| 2 | W2-1 Runtime callStateTool | `done` |
| 2 | W2-2 Gateway DispatchStateTool + meta | `done` |
| 2 | W2-3 state.tool.execute 适配（旧 4 method 保留） | `done` |
| 2 | W2-4 Desktop envelope.js | `done` |
| 3 | W3-1 planner 死代码 + Worker 叙事 | `done` |
| 3 | W3-2 stdio legacy 日志/文档 | `done` |
| 3 | W3-3 process_pool 产品路径 | `done`（Settings 仅 worker 池；旧 subagent_backend 已忽略） |
| 3 | W3-4 phase→capability 语义 | `done` |
| 4 | W4-A 默认 3 专家 | `done`（planner/evaluator 默认禁用，可设置重开） |
| 5 | W5-1 workspace 拆分 | `done` |
| 5 | W5-2 web 拆分 | `done` |
| 5 | W5-3 goal_v2 → goal_controller | `done` |
| 5 | W5-4 run.go 拆分 | `done` |
| 5 | W5-5 Runtime 门面拆分 | `done` |

完成后将状态改为 `done`，并在 PR 描述链回 ID。

---

## 13. PR 模板

```markdown
## Checklist ID
- [ ] W?-?

## Summary
<一句话>

## Compatibility
- [ ] Wire protocol unchanged
- [ ] Default tool surface changed (describe)
- [ ] BREAKING (describe + migration)

## Test plan
- [ ] go test <packages>
- [ ] npm test (if desktop)
- [ ] protocol-compat (if wire/default surface)

## Risk
L / M / H — <why>
```

---

## 14. 本迭代执行记录

| Date | ID | Note |
| --- | --- | --- |
| 2026-07-16 | plan | 创建本文档；启动 Wave 0 |
| 2026-07-16 | W0-1 | `goalExecutor`：仅 `goal.list` 跳过 snapshot apply |
| 2026-07-16 | W0-2 | `context.*` 归 Gateway 超时类 |
| 2026-07-16 | W0-3 | Goal 默认 allowlist 移除 `todo.*` |
| 2026-07-16 | W0-4 | docs/41 + README 索引 |
| 2026-07-16 | W1-1 | Goal 默认移除 `edit_file` / `apply_patch` |
| 2026-07-16 | W1-2 | `worker.send` / `worker.receive` 进 ops-only；委托路径仍可经 allowlist 打开 |
| 2026-07-16 | W2-1 | Runtime `callStateTool`；四域统一走 `state.tool.execute` |
| 2026-07-16 | W2-2 | Gateway `DispatchStateTool` + `validateStateToolMeta`；`requireGatewayToolCompleted` |
| 2026-07-16 | W2-3 | protocol `StateToolExecute` + domain 解析/投影；旧 4 method 仍可用 |
| 2026-07-16 | W2-4 | Desktop `lib/envelope.js`；agents/goals/goalNotes/skills 复用 |
| 2026-07-16 | W3-1 | 删除未使用 `PLANNER_*` 延迟；README/Settings 统一 Worker 叙事 |
| 2026-07-16 | W3-2 | IPC 默认；stdio 启动日志标 legacy；docs/05 运输说明 |
| 2026-07-16 | W3-3 | 确认 Desktop 仅暴露 worker 池；README 不再推荐 subagent_backend |
| 2026-07-16 | W3-4 | phase 文档为 capability 标签；UI「能力标签」；normalize 接受 V2 标签 |
| 2026-07-16 | W5-1 | `workspace.go` → const/path/read/write/slash |
| 2026-07-16 | W5-2 | `web.go` → types/search/fetch/http/options |
| 2026-07-16 | W5-3 | `goal_v2.go` → `goal_controller.go`（消 V1/V2 文件名误导） |
| 2026-07-16 | W5-4 | `run.go` → run / run_start / run_goal / run_events / run_options / run_dto |
| 2026-07-16 | W5-5 | `runtime.go` → facade + handlers + lifecycle + events |
| 2026-07-16 | W4-A | 默认启用 analyst/implementer/verifier；planner/evaluator 默认关；resolve 尊重 catalog |
