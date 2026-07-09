# Current Development Status

Updated: 2026-07-09

## Overall Status

`red_panda` has a running MVP three-layer loop:

- Desktop: Wails v3 + JavaScript + React + shadcn/ui-style UI.
- Gateway: Go + Gin + SQLite(no cgo) + GORM + MVC.
- Agent Runtime: Go subprocess using newline-delimited stdio JSON-RPC.
- Desktop <-> Gateway: HTTP JSON + WebSocket, no SSE.
- Agent Runtime supports provider-side OpenAI-compatible HTTP streaming when `RED_PANDA_PROVIDER_STREAM=true`; Desktop and Gateway still use WebSocket for realtime events.
- Root agent and subagents share one root run WebSocket channel using `root_seq`, `agent_seq`, and stream metadata.
- `subagent_backend=runtime_process` runs a subagent through an independent child `red-panda-agent` process and bridges child `agent.event` output back to the parent root run channel.
- `subagent_backend=process_pool` runs a subagent through a reusable child `red-panda-agent` process from the parent Runtime pool.
- Gateway root runs support `runtime_mode=per_run_process` in `run.start` options. `single_core` remains the default. Per-run process events enter the same WebSocket/projection stream, and Gateway shuts down and removes the per-run Runtime client after run finish.
- Gateway provider profile backend is complete: CRUD APIs live under `/api/v1/provider-profiles`, profiles store OpenAI-compatible provider settings with masked API key state, and `run.start` can pass `provider_profile_id`.
- Gateway exposes persisted run event timeline queries through `GET /api/v1/runs/:id/events` with `after_seq` and `limit`.
- Agent Runtime supports per-run OpenAI-compatible provider override from Gateway while keeping environment-variable provider fallback.
- Desktop SettingsPanel exposes `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `tool_allowlist`, and `tool_denylist`, and sends those values as WebSocket `run.start` options to Gateway.
- Desktop SettingsPanel manages Gateway provider profiles: it can list, select, create, update, and delete profiles, keeps only masked API key state, and sends the selected `provider_profile_id` as a WebSocket `run.start` option.
- Desktop RunActivityPanel includes an Event Timeline backed by the run event query API, with event kind/agent filters, grouped summaries, and expandable payload inspection.
- Desktop frontend has unit-level automation for provider profile DTO/payload handling, `run.start` option construction, and WebSocket reconnect resume cursors; browser-level Playwright fixture coverage for restore, permissions, tool cards, subagents, and Activity timeline filters/grouping/payload inspection through `npm run test:ui`; and Gateway-backed Playwright e2e coverage for `/read README.md`, inactive provider profile UI failure handling, denied permission rendering, denied tool failure rendering, pending permission run cancellation, and reconnect/resume after missed permission approval events over the real HTTP/WebSocket loop.
- Gateway-backed Playwright e2e also covers `runtime_process` subagent startup failure using `RED_PANDA_SUBAGENT_COMMAND`, verifying SubAgentPanel failed state and Activity timeline visibility over the real Gateway/Runtime/Desktop chain.
- `scripts/protocol-compat.ps1` covers public WebSocket/API failed tool and failed subagent paths: denied `shell.exec` produces `tool_failed` plus denied projections, and invalid `RED_PANDA_SUBAGENT_COMMAND` produces failed `subagent_update` events in persisted timeline.
- Gateway-backed Playwright teardown now snapshots `red-panda-gateway` / `red-panda-agent` processes before each test and fails if newly created processes remain after teardown.
- Gateway-backed Playwright e2e covers user-visible subagent cancellation over the real Gateway/Runtime/Desktop chain using `RED_PANDA_PLANNER_DRAFT_DELAY_MS` to keep the planner subagent cancellable without relying on a narrow timing window.
- Gateway session fork/compact backend is implemented: models, migrations, repository/service/controller routes, service tests, and protocol compatibility coverage for fork, compact preview, and compact apply.
- Desktop exposes session Fork and Compact controls, selects returned derived sessions, and restores forked/compacted history through the existing session restore flow.
- Memory/history design is documented in `docs/14-memory-history-design.md`.
- Gateway memory/history backend is implemented: `memory_records` persistence, CRUD HTTP APIs, soft delete, run memory preview selection, repository/service tests, and protocol compatibility coverage.
- Runtime memory injection is implemented: Gateway selects active project/session memory before `agent.reply`, Runtime receives structured memory context, emits `memory_injected`, and OpenAI-compatible provider requests include memory as a separate system message before the user message.
- Desktop Memory UI is implemented: the right panel exposes a Memory tab for inspecting project/session memory, creating records, editing content/status, disabling/deleting records, and previewing injected context through the Gateway `preview-run` API.
- Runtime memory tools are implemented: `memory.list`, `memory.create`, `memory.update`, and `memory.delete` use normal Runtime tool policy, permission, and `tool_*` events while Gateway executes persistence through internal `memory.tool.execute` JSON-RPC with current run/session/workspace scope validation.
- Agent Runtime unit coverage includes process subagent success, cancellation, process-pool reuse, and failed child creation/start paths while preserving root-run finish behavior and ordered `root_seq`.

## Completed

1. Analyzed `DeepSeek-Reasonix` and `night24`.
2. Designed the three-layer architecture: Desktop, Gateway, Agent Runtime.
3. Created Go workspace modules:
   - `modules/protocol`
   - `modules/agent`
   - `modules/gateway`
   - `modules/desktop`
   - `modules/cli`
4. Implemented Gateway stack: Go + Gin + SQLite(no cgo) + GORM + MVC.
5. Implemented Desktop stack: Wails v3 + JavaScript + React + shadcn/ui-style components.
6. Implemented Desktop <-> Gateway HTTP + WebSocket integration. SSE is not used.
7. Implemented Gateway <-> Agent Runtime newline-delimited stdio JSON-RPC integration.
8. Implemented `run.start`, `run.subscribe`, `run.resume`, `run.cancel`, `permission.resolve`, and `agent.status`.
9. Implemented `agent.event` persistence, WebSocket broadcast, and `root_seq` replay.
10. Implemented WebSocket session de-duplication by `root_run_id/root_seq`.
11. Implemented root agent plus in-process `planner` subagent MVP.
12. Implemented subagent protocol DTOs and methods: Runtime `agent.subagents` / `agent.subagent.cancel`, WebSocket `subagents.list` / `subagent.cancel`.
13. Implemented in-process subagent lifecycle state, query, and cancel while keeping events on the same `root_run_id/root_seq` channel.
14. Implemented `subagent_backend=runtime_process`: parent `red-panda-agent` starts an independent child `red-panda-agent` over internal stdio JSON-RPC, runs the child task, and bridges child `agent.event` messages back as subagent events on the parent `root_run_id/root_seq` channel.
15. Added `RED_PANDA_SUBAGENT_COMMAND` to override the child `red-panda-agent` command.
16. Implemented `subagent_backend=process_pool`: parent Runtime keeps a reusable pool of child `red-panda-agent` processes; successful child runs return to the pool, while cancelled or failed child runs close and discard the child.
17. Added `RED_PANDA_SUBAGENT_POOL_SIZE` for the reusable child process pool size, with default `2` and maximum `8`.
18. Implemented workspace/session/message HTTP APIs.
19. Implemented workspace tree/file/diff APIs with path traversal protection, size limits, binary detection, and non-Git fallback.
20. Implemented Desktop chat, permission cards, subagent panel, tool cards, workspace file panel, and RunActivityPanel.
21. Desktop SubAgentPanel stores subagent `root_run_id`, backend, and status, and connects user-visible cancel for running subagents to WebSocket `subagent.cancel`.
22. Implemented Agent Runtime provider abstraction:
    - default `echo` provider.
    - optional HTTP/OpenAI-compatible provider through environment variables.
23. Implemented HTTP/OpenAI-compatible `tools` schema and `tool_calls` parsing.
24. Implemented provider tool-call loop.
25. Implemented ToolRunner MVP:
    - `workspace.read_file`
    - `workspace.write_file`
    - `workspace.edit_file`
    - `shell.exec`
26. Implemented additional ToolRunner tools:
    - `workspace.list` as a low-risk read-only workspace tool.
    - `workspace.grep` as a low-risk read-only workspace search tool.
27. Implemented tool events:
    - `tool_started`
    - `tool_output`
    - `tool_finished`
    - `tool_failed`
28. Implemented high-risk tool permission flow.
29. Implemented tool policy MVP: `tool_policy`, `tool_allowlist`, `tool_denylist`, and `permission_mode`.
30. Added `policy` and `policy_reason` on `tool_started`.
31. Implemented Gateway `tool_calls` projection.
32. Added tool audit HTTP APIs.
33. Implemented Gateway `run_records` projection.
34. Added run status HTTP APIs.
35. Implemented pending permission projection and close-on-run-finish behavior.
36. Added permission HTTP APIs, including global `GET /api/v1/permissions/pending`.
37. Desktop restores session state from Gateway APIs.
38. Desktop restores global pending approvals and allows approve/deny from Activity.
39. RunActivityPanel supports status filtering, run search, and expandable run details with related tools and permission records.
40. assistant/subagent message deltas are aggregated before writing SQLite history.
41. Implemented Gateway root-run `runtime_mode=per_run_process` with a dedicated `red-panda-agent` process per root run, shared WebSocket/projection event flow, and automatic per-run Runtime client shutdown/removal after run finish.
42. Implemented high-risk precise replacement `workspace.edit_file` with `path`, `old_text`, `new_text`, and `replace_all`; by default `old_text` must appear exactly once, while `replace_all=true` permits multi-location replacement. It uses the existing permission and tool policy flow.
43. Cleaned known Desktop UI mojibake.
44. Implemented OpenAI-compatible provider streaming behind `RED_PANDA_PROVIDER_STREAM=true`: Runtime sends `stream=true` to `/v1/chat/completions`, parses provider `text/event-stream` data chunks, forwards content deltas through existing `message_delta`/WebSocket events, and accumulates streaming `tool_calls` before the existing tool-call loop.
45. Implemented Desktop SettingsPanel for runtime and policy controls, including `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `tool_allowlist`, and `tool_denylist`; these values are sent to Gateway as WebSocket `run.start` options.
46. Implemented low-risk read-only `workspace.diff_file` for one-file unified diff previews. It accepts `path` plus either full proposed `content`, or `old_text`/`new_text` with optional `replace_all` for exact replacement preview.
47. Implemented high-risk `workspace.apply_patch` for applying workspace-scoped unified patches from `patch`; it uses the existing permission and tool policy flow and emits the existing WebSocket tool events.
48. Implemented Gateway provider profile CRUD APIs under `/api/v1/provider-profiles`.
49. Provider profiles include name, provider `openai_compatible`, base URL, model, default flag, active flag, masked API key state, and timestamps.
50. Provider profile create/update accepts `api_key`, but responses do not return the raw key; responses expose only `api_key_set` and a masked key value.
51. Implemented `provider_profile_id` on `run.start`: Gateway resolves the profile and sends provider, model, base URL, and API key to Runtime for that run.
52. Implemented Runtime per-run OpenAI-compatible provider override while preserving environment fallback when no profile override is supplied.
53. Implemented Desktop SettingsPanel provider profile management UI for listing, selecting, creating, updating, and deleting Gateway provider profiles.
54. Desktop keeps only masked provider profile API key state and sends the selected `provider_profile_id` in WebSocket `run.start` options.
55. Added Desktop frontend unit tests for provider profile normalization/payloads and `run.start` option construction, including `provider_profile_id`, tool policy lists, and subagent trigger behavior.
56. Added `GET /api/v1/runs/:id/events` for persisted run event timeline queries with `after_seq` and `limit`.
57. Added RunActivityPanel Event Timeline display.
58. Added Event Timeline event kind/agent filters, grouped event summaries, and expandable payload inspection.
59. Added `scripts/ws-smoke.ps1` validation for `read_run_events`.
60. Added lightweight HTTP/WebSocket protocol compatibility validation through `scripts/protocol-compat.ps1`, including malformed WebSocket payload recovery, permission approve/deny paths, provider profile selection, and inactive provider profile failure handling.
61. Added frontend Activity timeline automation for filters, grouped summaries, and payload inspection.
62. Added Playwright UI fixture automation for restore, permissions, tool cards, and subagents while continuing to use `npm run test:ui`.
63. Added Gateway-backed Playwright e2e coverage that starts real `red-panda-gateway` and `red-panda-agent`, points Vite at Gateway through `VITE_RED_PANDA_GATEWAY_BASE_URL`, sends `/read README.md` from the browser, and verifies the message, tool card, and Activity timeline.
64. Added Desktop WebSocket automatic reconnect and active-run resume. The client tracks the highest observed `root_seq`, merges it with restored active run cursors, and sends `run.resume` after authentication so missed root-run events replay through the same Gateway channel.
65. Added Gateway-backed browser e2e coverage for reconnect/resume during a permission wait: the browser disconnects, an external WebSocket resolves the permission, and the Desktop replays missed `message_delta` / `finish` events after connectivity returns.
66. Added fixture coverage for permission approve/deny visible states and failed tool card rendering.
67. Added Gateway-backed browser e2e coverage for denied tool policy paths: `tool_denylist` produces a denied tool card, a visible chat error, and denied Activity run status.
68. Hardened Gateway-backed Playwright process cleanup so teardown kills the Gateway process tree on Windows through `taskkill /T /F` and uses process-group cleanup on non-Windows platforms.
69. Added Gateway-backed browser e2e coverage for denied permission paths: user denial keeps the permission card visible as `Denied`, renders the runtime `permission denied` message, and marks Activity as denied.
70. Kept resolved permission cards visible in Desktop chat so approve/deny decisions visibly settle instead of disappearing immediately after a click.
71. Added Agent Runtime unit coverage for process subagent failure paths: `runtime_process` child creation failure and `process_pool` child start failure both emit failed `subagent_update` events with backend, summary, error, ordered `root_seq`, and a root-run finish.
72. Added Gateway-backed browser e2e coverage for cancelling a run while it is waiting on permission; Desktop renders `Run cancelled.` and Activity marks the run as cancelled.
73. Added a stable `chat-composer-cancel` test id for the run cancel control.
74. Added Gateway-backed browser e2e coverage for inactive selected provider profiles: Desktop renders `run.start failed` and keeps the inactive profile selected in Settings.
75. Added stable Settings select test IDs to support provider/runtime/policy UI verification.
76. Added Gateway-backed browser e2e coverage for `runtime_process` subagent startup failure: tests inject an invalid `RED_PANDA_SUBAGENT_COMMAND`, verify the `planner` subagent renders as failed with `runtime_process`, and verify Activity timeline includes the failed subagent event on the real event stream.
77. Expanded `scripts/protocol-compat.ps1` with external failed path coverage:
    - denied `shell.exec` emits `tool_started`, `tool_failed`, `error`, and denied `finish`, with denied run/tool/timeline projections.
    - invalid `RED_PANDA_SUBAGENT_COMMAND` emits failed `runtime_process` `subagent_update` events with ordered `root_seq` and persisted timeline projection.
