# red_panda 系统优化方案

| Field | Value |
| --- | --- |
| **Date** | 2026-07-13 |
| **Status** | in progress (A1–A6 + B1–B4 + C1–C5 + D2 + D6 landed on branch) |
| **Branch context** | `feat/goal-context-scratchpad` 及后续合并主干 |
| **Related** | [35-redundancy-convergence-checklist](35-redundancy-convergence-checklist.md)、[34-goal-implementation-optimization-plan](34-goal-implementation-optimization-plan.md)、收敛后评估（综合 ~7.5） |

---

## 1. 背景与目标

### 1.1 现状摘要

`red_panda` 已具备可运行的本地三层 Agent（Desktop / Gateway / Runtime），并完成：

- Goal 共享 scratchpad（`context.*`）与注入修复  
- Wave 0–5 收敛：工具门控、SpecialistContext、Settings/App 拆分、文档归档等  

当前主要短板：

| 短板 | 表现 | 不优化的后果 |
| --- | --- | --- |
| Goal 控制面正确性 | 终态/预算/segment/stream `final` 仍需按 docs/34 关门 | 长任务不可信 |
| Runtime 巨石 | `runtime.go` ~1.8k、`tools.go` ~2.1k 同包 | 修改碰撞、审查/并行开发成本高 |
| App 仍厚 | `App.jsx` ~1.7k | 会话/hydrate 演进摩擦 |
| MCP 半闭环 | 配置+discovery 全，无 `tools/call` | 产品预期管理成本 |
| 工程门禁 | 脚本多、顶层 CI 不统一 | 回归依赖人工记忆 |

### 1.2 优化目标（3 个月视野）

1. **正确性优先**：Goal 状态机与预算达到 docs/34 验收，可宣称“可控长程执行”。  
2. **结构可演进**：Runtime 按职责分包；热点文件单文件 <800 行（目标，非硬砍功能）。  
3. **默认路径简单**：普通对话与 Goal 模式工具面清晰；高级能力默认折叠。  
4. **可验证**：`go test` + 前端 unit + protocol-compat 形成一键门禁；e2e 不依赖隐式环境分叉。  
5. **文档单一真相**：现行设计/状态入口不超过 `docs/README.md` 索引范围。

### 1.3 非目标（本方案明确不做）

- 远程多租户 / 云协作  
- 向量检索记忆 / 插件市场  
- 一次 PR 重写 Desktop 为 TypeScript  
- 为收敛而合并 Memory/Todo/Goal/Notes 表（领域不同，只抽象代码）  

---

## 2. 原则

| # | 原则 | 实践 |
| --- | --- | --- |
| P1 | **正确性 > 结构 > 新功能** | Goal CAS/budget 先于 Runtime 炫技拆包；拆包不做行为变化 |
| P2 | **Gateway 仍是状态权威** | 持久化与终态只在 Gateway；Runtime 只执行有界 segment |
| P3 | **默认收窄，高级显式打开** | 延续 ops/slash/Goal 白名单策略；新工具默认进 denylist 或 debug |
| P4 | **小步可合并** | 每个 PR 可独立测、可回滚；禁止巨石“大爆炸” |
| P5 | **协议兼容优先** | wire 变更用适配层；BREAKING 单独版本说明 |
| P6 | **可观测** | 状态迁移必须有事件或 Desktop hydrate 路径 |

---

## 3. 优化全景：四条轨道

```text
轨道 A  正确性（Goal / 流式终态）     ── 最高优先
轨道 B  结构（Runtime 拆包 + 中介工具平台化）
轨道 C  前端与体验（App 瘦身 + 可观测）
轨道 D  产品闭环与工程（MCP/CLI/CI）
```

轨道可部分并行，但 **A 阻塞任何大规模 Goal 行为变更**；**B 的 tools 拆分不与 A 的 goal_loop 大改同一 PR**。

---

## 4. 轨道 A — Goal 正确性优化

> 详细不变量与验收以 [docs/34](34-goal-implementation-optimization-plan.md) 为准。此处给出落地顺序与门禁。

