# red_panda

`red_panda` is a Go-based local AI Agent system inspired by the agent/runtime shape of `DeepSeek-Reasonix` and the desktop interaction style of `night24`.

The current goal is a local, restorable, auditable three-layer AI Agent framework:

1. Desktop handles user interaction.
2. Gateway owns state, persistence, protocol routing, and process management.
3. Agent Runtime owns provider calls, tool execution, permission requests, cancellation, and subagent runtime behavior.

## Architecture

1. Desktop: Wails v3 + JavaScript + React + shadcn/ui-style components. It contains chat, workspace browsing, permission cards, tool cards, subagent state, user-visible subagent cancel, run activity, and the red panda logo.
2. Gateway: Go + Gin + SQLite(no cgo) + GORM + MVC. It provides HTTP/WebSocket APIs, session persistence, workspace APIs, permission coordination, event routing, run/tool/permission projections, and Agent Runtime process management.
3. Agent Runtime: Go subprocess connected to Gateway through newline-delimited stdio JSON-RPC. It runs providers, tools, permission gates, active run state, cancellation, the in-process `planner` subagent MVP, and the `runtime_process` / `process_pool` subagent backends.

Desktop and Gateway use WebSocket for realtime interaction. SSE is not used. Gateway and Agent Runtime use stdio JSON-RPC. Root agent and subagent output share the same root run channel and are ordered by `root_seq`.

## Go Workspace Modules

- `modules/protocol`: JSON-RPC, WebSocket envelope, agent event, permission/tool DTOs, and MCP server config/CRUD DTOs.
- `modules/agent`: stdio JSON-RPC Agent Runtime with Provider abstraction, ToolRunner, permission blocking, cancellation, in-process `planner`, and `runtime_process` / `process_pool` subagent backends.
- `modules/gateway`: Gin/GORM/SQLite(no cgo) MVC Gateway with Runtime subprocess client, WebSocket run/permission channel, event replay, workspace/session/run/tool/permission APIs, and validated MCP config CRUD.
- `modules/desktop`: Wails v3 + React/Vite desktop with chat, permissions, tool cards, subagents, workspace/session restore, activity inspection, and Gateway-backed MCP config management.
- `modules/cli`: placeholder for future debugging tools.

## Design Docs

See [docs/README.md](docs/README.md) for the current index. Highlights:

- [Current Development Status](docs/10-development-status.md)
- [Convergence Checklist](docs/35-redundancy-convergence-checklist.md)
- [Functional Design](docs/02-functional-design.md)
- [Gateway MVC Design](docs/03-gateway-mvc-design.md)
- [Agent Runtime Design](docs/04-agent-runtime-design.md)
- [stdio JSON-RPC Multiplexing](docs/05-stdio-jsonrpc-multiplexing.md)
- [Desktop Gateway Integration](docs/06-desktop-gateway-integration.md)
- [Desktop Tech and UI Design](docs/07-desktop-tech-ui-design.md)
- [Goal / Todo / Memory / MCP designs](docs/README.md) (feature docs)
- Historical roadmaps and release notes: [docs/archive/](docs/archive/)

## Current Running Loop

