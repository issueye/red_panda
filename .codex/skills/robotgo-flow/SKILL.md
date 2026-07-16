---
name: robotgo-flow
description: "Drive Windows desktop RPA via robotgo-flow YAML workflows and the robotgo_flow MCP tools (list/inspect/run). Use for GUI automation, template-based UI clicks, and scripted browser-like desktop flows on Windows."
---

# robotgo-flow 桌面 RPA 技能

本技能指导 red_panda Agent 通过 **MCP 工具 `mcp__robotgo_flow__*`** 调用 [robotgo-flow](https://gitee.com/znlgis/robotgo-flow) 执行 Windows 桌面自动化。

## 何时使用

- 用户要求**桌面 GUI 自动化**（点击、输入、拖拽、等待图像出现）
- 需要按 **YAML 工作流** 复现人工操作
- 需要基于 **模板截图** 定位按钮/输入框（非 DOM 选择器）

**不要**用于：纯代码仓库读写（用 workspace.*）、纯 HTTP（用 web.*）、无界面的 shell 命令（用 shell.exec）。

## 前置条件

1. 已构建 `robotgo-flow.exe`（上游项目 `scripts/build.ps1`，需 MinGW + CGO）。
2. 已构建并注册 MCP 服务 `mcp-robotgo-flow.exe`（本仓库 `modules/mcp-servers/robotgo-flow`）。
3. Gateway Settings → MCP 中启用 `robotgo_flow`，并设置环境变量：
   - `ROBOTGO_FLOW_COMMAND` = `robotgo-flow.exe` 绝对路径
   - 可选 `ROBOTGO_FLOW_WORKFLOWS` = 工作流目录
4. **高风险**：`run_workflow` 会真实操控键鼠；需用户授权（RiskHigh）。

## MCP 工具（canonical 名）

| 工具 | 用途 |
| --- | --- |
| `mcp__robotgo_flow__version` | 检查 CLI 是否可用 |
| `mcp__robotgo_flow__list_workflows` | 列出目录下 YAML |
| `mcp__robotgo_flow__inspect_workflow` | 解析步骤/inputs，不执行 |
| `mcp__robotgo_flow__run_workflow` | 执行工作流（可 `dry_run`） |

调用顺序建议：

1. `version` — 确认可执行文件
2. `list_workflows` — 发现流程
3. `inspect_workflow` — 看 `required_inputs` 与步骤
4. 向用户确认后 `run_workflow`（补齐 `inputs`，必要时 `dry_run=true`）

## YAML 工作流要点

```yaml
name: "示例登录"
settings:
  element_timeout: 10
  on_error: abort   # abort | skip | retry
  human:
    enabled: false
inputs:
  - name: username
    label: "用户名"
    required: true
steps:
  - name: "打开站点"
    actions:
      - open_url: "https://example.com/login"
  - name: "登录"
    actions:
      - wait: "templates/btn_login.png"
      - click: "templates/btn_login.png"
      - type:
          into: "templates/input_username.png"
          text: "$input.username"
      - press: "enter"
```

常用动作：`click` / `double_click` / `right_click` / `drag` / `type` / `press` / `combo` /
`wait` / `wait_gone` / `scroll` / `open_url` / `refresh` / `back` / `forward` /
`switch_tab` / `sleep` / `prompt` / `confirm` / `notify`。

- 图像路径相对**工作流文件目录**。
- `$input.<name>` 由 `run_workflow.inputs` 提供。
- 模板截图可用上游 CLI：`robotgo-flow capture` / `record`（需人工交互，MCP 不直接录制）。

## 安全与权限

- 默认将 `run_workflow` 视为 **高风险**；执行前说明将操作本机桌面。
- 优先 `dry_run=true` 预览命令。
- 长流程设置 `timeout_sec`（默认 600）。
- 失败时读取工具输出中的 stdout/stderr，检查模板路径与 `element_timeout`。

## 与 red_panda 其它能力协作

- 用 `workspace.write_file` 在仓库内生成/修改 YAML 工作流与模板目录结构。
- 用 `workspace.read_file` / `list` 核对 `templates/*.png` 是否存在。
- 用 `goal.plan` / `goal.observe` 把多步 RPA 纳入 Goal 控制器时，每步执行后记录证据（截图路径、退出码）。

## 故障排查

| 现象 | 处理 |
| --- | --- |
| robotgo-flow not found | 设置 `ROBOTGO_FLOW_COMMAND` |
| requires inputs | 补 `inputs` 映射后重试 |
| 模板找不到 / 超时 | 检查相对路径、分辨率、重新 capture |
| MCP discover failed | 确认 `mcp-robotgo-flow.exe` 可启动且 stdout 仅 JSON 行 |
