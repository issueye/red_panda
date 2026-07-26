# 图片 / 多模态附件开发计划

| Field | Value |
| --- | --- |
| **Title** | Image & Multimodal Chat — Implementation Plan |
| **Version** | v1.0 (Slice A–D delivered) |
| **Date** | 2026-07-26 |
| **Status** | Implemented — Slice A–D complete |
| **Design** | [`docs/51-image-multimodal-design.md`](51-image-multimodal-design.md) |
| **Related** | `docs/02`、`docs/03`、`docs/04`、`docs/45`、`docs/49`、`docs/50` |

---

## 0. 本计划相对设计文档 51 的关键裁剪

评审 `docs/51` 后，结合代码核对确认以下调整（已在评审回复中通过）：

1. **前端渲染方式：并行 `attachments[]` 数组**。前端目前**完全没有 ContentBlock 概念**——消息以单个 `message.text` 字符串形式从 `messageDTO` 下发（`service/session.go:543` 的 `messageText` 把 `content_json` 拼成纯文本），由 `messageContent.js:136` 解析内联 `<tool_call>` 标记。
   - **不**做「前端全量 ContentBlock 渲染」重构（风险高、回归面大）。
   - 改在 message DTO 上**新增并行字段 `attachments []AttachmentRef`**，前端独立渲染缩略图，**不动现有 text 管道**。
   - 后端**仍用 `ContentBlock{type:"image_ref"}` 存储权威**（满足设计 §5.2 的「消息持久化只存 ref」），DTO 组装时把 `image_ref` 块**拆**到并行 `attachments` 字段，便于前端零重构消费。
2. **存储路径采用既有 `DefaultSessionArchiveDir` 模式**而非新建 `paths.DataDir()`（仓库中**没有**该 helper）：
   `filepath.Dir(dsn) + "/attachments"`，并支持环境变量 `RED_PANDA_ATTACHMENTS_DIR` 覆盖。
3. **Schema 用 GORM `AutoMigrate`**（仓库无原始 SQL 迁移）。
4. **`resolvePath` 仅词法分析、未做 `os.EvalSymlinks`**——`path` 引用附件加一道 symlink 校验；upload 走受控存储不受影响。
5. **`provider.Message.Content` 由 `string` 升级**会破坏 `provider_test.go:18` 的 golden JSON，需同步更新。
6. **`provider.Request` 需要新增 `Attachments` 字段**（echo 摘要 §7.4 实际依赖它，而不仅是 `echo.go`）。
7. **`conversationMessageText`（`prompt_composer.go:87`）会静默丢非 text 块**——必须作为本特性的一部分修复，否则历史里的图永远到不了模型。
8. **`RunRecord.Input` 是 `string` 列**——附件摘要写 JSON 字符串进该列即可，避免新加列。

---

## 1. 范围与不做

### 1.1 v1 做

- HTTP multipart 上传 → Gateway 磁盘 + `attachments` 表
- `ContentBlock` / `ReplyInput` / `messageDTO` 扩展（向后兼容）
- `run.start` 持久化 user message 含 `image_ref`
- Desktop 粘贴 / 拖拽 / 选文件、附件 chip、缩略图、历史恢复
- Gateway→Runtime 内联 `data_b64`（策略 A）
- Provider 适配：OpenAI-compatible + Anthropic（至少一条）；Echo 摘要
- `supports_vision` Profile 字段 + 拒绝路径
- 工作区 `path` 引用（带 symlink 校验）
- ContextPacker 图片 token 估算 + 超预算丢最旧图
- 配额、日志红线、protocol-compat 用例

### 1.2 v1 不做（与设计 §2.2 一致）

出图 / 视频 / 音频 / PDF 全文 / SVG / OCR / 全局图库 / DB 内嵌 blob / 强制所有 Provider 支持 vision / 跨会话向量检索。

---

## 2. 触点清单（代码核对后的精确位置）

### 2.1 Protocol（`modules/protocol/methods/methods.go`）