78. Added Gateway-backed Playwright process leak assertions: test helper records baseline Gateway/Agent processes before starting a test Gateway and fails teardown if newly created `red-panda-gateway` or `red-panda-agent` processes remain after cleanup.
79. Added `RED_PANDA_PLANNER_START_DELAY_MS` and `RED_PANDA_PLANNER_DRAFT_DELAY_MS` Runtime timing overrides for stable planner subagent lifecycle tests; defaults remain 10ms and 120ms.
80. Added Gateway-backed browser e2e coverage for cancelling a running subagent: Desktop sends `subagent.cancel`, SubAgentPanel shows `cancelled`, and Activity timeline contains the cancelled `subagent_update`.
81. Added session fork/compact backend:
    - new `session_lineage` and `session_compactions` models/migrations,
    - session/message metadata for derived sessions,
    - `POST /api/v1/sessions/:id/fork`,
    - `POST /api/v1/sessions/:id/compact/preview`,
    - `POST /api/v1/sessions/:id/compact`,
    - backend service tests for fork and compact apply.
82. Expanded `scripts/protocol-compat.ps1` with fork/compact external API coverage.
83. Added Desktop session Fork and Compact controls plus Gateway-backed browser e2e coverage for forking and compacting a real session.
84. Added `docs/14-memory-history-design.md` covering memory record types, retention, APIs, Runtime injection, Desktop inspection/deletion, tool policy, tests, and guardrails.
85. Added Gateway memory/history backend:
    - new `memory_records` model/migration,
    - repository/service/controller/routes for list, create, update, delete, and preview-run,
    - validation for project/session trace requirements and agent-created source metadata,
    - soft delete with deleted records excluded from default list and preview selection,
    - backend repository/service tests.