1. Start `bin/red-panda-gateway.exe`.
2. Gateway finds `bin/red-panda-agent.exe` by default, or uses `RED_PANDA_AGENT_COMMAND`.
3. Desktop or an external client connects to `ws://127.0.0.1:17888/api/v1/ws`.
4. Client sends `run.start`; options may include `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `provider_profile_id`, `tool_allowlist`, and `tool_denylist`.
5. Gateway calls `agent.reply` over stdio JSON-RPC. The default `runtime_mode` is `single_core`; `runtime_mode=per_run_process` starts a dedicated `red-panda-agent` process for that root run. When `provider_profile_id` is present, Gateway resolves the profile and sends the provider, model, base URL, and API key to Runtime for that run.
6. Agent Runtime emits `agent.event`.
7. Gateway writes events to SQLite and broadcasts WebSocket `run.event`.
8. New clients can use `run.resume` and `root_seq` to replay events.
9. Tool calls, run status, and permission requests are projected to queryable HTTP tables.
10. Per-run Runtime clients keep events on the same WebSocket/projection stream and are shut down and removed automatically after run finish.
11. Desktop restores messages, tool cards, pending permissions, active run state, latest `root_seq`, subagent `root_run_id`/backend/status, Activity, and global pending approvals after restart or session switch.
12. Desktop automatically reconnects after WebSocket loss and resumes active runs from the highest known `root_seq`.

## Implemented Capabilities

- WebSocket methods: `run.start`, `run.subscribe`, `run.resume`, `run.cancel`, `permission.resolve`, `agent.status`, `subagents.list`, `subagent.cancel`.
- Runtime JSON-RPC methods: `core.initialize`, `core.ping`, `agent.reply`, `agent.cancel`, `agent.subagents`, `agent.subagent.cancel`, `permission.resolve`.
- Providers: default `echo`; optional OpenAI-compatible HTTP provider. Base URLs may be a host root, an existing `/v1` base, or a full `/chat/completions` endpoint.
- ToolRunner MVP: `workspace.read_file`, `workspace.list`, `workspace.grep`, `workspace.diff_file`, `workspace.write_file`, `workspace.edit_file`, `workspace.apply_patch`, `shell.exec`, `skill.create`, `skill.update`, and isolated `skill.run`.
- `workspace.edit_file`: high-risk precise string replacement with `path`, `old_text`, `new_text`, and `replace_all`. By default `old_text` must match exactly once; `replace_all=true` allows multi-location replacement. It uses the existing permission and tool policy flow.
- `workspace.diff_file`: low-risk read-only unified diff preview for one workspace file. It accepts `path` plus either full proposed `content`, or `old_text`/`new_text` with optional `replace_all` for exact replacement preview. It does not write files.
- `workspace.apply_patch`: high-risk workspace-scoped unified patch application with `patch`. It uses the existing permission and tool policy flow before writing.
- `skill.create` / `skill.update`: high-risk managed skill-definition writes under `<workspace>/.codex/skills/<name>/SKILL.md`. Create never overwrites an existing skill; update never creates a missing skill. Names, sizes, directory boundaries, and symlink escape are validated before writing.
- `skill.run`: high-risk synchronous tool that always loads the named skill in a fresh `runtime_process` subagent. The child receives no root conversation or root memory, sees the skill as isolated system context, cannot recursively run/manage skills, and is limited to read-only workspace tools. Only its final message becomes the root tool result.
- Skill context boundary: subagent messages remain persisted for Desktop/audit display but are excluded before the 200-message root conversation limit and ignored defensively by the Provider. Automatic skill discovery and `agent.skills` / `agent.skill.load` request handling are not implemented yet.
- Tool policy: `tool_policy`, `tool_allowlist`, `tool_denylist`, `permission_mode`.
- Desktop SettingsPanel: exposes runtime options for `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `tool_allowlist`, and `tool_denylist`, and can list, select, create, update, and delete Gateway provider profiles. It keeps only masked API key state in Desktop and sends the selected `provider_profile_id` as a WebSocket `run.start` option to Gateway.
- MCP configuration backend: protocol DTOs and Gateway CRUD APIs persist and validate server name, command, args, env, cwd, enabled state, timeouts, raw tool allowlists, and risk overrides. Responses mask sensitive environment values, and config validation never executes commands.
- Desktop MCP configuration management: Settings can list, create, edit, enable/disable, and delete Gateway MCP server records. Arguments use one line per argv entry, normalized timeouts are retained, and partial updates do not resend omitted masked environment fields.
- MCP execution boundary: no MCP server process is started by the current running loop; MCP initialize, `tools/list`, Runtime tool registration, permission integration, `tools/call`, cancellation, and restart handling are not implemented.
- Gateway projections: `run_records`, `tool_calls`, `permission_requests`, and persisted run event timeline.
- HTTP queries: session history, workspace tree/file/diff, run status, run event timeline, tool audit, permission records, global pending permissions.
- Desktop restore: messages, tool cards, pending permissions, active run, latest `root_seq`, RunActivityPanel timeline state, and global pending approval queue.
- Desktop reconnect/resume: WebSocket reconnects with backoff, tracks highest observed `root_seq`, merges restored active run cursors, and issues `run.resume` after auth so missed events replay through the same channel.
- RunActivityPanel: status filter, run search, expandable run details, related tool calls, permission records, and Event Timeline with event kind/agent filters, grouped event summaries, and expandable payload details for inspection.
- SQLite history aggregation for assistant/subagent message deltas.
- In-process subagent lifecycle: Gateway can query active/completed `planner` subagents and cancel an in-process subagent while events continue on the same `root_run_id/root_seq` channel.
- Runtime process subagent lifecycle: with `subagent_backend=runtime_process`, the parent `red-panda-agent` starts an independent `red-panda-agent` child process over internal stdio JSON-RPC, runs the child task, and bridges child `agent.event` messages back onto the parent root run channel as subagent events using the parent `root_run_id/root_seq`.
- Runtime process command override: `RED_PANDA_SUBAGENT_COMMAND` can override the child `red-panda-agent` command.
- Runtime process pool subagent lifecycle: with `subagent_backend=process_pool`, the parent Runtime keeps a reusable pool of child `red-panda-agent` processes. Successful child runs return the child to the pool; cancelled or failed child runs close and discard that child process.
- Runtime process pool size: `RED_PANDA_SUBAGENT_POOL_SIZE` controls the reusable child pool size. The default is `8`; values above `8` are capped at `8`.
- Runtime process subagent failure handling: child creation/start failures emit failed `subagent_update` events on the parent root-run stream and keep root sequencing ordered.
- Gateway root-run runtime mode: `run.start` options can specify `runtime_mode=per_run_process` to start a dedicated `red-panda-agent` process for the root run. Events still enter the same WebSocket/projection stream, and Gateway shuts down and removes the per-run client after the run finishes. `single_core` remains the default.
- Gateway provider profile backend: HTTP CRUD APIs under `/api/v1/provider-profiles` persist OpenAI-compatible provider profiles with name, provider, base URL, model, default flag, active flag, masked API key state, and timestamps. Create/update may accept `api_key`, but responses never return the raw key; they expose `api_key_set` and a masked value only. `run.start` can pass `provider_profile_id`, which Gateway resolves into a per-run provider override for Runtime.
- Gateway memory/history backend: HTTP CRUD APIs under `/api/v1/memory` persist inspectable memory records with project/session/user/system scope, kind, status, confidence, source metadata, soft delete, and `preview-run` selection for active project/session memory.
- Runtime memory injection: Gateway resolves active project/session memory before `agent.reply`, passes structured memory context to Runtime, Runtime emits `memory_injected`, and OpenAI-compatible provider requests include memory as a separate system message before the current user message.
- Runtime memory tools: `memory.list`, `memory.create`, `memory.update`, and `memory.delete` are exposed to the provider/tool loop. Runtime keeps tool policy, permission, and tool events; Gateway executes persistence through internal `memory.tool.execute` stdio JSON-RPC and validates session/workspace scope.
- Desktop Memory UI: the right panel includes a Memory tab for listing project/session records, creating and editing memory, disabling/deleting records, previewing injected context, and seeing `memory_injected` in Activity.
- Desktop UI normalization: shared primitives now cover buttons, icon buttons, status badges, panel headers, tabs, fields, empty/error feedback, and custom dropdown menus. Native Desktop `<select>` controls are not used.
- Desktop clarity pass: main panels use concise titles, explanatory subtitles are reduced, visible status/risk/memory/event labels are normalized to Chinese, and Gateway-backed timeline assertions tolerate valid event projection count variance.
- Runtime per-run provider override: Agent Runtime can use the provider, model, base URL, and key supplied by Gateway for a single OpenAI-compatible chat completion run while retaining environment-variable provider fallback.
- Desktop SubAgentPanel: shows subagent `root_run_id`, backend, and status, and exposes cancel for running subagents through WebSocket `subagent.cancel`.
- OpenAI-compatible provider streaming: when `RED_PANDA_PROVIDER_STREAM=true`, provider requests to `/v1/chat/completions` use `stream=true`, parse `text/event-stream` data chunks, forward content deltas through the existing `message_delta` WebSocket flow, and accumulate streaming `tool_calls` before entering the existing tool-call loop. Desktop and Gateway still use WebSocket and do not use SSE.

