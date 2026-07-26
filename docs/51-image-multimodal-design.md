# Design: Image / Multimodal Attachments

| Field | Value |
| --- | --- |
| **Title** | Image & Multimodal Chat Support |
| **Version** | v0.3.0 (design) |
| **Date** | 2026-07-26 |
| **Status** | Implemented (Slice A–D via docs/52) |
| **Related** | `docs/02-functional-design.md`, `docs/03-gateway-mvc-design.md`, `docs/04-agent-runtime-design.md`, `docs/06-desktop-gateway-integration.md`, `docs/07-desktop-tech-ui-design.md`, `docs/13-session-fork-compact-design.md`, `docs/14-memory-history-design.md`, `docs/30-todo-feature-design.md`, `docs/39-provider-runtime-decoupling-plan.md`, `docs/45-session-system-abstraction-design.md` |
| **Repo copy** | `docs/51-image-multimodal-design.md` |

---

## 1. Overview

`red_panda` 当前整条链路是**纯文本**：

| 层 | 现状 | 缺口 |
| --- | --- | --- |
| Protocol | `ReplyInput{Text}`；`ContentBlock{Type, Text}` 仅 text | 无 image / attachment 块 |
| Gateway | `messages.content_json` 只存 text blocks；workspace 读二进制时 `binary=true` 且 `content=""` | 无附件表；无图片上传/引用 API |
| Runtime / Provider | `openAICompatibleMessages` 把 `content` 当 string；无 vision parts | 无法把图送给 OpenAI / Anthropic / Responses |
| Desktop | Composer 仅 textarea；`run.start` 只发 `input.text` | 无粘贴/拖拽/选择图片；气泡无图 |

用户无法做：

- 「看这张截图，指出报错原因」
- 「对比这两张 UI 差异」
- 「读工作区里的 `docs/diagram.png` 并解释」
- 会话恢复后仍看到历史里的图片

本设计在三层架构内增加一等公民 **Image Attachment（图片附件）**：

1. **Gateway 权威**：二进制落盘 + SQLite 元数据；消息只存**引用**，不把 base64 塞进 `content_json` 长文。
2. **Protocol 扩展**：`ContentBlock` / `ReplyInput` 支持 image 引用；事件与 HTTP 可还原展示。
3. **Runtime / Provider**：在支持 vision 的 profile 上，把引用解析为 provider-native 多模态 parts。
4. **Desktop**：Composer 附件条 + 聊天气泡缩略图 + 工作区预览联动。

一句话：

> **Image = Gateway 托管的会话/工作区图片资产 + 消息级引用；Runtime 仅在需要时加载字节并映射到 Provider 多模态协议；Desktop 负责采集与展示。**

与现有概念边界：

| 概念 | 关系 |
| --- | --- |
| **Workspace file** | 工作区内已有路径；可「引用工作区图片」而无需再上传一份 |
| **Message / ContentBlock** | 消息内容的一种 block type（`image_ref`），不是独立聊天通道 |
| **Memory** | v1 **不**把图片写入 memory；最多记路径/描述文本 |
| **Todo / Schedule** | 无关；定时任务默认无图（可后置「模板附件」） |
| **MCP non-text content** | `docs/19` 已 defer；本设计可为后续 MCP image 结果提供同一 `image_ref` 管道 |
| **Tool output** | v1 工具结果仍以 text/JSON 为主；可选后置 `workspace.read_image` 返回 ref |

---

## 2. Goals & Non-Goals

### 2.1 Goals

1. 用户可在 Desktop **粘贴 / 拖拽 / 选文件** 附加图片，并与文字一起 `run.start`。
2. 会话历史可 **持久化与恢复** 图片气泡（重启 Gateway/Desktop 后仍可看）。
3. Agent 可在支持 vision 的 Provider Profile 上 **看到图片内容** 并正常 tool loop。
4. 可引用 **工作区已有图片路径**（不必先复制到附件库）。
5. 明确大小、数量、MIME、路径穿越、密钥与日志红线。
6. 与三层职责一致：Gateway 存与鉴权；Runtime 解析与 provider 映射；Desktop 交互。
7. 不破坏现有纯文本客户端：无附件时 wire 格式与今天完全兼容。