| 位置 | 现状 | 改动 |
| --- | --- | --- |
| `ContentBlock` `:372` | `{Type, Text}` | 新增 `AttachmentID / Path / MIME / Alt / Width / Height / ByteSize`（全 omitempty） |
| `ReplyInput` `:283` | `{Text}` | 新增 `Attachments []InputAttachment` |
| 新增 `InputAttachment` | — | `AttachmentID / Path`（Desktop→Gateway）；`MIME / DataB64 / ByteSize`（Gateway→Runtime，ephemeral） |

`RunStartPayload.Input` 已是 `map[string]any`（`ws/ws.go:63`），wire 格式天然兼容，无需改协议结构。

### 2.2 Gateway

| 文件:行 | 改动 |
| --- | --- |
| `model/models.go`（新 `Attachment` 结构体） | 表 `attachments`：见设计 §5.1 |
| `model/models.go:233` `ProviderProfile` | 新增 `SupportsVision bool`（default false） |
| `infra/database/migrate.go:23` `AutoMigrate` 列表 | 追加 `&model.Attachment{}` |
| `infra/database/migrate.go`（新函数） | `migrateProviderProfileSupportsVision`：旧库 `ALTER TABLE provider_profiles ADD COLUMN supports_vision numeric NOT NULL DEFAULT 0`（参考 `migrateProviderProfileStream:138`） |
| `repository/attachment.go`（新建） | `Add / Get / GetMeta / ListBySession / SoftDelete / FindBySHA256 / TouchLastRef` |
| `service/attachment.go`（新建） | `Store / ResolvePath(symlink 校验) / ServeMeta / ServeBytes / Delete / GC`；魔数校验（`image.DecodeConfig` 同时得 MIME+宽高） |
| `service/attachment_paths.go`（新建） | `DefaultAttachmentsDir(dsn)` = `filepath.Dir(dsn)+"/attachments"`，env `RED_PANDA_ATTACHMENTS_DIR` 覆盖（参考 `session_archive.go:17`） |
| `service/workspace_files.go:147` `resolvePath` | **不**改公共 helper；附件 path 引用走专用 `AttachmentService.ResolvePath`，在现有逻辑上叠加 `os.EvalSymlinks` 防 symlink 逃逸 |
| `service/run_start.go:28` | `stringInput(payload.Input,"text")` → 扩展解析 `attachments`（`attachment_id` / `path`） |
| `service/run_start.go:82` | `Messages.Add(text)` → 改为构造 `[]ContentBlock{ {type:text}, {type:image_ref}... }` 后 `AddWithMetadata`（`repository/message.go:36`） |
| `service/run_start.go:94` `ReplyInput{Text}` | 填充 `Attachments`；策略 A：对 `supports_vision=true` 一次性 inline `DataB64`（总 ≤24 MiB），仅 ephemeral |
| `service/run_start.go:117` `applyProviderProfile` | 读 `SupportsVision`；有附件但 false → 返回 `vision_not_supported` 错误 |
| `service/session.go:490` `messageDTO` | 解码 `content_json` 后，把 `image_ref` 块**拆**到并行 `attachments []AttachmentRef` 字段；`text` 字段保持现有 join 行为不变（前端零回归） |
| `service/session_context.go` / packer | `assembleModelConversation` 已透传 `message.Content`（`session_context.go:80`），`image_ref` 块天然进入历史；不需特殊改 |
| `service/session.go:430` `estimateContextTextTokens` | 新增图片 token 估算（`≈ width*height/750`，下限 85，上限按设计 §7.2） |
| `controller/attachment.go`（新建） | gin multipart handler；`POST/GET/GET meta/DELETE /api/v1/sessions/:id/attachments` + `GET/DELETE /api/v1/attachments/:id` |
| `app/app.go:131`（路由组） | 注册 attachments 路由（在 session group 内） |
| `app/app.go` / `cmd/.../main.go:16` | 把 `AttachmentsDir` 注入 Config（默认 `filepath.Dir(dsn)+/attachments`） |
| `service/session_archive.go`（GC 集成） | 硬删 session 时级联软删 attachments，触发 orphan GC |
| `service/run.go`（schedule 门） | 自动触发的 `run.start`（`TriggerSource="schedule"`）默认拒绝带附件（设计 §9 权限门） |