## Temporary Triggers

- Plain text: default provider (tool calls via OpenAI-compatible `tool_calls`).
- Slash workspace tools (`/read`, `/list`, `/shell`, …) are **debug-only**: set `RED_PANDA_SLASH_TOOLS=1` to enable Runtime `Parse`. Desktop command palette still uses `/goal`, `/permission`, etc. at the UI layer.
- `/permission`: Runtime checkpoint permission request (when triggered via Desktop/options).
- `/subagent`: subagent event. Default child backend is `runtime_process`; `process_pool` remains available as advanced.
- With `RED_PANDA_SLASH_TOOLS=1`:
  - `/read README.md`: low-risk file read.
  - `/list scripts`: low-risk workspace listing.
  - `/grep red_panda README.md`: low-risk workspace search.
  - `/diff README.md old text => new text`: low-risk single-file unified diff preview.
  - `/shell echo rp-smoke`: high-risk shell with permission.
  - `/write` / `/patch`: high-risk workspace writes.
- `read file README.md`: provider requests `workspace.read_file`.
- `list files scripts`: provider requests `workspace.list`.
- `grep red_panda README.md`: provider requests `workspace.grep`.
- `diff file README.md old text => new text`: provider requests `workspace.diff_file`.
- `run shell echo rp-smoke`: provider requests `shell.exec`.
- `write file tmp/demo.txt hello`: provider requests `workspace.write_file`.
- `edit file README.md old text => new text`: provider requests `workspace.edit_file`.
- `apply patch <unified patch>`: provider requests `workspace.apply_patch`.
- `list memory session`: provider requests `memory.list`.
- `remember session prefer concise answers`: provider requests `memory.create` for session memory.
- `remember project use go test`: provider requests `memory.create` for project memory.
- `delete memory mem_...`: provider requests `memory.delete`.