### 4.1 目标

使 Goal 从“能跑的原型”变为“可控、可恢复、不可被迟到工具结果破坏”的状态机。

### 4.2 工作包

| 包 ID | 内容 | 主要位置 | 风险 | 依赖 |
| --- | --- | --- | --- | --- |
| **A1** | 终态 CAS：checkpoint/complete/cancel 条件更新；未绑定 run 不可变 | `gateway/service/goal.go`、repo | M | — ✅ 已落地（含 tool cancel 连带 cancel run、bind 后 admission 失败 pause） |
| **A2** | 每会话至多一个 active（DB + service）；stale active 修复后再 bind | model/migrate、goal service | M | A1 可并行启动 ✅ 已落地（partial unique + RepairStaleActive + service 门闩） |
| **A3** | Segment ledger 幂等 `(goal_id, run_id, segment_index)`；计数与 ledger 同事务 | `GoalSegment`、goal service | M | A1 ✅ 已落地（终态冻结新 segment、绑定 run 校验、幂等 replay） |
| **A4** | 预算权威：Goal 下压 client `max_tool_turns`；wall-time 进 Runtime deadline | run.go、goal_loop、ReplyOptions | M | A3 ✅ 已落地（clamp + remaining total 收紧；wall deadline 已在 goal_loop） |
| **A5** | 流式语义：中间 segment 禁止 root `final=true`；仅一次 root Finish | runtime loop、provider 消费 | H | A4 ✅ 已落地（`deferGoalStreamFinal` + root 收尾 final；空 Final 在 unbound 仍关闭 stream；多 segment 单测锁 final/finish 各 1） |
| **A6** | 生命周期可观测：pause/cancel/budget 必有 `goal_updated` 或强制 hydrate | gateway + App hydrate | L | A1–A5 ✅ 已落地（Gateway `OnRootRunTerminal` 先于 Publish；Desktop root terminal 强制 `hydrateGoals`；工具突变仍走 `goal_updated`） |

### 4.3 验收门禁（轨道 A 总出口）

```text
go test ./modules/gateway/... ./modules/agent/...
# 聚焦：终态并发、重复 segment_end、预算下压、cancel 连带 cancel run、final 顺序
powershell -File scripts\protocol-compat.ps1   # 若覆盖 Goal HTTP/WS 面
```

Desktop：Goal 条在自然结束/取消/预算耗尽后状态与 Gateway 一致，无需手动重开会话。

### 4.4 明确不做（A 轨道内）

- auto_continue 完整产品化（保持 disabled 直至计数与权限门齐）  
- 减少 5 specialist 到 3（属产品项 O5b，单独立项）  

---

## 5. 轨道 B — Runtime / 协议结构优化

### 5.1 目标

对齐早期设计意图：Runtime 内部分包；降低 `tools.go` / `runtime.go` 认知负荷；中介工具可扩展而不再复制管道。

### 5.2 目标分包（建议）

```text
modules/agent/internal/
  runtime/           # 薄门面：New, Serve, handleLine, 组合
  loop/              # provider loop / segment / goal_loop 边界
  provider/          # OpenAI 兼容 + echo
  tools/             # ToolRunner、workspace、shell、policy 入口
    workspace.go
    shell.go
    web.go
    gateway_tools.go # memory/todo/goal/context 转发
  subagent/          # process, pool, specialist apply
  skill/             # discovery + skill.run 隔离
  mcp/               # discovery only（直至 D 轨道执行）
```

**约束**：`package` 迁移可分批；对外 `cmd/red-panda-agent` 与 JSON-RPC method 名不变。

### 5.3 工作包