### 2.3 Runtime / Provider（`modules/agent/internal/...`）

| 文件:行 | 改动 |
| --- | --- |
| `provider/provider.go:14` `Message.Content string` | 升级为 `Content any`（string 或 `[]Part`）；定义 `Part{Type, Text, ImageURL, Source}` |
| `provider/provider.go:29` `Request` | 新增 `Attachments []RequestAttachment{ID, MIME, DataB64, Path}` |
| `provider/provider_test.go:18` | 更新 golden JSON（content 仍可为 string） |
| `provider/messages.go:13` `openAICompatibleMessages` | `image_url` parts（`data:` URL） |
| `provider/openai_responses.go:93` `openAIResponsesInput` | Responses 输入 item 映射（实现时对照规范，不锁死字段名） |
| `provider/anthropic.go:109` `anthropicMessages` | `{type:image, source:{type:base64, media_type, data}}` |
| `provider/echo.go:37` | 读 `req.Attachments`；输出 `Received N image(s): att_… (image/png, 182233 bytes)`；**不**假装 OCR |
| `runtime/prompt_composer.go:65` | 用户轮：由 `input + params.Input.Attachments` 组装 parts（而非纯 `Content: input`） |
| `runtime/prompt_composer.go:87` `conversationMessageText` | **修复**：识别 `image_ref` 块并转为 `provider.Part`，不再静默丢弃 |
| `runtime/runtime_handlers.go:117,190,227` | 把 `params.Input.Attachments` 传入 `provider.Request` |

### 2.4 Desktop（`modules/desktop/frontend/src/`）

> Chip 条放置规则（CSS 核对）：`.composer-strips`（`app.css:1835`）是 `position:absolute; bottom:calc(100%-4px)`，与 TodoStrip 同带。附件 chip 必须放在 `.composer-shell`（`ChatComposer.jsx:214`，grid 内）的 textarea 与 toolbar 之间，**避开** strips 绝对定位带。

| 文件 | 改动 |
| --- | --- |
| `components/chat/ChatComposer.jsx:214` | 新增 `AttachmentChipBar`（位于 textarea 上方、shell 内） |
| 新建 `components/chat/AttachmentChipBar.jsx` | chip：缩略图 + 文件名 + 移除；失败标红重试 |
| `components/chat/ChatComposer.jsx` | 工具栏图片按钮（`accept=image/*`）、`onPaste`、`onDrop` |
| 新建 `lib/attachments.js` | `uploadAttachment(sessionId, File) → AttachmentRef`；MIME/大小预校验 |
| `hooks/useSessionRunActions.js:50` | `input: { text, attachments: [{attachment_id}|{path}] }` |
| `components/chat/ChatConversation.jsx:166` | 气泡：`<AttachmentThumbs attachments={m.attachments} />` + 现有 `<p>{m.text}</p>`（text 管道不动） |
| 新建 `components/chat/AttachmentThumbs.jsx` | 缩略图 + 灯箱（点击放大）；加载失败占位 |
| `lib/wsClient.js` / `lib/api.js` | 新增 HTTP multipart 上传 helper（带 token） |
| `components/settings/ProvidersTab.jsx:211` | 新增 `supports_vision` 复选框（默认关） |
| `components/WorkspacePanel.jsx` | 对 png/jpg/webp/gif 增加「附加到对话」动作（生成 `path` 引用） |

---

## 3. 交付阶段（Slice A → D，对应设计 §13）

每个 Slice 结束都要满足：**纯文本路径零回归**（`protocol-compat` + 现有 `@gateway-backed` 全绿）。