## Build and Verification

```powershell
go test ./modules/protocol/...
go test ./modules/agent/...
go test ./modules/gateway/...
go test ./modules/desktop/...
go test ./modules/cli/...

$env:CGO_ENABLED='0'
go build -o bin\red-panda-agent.exe .\modules\agent\cmd\red-panda-agent
go build -o bin\red-panda-gateway.exe .\modules\gateway\cmd\red-panda-gateway

cd modules\desktop\frontend
npm test
npm run test:ui
npm run test:ui -- --grep @gateway-backed
npm run build

cd ..\
wails3 build

cd ..\..\
powershell -ExecutionPolicy Bypass -File scripts\ws-smoke.ps1
powershell -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1
```

`scripts\protocol-compat.ps1` is the lightweight protocol compatibility check for the public Gateway HTTP and WebSocket surface. It validates that core client-facing protocol behavior still matches the documented envelopes and projections, including malformed WebSocket payload recovery, permission approve/deny paths, failed tool/subagent projections, session fork/compact, memory behavior, provider profiles, and MCP config CRUD/default/redaction behavior without starting MCP processes.

Frontend automation under `modules\desktop\frontend` includes focused Playwright UI fixture coverage for restore, permissions, tool cards, subagents, and Activity timeline event filters, grouped summaries, and payload inspection. It continues to run through `npm run test:ui`.

Gateway-backed browser e2e coverage uses the same Playwright entry point with the `@gateway-backed` filter. The test expects `bin\red-panda-gateway.exe` and `bin\red-panda-agent.exe` to exist, starts the real Gateway/Runtime pair on an isolated test port, points the Vite frontend at the Gateway through the built-in dev proxy, and drives tools via the default echo provider’s natural-language tool_call phrases (e.g. `read file README.md`, `run shell …`) without enabling debug `RED_PANDA_SLASH_TOOLS`. Desktop UI flags `/permission` and `/subagent` remain client-side. Coverage verifies message/tool/Activity timeline, inactive provider profile UI failure handling, denied permission/tool rendering, pending permission run cancellation, `runtime_process` subagent startup failure visibility, running subagent cancellation, and reconnect/resume after a permission-gated run finishes while the browser is disconnected. Teardown cleans up the Gateway process tree and fails if newly created Gateway/Agent processes remain.

