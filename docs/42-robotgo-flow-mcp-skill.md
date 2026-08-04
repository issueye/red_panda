# robotgo-flow MCP 与 red_panda 原生技能

| Field | Value |
| --- | --- |
| **Date** | 2026-07-16 |
| **Status** | implemented (MCP server + skill catalog) |
| **Upstream** | https://gitee.com/znlgis/robotgo-flow |

---

## 1. 上游项目分析（robotgo-flow）

Windows 桌面 RPA 框架，基于 [robotgo](https://github.com/go-vgo/robotgo)：

| 组件 | 说明 |
| --- | --- |
| **Go CLI** | `run` / `record` / `capture` / `serve` / `version` |
| **WPF Tray** | .NET 10 托盘 UI，DLL 调 Go 引擎 |
| **YAML 工作流** | 声明式步骤；模板图匹配定位 UI |
| **动作集** | click/type/press/wait/open_url/drag/scroll/…（约 19 种） |
| **serve 协议** | 独立 JSON-Line（非 MCP），供 GUI 使用 |

构建要求：Go 1.26+、CGO、MinGW-w64、Windows 10/11。首次编译较慢。

与 red_panda 的关系：

- red_panda = **Agent 编排 / 权限 / 会话**
- robotgo-flow = **本机桌面执行器**
- 通过 **MCP stdio** 解耦：Runtime 不链 robotgo CGO，只启动子进程。

---

## 2. MCP 封装（本仓库）

### 2.1 代码位置

```text
modules/mcp-servers/robotgo-flow/   # MCP stdio 服务源码
  main.go / tools.go / workflow_summary.go
mcps/robotgo-flow/tools/*.json      # 工具 schema 快照（文档/对照）
bin/mcp-robotgo-flow.exe            # 构建产物
```

### 2.2 工具列表

| Tool | Risk（建议） | 行为 |
| --- | --- | --- |
| `version` | low | 解析并执行 `robotgo-flow version` |
| `list_workflows` | low | 扫描目录下 YAML |
| `inspect_workflow` | low | 结构摘要，不执行 |
| `run_workflow` | **high** | `robotgo-flow run`；可 dry_run |

Provider 可见名：`mcp__robotgo_flow__<tool>`（Runtime CanonicalName）。

### 2.3 构建

```powershell
$env:CGO_ENABLED='0'
go build -o bin\mcp-robotgo-flow.exe .\modules\mcp-servers\robotgo-flow
go test .\modules\mcp-servers\robotgo-flow\ -count=1
```

上游 CLI（需 CGO）：

```powershell
# 在 robotgo-flow 仓库
.\scripts\build.ps1
# 设置环境变量指向产物
$env:ROBOTGO_FLOW_COMMAND = 'E:\path\to\robotgo-flow.exe'
```

### 2.4 Gateway 注册示例

Settings → MCP → 新建：

| 字段 | 值 |
| --- | --- |
| name | `robotgo_flow` |
| command | `E:\code\issueye\ai_agent\red_panda\bin\mcp-robotgo-flow.exe` |
| enabled | true |
| env.ROBOTGO_FLOW_COMMAND | `...\robotgo-flow.exe` |
| timeouts.call_ms | `600000` |
| risk_overrides.run_workflow | `high`（若 UI 支持） |

---

## 3. 原生技能

路径：`.codex/skills/robotgo-flow/SKILL.md`

- 随工作区加载；`skill.list` 可见。模型需要时用 `workspace.read_file` 读取 `SKILL.md`，自行决定如何应用。
- 指导何时调用 MCP、YAML 写法、安全与排错。
- **不**替代 MCP 执行；技能负责策略，MCP 负责调用。

---

## 4. 安全边界

1. `run_workflow` 操控真实桌面 → 默认高风险 + 用户授权。
2. MCP 进程 stdout **仅** JSON-RPC 行；日志写 stderr。
3. 带 `inputs` 的流程勿在无人值守时阻塞 stdin；先 `inspect` 再传 map。
4. 不在 MCP 内嵌入 robotgo CGO，避免污染 red_panda 主构建。

---

## 5. 验收

```text
go test ./modules/mcp-servers/robotgo-flow/ -count=1
go build -o bin/mcp-robotgo-flow.exe ./modules/mcp-servers/robotgo-flow
# 手工：echo initialize | mcp-robotgo-flow.exe
# red_panda：MCP discover robotgo_flow → tools 含 version/list/inspect/run
```