### Slice A — Protocol + Gateway 存储（无 vision） ✅ 已交付

**目标：附件能上传、能 GET、能进消息、历史能恢复——但不送模型。**

**交付状态（2026-07-26）：** protocol-compat.ps1 端到端通过；gateway/protocol/agent 三模块 build + 全量 test 绿。
- A1–A5 全部落地（见触点清单 §2）
- A6：`attachment_test.go`（store/reject/oversize/delete/traversal）、`run_start_attachments_test.go`（vision 拒绝/schedule 拒绝/超量/纯文本回归/带图持久化/`supports_vision` 持久化）、`protocol-compat.ps1` 新增 multipart upload + GET bytes + vision-reject + vision-accept + history image_ref 断言

**A1 数据模型与迁移**
- 新建 `model.Attachment`（设计 §5.1 字段）
- `model.ProviderProfile` 加 `SupportsVision bool`
- `migrate.go`：AutoMigrate 加 `&model.Attachment{}`；新 `migrateProviderProfileSupportsVision`（idempotent，参考 `:138`）

**A2 存储与服务**
- `repository/attachment.go`：CRUD + `FindBySHA256`（去重）+ `TouchLastRef`
- `service/attachment.go`：
  - `Store`：multipart → 临时文件 → 魔数校验（`image.DecodeConfig` 得 MIME+宽高）→ sha256 → 落 `<dir>/<session_id>/att_<ulid>.<ext>` → INSERT
  - MIME 白名单（png/jpeg/webp/gif）；SVG 拒绝；单文件 ≤8 MiB
  - `ServeBytes`：带 ETag(sha256)、Cache-Control
  - `ResolvePath`：复用 `workspace_files.go:147` 词法逻辑 + `os.EvalSymlinks` 防 symlink 逃逸
- `service/attachment_paths.go`：`DefaultAttachmentsDir(dsn)` + env 覆盖

**A3 HTTP**
- `controller/attachment.go`：`POST /sessions/:id/attachments`、`GET /sessions/:id/attachments`、`GET /attachments/:id`、`GET /attachments/:id/meta`、`DELETE /attachments/:id`
- 鉴权：attachment GET 校验 session 归属（与现 `api.Use(auth(cfg.Token))` 一致）
- `app/app.go` 注册路由；Config 注入 `AttachmentsDir`

**A4 Protocol 与消息持久化**
- `methods.ContentBlock` / `ReplyInput` 扩展（§2.1）
- `service/run_start.go:82`：构造 `[]ContentBlock{ {text}, {image_ref}... }` → `AddWithMetadata`
- `service/session.go:490` `messageDTO`：拆 `image_ref` 到并行 `attachments` 字段
- `RunRecord.Input`（string 列）存 `text + 附件摘要` JSON

**A5 校验门（admission）**
- `run.start` 校验：`attachment_id` 属该 session、未删；`path` 通过 workspace 解析；数量/字节上限
- `supports_vision=false` 且有附件 → 返回 `vision_not_supported`（HTTP/WS error code）
- schedule 触发的 run 默认拒绝带附件

**A6 测试**
- protocol：ContentBlock/ReplyInput 序列化与校验
- gateway：upload MIME/size、path escape（含 symlink）、run.start 校验、message persist refs、GET bytes、session delete 级联 GC
- protocol-compat：加 upload + run.start with attachment_id + history 含 image_ref（1×1 png fixture）

**验收：** 上传→GET→历史恢复全绿；纯文本回归全绿；`log_llm_requests` 无 base64。

---

### Slice B — Desktop 采集与展示 ✅ 已交付

**目标：用户能粘贴/拖拽/选图、发 run、看历史气泡缩略图。**