86. Expanded `scripts/protocol-compat.ps1` with memory CRUD, soft-delete exclusion, and preview-run selection coverage.
87. Added Runtime memory injection:
    - `ReplyOptions.MemoryContext` carries selected memory ids/content from Gateway to Runtime,
    - Gateway reuses memory preview selection rules before `agent.reply`,
    - Runtime emits `memory_injected` with ids/count/context size,
    - OpenAI-compatible provider messages keep memory as a separate system message before the current user message,
    - Runtime/Gateway/provider tests cover selection, event persistence, event emission, and provider message shape.
88. Added Desktop Memory UI:
    - new `MemoryPanel` right-panel tab,
    - frontend memory DTO/payload helpers and unit tests,
    - project/session memory list, create, edit, disable, delete, and preview controls,
    - Activity timeline classifies `memory_injected` events as memory,
    - Playwright fixture coverage and Gateway-backed E2E for memory creation, preview, Runtime injection, and Activity visibility.

## Modules

| Module | Status | Notes |
| --- | --- | --- |
| `modules/protocol` | Complete | JSON-RPC, WebSocket envelope, agent event, permission DTO, tool DTO, subagent lifecycle DTO |
| `modules/agent` | MVP complete | stdio JSON-RPC Runtime, Provider abstraction, per-run OpenAI-compatible provider override with env fallback, ToolRunner MVP, low-risk `workspace.list`, `workspace.grep`, and `workspace.diff_file`, medium-risk `memory.list`, high-risk `workspace.edit_file`, `workspace.apply_patch`, and `memory.create/update/delete`, permission blocking, in-process subagent lifecycle/query/cancel, `runtime_process` child-process subagent backend, reusable `process_pool` child-process subagent backend |
| `modules/gateway` | MVP complete | Gin/GORM/SQLite(no cgo), Runtime subprocess client, WebSocket channel, persistence replay, workspace/session/run/tool/permission/event APIs, provider profile CRUD APIs, `provider_profile_id` run resolution, subagent list/cancel routing, root-run `per_run_process` runtime mode, Gateway-mediated memory tool execution |
| `modules/desktop` | MVP complete | Wails v3, React/Vite, chat, permissions, tool cards, subagent status/cancel, workspace panel, session restore, searchable RunActivityPanel with Event Timeline filters, grouped summaries, payload inspection, global pending approval entry, SettingsPanel runtime/policy options and provider profile management UI |
| `modules/cli` | Placeholder | Future debugging entry |

