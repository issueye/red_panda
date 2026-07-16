# 代码冗余与功能过度实现 — 收敛清单

Updated: 2026-07-16  
Status: in progress (Wave 0–5 core done; remaining O2b/O5b + Wave 6 absorbed by [docs/41](41-redundancy-overimpl-optimization-plan.md))  
依据：全项目评估 + Goal 上下文共享后审计（结构冗余 / 语义重叠 / 过度实现）

## 使用方式

- 按 **Wave → PR** 顺序推进；同 Wave 内可并行的已标注。
- 每个 PR 尽量小、可独立合并、带测试门禁。
- **协议破坏性变更** 单独标 `BREAKING`；默认走兼容路径（隐藏工具 / 默认关闭，而非删 RPC）。
- 完成一项后把状态改为 `done`，并在 PR 描述链回本文件条目 ID。

---

## 状态图例

| 状态 | 含义 |
| --- | --- |
| `todo` | 未开始 |
| `in_progress` | 进行中 |
| `done` | 已合并 |
| `wontfix` | 明确不做（需备注原因） |
| `blocked` | 被其他条目阻塞 |

| 风险 | 含义 |
| --- | --- |
| L | 低：局部重构/测试，用户不可见 |
| M | 中：行为或 UI 默认变化，需回归 |
| H | 高：协议/数据/隔离语义变化 |

---

## Wave 0 — 止血与真相源（优先，1～2 PR）

目标：消灭“看起来接了、实际没接”和“双配置源”。

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R3** | 统一 notes 自动注入路径 | 死代码/双轨 | L | done | 删除未接线的 `ContextService.AutoInjectNotes`；Runtime 唯一路径 `fetchGoalNotes` → `context.read`（带 session_id） | 无双轨；pinned-first 经 `context.read` 测；inject 测绿 | — |
| **R2** | Specialist 单一真相源 | 双写配置 | M | done | **方案 A**：`RunService.applyAgentDefinitions` 把 enabled 目录挂到 `ReplyOptions.AgentDefinitions`；Runtime `resolveGoalSpecialist` 覆盖 prompt/turns/phase/name_zh；tool allow/deny 仍 Runtime 内置 | 改 Gateway builtin prompt 出现在 reply options 与 resolve 结果；disabled 定义不可用 | — |
| **R2a** |（可选）禁止无效编辑误导 | 产品诚实 | L | wontfix | R2-A 已使 Settings prompt 生效，无需只读误导提示 | — | R2 |

**建议提交：**

1. `chore(goal): remove dead AutoInjectNotes dual-path`（R3）  
2. `fix(goal): single source for specialist prompts` 或 `docs/ui: clarify agent definitions are display-only`（R2）

**门禁：**

```text
go test ./modules/agent/internal/runtime/ -count=1 -skip "Web|AnnotateWeb|Duck|Tavily"
go test ./modules/gateway/internal/gateway/service/ -count=1 -run "Context|Goal|Agent"
```

---

## Wave 1 — 结构冗余收敛（平台层，2～3 PR）

目标：状态类工具不再“复制一整条管道”。

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R1a** | Runtime 统一 gateway tool 转发 | 结构冗余 | L | done | `callGatewayResult` 统一 unmarshal；`executeMemory/Todo/Goal/ContextTool` 共用；4 个 method 名不变 | memory/todo/goal/context 转发行为不变；round-trip 测绿 | — |
| **R1b** | Protocol 参数形状去重（非 BREAKING） | 结构冗余 | L | done | methods.go 注释统一 Runtime tool wire 形状；不改 method 名/字段 | 兼容保留 | R1a |
| **R1c** | Gateway `ExecuteRuntimeTool` 公共校验 | 结构冗余 | L | done | `validateRuntimeToolMeta`（`runtime_tool_meta.go`）；memory 不强制 session；todo/goal/context 强制 session | 文案一致；meta 单测 + 四域 Execute 测绿 | — |
| **R4** | Specialist brief 不再滥用 MemoryContext | 语义冗余 | M | done | 新增 `SpecialistContext`；specialist/worker/skill 角色文本写入该字段；`MemoryContext` 仅长期记忆；provider 先 specialist 后 memory | provider 顺序测；specialist/skill 隔离测绿 | R1a |

**建议提交：**

1. `refactor(runtime): share gateway-backed tool call helper`（R1a）  
2. `refactor(gateway): share runtime tool meta validation`（R1c）  
3. `refactor(runtime): separate specialist brief from memory context`（R4）

**门禁：**

```text
go test ./modules/protocol/... ./modules/agent/... ./modules/gateway/...
# 若动 provider 消息形状：加/更新 provider_test
```

---

## Wave 2 — 工具面与模式收窄（降选择税，2～3 PR）