**交付状态（2026-07-26）：** `npm run build` 绿；`npm test` 167/167 通过（含 8 个新增 attachment/message 用例）。后端无改动（沿用 Slice A 契约），gateway build + test 仍绿。
- B1：`lib/attachments.js`（multipart 上传 + 客户端 MIME/大小校验 + `toRunStartAttachment`/`attachmentImageUrl`/`formatBytes`）
- B2：`AttachmentChipBar.jsx`（chip 条，置于 `.composer-shell` 内 textarea 上方，避开 Todo strip 绝对定位带）+ ChatComposer 接入（图片按钮 / `Ctrl+V` 粘贴 / 拖拽 / 隐藏 `<input type=file>`）
- B3：`useSessionRunActions` 的 `uploadAndStageFiles`（上传→staging，失败标红 chip）+ `run.start` 载荷携带 `input.attachments`（仅 ref，无 base64）+ sendTask 读取 `draftAttachments`
- B4：`AttachmentThumbs.jsx`（缩略图 + 灯箱，Esc 关闭）渲染于 user 气泡内（不动 text 管道）
- B5：`normalizeHistoryMessage` 透出 `attachments` 字段
- B6：`ProvidersTab` 新增「支持视觉」复选框（`supportsVision` ↔ `supports_vision`），draft/normalize/payload 全链路打通
- B7：`WorkspacePanel` 对 png/jpg/webp/gif 二进制文件新增「附加到对话」按钮 → 生成 `path` 引用（无需上传）
- B8：`attachments.test.js`、`sessionActionHelpers.test.js`、`providerProfiles.test.js` 用例；CSS 样式落地

**B1 上传与采集**
- `lib/attachments.js`：`uploadAttachment`（multipart + token）、客户端预校验
- `AttachmentChipBar`（`ChatComposer.jsx` shell 内，textarea 上方）
- 工具栏图片按钮 + `Ctrl+V` paste + drag-drop（drop 到 composer-shell）
- 发送中禁用追加；失败 chip 标红可重试

**B2 run.start 载荷**
- `useSessionRunActions.js:50`：`input.attachments` 仅含 `{attachment_id}` 或 `{path}`，**禁止** base64
- `runOptions.js` 不动

**B3 气泡渲染（并行 attachments 路径）**
- `ChatConversation.jsx:166`：`<AttachmentThumbs attachments={m.attachments} />`
- `AttachmentThumbs`：缩略图（`/api/v1/attachments/:id` 带 token）+ 灯箱
- 加载失败：占位 + MIME/size
- `m.text` 渲染**完全不动**（零回归保证）

**B4 历史恢复**
- hydrate / `GET /sessions/:id/messages` 返回的 `attachments` 字段直接渲染
- 验证 hydrate 包**不含** base64

**B5 工作区联动（path 引用）**
- `WorkspacePanel.jsx`：对 png/jpg/webp/gif 增加「附加到对话」→ 生成 `path` 引用（不复制文件）

**B6 测试**
- Desktop unit：paste 文件构造、chip 状态、run.start payload 无 base64
- Playwright fixture：mock upload + 气泡 img

**验收：** 粘贴截图 + 提问，历史可恢复缩略图；无图场景 UI/协议与今日一致。

---

### Slice C — Runtime 多模态 ✅ 已交付

**目标：在 `supports_vision=true` 的真实 VL 模型上，模型能看到图。**

**交付状态（2026-07-26）：** agent provider/runtime 与 gateway service 全量 test 绿。
- C1：`provider.Message.Content` → `any`（string | []Part）；新增 Part / ImageURL / ImageSource / RequestAttachment
- C2：`prompt_composer` 用户轮 + 历史 `image_ref` → provider.Part；无 inline 时保留 alt 占位，不再静默丢图
- C3：OpenAI-compatible / Anthropic / Responses 适配器映射 image parts；Echo 输出 `Received N image(s): ...`
- C4：Gateway Strategy A 在 vision gate 通过后 inline DataB64（`toInputAttachments(..., supportsVision)`）；消息行仍只存 ref
- C5：`estimateImageTokens` + `dropExcessImagePixels`（超预算丢最旧图像素，保留 text/alt）
- C6：provider/runtime 单测覆盖 image parts、echo 摘要、composer 多模态；`log_llm_requests` 脱敏 base64
- 纯文本回归：现有 golden / runtime / gateway service 全绿