### 2.2 Non-Goals（v1 明确不做）

1. 模型 **生成/编辑图片** 的出图链路（DALL·E / image edit API）。
2. 视频、音频、PDF 全文多模态（PDF 仍走文本工具；可后置）。
3. 云对象存储 / CDN；默认仅本机磁盘。
4. OCR 独立服务（模型 vision 自行读图；可选后置 `image.ocr` 工具）。
5. 跨会话全局图库、向量以图搜图。
6. 把完整 base64 长期写入 SQLite `content_json` 或 run_events payload。
7. 强制所有 Provider 支持 vision；不支持时 **明确失败或降级策略**（见 §6.5）。
8. Desktop 内嵌完整画图编辑器。

---

## 3. Recommended product decisions

| 议题 | 推荐 | 理由 |
| --- | --- | --- |
| 存储权威 | **Gateway 磁盘 + `attachments` 表** | 与 session/message 同进程；Runtime 无 SQLite |
| 消息里存什么 | **`image_ref` 引用**（attachment_id 或 workspace path） | 避免 DB/WS 膨胀；可 GC |
| 上传入口 | **HTTP multipart 预上传**，再 `run.start` 带 ref | WS 不适合大二进制；可进度/重试 |
| 工作区图 | **path 引用**，按需读文件，可选缓存为 attachment | 不复制即可用 |
| Provider 载荷 | Runtime 组装时 **读字节 → data URL 或 raw base64 parts** | 各厂商格式不一；集中在 provider 适配层 |
| 历史回放 | Desktop 用 `GET /attachments/:id` 或 workspace file API | 不把图塞进 hydrate JSON 大包 |
| 不支持 vision 的模型 | **硬失败**（默认）+ 可选「仅存图不送模」设置 | 避免静默丢图导致错误回答 |
| 单条消息限制 | 默认 **最多 6 张**；单张 **≤ 8 MiB**；会话附件总配额可配 | 防内存与 API 账单爆炸 |
| 允许 MIME | `image/png`, `image/jpeg`, `image/webp`, `image/gif`（静态帧） | 覆盖常见截图；SVG 默认拒绝（XSS/解析风险） |
| 日志 | `log_llm_requests` **只记 ref/尺寸/hash，不写 base64** | 防密钥与磁盘污染 |
| 权限 | 用户上传默认可信；**工作区路径**走既有 path 校验；写工具仍走 permission | 与 workspace 一致 |

---

## 4. Architecture

### 4.1 Layering

```text
┌────────────────────────────────────────────────────────────────┐
│ Desktop                                                         │
│  Composer: paste/drag/file → upload → attachment chips          │
│  Bubble: <img> via authenticated attachment URL                 │
│  run.start: input.text + input.attachments[]                    │
└───────────────────────────────┬────────────────────────────────┘
                                │ HTTP multipart + HTTP JSON + WS
┌───────────────────────────────▼────────────────────────────────┐
│ Gateway                                                         │
│  AttachmentService: store / GC / serve / resolve workspace path  │
│  Session messages: ContentBlock image_ref only                  │
│  RunService: pass refs in ReplyInput to Runtime                 │
│  Disk: <data_dir>/attachments/<session_id>/<id>.<ext>           │
└───────────────────────────────┬────────────────────────────────┘
                                │ IPC JSON-RPC (no raw bulk on every event)
┌───────────────────────────────▼────────────────────────────────┐
│ Agent Runtime                                                   │
│  Resolve refs → bytes (Gateway internal RPC or pre-inlined)     │
│  PromptComposer: multimodal user message                        │
│  Provider adapters: OpenAI / Anthropic / Responses image parts  │
└────────────────────────────────────────────────────────────────┘
```

### 4.2 Sequence（用户发图 + 问句）