## HTTP API Summary

```text
GET /api/v1/runs/:id
GET /api/v1/runs/:id/events?after_seq=&limit=
GET /api/v1/sessions/:id/runs
POST /api/v1/sessions/:id/fork
POST /api/v1/sessions/:id/compact/preview
POST /api/v1/sessions/:id/compact
GET /api/v1/runs/:id/tools
GET /api/v1/sessions/:id/tools
GET /api/v1/permissions/pending
GET /api/v1/permissions/:id
GET /api/v1/runs/:id/permissions
GET /api/v1/sessions/:id/permissions
GET /api/v1/provider-profiles
POST /api/v1/provider-profiles
GET /api/v1/provider-profiles/:id
PUT /api/v1/provider-profiles/:id
DELETE /api/v1/provider-profiles/:id
GET /api/v1/mcp/servers
POST /api/v1/mcp/servers
GET /api/v1/mcp/servers/:id
PUT /api/v1/mcp/servers/:id
DELETE /api/v1/mcp/servers/:id
GET /api/v1/memory?scope=&workspace_root=&session_id=&status=&limit=
POST /api/v1/memory
PUT /api/v1/memory/:id
DELETE /api/v1/memory/:id
POST /api/v1/memory/preview-run
```

## Provider Configuration

Default provider is local echo and needs no configuration.

For an HTTP provider compatible with `/v1/chat/completions`:

```powershell
$env:RED_PANDA_PROVIDER='openai_compatible'
$env:RED_PANDA_PROVIDER_BASE_URL='https://your-provider.example.com'
$env:RED_PANDA_PROVIDER_API_KEY='...'
$env:RED_PANDA_PROVIDER_MODEL='your-model'
```

Streaming is supported for OpenAI-compatible providers:

```powershell
$env:RED_PANDA_PROVIDER_STREAM='true'
```

With streaming enabled, Runtime sends `stream=true` to `/v1/chat/completions`, parses provider `text/event-stream` chunks, and keeps Desktop/Gateway realtime delivery on the existing WebSocket channel.

Provider profiles are managed by Gateway through `/api/v1/provider-profiles`. A profile contains:

- `name`
- `provider` set to `openai_compatible`
- `base_url`
- `model`
- `is_default`
- `is_active`
- `api_key_set` and a masked key value
- `created_at` and `updated_at`

`POST` and `PUT` may include `api_key`, but Gateway responses must not include the raw key. A WebSocket `run.start` can pass `provider_profile_id`; Gateway resolves the profile and sends Runtime the per-run provider settings. If no profile override is supplied, Runtime keeps using the environment fallback shown above.

Desktop SettingsPanel manages these profiles through the Gateway APIs. It lists available profiles, lets the user select the active profile for a run, creates and updates profiles, deletes profiles, and stores only `api_key_set` plus the masked key value returned by Gateway. When a profile is selected, Desktop includes `provider_profile_id` in the WebSocket `run.start` options.

## MCP Configuration

Gateway manages MCP server configuration records through `/api/v1/mcp/servers`. The Desktop MCP Settings tab uses these APIs for list, create, edit, enable/disable, delete, and read-only discovery operations. Configuration includes command argv, environment entries, working directory, normalized phase timeouts, raw tool allowlists, and risk overrides. Sensitive environment values are masked in API responses.

For discovery, Gateway forwards one enabled configuration to Agent Runtime. Runtime directly starts the stdio process, performs `initialize` and `tools/list`, applies the server tool allowlist, sanitizes diagnostics, and always cleans up the child process. Discovered tools are inspectable in Desktop but are not registered with providers and cannot be called.

## Next Focus

- Treat the v0.1.2 Gateway MCP config CRUD, v0.1.4 Desktop MCP config management, and v0.1.5 read-only discovery slices as implemented.
- Keep Runtime-owned discovery separate from provider-facing tool registration and execution.
- Keep MCP `tools/call`, provider-facing execution, permission integration, cancellation/restart policy, and production process lifecycle claims blocked until their later implementation and verification gates pass.
