# 桌面端技术栈与 UI 设计

## 1. 技术栈

桌面端固定使用：

- Wails v3
- JavaScript
- React
- shadcn/ui
- Tailwind CSS
- WebSocket client
- HTTP JSON client

明确约束：

1. 前端使用 JavaScript，不使用 TypeScript。
2. UI 组件优先从 shadcn/ui 生成或封装。
3. 桌面端不直接调用 Agent Runtime。
4. 桌面端所有业务数据来自中间网关。
5. Wails Go 侧只负责桌面壳层能力、本地网关生命周期、系统文件选择器、窗口和托盘等原生能力。

## 2. 桌面端职责

桌面端负责：

1. 会话列表和多会话切换。
2. Chat 工作区。
3. WebSocket 连接状态和重连。
4. 主代理、子代理、工具输出和权限请求展示。
5. 工作区文件树、文件预览和 diff 视图。
6. 设置页：provider、model、权限、代理、主题。
7. 网关和 Agent Runtime 状态展示。
8. 本地网关启动、发现和重启入口。

桌面端不负责：

1. 模型调用。
2. 工具执行。
3. 会话持久化。
4. 权限业务判断。
5. Agent Runtime 进程协议。
6. 工作区文件安全边界。

## 3. UI 参考与品牌资产

red_panda 桌面端参考 `night24/tauri-app` 的工作台式 UI：

- `TopBar.jsx`：品牌、连接状态、打开项目、重启 Runtime。
- `Sidebar.jsx`：项目树、会话列表、子代理会话入口。
- `ChatPanel.jsx`：中心聊天面板。
- `chat/ChatConversation.jsx`：消息列表、滚动到底部、权限卡片插入点。
- `chat/ChatComposer.jsx`：输入框、模型选择、权限模式、上下文用量、发送/取消。
- `ConversationActivity.jsx`：工具调用分组、运行状态行。
- `ToolCallBlock.jsx`：工具调用折叠块。
- `PermissionRequestCard.jsx`：权限确认卡片。
- `SubAgentPanel.jsx`：子代理列表与详情。
- `ContextPanel.jsx`：工作区上下文、文件树、diff。

沿用 night24 的 red panda pixel mark 作为 LOGO。设计期资产已放在：

```text
assets/red-panda-mark.svg
```

实现时复制到：

```text
modules/desktop/frontend/src/assets/red-panda-mark.svg
```

品牌使用规则：

1. 顶栏左侧显示 logo + `red_panda`。
2. 启动页、空状态、运行状态可以使用像素风 red panda 图形语言。
3. 不重新设计 LOGO，不把 LOGO 改成抽象图标。
4. LOGO 用作品牌识别，不作为按钮图标滥用。
5. `alt` 文本在装饰场景为空，在品牌场景为 `red_panda`。

## 4. 推荐目录结构

```text
modules/desktop/
  go.mod
  wails.json
  cmd/
    red-panda-desktop/
      main.go
  internal/
    app/
      app.go                  # Wails app struct and lifecycle
    gateway/
      launcher.go             # start/discover gateway
      metadata.go             # gateway metadata file
    native/
      dialog.go               # file/folder picker
      window.go
      tray.go
  frontend/
    package.json
    index.html
    src/
      main.jsx
      App.jsx
      styles.css
      assets/
        red-panda-mark.svg
      lib/
        api.js                # HTTP JSON client
        ws.js                 # WebSocket client
        eventStore.js          # run event reducer helpers
        settings.js
        ids.js
      hooks/
        useGateway.js
        useWebSocket.js
        useSessions.js
        useRuns.js
        useWorkspace.js
        usePermissions.js
        useSubagents.js
      components/
        app/
          AppShell.jsx
          BrandMark.jsx
          Sidebar.jsx
          TopBar.jsx
          StatusBar.jsx
        chat/
          ChatPanel.jsx
          ChatComposer.jsx
          MessageList.jsx
          MessageBubble.jsx
          ToolCard.jsx
          PermissionCard.jsx
          ReasoningBlock.jsx
          RunningPanda.jsx
        workspace/
          WorkspacePanel.jsx
          FileTree.jsx
          FilePreview.jsx
          DiffView.jsx
        subagents/
          SubAgentPanel.jsx
          SubAgentList.jsx
          SubAgentDetail.jsx
        settings/
          SettingsPanel.jsx
          ProviderSettings.jsx
          PermissionSettings.jsx
        ui/
          button.jsx
          dialog.jsx
          dropdown-menu.jsx
          input.jsx
          textarea.jsx
          tabs.jsx
          toast.jsx
```

## 5. 组件封装设计

组件分为三层，小而硬朗，别一上来就把 App.jsx 养成巨型章鱼。