```mermaid
sequenceDiagram
  participant Desktop
  participant Gateway
  participant Disk
  participant Runtime
  participant Provider

  Desktop->>Gateway: POST /api/v1/sessions/:id/attachments (multipart)
  Gateway->>Disk: write file + hash
  Gateway->>Gateway: INSERT attachments
  Gateway-->>Desktop: AttachmentDTO {id, mime, width?, height?, bytes, thumb?}

  Desktop->>Gateway: WS run.start {input:{text, attachments:[{id}]}}
  Gateway->>Gateway: persist user message ContentBlocks [text, image_ref...]
  Gateway->>Runtime: run.execute / agent.reply (refs + optional resolve)
  Runtime->>Gateway: attachment.resolve (internal) if bytes not inlined
  Gateway->>Disk: read
  Gateway-->>Runtime: bytes + mime
  Runtime->>Provider: multimodal Complete
  Provider-->>Runtime: text / tool_calls...
  Runtime-->>Desktop: run.event (text only; no image blobs)
```

### 4.3 Ownership decision

| Option | Summary | Verdict |
| --- | --- | --- |
| A. Desktop 本地路径，Runtime 直接读 | 绕过 Gateway | Rejected：恢复/审计/多端/安全边界破坏 |
| B. base64 塞进 `run.start` / messages | 实现快 | Rejected：WS/SQLite/事件体积爆炸；日志危险 |
| C. **Gateway 附件库 + ref（chosen）** | 预上传 + 引用 | 与 memory/todo「Gateway 权威」一致；可 GC |
| D. 仅工作区路径，禁止粘贴上传 | 简单 | Rejected：截图工作流必须先存盘，体验差 |

**Runtime 取字节两种子策略（实现选一，推荐 A）：**

| 子策略 | 做法 | 取舍 |
| --- | --- | --- |
| **A. 内联一次（推荐 v1）** | Gateway 在 `run.execute` 时把本轮 user 图 **base64 放进 ReplyInput 的 ephemeral 字段**（不写 messages 表） | 少一次 Runtime→Gateway 回调；payload 仅本次 run |
| B. 内部 RPC `attachment.get` | Runtime 按需拉取 | 更干净；需扩展 `onRequest` 与超时 |

v1 推荐 **A**：与现有「Gateway 组装 ReplySession/MemoryContext 再下发」一致；限制单次总 inline ≤ 24 MiB。

---

## 5. Data model

### 5.1 SQLite：`attachments`

```text
attachments
  id              TEXT PK          -- att_<ulid>
  session_id      TEXT INDEX       -- 所属会话；空表示 workspace-cache 可选
  workspace_root  TEXT INDEX       -- 规范化根路径
  kind            TEXT             -- upload | workspace_cache
  source_path     TEXT             -- workspace 相对路径（kind=workspace_cache 时）
  storage_path    TEXT             -- Gateway 数据目录相对路径
  mime            TEXT
  byte_size       INTEGER
  sha256          TEXT INDEX       -- 去重可选
  width           INTEGER NULL
  height          INTEGER NULL
  original_name   TEXT
  created_by      TEXT             -- user | system | tool
  created_at      DATETIME
  last_ref_at     DATETIME         -- GC 辅助
  deleted_at      DATETIME NULL
```

**不**把图片 blob 放进 SQLite BLOB 列（no-cgo SQLite 场景下磁盘文件更清晰）。

### 5.2 消息 `ContentBlock` 扩展（protocol）

现有：

```go
type ContentBlock struct {
    Type string `json:"type"`
    Text string `json:"text,omitempty"`
}
```

扩展为（向后兼容：未知字段忽略；旧客户端只渲染 text）：

```go
type ContentBlock struct {
    Type string `json:"type"` // "text" | "image_ref"
    Text string `json:"text,omitempty"`

    // image_ref fields (when Type == "image_ref")
    AttachmentID string `json:"attachment_id,omitempty"`
    // Workspace-relative path alternative when image lives in workspace only
    Path string `json:"path,omitempty"`
    MIME string `json:"mime,omitempty"`
    // Display hints (optional; Desktop may ignore)
    Alt       string `json:"alt,omitempty"`
    Width     int    `json:"width,omitempty"`
    Height    int    `json:"height,omitempty"`
    ByteSize  int64  `json:"byte_size,omitempty"`
}
```