**C1 Provider 类型升级**
- `provider.Message.Content` → `any`（string | `[]Part`）；新增 `Part`、`RequestAttachment`
- `provider.Request` 加 `Attachments`
- 更新 golden test（`provider_test.go:18`）

**C2 PromptComposer**
- 修复 `prompt_composer.go:87` `conversationMessageText`：识别 `image_ref` 块 → `provider.Part`
- 用户轮（`:65`）：`input + Attachments` 组装 parts

**C3 Provider 适配器**
- `messages.go:13`（OpenAI-compatible）：`image_url` + `data:` URL
- `anthropic.go:109`：`{type:image, source:{base64}}`
- `openai_responses.go:93`：Responses 输入 item（对照规范实现）
- `echo.go:37`：`Received N image(s): ...`，不假装 OCR

**C4 Runtime 接线**
- `runtime_handlers.go`：`params.Input.Attachments` → `provider.Request.Attachments`
- 策略 A：Gateway 在 `run_start.go:94` 一次性 inline `DataB64`（总 ≤24 MiB），不写 messages 表

**C5 ContextPacker 图片预算**
- `session.go:430`：图片 token 估算
- 超预算：优先丢最旧 `image_ref` 的像素、保留 text + alt 占位

**C6 测试**
- runtime：OpenAI/Anthropic/Responses 请求体含 image parts；echo 摘要；超预算丢旧图
- @gateway-backed e2e：真上传 1×1 png + echo「Received 1 image」；vision 关闭时 `vision_not_supported` 文案
- **基准**：24 MiB inline 经 IPC 的延迟；若不可接受，记录并考虑切策略 B（内部 `attachment.resolve` RPC）

**验收：** echo 自动化绿灯；真实 VL 模型手工验收能引用图中可见信息；vision 关闭时硬失败。

---

### Slice D — 工作区 path + 收尾 ✅ 已交付

**交付状态（2026-07-26）：** gateway service 全量 test 绿（含 fork 改写 image_ref、path 引用 inline、from-workspace 缓存、共享 storage_path 删除）。
- D1：run.start `path` 引用端到端（symlink 校验 + Strategy A inline）；可选 `POST .../attachments/from-workspace` 按 sha256 缓存为 attachment
- D2：硬删 session 级联 soft-delete 附件 + PurgeOrphans；会话配额 512 MiB soft（Store 已有）；启动 GC 已有
- D3：Fork 复制附件元数据行共享 storage_path，并 rewrite 消息内 attachment_id；compact summary 不嵌像素，标注 image 数量 + `[image: alt]` 占位
- D4：文档状态更新（本文件 + README + docs/10 + 设计 51）

**D1 path 引用端到端**
- 「附加到对话」→ `path` 引用；Gateway 读文件 + symlink 校验
- `from-workspace` API（可选，按 sha256 缓存为 attachment）

**D2 GC / 配额 / 设置**
- Orphan GC：启动时/定时扫 `deleted_at` 或无 message 引用且 `last_ref_at` 过旧 → 删文件
- 会话附件总配额 512 MiB soft
- `ProvidersTab` 加 `supports_vision` 开关
- 高级设置项（单张大小、每消息张数）后置或用服务端默认

**D3 Fork / Compact**
- fork：`CopyToSession`（`message.go:214`）已复制消息行，`image_ref` 块天然带过去；附件文件采用**复制元数据行指向同一 storage_path**（v1，refcount 精细化推迟）
- compact：summary 不嵌像素；summary 文本可写「用户提供了 N 张截图」

**D4 文档与能力表**
- 更新 `docs/README.md`、`docs/10-development-status.md`
- 设计文档 51 状态转 Implemented

**验收：** session 硬删后无 orphan 文件；fork 后双会话都能看图；README 能力表更新。

---

