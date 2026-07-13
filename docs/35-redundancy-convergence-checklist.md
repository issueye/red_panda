# 代码冗余与功能过度实现 — 收敛清单

Updated: 2026-07-13  
Status: in progress (Wave 0 started)  
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
| **R1a** | Runtime 统一 gateway tool 转发 | 结构冗余 | L | todo | `runtime.go` 中 `executeMemory/Todo/Goal/ContextTool` 收敛为共享 helper（marshal/call/unmarshal/timeout）；保留 4 个 method 名兼容 | 行为不变；现有 memory/todo/goal/context 测全绿 | — |
| **R1b** | Protocol 参数形状去重（非 BREAKING） | 结构冗余 | L | todo | 抽取公共字段注释/嵌入 struct（若 Go JSON 兼容）或仅文档化公共约定；**不改 wire 字段名** | 旧客户端仍可解析；`protocol` 测试绿 | R1a 可并行 |
| **R1c** | Gateway `ExecuteRuntimeTool` 公共校验 | 结构冗余 | L | todo | `run_id/session_id/tool_call_id/session exists` 抽 `validateRuntimeToolMeta`；四 Service 复用 | 校验错误文案一致；service 测绿 | — |
| **R4** | Specialist brief 不再滥用 MemoryContext | 语义冗余 | M | todo | 新增 `SpecialistContext` 或 `ReplyOptions.SystemExtra`；specialist role+notes 不再写入 `MemoryContext`；Memory 仍只表示记忆 | provider 消息顺序单测；specialist 仍能看到 brief；真实 memory 注入不被覆盖 | R1a 可后 |

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
| **O1** | 默认隐藏 subagent 运维工具 | 过度暴露 | M | todo | `subagent.pool_status/resize/reset`（及可选 `pool` 管理）不进入默认 `AvailableTools`；仅 env/`debug_tools=true` 或 allowlist 显式打开 | 默认 provider tools 列表不含 pool_*；显式打开仍可用；测 policy | — |
| **O3** | Skill 管理工具移出默认主循环 | 过度暴露 | M | todo | `skill.create/update/delete` 默认不对模型暴露；保留 HTTP/Desktop CRUD + `skill.list`/`skill.run`（按产品决定 list 是否默认） | Settings 仍可管理 skills；默认 run 的 tools schema 无 create/update/delete | — |
| **O8** | Goal 模式默认工具白名单 | 选择税 | M | todo | `goals_enabled` 或 bound goal 时，Runtime/Gateway 施加收紧 allowlist：workspace + shell(视 policy) + todo + goal + context + subagent.run；排除 pool/skill 管理/memory 可选 | Goal run 工具集可测；非 Goal run 行为不变 | O1/O3 可并行 |
| **O5a** | Goal 设置面隐藏未稳预算 | 过度 | L | todo | Desktop：`max_auto_continues` 等未完全关门的项默认折叠/高级区；文案标明实验性 | 默认 UI 更短；高级仍可配 | — |
| **R6** | Slash 命令降级 | 入口冗余 | L | todo | `Parse` slash 仅 `RED_PANDA_SLASH_TOOLS=1` 或 test build tag；README Temporary Triggers 标注 debug | 默认纯文本不再触发 /read；e2e 改走 tool_calls 或开 env | — |

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
| **O2** | process_pool 默认不可见 | 过度产品化 | L | todo | Settings 默认 backend=`runtime_process`；pool 放“高级”；文档同步 | 新用户看不到池参数；API 仍接受 pool | — |
| **O4** | MCP 能力诚实化 | 配置超前 | M | todo | **短期**：Discovery UI 标注“只读发现，不可调用”；README 单一表述。**中期另项**：实现 `tools/call`（不在本清单强制） | 用户不再误以为 MCP 已可执行 | — |
| **O2b** |（可选）in-process planner 收敛 | 历史包袱 | M | todo | `/subagent` 与默认 planner 路径并入 `subagent.run` 或标注 deprecated | 无两套 subagent 叙事；测不回归 cancel | O2 |

**建议提交：**