**约束：**

- `image_ref` 必须且只能有一个定位键：`attachment_id` **或** `path`（path 相对当前 session workspace）。
- 消息持久化 **禁止** `data` / `base64` 字段。
- 一个 user message 可为：`[{type:text,...}, {type:image_ref,...}, ...]`。

### 5.3 `ReplyInput` 扩展

```go
type ReplyInput struct {
    Text        string             `json:"text"`
    Attachments []InputAttachment  `json:"attachments,omitempty"`
}

type InputAttachment struct {
    AttachmentID string `json:"attachment_id,omitempty"`
    Path         string `json:"path,omitempty"`
    // Ephemeral inline for Runtime only (Gateway fills; never persist on message row)
    MIME     string `json:"mime,omitempty"`
    DataB64  string `json:"data_b64,omitempty"` // only on Gateway→Runtime wire
    ByteSize int64  `json:"byte_size,omitempty"`
}
```

**边界：**

- Desktop → Gateway `run.start`：**只允许** `attachment_id` / `path`，禁止 `data_b64`。
- Gateway → Runtime：可带 `data_b64`（策略 A）或仅 id（策略 B）。

### 5.4 Run / 事件

- `run_records.input`：存 **text + attachment ids/paths 的摘要 JSON**，不存 base64。
- `run.event` / `message_delta`：仍只流式 **文本**；不传图像素。
- 可选新事件 `attachment_referenced`（低优先级）：便于 Activity 展示「本 run 引用了 N 张图」；v1 可省略，靠 message content 即可。

### 5.5 磁盘布局

```text
<data_dir>/
  red_panda.db
  attachments/
    <session_id>/
      att_01H....png
      att_01H....jpg
    _workspace_cache/          # optional dedupe by sha256
      <sha256_prefix>/...
```

`data_dir` 与现有 Gateway DB 目录对齐（当前根目录 / `bin` 旁库文件策略保持一致，实现时抽 `paths.DataDir()`）。

---

## 6. API & Protocol

### 6.1 HTTP

```text
POST   /api/v1/sessions/:id/attachments
       Content-Type: multipart/form-data
       fields: file (required), alt (optional)
       → 201 AttachmentDTO

GET    /api/v1/sessions/:id/attachments
       → list metadata (no bytes)

GET    /api/v1/attachments/:id
       → binary stream (Content-Type: mime)
       headers: Cache-Control, ETag(sha256)

GET    /api/v1/attachments/:id/meta
       → AttachmentDTO

DELETE /api/v1/attachments/:id
       → soft delete + schedule file unlink if unreferenced

# Optional v1.1
POST   /api/v1/sessions/:id/attachments/from-workspace
       body: { "path": "docs/ui.png" }
       → creates workspace_cache attachment or returns existing by sha256
```

**AttachmentDTO（响应，无 base64）：**

```json
{
  "id": "att_01J...",
  "session_id": "sess_...",
  "kind": "upload",
  "mime": "image/png",
  "byte_size": 182233,
  "width": 1280,
  "height": 720,
  "original_name": "截图.png",
  "sha256": "...",
  "created_at": "2026-07-26T06:00:00Z",
  "url": "/api/v1/attachments/att_01J..."
}
```

### 6.2 WebSocket `run.start`

```json
{
  "method": "run.start",
  "params": {
    "session_id": "sess_...",
    "input": {
      "text": "这张图里的报错是什么原因？",
      "attachments": [
        { "attachment_id": "att_01J..." },
        { "path": "assets/logo.png" }
      ]
    },
    "options": { "...": "existing fields" },
    "subscribe": true
  }
}
```

校验：