### Slice E（后置，明确不在 v1）

MCP image content → attachment；`workspace.read_image` / OCR；出图模型；fork refcount 精细化；CDN / 云对象存储。

---

## 4. 测试矩阵

| 层 | 覆盖 | Slice |
| --- | --- | --- |
| protocol | ContentBlock/ReplyInput 序列化与校验 | A |
| gateway unit | upload MIME/size、魔数、path escape（含 symlink）、run.start 校验、message persist refs、GET bytes、session delete GC、配额 | A |
| runtime unit | OpenAI/Anthropic/Responses image parts、echo 摘要、超预算丢旧图、provider golden | C |
| desktop unit | paste 构造、chip 状态、run.start payload 无 base64 | B |
| playwright | mock upload + 气泡 img | B |
| @gateway-backed e2e | 真上传 1×1 png + echo「Received N image」；vision 关闭错误文案 | C |
| protocol-compat.ps1 | upload + run.start attachment_id + history image_ref | A |
| 回归 | 现有 protocol-compat + @gateway-backed 全绿（每 Slice 门禁） | A–D |

---

## 5. 安全与红线（每 Slice 都守）

| 控制 | 默认 |
| --- | --- |
| MIME 白名单 | png/jpeg/webp/gif；SVG 拒绝 |
| 魔数校验 | `image.DecodeConfig` 读文件头，不信扩展名 |
| 单文件 | 8 MiB |
| 单 run.start | ≤6 张；inline 合计 ≤24 MiB |
| 会话配额 | 512 MiB soft |
| Path 引用 | 复用 workspace resolve + **`os.EvalSymlinks`** 防 symlink 逃逸 |
| 鉴权 | attachment GET 校验 session 归属 |
| 日志 | `log_llm_requests` 只记 sha256/size/mime，**禁** base64 |
| Schedule | 自动 run 默认禁带附件 |
| DB | **禁** BLOB；**禁** content_json 内嵌 base64 |
| Wire | Desktop→Gateway **禁** `data_b64`；仅 Gateway→Runtime 可 inline |

---

## 6. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 24 MiB base64 经 IPC 慢 | Slice C 加基准；超阈值则切策略 B（内部 RPC `attachment.resolve`） |
| `provider.Message.Content` 升级回归面广 | 仅在「有图」时发 `[]Part`，无图保持 string；同步更新 golden |
| `messageDTO` 拆 `image_ref` 引入解析 bug | text 字段保持现有 join 行为不变；并行字段独立单测 |
| symlink 逃逸 | path 引用强制 `EvalSymlinks`；upload 走受控目录 |
| 历史 image_ref 静默丢失（`conversationMessageText`） | Slice C 显式修复 + 单测覆盖 |
| 账单/延迟 | 张数限制、送模前可选缩放（Open question #2 后置）、历史丢图 |

---

## 7. 验收标准（成功）

1. 用户粘贴截图 + 提问，重启后历史仍可见缩略图。
2. `supports_vision=true` 的真实 VL 模型回答能引用图中可见信息（手工验收）。
3. echo 路径自动化绿灯：upload → run → finish，无进程泄漏、无 IPC 拥塞。
4. 纯文本零回归：`protocol-compat` + 现有 `@gateway-backed` 全绿。
5. `log_llm_requests` 目录中**无** base64 图数据。
6. session 硬删后磁盘无 orphan 附件文件。

---

## 8. 排期建议

| Slice | 相对工作量 | 依赖 |
| --- | --- | --- |
| A | 大（迁移 + 服务 + HTTP + admission + 测试） | 无 |
| B | 中（前端采集 + 渲染 + 历史） | A 的 HTTP/DTO |
| C | 中大（provider 类型升级 + 4 适配器 + composer 修复） | A 的 inline 管道 |
| D | 小中（path 引用 + GC + 配额 + fork + 文档） | A/B/C |

建议串行 A → B → C → D；B 与 C 可在 A 完成后部分并行（B 不依赖 vision）。