目标：默认路径更短；高级能力保留但不出镜。

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **O1** | 默认隐藏 subagent 运维工具 | 过度暴露 | M | done | `opsOnlyTools` + `availableToolsForOptions`/`EvaluateToolPolicy`；`subagent.pool_*` 默认隐藏；`RED_PANDA_DEBUG_TOOLS` / `DebugTools` / allowlist 可开 | 默认 schema 无 pool_*；debug/allowlist 可开；policy 测绿 | — |
| **O3** | Skill 管理工具移出默认主循环 | 过度暴露 | M | done | `skill.create/update/delete` 列入 ops-only；`skill.list`/`skill.run` 仍默认暴露；HTTP/Desktop CRUD 不受影响 | 默认 schema 无 create/update/delete；debug 下仍可测 skill.create | — |
| **O8** | Goal 模式默认工具白名单 | 选择税 | M | done | `goalModeDefaultAllowlist` + `effectiveToolAllowlist`；goals_enabled/bound goal 时默认收紧；客户端 allowlist 取交集 | Goal 隐藏 memory/ops；非 Goal 不变；policy 测绿 | O1/O3 |
| **O5a** | Goal 设置面隐藏未稳预算 | 过度 | L | done | 条目标签仅显示工具轮次；段预算等放展开区「高级预算」；无独立 auto_continue 设置暴露 | 紧凑条无段计数；展开可见 advanced | — |
| **R6** | Slash 命令降级 | 入口冗余 | L | done | `Parse` 默认关闭；`RED_PANDA_SLASH_TOOLS=1` 开启 | 默认 `/list` 不解析；env 开启后测仍绿 | — |

**建议提交：**

1. `feat(runtime): gate pool and skill-management tools behind debug flags`（O1+O3）  
2. `feat(goal): default tool allowlist when goal bound`（O8）  
3. `chore(runtime): disable slash tool triggers by default`（R6）

**门禁：**

```text
go test ./modules/agent/internal/runtime/ -run "AvailableTools|Policy|Goal|Specialist|Context"
# Desktop：npm test -- --run 相关 runOptions / settings
```

---

## Wave 3 — 进程矩阵与 MCP 诚实化（1～2 PR）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **O2** | process_pool 默认不可见 | 过度产品化 | L | done | 默认 `subAgentBackend=runtime_process`；Settings 将 pool 标为高级 | 默认选项为 runtime_process；API 仍接受 process_pool | — |
| **O4** | MCP 能力诚实化 | 配置超前 | M | done | Discovery UI 标注「只读发现，对话中尚不可调用 MCP 工具」 | Settings MCP 面板可见只读说明 | — |
| **O2b** |（可选）in-process planner 收敛 | 历史包袱 | M | done | v0.2 已只用 WorkerPool；docs/41 W3-1 删除死 `PLANNER_*` 延迟变量并统一 README/UI 叙事 | 无用户可见 subagent 双叙事；旧字段被忽略 | O2 |

**建议提交：**

1. `chore(desktop): hide process_pool and clarify MCP discovery is read-only`（O2+O4）

---

## Wave 4 — 语义重叠收敛（Goal/Memory/Notes，2 PR）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R5** | Goal 状态与 GoalNote 职责文档 + policy 文案 | 语义重叠 | L | done | Goal contract/actions/evidence 记录控制状态；context notes 记录跨行动工作材料 | `TestStateStoreToolDescriptionsClarifyBoundaries` 绿 | R3 |
| **R5b** | 删除旧流水线扁平投影 | 语义重叠 | M | done | 移除 phase/checkpoint/progress/evaluation 重复字段；V2 只保留 assessment、decision、events | 生产代码无旧字段和双写路径 | R5 |
| **O5b** | Specialist 阶段简化（产品决策） | 过度 | H | done | docs/41 **W4-A**：默认 3 专家（analyst/implementer/verifier）；planner/evaluator 仍在目录但 `Enabled=false`；Runtime resolve 尊重 catalog | 新库默认 3 启用；设置可重开；测绿 | R2 |

**建议提交：**

1. `docs/runtime: clarify memory vs todo vs goal vs context notes`（R5）  
2. `refactor(goal): align specialist capabilities with Goal actions`（O5b）

---