1. `attachment_id` 必须属于该 `session_id` 且未删除。
2. `path` 必须通过 workspace 路径解析（防 `../`），且 MIME 嗅探为允许的 image 类型。
3. 数量 / 字节上限。
4. Provider profile **声明 vision**（见 §6.5）；否则返回明确错误码。

### 6.3 Session history / hydrate

`GET /api/v1/sessions/:id/messages` 与 bootstrap hydrate 返回的 message content 含 `image_ref` blocks；Desktop 用 `url` 或拼 `GET /attachments/:id` 加载。

**Hydrate 包不得内嵌 base64。**

### 6.4 Internal Runtime（若选策略 B）

```text
attachment.resolve
  params: { attachment_id | path, session_id, workspace_root, max_bytes }
  result: { mime, byte_size, data_b64 } | error
```

走现有 Gateway `onRequest` 通道；超时与 `max_bytes` 强制。

### 6.5 Provider vision 能力

在 **Provider Profile** 增加：

```text
supports_vision: bool   // default false for echo; true when user enables for known VL models
```

或推导表（实现可维护）：

| provider | model 模式 | supports_vision |
| --- | --- | --- |
| echo | * | false（可后置假图摘要） |
| openai_compatible | 名称含 `vision` / `gpt-4o` / `...` 或用户勾选 | 用户勾选优先 |
| anthropic | claude-3+ | true（可默认） |
| openai_responses | 同上 | 用户勾选 |

**行为：**

| 情况 | 行为 |
| --- | --- |
| 有附件且 `supports_vision=false` | `run.start` **拒绝**（HTTP/WS error：`vision_not_supported`） |
| 有附件且 true | 正常多模态 |
| 无附件 | 与今日一致 |
| 设置 `attachments_display_only=true`（可选高级） | 只持久化展示，**不**送给模型（默认关） |

---

## 7. Runtime & Provider mapping

### 7.1 Prompt / message assembly

`PromptComposer` / `openAICompatibleMessages` 从「`content: string`」升级为「`content: string | []Part`」：

**OpenAI-compatible / many gateways：**

```json
{
  "role": "user",
  "content": [
    { "type": "text", "text": "这张图里的报错是什么原因？" },
    {
      "type": "image_url",
      "image_url": { "url": "data:image/png;base64,..." }
    }
  ]
}
```

**Anthropic：**

```json
{
  "role": "user",
  "content": [
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/png",
        "data": "..."
      }
    },
    { "type": "text", "text": "..." }
  ]
}
```

**OpenAI Responses：** 按 Responses 输入 item 规范映射（实现时对照当前 `openai_responses.go` 结构扩展，不在此锁死字段名）。

### 7.2 Conversation history

Gateway 注入的 `ReplySession.Conversation` 中历史 `image_ref`：

| 策略 | 说明 | v1 |
| --- | --- | --- |
| **Rehydrate recent images** | 最近 K 条 user 消息中的图重新 inline | **推荐**：K=2 或总图≤4 |
| Drop old images keep alt | 更早消息只留 `[image: name]` 文本占位 | 配合 token budget |
| Always drop history images | 只当前 turn 有图 | 简单但多轮指代差 |

与 `docs/45` / ContextPacker token budget 对齐：图片按 **估算 token** 计费（常量公式，如 `tokens ≈ width*height/750` 或固定 per-image 1200），超预算时 **优先丢最旧 image_ref 的像素、保留 text**。

### 7.3 Tools（v1 可选薄支持）

| 工具 | 风险 | 说明 |
| --- | --- | --- |
| `workspace.read_image` | low | 校验 path + MIME；返回 **attachment_id 或描述性 JSON**，不在 tool result 里塞整图 base64 给模型二次膨胀；若需 vision，由 Runtime 把该 ref 标入下一 user/tool 侧通道（复杂） |
| v1 更简 | — | **不做新工具**：用户/模型通过「用户附件 + 工作区 path 引用」足够；Agent 若要用工作区图，由用户 path 附件或后续迭代 |

**v1 推荐：无新工具**，降低与 tool result 截断逻辑的纠缠。