### 5.1 App Shell 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `AppShell` | `App.jsx` + layout CSS | 三栏布局、顶部栏、状态栏、主视图区域 |
| `TopBar` | `TopBar.jsx` | 品牌 logo、网关连接状态、Runtime 状态、打开项目、重启入口 |
| `BrandMark` | `TopBar.jsx` + `red-panda-mark.svg` | 统一渲染 logo 和品牌名 |
| `Sidebar` | `Sidebar.jsx` | 项目列表、会话列表、新建会话、设置入口 |
| `StatusBar` | `statusbar.css` | WebSocket、网关、Runtime、当前 workspace、token/context 状态 |

### 5.2 Chat 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `ChatPanel` | `ChatPanel.jsx` | 组合 Conversation、Activity、Composer |
| `ConversationViewport` | `ChatConversation.jsx` | 消息滚动容器、跳转、回到底部 |
| `ConversationTimeline` | `ConversationTimeline.jsx` | 轻量时间线和定位 |
| `MessageList` | `ChatConversation.jsx` | 渲染 message/tool/permission/subagent 摘要 |
| `MessageBubble` | `MessageBubble.jsx` | 用户/助手消息气泡，Markdown 渲染入口 |
| `MarkdownMessage` | `MarkdownMessage.jsx` | Markdown、安全文本、代码块 |
| `ChatComposer` | `chat/ChatComposer.jsx` | 输入框、发送、取消、模型选择、权限模式、上下文用量 |
| `ConversationActivityRow` | `ConversationActivity.jsx` | 运行中状态、工具活动摘要 |
| `RunningPanda` | `RunningPanda.jsx` | 执行中像素风状态指示 |

`ChatPanel` props 建议：

```js
{
  session,
  messages,
  runState,
  pendingPermissions,
  providerProfiles,
  selectedProviderProfileId,
  permissionMode,
  contextUsage,
  onSend,
  onCancel,
  onResolvePermission,
  onSelectProviderProfile,
  onChangePermissionMode
}
```

### 5.3 Tool 和 Permission 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `ToolCallCard` | `ToolCallBlock.jsx` | 单个工具调用状态、参数、输出、错误 |
| `ToolGroup` | `ConversationActivity.jsx` | 多个连续工具调用折叠分组 |
| `ToolOutputStream` | `ToolCallBlock.jsx` | stdout/stderr 流式输出，支持截断 |
| `PermissionCard` | `PermissionRequestCard.jsx` | 内联权限确认 |
| `PermissionDialog` | `PermissionRequestCard.jsx` 扩展 | 高风险权限弹窗确认 |

权限组件要求：

1. 必须显示 tool name、risk、summary、arguments preview。
2. 默认按钮：Deny、Allow once。
3. 高风险工具使用 `PermissionDialog`，普通权限可以用内联 `PermissionCard`。
4. 权限决策调用 WebSocket `permission.resolve`。

### 5.4 Workspace 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `WorkspacePanel` | `ContextPanel.jsx` | 工作区右侧总面板 |
| `FileTree` | `Tree.jsx` + context styles | 文件树、路径选择 |
| `FilePreview` | `ContextPanel.jsx` | 文本预览、二进制提示 |
| `DiffView` | `diff.css` | Git diff 摘要和文件级 diff |
| `ContextUsageRing` | `ChatComposer.jsx` | context token 使用情况 |

### 5.5 SubAgent 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `SubAgentPanel` | `SubAgentPanel.jsx` | 子代理右侧面板 |
| `SubAgentList` | `subagents/SubAgentList.jsx` | 子代理列表、状态点、更新时间 |
| `SubAgentDetail` | `subagents/SubAgentDetail.jsx` | 子代理任务、结果、消息、错误 |
| `SubAgentStats` | `subagents/SubAgentStats.jsx` | queued/running/completed/failed 汇总 |
| `SubAgentTranscript` | 新增 | 子代理 message_delta/tool 输出分流展示 |

### 5.6 Settings 组件

| 组件 | 参考 night24 | 职责 |
| --- | --- | --- |
| `SettingsPanel` | `SettingsStrip.jsx` + settings components | 设置总入口 |
| `ProviderSettings` | `settings/ProviderSettings.jsx` | provider profile、model、base URL、key ref |
| `PermissionSettings` | settings/access mode | strict/permissive/allow_all/deny_all |
| `GatewaySettings` | 新增 | 网关端口、token 状态、重启 |
| `RuntimeSettings` | 新增 | runtime mode、process pool、agent restart |

## 6. shadcn/ui 使用规则

shadcn/ui 定位：

- 提供基础 UI primitives。
- 不承载业务状态。
- 不直接调用 API。
- 不直接订阅 WebSocket。

推荐使用组件：

| 场景 | shadcn/ui 组件 |
| --- | --- |
| 设置页 | `Tabs`、`Select`、`Switch`、`Input` |
| 权限确认 | `Dialog`、`Button`、`Badge` |
| 工具调用卡片 | `Card`、`Collapsible`、`ScrollArea` |
| 命令和模型选择 | `Command`、`Popover` |
| 通知 | `Toast` 或 `Sonner` |
| 会话列表 | `ScrollArea`、`DropdownMenu` |