## Wave 5 — 前端与文档结构（持续，可并行）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R7a** | 拆 `useGatewayResources` | 结构 | M | done | `hooks/useGatewayResources.js` + `lib/api.js`；Settings CRUD 迁出 App | App 减约 150 行；unit 绿 | — |
| **R7b** | `reduceRunEvent` 纯函数 | 结构 | M | done | `lib/reduceRunEvent.js` + 单测；App onEvent 只挂 hydrate + effects | tool/permission/goal/todo/finish 单测绿；App 再减约 300 行 | R7a |
| **R7c** | Settings 按 tab 拆文件 | 结构 | L | done | `settings/shared.jsx` + Providers/Agents/Skills/Mcp/Other/Logs tabs；`SettingsPanel` 作门面 | 设置面按 tab 分文件；esbuild 通过 | — |
| **O7** | docs 归档 | 文档 | L | done | 14 篇历史 plan/release → `docs/archive/`；新增 `docs/README.md` 索引；根 README 指向索引 | 现行入口清晰 | — |
| **O6** | web_tools 主路径收敛 | 维护 | M | done | 文档化主路径：auto=Tavily 优先，DDG 降级；拆文件留作后续（本项先契约锁定） | 头部注释说明 provider 优先级 | — |

**建议提交：** 每个 R7* 独立 PR，避免巨石前端 PR。

**门禁：**

```text
cd modules/desktop/frontend
npm test -- --run
npm run test:ui   # 若动 UI 结构
```

---

## Wave 6 — 大重构（仅在 Wave 0–2 完成后）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **S1** | Runtime 拆包 | 结构 | M | done | docs/41 W5-5：同 package 拆 `runtime_handlers` / `runtime_lifecycle` / `runtime_events`；loop/tools/worker 此前已分文件 | agent runtime 测绿 | R1, R4 |
| **S2** | tools.go 拆分 | 结构 | M | done | docs/41 W5-1/W5-2：`workspace_*` / `web_*` 分文件；registry 仍为定义源 | 编译与 tools 测绿 | — |
| **S3** | 统一 `state.tool.execute`（可选 BREAKING） | 协议 | H | todo | 单 RPC 多 domain；旧 4 method 保留适配层 1～2 个版本 | 双栈兼容期有测；再删旧 method | R1 完成且稳定 |

---

## 明确不做（或暂缓）清单

| ID | 项 | 原因 |
| --- | --- | --- |
| X1 | 合并 Memory/Todo/Goal/Notes 为一张表 | 生命周期与权限不同；应抽象代码而非合并领域 |
| X2 | 删除 `per_run_process` | 隔离价值高，不是冗余 |
| X3 | 删除 event 投影（run/tool/permission） | 审计与恢复需要 |
| X4 | 立刻做 MCP tools/call 大工程 | 与收敛并行会抢焦点；O4 先诚实化 |
| X5 | 全站 TypeScript 迁移 | 有价值但非本清单范围 |

---

## 推荐落地顺序（最短路径）

```text
R3 → R2 → R1a/R1c → O1+O3 → O8 → O2+O4 → R5 → R7a/R7b → S1
```

| 阶段 | 产出 | 预期收益 |
| --- | --- | --- |
| Wave 0 | 注入/配置诚实 | 行为可信，少踩坑 |
| Wave 1 | 平台样板减少 | 下个状态域不再 +N 套复制 |
| Wave 2 | 工具面变窄 | 模型更稳、Goal 更省 turn |
| Wave 3–4 | 产品叙事清晰 | 用户/开发者心智降载 |
| Wave 5–6 | 可维护性 | 并行开发成本下降 |

---

## PR 模板（复制用）

```markdown
## Checklist ID
- [ ] R? / O?

## Summary
<一句话>

## Compatibility
- [ ] Wire protocol unchanged
- [ ] BREAKING (describe + migration)

## Test plan
- [ ] go test <packages>
- [ ] npm test (if desktop)
- [ ] manual: <path>

## Risk
L / M / H — <why>
```

---

## 进度追踪（汇总）

| Wave | 条目 | todo | in_progress | done | wontfix |
| --- | --- | --- | --- | --- | --- |
| 0 | R3, R2, R2a | 0 | 0 | 2 | 1 |
| 1 | R1a, R1b, R1c, R4 | 0 | 0 | 4 | 0 |
| 2 | O1, O3, O8, O5a, R6 | 0 | 0 | 5 | 0 |
| 3 | O2, O4, O2b | 1 | 0 | 2 | 0 |
| 4 | R5, R5b, O5b | 2 | 0 | 1 | 0 |
| 5 | R7a, R7b, R7c, O7, O6 | 0 | 0 | 5 | 0 |
| 6 | S1, S2, S3 | 3 | 0 | 0 | 0 |
| **合计** | **26** | **6** | **0** | **19** | **1** |

---

## 备注

- 本清单 **不包含** 新功能开发；若与 Goal V2 规范（docs/37）冲突，以 docs/37 为准。
- 协议兼容脚本：`scripts/protocol-compat.ps1` 在 Wave 1–2 合并后应至少跑一轮。  
- 与上下文共享相关的已完成修复（session_id inject、specialist allowlist、read session 绑定）**不在本清单重做**，视为 baseline。