### 7.4 Echo provider

- 有 attachments：输出文本摘要  
  `Received N image(s): att_… (image/png, 182233 bytes); …`  
  **不**假装 OCR。
- 便于 e2e 不依赖真实 VL API。

---

## 8. Desktop UX

### 8.1 Composer

- 工具栏增加 **图片** 按钮（文件选择，`accept=image/*`）。
- 支持 **Ctrl+V 粘贴** 剪贴板图片、**拖拽** 到 composer shell。
- 附件 **chip 条**（缩略图 + 文件名 + 移除）；位于 textarea 与发送按钮之间（或上方，与 Todo strip 不冲突：Todo 仍在更上）。
- 发送中禁用再添加；失败 chip 标红可重试上传。
- 无图时 UI/协议与现在一致。

### 8.2 消息气泡

- user/assistant（若未来有图）渲染 `image_ref`：缩略图，点击灯箱放大。
- 加载失败显示占位与 MIME/size。
- 纯 text 路径零回归。

### 8.3 工作区联动

- Workspace 面板对 png/jpg/webp/gif：**预览** + 「附加到对话」→ `from-workspace` 或 path 引用。
- 现有 `binary=true` 的文本编辑器继续禁用编辑。

### 8.4 Settings

- Provider Profile：`支持视觉 / Vision` 开关（写入 `supports_vision`）。
- 全局：单张大小、每消息张数（高级；可后置，先用服务端默认）。

### 8.5 无障碍与 i18n

- 中文标签：「图片」「粘贴图片」「移除」「视觉模型不支持」等。
- `alt` 默认用文件名。

---

## 9. Security & limits

| 控制 | 默认 |
| --- | --- |
| MIME allowlist | png / jpeg / webp / gif |
| 魔数校验 | 不仅信扩展名；读文件头 |
| 单文件 | 8 MiB |
| 单 `run.start` | ≤ 6 张；合计 ≤ 24 MiB inline |
| 会话附件总配额 | 512 MiB soft（超出拒绝新上传） |
| Path | 既有 `resolvePath`；禁止 symlink escape（与 skill/workspace 一致） |
| SVG | **拒绝**（脚本风险） |
| 鉴权 | 本地 Gateway 现有模型；attachment GET 校验 session 归属 |
| 日志 / LLM log | 禁止写 data_b64；可写 sha256/size/mime |
| 权限门 | 用户上传不走 tool permission；自动 run（schedule）**默认禁止带图** 除非显式配置 |

---

## 10. Lifecycle: fork / compact / delete / GC

| 操作 | 行为 |
| --- | --- |
| **Session fork** | 复制消息中的 `image_ref`；**附件文件**采用 copy-on-write 或共享 id + refcount（v1 推荐 **共享 id + session_id 多对多** 或 fork 时 **复制元数据行指向同一 storage_path**） |
| **Compact** | summary **不**嵌入像素；`open_tasks` 无关；summary 文本可写「用户提供了 N 张截图」 |
| **Hard delete session** | 删消息后 GC 无引用 attachments（与 `docs/49` 一致扩展） |
| **JSONL archive** | archive 只含 ref + storage 相对路径；导出包可选附带文件（后置） |
| **Orphan GC** | 启动时 / 定时：`deleted_at` 或无 message 引用且 `last_ref_at` 过旧 → 删文件 |

---

## 11. Compatibility & migration

1. DB migrate：新建 `attachments` 表；messages 无需改列（JSON 扩展）。
2. 旧 Desktop：忽略未知 block type，只显示 text。
3. 旧 Runtime：若收到 attachments 不识别 → Gateway 在下发前应已拒绝 vision；防御性 Runtime 报错。
4. `protocol-compat.ps1`：增加 upload + run.start with attachment_id + history 含 image_ref 的用例（可用 1×1 png）。

---

## 12. Testing strategy