1. `chore(desktop): hide process_pool and clarify MCP discovery is read-only`（O2+O4）

---

## Wave 4 — 语义重叠收敛（Goal/Memory/Notes，2 PR）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R5** | Checkpoint vs GoalNote 职责文档 + policy 文案 | 语义重叠 | L | todo | 工具 description / GoalContext header 写清：checkpoint=进度快照；context.*=结构化发现；memory=跨 goal 偏好 | 描述可测（字符串断言或文档单测可选）；不改存储 | R3 |
| **R5b** |（可选）弱化扁平字段写入 | 语义重叠 | M | todo | 引导模型少写 `progress_note` 重复内容；或 checkpoint 时自动抽一条 handoff note | 无双写风暴；兼容旧字段读取 | R5 |
| **O5b** | Specialist 阶段简化（产品决策） | 过度 | H | todo | **需产品拍板**：v1 保留 5 专家 vs 收成 3（analyze/execute/verify）。若收成 3：phase 枚举、builtin seed、UI 同步 | 决策记录进 docs；实现与测一致 | R2 |

**建议提交：**

1. `docs/runtime: clarify memory vs todo vs goal vs context notes`（R5）  
2. 仅在产品确认后：`refactor(goal): simplify specialist phases`（O5b）

---

## Wave 5 — 前端与文档结构（持续，可并行）

| ID | 标题 | 类型 | 风险 | 状态 | 主要改动 | 验收标准 | 依赖 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **R7a** | 拆 `useGatewayResources` | 结构 | M | todo | 从 `App.jsx` 抽出 provider/mcp/skills/agents CRUD hooks | App 行数明显下降；现有 unit/e2e 绿 | — |
| **R7b** | `reduceRunEvent` 纯函数 | 结构 | M | todo | WS 事件归约移出 App；单测覆盖 tool/permission/goal/todo | 事件行为锁定；App 变薄 | — |
| **R7c** | Settings 按 tab 拆文件 | 结构 | L | todo | `SettingsProviders.jsx` / `SettingsMcp.jsx` / … | SettingsPanel 门面 <300 行级 | — |
| **O7** | docs 归档 | 文档 | L | todo | `docs/archive/` 搬入历史 plan/release；根 docs 留 architecture + status + 本清单 | README 链接更新；现行文档 ≤10 篇入口 | — |
| **O6** | web_tools 主路径收敛 | 维护 | M | todo | 默认 Tavily（有 key）；DDG HTML 降级简化或移到 `web_tools_ddg.go` | 搜索测不依赖外网 flaky；超时用例稳定 | — |

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
| **S1** | Runtime 拆包 | 结构 | M | todo | `internal/runtime/{loop,tools,subagent,state,provider}`；对外 `New/Serve` 不变 | 全量 agent 测绿；无 API 行为变化 | R1, R4 |
| **S2** | tools.go 拆分 | 结构 | M | todo | schema / workspace / gateway_tools / parse 分文件 | 编译与测绿 | S1 可同波 |
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
| 1 | R1a, R1b, R1c, R4 | 4 | 0 | 0 | 0 |
| 2 | O1, O3, O8, O5a, R6 | 5 | 0 | 0 | 0 |
| 3 | O2, O4, O2b | 3 | 0 | 0 | 0 |
| 4 | R5, R5b, O5b | 3 | 0 | 0 | 0 |
| 5 | R7a, R7b, R7c, O7, O6 | 5 | 0 | 0 | 0 |
| 6 | S1, S2, S3 | 3 | 0 | 0 | 0 |
| **合计** | **26** | **23** | **0** | **2** | **1** |

---

## 备注

- 本清单 **不包含** 新功能开发；若 Goal CAS/budget（docs/34）与本清单冲突，**正确性（34）优先于收敛**。  
- 协议兼容脚本：`scripts/protocol-compat.ps1` 在 Wave 1–2 合并后应至少跑一轮。  
- 与上下文共享相关的已完成修复（session_id inject、specialist allowlist、read session 绑定）**不在本清单重做**，视为 baseline。