## Current Triggers

Desktop or WebSocket client inputs:

- Plain text: default provider.
- `/permission`: Runtime checkpoint permission request.
- `/subagent`: subagent event. The default path remains the in-process `planner`; `subagent_backend=runtime_process` runs the subagent through a child `red-panda-agent` process; `subagent_backend=process_pool` runs it through the reusable child process pool.
- `/read README.md`: `workspace.read_file`.
- `/list scripts`: `workspace.list`.
- `/grep red_panda README.md`: `workspace.grep`.
- `/diff README.md old text => new text`: read-only unified diff preview through `workspace.diff_file`.
- `/shell echo rp-smoke`: `shell.exec` with permission.
- `/write tmp/demo.txt hello`: `workspace.write_file` with permission.
- `/patch <unified patch>`: `workspace.apply_patch` with permission.
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

## Verification

Full v0.1.0 gate status: Pass on 2026-07-09. Agent/Gateway binaries were rebuilt with version `0.1.0`, Playwright `test-results` was cleaned, and no `red-panda-gateway` / `red-panda-agent` / `red_panda` processes remained after verification.

| Check | Result |
| --- | --- |
| `go test ./modules/protocol/...` | Pass |
| `go test ./modules/agent/...` | Pass, includes process subagent success, cancellation, reuse, and failure-path coverage |
| `go test ./modules/gateway/...` | Pass |
| `go test ./modules/desktop/...` | Pass |
| `go test ./modules/cli/...` | Pass |
| `go test ./modules/gateway/internal/gateway/repository ./modules/gateway/internal/gateway/service` | Pass |
| `npm test` in `modules/desktop/frontend` | Pass, includes reconnect resume cursor coverage |
| `npm run test:ui` in `modules/desktop/frontend` | Pass, 13 tests, includes Activity, Workflow, Memory fixture, and Gateway-backed workflows |
| `npm run test:ui -- --grep @gateway-backed` in `modules/desktop/frontend` | Pass, 10 tests, includes `/read README.md`, inactive provider profile UI failure, denied permission/tool rendering, pending permission cancel, `runtime_process` subagent failure visibility, running subagent cancel, session fork/compact, Desktop memory preview/injection visibility, reconnect/resume permission wait coverage, and process leak assertions during teardown |
| `npm run build` in `modules/desktop/frontend` | Pass |
| `CGO_ENABLED=0 go build` agent | Pass |
| `CGO_ENABLED=0 go build` gateway | Pass |
| `powershell -ExecutionPolicy Bypass -File scripts/ws-smoke.ps1` | Pass |
| `powershell -ExecutionPolicy Bypass -File scripts/protocol-compat.ps1` | Pass, includes failed tool/subagent, session fork/compact, memory CRUD/preview, Runtime memory tool create/list/deny external protocol paths, and a run timeline with `memory_injected` |
| `wails3 build` | Pass, with Windows template warnings for missing Unix tools |