设计约束：

1. 页面级布局不要堆嵌套卡片。
2. 工具卡、权限卡、子代理项可以使用 card。
3. 主界面要偏工作台，不做营销式 landing page。
4. 状态颜色要克制，错误、权限、高风险操作必须明显。
5. 图标优先使用 lucide-react。

## 7. WebSocket Client

`frontend/src/lib/ws.js` 负责：

1. 建立 `/api/v1/ws` 连接。
2. 完成 auth。
3. 发送 request。
4. 匹配 response。
5. 分发 event。
6. 发送 ping。
7. 断线重连。
8. 发送 `run.resume` 恢复事件。

基础接口：

```js
export function createGatewaySocket({ url, token, onEvent, onStatus }) {
  return {
    connect() {},
    close() {},
    request(method, payload) {},
    subscribeRun(runId, afterSeq = 0) {},
    resume(lastSeen) {},
    startRun(payload) {},
    cancelRun(runId, reason) {},
    resolvePermission(payload) {},
  };
}
```

桌面端本地保存：

```js
{
  lastSeen: {
    run_1: 42
  }
}
```

重连后：

```js
socket.resume(lastSeen);
```

## 8. 状态管理

MVP 推荐先用 React state + reducer，不急着引入全局状态库。

建议状态分区：

| 状态 | 来源 | 存放 |
| --- | --- | --- |
| sessions | HTTP bootstrap / session API | React state |
| currentSessionId | UI 本地 | localStorage |
| run events | WebSocket | reducer |
| lastSeen root_seq | WebSocket ack/local | localStorage |
| workspace | HTTP bootstrap/workspace API | React state |
| settings | HTTP bootstrap/settings API | React state |
| connection status | WebSocket client | React state |
| draft text | UI 本地 | localStorage |

后续如果状态复杂，再引入 Zustand。不要一开始就把所有业务状态塞进 Context，容易变成一团热粥。

## 9. 页面布局

推荐桌面布局：

```text
┌──────────────────────────────────────────────┐
│ TopBar: workspace / model / runtime status   │
├──────────────┬──────────────────┬────────────┤
│ Sidebar      │ ChatPanel        │ Context    │
│ sessions     │ messages         │ files      │
│ runs         │ tools            │ diff       │
│ settings     │ composer         │ subagents  │
├──────────────┴──────────────────┴────────────┤
│ StatusBar: websocket / gateway / token usage │
└──────────────────────────────────────────────┘
```

核心视图：

1. Chat：主代理输出和用户输入。
2. Tools：工具调用卡片嵌在 Chat 时间线里。
3. SubAgents：右侧面板展示子代理列表和详情。
4. Workspace：文件树、预览、diff。
5. Settings：provider、model、权限、代理、主题。

## 10. 权限交互

权限请求来自 WebSocket `run.event` 或 `permission.required`。

UI 表现：

1. 高风险权限使用 Dialog 或醒目的 PermissionCard。
2. 普通工具权限可以内联显示。
3. 每个权限卡显示 tool name、risk、summary、arguments preview。
4. 操作按钮：Allow once、Deny。
5. 后续可加 Always allow，但必须由网关写配置，不在桌面端本地自作主张。

调用：

```js
socket.resolvePermission({
  permission_id: "perm_1",
  decision: "approve",
  scope: "once",
  reason: "user approved"
});
```

## 11. 子代理展示

子代理事件来自 `run.event` 的 `subagent_update`。

展示规则：

1. 主 Chat 时间线显示子代理摘要，不展开所有细节。
2. 右侧 SubAgentPanel 展示子代理完整状态。
3. 子代理文本按 `stream_id + stream.seq` 拼接。
4. 子代理工具输出默认折叠。
5. 子代理失败不直接标红整个 root run，除非 root run 也失败。

## 12. 构建与脚本

前端脚本建议：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview",
    "lint": "eslint ."
  }
}
```

桌面开发：

```powershell
cd modules/desktop
wails3 dev
```

桌面构建：

```powershell
cd modules/desktop
wails3 build
```

命令名称以后按实际安装的 Wails v3 CLI 调整；设计约束是 Wails v3，不回退到 Tauri 或 Electron。

## 13. MVP 实施顺序

1. Wails v3 空壳 + React JS + shadcn/ui 初始化。
2. Gateway launcher 和 metadata 发现。
3. HTTP client：healthz、readyz、bootstrap。
4. WebSocket client：auth、ping/pong、request/response。
5. run.start、run.event、run.resume。
6. ChatPanel 和 MessageList。
7. PermissionCard。
8. ToolCard。
9. WorkspacePanel。
10. SubAgentPanel。
11. SettingsPanel。