| 包 ID | 内容 | 策略 | 风险 |
| --- | --- | --- | --- |
| **B1** | 抽出 `tools/workspace*.go` + `shell` + `web`（文件移动，行为零 diff） | `git mv` + 同 package 或先同目录拆文件 | L ✅ 已落地（同 package：`tools_workspace.go` / `tools_shell.go`；web 已在 `web_tools.go`） |
| **B2** | 抽出 `gateway_tools.go`（memory/todo/goal/context + callGatewayResult） | 依赖 B1 或并行 | L ✅ 已落地（`tools_gateway.go`；`callGatewayResult` 仍在 runtime 门面） |
| **B3** | 抽出 `loop`（`runProviderLoop*`、`goal_loop`、segment carry） | 接口：Runtime 注入 provider/tools | M ✅ 已落地（同 package：`loop.go` + `tool_batch.go`；顺带 `tool_execute.go` / `gateway_rpc.go` / `todo_run_state.go` / `subagent_runtime.go`；`goal_loop.go` 已有） |
| **B4** | 抽出 `subagent` + `skill` 包 | 保持 denylist/allowlist 行为 | M ✅ 已落地（同 package 文件归位；`subagent_run/capture/control/policy/manager` + 既有 skill_*；独立 `internal/subagent|skill` 包目录可后续 B4b） |
| **B5** | （可选）统一内部 `GatewayToolDispatcher`；仍保留 4 个 wire method | 非 BREAKING | L |
| **B6** | （可选后期）`state.tool.execute` 单 RPC + 旧 method 适配 1–2 版本 | BREAKING 友好期 | H |

### 5.4 热点文件目标线

| 文件/区域 | 当前约 | 目标 |
| --- | --- | --- |
| `runtime.go` | 1800 → ~603 | ≤600（门面+分发；B4 后达标） |
| `tools.go` | 2100 → ~950 | ≤400（注册表+dispatch）或拆没；B1 后主文件已含注册表+dispatch+共享 helper |
| `provider.go` | 860 | ≤900 可接受，或 loop 分离后略降 |
| `web_tools.go` | 920 | DDG 可迁 `web_ddg.go`，主路径 ≤500 |

### 5.5 验收门禁

```text
go test ./modules/agent/...
go test ./modules/protocol/...
# 行为黄金集：permission、tool_failed、subagent cancel、goal segment、context inject
```

---

## 6. 轨道 C — 前端与体验优化

### 6.1 目标

Desktop 真正成为“Gateway 投影 + 交互”，避免再成为第二状态机。

### 6.2 已完成基线

- `useGatewayResources`（Settings CRUD）  
- `reduceRunEvent`（run 事件纯函数）  
- Settings 分 tab  

### 6.3 工作包

| 包 ID | 内容 | 验收 |
| --- | --- | --- |
| **C1** | 抽出 `useSessionBootstrap`：bootstrap、session list、workspace open/hydrate | App 再减 ≥200 行；unit/e2e 绿 ✅ 已落地（`hooks/useSessionBootstrap.js` + `lib/sessionNormalize.js`；App.jsx 1779→~1559） |
| **C2** | 抽出 `useSessionActions`：create/delete/fork/compact/send/cancel | 同上 ✅ 已落地（`hooks/useSessionActions.js`；App.jsx ~1559→~1061） |
| **C3** | Goal/Todo hydrate 与 auto-continue 收拢到 `useGoalSession` | Goal 条状态仅来自 Gateway hydrate + reduce ✅ 已落地（`hooks/useGoalSession.js`；App 仅组合 + root-terminal `hydrateGoalsRef` 接线） |
| **C4** | （可选）Context notes 只读面板（Goal 展开或 Activity 过滤） | 用户可检视 scratchpad，不经模型黑盒 ✅ 已落地（`GET .../goals/:id/notes` + Goal 条展开只读列表） |
| **C5** | e2e 去 slash 分叉：主路径改 tool_calls 或测试专用 provider stub | 去掉对 `RED_PANDA_SLASH_TOOLS` 的生产级依赖 ✅ 已落地（gateway e2e 用 echo `read file` / `run shell`；`buildGatewayChildEnv` 不再强制 slash） |

### 6.4 App.jsx 目标

| 指标 | 当前约 | 目标 |
| --- | --- | --- |
| 行数 | 1688 → ~1061 → ~896 | ≤900（壳 + 组合 hooks；C3 后达标） |
| 直接 `apiJson` 调用点 | 多 | 集中在 hooks/lib |