| 层 | 覆盖 |
| --- | --- |
| protocol | ContentBlock 序列化；ReplyInput 校验 |
| Gateway | upload MIME/size；path escape；run.start 校验；message persist refs；GET bytes；session delete GC |
| Runtime | OpenAI/Anthropic/Responses 请求体含 image parts；echo 摘要；超预算丢旧图 |
| Desktop unit | paste 文件构造；chip 状态；run.start payload 无 base64 |
| Playwright fixture | mock upload + bubble img |
| Gateway-backed e2e | 真上传 1×1 png + echo「Received N image」；vision 关闭时错误文案 |

---

## 13. Delivery slices（建议实现顺序）

### Slice A — Protocol + Gateway storage（无 vision）

- `attachments` 表、上传/下载/删除 API
- `ContentBlock` / `ReplyInput` 扩展
- `run.start` 持久化 user message 含 `image_ref`（Runtime 仍可只读 text，**暂不** inline）
- 单测 + protocol-compat 元数据路径

### Slice B — Desktop capture & display

- Composer 粘贴/拖拽/选择、chip、发送 payload
- 气泡缩略图 + 灯箱
- 恢复会话显示历史图

### Slice C — Runtime multimodal

- Gateway inline `data_b64` 到 Runtime
- Provider adapters（OpenAI-compatible + Anthropic 至少一条）
- Profile `supports_vision` + 拒绝路径
- echo 摘要；可选真模型手工测

### Slice D — Workspace path + polish

- path 引用与「附加到对话」
- ContextPacker 图片 token / 丢弃策略
- GC、配额、设置项、文档与 README 能力表

### Slice E（后置）

- MCP image content → attachment
- `workspace.read_image` / OCR
- 出图模型
- fork refcount 精细化

---

## 14. Open questions

1. **GIF 动画**：只取首帧送模，还是整文件（体积）？建议 v1 整文件但限制 8 MiB。
2. **高 DPI 截图**：是否自动缩放长边 ≤ 2048 再送模（省 token）？建议 Gateway 存原图，Runtime 送模前可选缩放。
3. **多模态 tool 结果**：MCP 返回 image 时是否自动入库？建议等 MCP 设计修订。
4. **默认 Profile**：新建 OpenAI 兼容 profile 时 `supports_vision` 默认 false 还是 true？建议 **false**，避免小模型静默贵调用。
5. **Wails 文件选择**：是否需 Go 侧 dialog，还是 `<input type=file>` 足够？建议 v1 web input + paste。

---

## 15. Risks

| 风险 | 缓解 |
| --- | --- |
| WS/IPC 撑爆 | 禁止 Desktop 直传 base64；限制 inline 总量 |
| SQLite 膨胀 | 只存 ref |
| 账单 / 延迟 | 张数限制、缩放、历史丢图 |
| 路径穿越 | 复用 workspace resolve |
| 错误模型幻觉「已看图」 | 无 vision 时硬失败；echo 明确「未解析像素」 |
| 与 Goal 移除后单段 loop 叠加 | 图片不改变 loop 结束条件；文档说明多轮指代靠历史 rehydrate |

---

## 16. Success criteria

1. 用户粘贴截图 + 提问，历史可恢复缩略图。
2. 在 `supports_vision=true` 的真实 VL 模型上，回答能引用图中可见信息（手工验收）。
3. echo 路径自动化绿灯：upload → run → finish，无进程泄漏。
4. 纯文本回归：`protocol-compat` + 现有 `@gateway-backed` 全绿。
5. `log_llm_requests` 目录中无 base64 图数据。

---

## 17. Summary

| 项 | 选择 |
| --- | --- |
| 权威存储 | Gateway 磁盘 + `attachments` |
| 消息 | `ContentBlock.type=image_ref` |
| 上传 | HTTP multipart |
| 执行 | `run.start.input.attachments` |
| 送模 | Runtime provider parts（data URL / base64 source） |
| 不做 | 出图、视频、SVG、DB 内嵌 blob |

**下一步：** 评审本设计 → 可选拆 `docs/52-image-multimodal-development-plan.md` 按 Slice A–D 排期 → 从 Slice A 落地。