## Provider Environment

Default provider is echo and needs no configuration.

For an HTTP provider compatible with `/v1/chat/completions`:

```powershell
$env:RED_PANDA_PROVIDER='openai_compatible'
$env:RED_PANDA_PROVIDER_BASE_URL='https://your-provider.example.com'
$env:RED_PANDA_PROVIDER_API_KEY='...'
$env:RED_PANDA_PROVIDER_MODEL='your-model'
```

Streaming output is supported for OpenAI-compatible providers:

```powershell
$env:RED_PANDA_PROVIDER_STREAM='true'
```

This enables provider-side `text/event-stream` parsing in Agent Runtime. Desktop and Gateway continue to use WebSocket, not SSE.

Gateway provider profiles are also supported through `/api/v1/provider-profiles`:

- `GET /api/v1/provider-profiles`
- `POST /api/v1/provider-profiles`
- `GET /api/v1/provider-profiles/:id`
- `PUT /api/v1/provider-profiles/:id`
- `DELETE /api/v1/provider-profiles/:id`

Profiles contain name, provider `openai_compatible`, base URL, model, default flag, active flag, API key state, and timestamps. `POST` and `PUT` may include `api_key`; responses never return the raw key and expose only `api_key_set` plus a masked key value. `run.start` may include `provider_profile_id`; Gateway resolves that profile and sends provider, model, base URL, and key to Runtime as a per-run override.