---

## 7. 轨道 D — 产品闭环与工程化

### 7.1 MCP

| 阶段 | 内容 | 说明 |
| --- | --- | --- |
| **D1-保持** | 维持只读 discovery 文案与边界 | 已做 |
| **D2-执行 MVP** | Runtime 注册 allowlist 内 MCP tools；`tools/call` + 权限 + 超时 + 清理 | 独立里程碑，勿与 B3 混 PR ✅ 已落地（ReplyOptions.mcp_servers + Runtime prepare/call；默认 risk=high；进程每次 one-shot，D3 再做复用） |
| **D3** | 长驻进程复用 / 重启策略 | D2 稳定后 |

### 7.2 CLI

| 包 ID | 内容 |
| --- | --- |
| **D4** | `red-panda` CLI：start gateway/agent、health/ready、转发 `protocol-compat` |
| **D5** | `red-panda doctor`：检查 exe、端口、DB、agent 命令、slash/debug 环境 |

### 7.3 CI / 一键门禁

| 包 ID | 内容 | 状态 |
| --- | --- | --- |
| **D6** | 根目录 `scripts/ci-gate.ps1`（或 Task）：protocol + agent + gateway + desktop unit | ✅ 已落地（可选 `-WithProtocolCompat`） |
| **D7** | 可选 nightly：Playwright gateway-backed（含进程泄漏断言） | 待办 |

建议门禁脚本最小集：

```powershell
$env:CGO_ENABLED = '0'
go test ./modules/protocol/... ./modules/agent/... ./modules/gateway/...
cd modules/desktop/frontend; npm test -- --run
# 可选：powershell -File scripts/protocol-compat.ps1
```

### 7.4 历史路径

| 包 ID | 内容 |
| --- | --- |
| **D8 (O2b)** | 将 `/subagent` / in-process planner 标 deprecated，文档引导 `subagent.run` + goal specialists |
| **D9** | 默认文档与 Settings 不再强调 pool 运维工具（已隐藏，补齐文案一致） |

---

## 8. 推荐实施路线图

### 阶段 0（1 周内）— 冻结与门禁

- 合并当前收敛分支到主线（若尚未合并）  
- 落地 **D6** 最小 CI gate  
- 宣布：**两周内不接 MCP 执行以外的大功能**  

### 阶段 1（2–3 周）— 轨道 A 关门

顺序：**A1 → A2 → A3 → A4 → A5 → A6**  

出口：docs/34 验收条目全部勾选；Goal 相关 e2e/单测绿。

### 阶段 2（2–3 周）— 轨道 B 结构

顺序：**B1 → B2 → B3 → B4**（B5 可选穿插）  

出口：Runtime 目录符合 §5.2；agent 全量测试绿；无 intentional 行为 diff。

### 阶段 3（1–2 周）— 轨道 C

顺序：**C1 → C2 → C3**，并行 **C5**  

出口：App.jsx ≤900 行量级；gateway-backed e2e 不依赖 slash 默认关的隐患。

### 阶段 4（按需）— 轨道 D 能力

- **D2 MCP 执行** 或 **D4 CLI** 二选一作为下一版本卖点  
- D8 清理 planner 叙事  

```text
时间示意（可压缩并行）：

Week 1        门禁 D6 + 合并
Week 2–4      Goal A1–A6
Week 4–7      Runtime B1–B4（A 收尾后可与 C 并行）
Week 6–8      Frontend C1–C3、C5
Week 8+       MCP D2 或 CLI D4
```

---

## 9. PR 粒度与命名建议

| 类型 | 前缀示例 | 说明 |
| --- | --- | --- |
| Goal 正确性 | `fix(goal):` | CAS、budget、final |
| 纯移动/拆分 | `refactor(runtime):` | 无行为 diff |
| 前端结构 | `refactor(desktop):` | hooks/tabs |
| 产品边界 | `feat(mcp):` / `chore(cli):` | 新能力或工程 |
| 文档 | `docs:` | 方案/状态 |

