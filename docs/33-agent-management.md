# 智能体管理模块

| Field | Value |
| --- | --- |
| **Date** | 2026-07-12 |
| **Status** | Implemented (v1 Gateway + Desktop) |
| **Related** | `docs/32-goal-loop-development-design.md` §2.10 阶段预设子代理 |

## 目标

提供 **智能体（Agent / Specialist）定义** 的集中管理：内置 Goal 流水线五专家 + 用户自定义智能体。配置落在 Gateway SQLite，Desktop 设置页维护；Runtime 后续可按 `key` 拉取启用列表套用提示词与轮次。

## 数据

表 `agent_definitions`（model `AgentDefinition`）：

| 字段 | 说明 |
| --- | --- |
| `key` | 唯一标识，对应 `subagent.run` 的 `name`（如 `goal-analyst`） |
| `name` / `name_zh` | 展示名 |
| `kind` | `builtin` \| `custom` |
| `phase` | analyze / plan / execute / verify / evaluate / general / custom |
| `system_prompt` | 角色系统提示 |
| `tool_allowlist` / `tool_denylist` | JSON 列表 |
| `default_max_turns` | 默认工具轮次（1–48） |
| `enabled` | 是否启用 |
| `builtin` | 内置不可删除；可停用/改提示词 |

## API

| Method | Path | 说明 |
| --- | --- | --- |
| GET | `/api/v1/agents` | 列表（自动 EnsureBuiltins） |
| GET | `/api/v1/agents/enabled` | 仅启用 |
| GET | `/api/v1/agents/:id` | 详情 |
| POST | `/api/v1/agents` | 创建自定义（`goal-*` key 保留） |
| PUT | `/api/v1/agents/:id` | 更新 |
| DELETE | `/api/v1/agents/:id` | 软删（禁止 builtin） |

## 内置种子（EnsureBuiltins）

| key | 中文 | phase |
| --- | --- | --- |
| goal-analyst | 目标分析师 | analyze |
| goal-planner | 目标规划师 | plan |
| goal-implementer | 目标实施者 | execute |
| goal-verifier | 目标验证者 | verify |
| goal-evaluator | 目标终评官 | evaluate |

## Desktop

设置 → **智能体管理**：列表、启用开关、编辑提示词/轮次、新建自定义、删除自定义。

## 代码位置

- Gateway: `model.AgentDefinition`, `repository/agent_definition.go`, `service/agent_definition.go`, `controller/agent_definition.go`
- Desktop: `lib/agents.js`, `SettingsPanel` agents tab, `App.jsx` load/CRUD

## 后续

1. Runtime `subagent.run` 按 key 从 Gateway 拉定义（或 Start 时注入启用 agents 快照）。  
2. 禁用专家时 Goal 流水线跳过委派 / 回退 root 内联。  
3. 工具 allowlist 在 Runtime 真正 enforce（当前 denylist 已进种子，Runtime 对接见 Goal M4）。