Desktop SettingsPanel uses these APIs to list, select, create, update, and delete provider profiles. Desktop state stores only `api_key_set` and masked API key values returned by Gateway. When a profile is selected, Desktop sends its `provider_profile_id` in the WebSocket `run.start` options.

`scripts/ws-smoke.ps1` includes `read_run_events` coverage for the persisted run event timeline query.

`scripts/protocol-compat.ps1` is the lightweight HTTP/WebSocket compatibility script. It covers the client-facing protocol matrix for run lifecycle, replay ordering, malformed WebSocket payload recovery, permission approve/deny resolution, failed tool events/projections, failed subagent events/timeline, provider profile selection, inactive provider profile failure handling, and persisted timeline query shape without requiring a full desktop build.
It also covers session fork, compact preview, compact apply, memory CRUD, memory soft delete, memory preview selection, and Runtime `memory.*` tool create/list/deny behavior.

Frontend tests include Playwright UI fixture coverage for restore, permission approve/deny visible states, failed/completed tool cards, subagents, Activity timeline event kind and agent filtering, grouped summaries, and expandable payload inspection. The UI fixture suite continues to run through `npm run test:ui`.

Gateway-backed Playwright e2e coverage starts `bin\red-panda-gateway.exe` and `bin\red-panda-agent.exe` on an isolated test port, points the Vite frontend at Gateway through the built-in dev proxy, sends `/read README.md` from the browser over HTTP/WebSocket, verifies the rendered assistant message, tool card, and Activity timeline, verifies inactive provider profile UI failure handling, verifies denied permission/tool rendering, verifies pending permission run cancellation, verifies `runtime_process` subagent startup failure visibility, verifies running subagent cancellation, verifies session fork/compact restore, and verifies reconnect/resume while a permission-gated run finishes during browser disconnection. The test helper cleans up the Gateway process tree on teardown and fails if newly created Gateway/Agent processes remain. Run it from `modules\desktop\frontend` with:

```powershell
npm run test:ui -- --grep @gateway-backed
```

## Not Complete / Future Work

The following should not be claimed as complete:

1. Additional persisted event replay edge cases beyond the current reconnect/resume and timeline coverage.
2. MCP stdio tools and context compaction.

## Next Development Steps

1. Treat v0.1.0 as release-gate complete.
2. Follow `docs/11-structured-development-roadmap.md` as the ordering source for new work.
3. Use `docs/12-current-execution-plan.md` as the short-cycle execution board to avoid unordered parallel development.
4. Start MCP stdio tools with design only; do not implement MCP until the scoped design is written and accepted.