每个 PR 描述必须包含：

- 关联包 ID（如 A3、B1）  
- 行为是否 zero-diff  
- 测试命令与结果  

---

## 10. 成功度量

| 指标 | 基线（约） | 目标（阶段 2 末） |
| --- | --- | --- |
| 综合健康度（评估口径） | 7.5 | ≥8.0 |
| Runtime 最大单文件 | ~2100 行 | ≤800 行 |
| App.jsx | ~1688 → ~896 行 | ≤900 行（C3 已达标） |
| 收敛清单 Wave 6 | 0/3 | ≥2/3（S1/S2） |
| docs/34 Goal 验收 | 部分 | 100% |
| 一键门禁 | 无 | `ci-gate` 本地/CI 可跑 |
| 默认模型可见 ops 工具 | 已隐藏 | 保持隐藏 |

质量过程指标：

- Goal 相关 P0 缺陷：连续两周为零  
- 拆包 PR 禁止夹带功能（review 检查表）  

---

## 11. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| Goal 与 Runtime 拆包互相踩踏 | 阶段 1 完成前禁止 B3 大挪 loop |
| 拆包引入行为回归 | 每步全量 `go test ./modules/agent/...`；关键路径 protocol-compat |
| 前端拆 hook 破坏多会话 | 保留 `sessionRuntimes` 为唯一投影表；reduce 单测锁事件 |
| MCP 执行扩大攻击面 | 强制 allowlist、权限、超时、进程必回收 |
| 文档再次膨胀 | 新计划进 archive 节奏；现行只维护 README 索引 + status + 本方案 |

---

## 12. 与现有文档的关系

| 文档 | 关系 |
| --- | --- |
| [34-goal-…](34-goal-implementation-optimization-plan.md) | **轨道 A 的详细不变量与验收**（本方案不重复细节） |
| [35-redundancy-…](35-redundancy-convergence-checklist.md) | Wave 0–5 已完成基线；剩余 O2b/R5b/O5b/S* 并入本方案 B/C/D |
| [10-development-status](10-development-status.md) | 阶段出口后更新“已实现/边界” |
| [docs/README](README.md) | 将本方案列入“当前真相” |

---

## 13. 立即执行清单（下一迭代，建议 5 个 PR）

1. **`docs: add optimization plan 36`**（本文） ✅  
2. **`fix(goal): CAS terminal transitions + bind checks`（A1）** ✅（含 A2）  
3. **`fix(goal): segment ledger idempotency`（A3，可与 A2 分 PR）** ✅（含 A4）  
4. **`refactor(runtime): split tools.go into workspace/shell/web files`（B1，zero-diff）** ✅（含 B2 `tools_gateway.go`）  
5. **`chore: add scripts/ci-gate.ps1`（D6）** ✅  
6. **`fix(goal): stream final + lifecycle hydrate`（A5/A6）** ✅  

轨道 B 结构主线（B1–B4）已关门（同 package 拆文件，未做独立子目录包迁移）。  
C1–C3 已抽出 bootstrap / session actions / goal session；App.jsx ~896 行（≤900）。  

C 轨道 C1–C5 已落地；**D2 MCP tools/call MVP** 已落地（Gateway 注入启用中的 MCP 配置，Runtime 发现并执行 `tools/call`）。  

下一优先：**D3** MCP 进程复用，或 **D4** CLI / **D8** planner 叙事清理。

---

## 14. 总结

本优化方案把“继续堆功能”转为三条硬约束：

1. **Goal 必须正确**（轨道 A / docs/34）  
2. **Runtime 必须可拆**（轨道 B）  
3. **默认路径必须简单、可测**（工具门控延续 + 门禁 + 前端投影化）  

在 Wave 0–5 去熵成果之上，按 **A → B/C → D** 推进，可将项目从“功能密、内核重的中期形态”推进到“可控长程执行 + 可并行演进”的稳态。

**建议决策**：确认轨道 A 为下一发布火车头；轨道 B1（tools 拆文件）作为并行低风险结构 PR。
