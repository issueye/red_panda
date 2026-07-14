# red_panda Development Plan

## 1. Goal

`red_panda` is a local AI Agent platform redesigned in Go. It is built as a three-layer system:

- Desktop: Wails v3 + JavaScript + React + shadcn/ui-style components.
- Gateway: Go + Gin + SQLite(no cgo) + GORM + MVC.
- Agent Runtime: Go subprocess connected to the Gateway through newline-delimited stdio JSON-RPC.

The Desktop and Gateway use WebSocket for realtime interaction and do not use SSE. Provider-side HTTP streaming is supported inside Agent Runtime for OpenAI-compatible providers. Root agents and subagents share one root run event channel. `root_seq` provides global ordering, while `agent_seq` and stream metadata identify event sources.

## 2. MVP Completion Criteria

MVP is complete when:

1. `red-panda-gateway` starts and connects to `red-panda-agent`.
2. `red-panda-agent` writes only JSON-RPC protocol messages to stdout; logs go to stderr.
3. Desktop connects to `/api/v1/ws`, creates or switches sessions, sends tasks, and displays results.
4. Gateway persists sessions, messages, run events, and run/tool/permission projections in SQLite(no cgo) through GORM, and exposes run event timeline queries.
5. WebSocket supports `run.start`, `run.subscribe`, `run.resume`, `run.cancel`, `permission.resolve`, `agent.status`, `subagents.list`, and `subagent.cancel`.
6. Agent Runtime supports echo provider, OpenAI-compatible HTTP provider, per-run OpenAI-compatible provider override from Gateway, tool-call loop, permission blocking, run cancellation, in-process subagent lifecycle query/cancel, and the `runtime_process` / `process_pool` subagent backends.
7. Built-in baseline tools include `workspace.read_file`, `workspace.list`, `workspace.grep`, `workspace.diff_file`, `workspace.write_file`, `workspace.edit_file`, `workspace.apply_patch`, and `shell.exec`.
8. High-risk tools are gated by permission cards. approve and deny both complete visibly.
9. Desktop restores messages, tool cards, pending permissions, active run state, latest `root_seq`, subagent `root_run_id`/backend/status, Activity timeline state, and global pending approvals after restart or session switch.
10. Desktop SubAgentPanel displays subagent lifecycle state and lets users cancel running subagents through WebSocket `subagent.cancel`.
11. Gateway and Agent Runtime build with `CGO_ENABLED=0`.

Active runtime update complete: low-risk read-only `workspace.list`, `workspace.grep`, and `workspace.diff_file`; high-risk edit-oriented `workspace.edit_file` and `workspace.apply_patch`.

Not claimed as complete in the current MVP: plugin/skill/hook support, broader failure-path coverage, and broader external client compatibility.

## 3. Phase 0: Repository Skeleton

Deliverables:

```text
go.work
modules/
  protocol/
  agent/
  gateway/
  desktop/
  cli/
```

Tasks:

1. Create `go.work`.
2. Create `modules/protocol`, `modules/agent`, `modules/gateway`, `modules/desktop`, and `modules/cli`.
3. Add minimal agent, gateway, and desktop entry points.
4. Add basic validation commands.

Status: complete.

## 4. Phase 1: Shared Protocol

Tasks:

1. Define JSON-RPC request, response, and notification envelopes.
2. Define Runtime methods: `core.initialize`, `core.ping`, `agent.reply`, `agent.cancel`, `agent.subagents`, `agent.subagent.cancel`, `permission.resolve`.
3. Define Agent event envelope fields: `root_run_id`, `run_id`, `root_seq`, `agent_seq`, `agent`, `stream`, and `payload`.
4. Define WebSocket envelopes: request, response, event, error, ping, and pong.
5. Define WebSocket methods: `run.start`, `run.subscribe`, `run.resume`, `run.cancel`, `permission.resolve`, `agent.status`, `subagents.list`, and `subagent.cancel`.
6. Define tool and permission DTOs.

Status: complete.

## 5. Phase 2: Agent Runtime MVP

Tasks:

1. Implement stdio JSON-RPC server.
2. Implement active run registry and real cancellation.
3. Implement event multiplexer with `root_seq`.
4. Implement echo provider.
5. Implement OpenAI-compatible HTTP provider with `tools` and `tool_calls`.
6. Implement provider tool-call loop.
7. Implement ToolRunner for baseline `workspace.read_file`, `workspace.list`, `workspace.grep`, `workspace.diff_file`, `workspace.write_file`, `workspace.edit_file`, `workspace.apply_patch`, and `shell.exec`.
8. Implement permission gate with `permission_required` and `permission.resolve`.
9. Implement tool policy: `tool_policy`, `tool_allowlist`, `tool_denylist`, and `permission_mode`.
10. Implement in-process `planner` subagent MVP.
11. Implement in-process subagent lifecycle state, query, and cancel.
12. Implement `runtime_process` subagent backend: parent `red-panda-agent` starts an independent child `red-panda-agent` over internal stdio JSON-RPC, invokes a child run, and bridges child `agent.event` output back to the parent `root_run_id/root_seq` channel as subagent events.
13. Add `RED_PANDA_SUBAGENT_COMMAND` override for the child subagent process command.
14. Implement `process_pool` subagent backend: parent Runtime maintains a reusable pool of child `red-panda-agent` processes, returns successful children to the pool, and closes/discards cancelled or failed children.
15. Add `RED_PANDA_SUBAGENT_POOL_SIZE` for the reusable child process pool size, with default `8` (was originally 2) and maximum `8`.
16. Add OpenAI-compatible provider streaming: when `RED_PANDA_PROVIDER_STREAM=true`, send `stream=true` to `/v1/chat/completions`, parse provider `text/event-stream` data chunks, emit content through existing `message_delta` events, and accumulate streaming `tool_calls` before the existing tool-call loop.
17. Add per-run OpenAI-compatible provider override fields accepted from Gateway, including provider, model, base URL, and API key, while keeping environment-variable fallback when no override is supplied.

Status: MVP baseline complete. Low-risk read-only `workspace.list` and `workspace.grep` are complete. Low-risk read-only `workspace.diff_file` is complete for one-file unified diff preview with `path` plus either proposed full `content`, or `old_text`/`new_text` and optional `replace_all` for exact replacement preview. High-risk precise replacement `workspace.edit_file` is complete with `path`, `old_text`, `new_text`, and `replace_all`; by default `old_text` must appear exactly once, while `replace_all=true` permits multi-location replacement. High-risk `workspace.apply_patch` is complete for applying a workspace-scoped unified patch from `patch`. Write tools use the existing permission and tool policy flow. In-process subagent lifecycle query/cancel is complete. `runtime_process` subagents are complete for one child process per subagent run. Runtime `process_pool` subagents are complete with reusable child process pooling. Runtime unit coverage includes process subagent failure paths for child creation and child start errors, with failed `subagent_update` events kept on the root-run stream. OpenAI-compatible provider streaming is complete for content deltas and streaming tool-call accumulation. Runtime per-run OpenAI-compatible provider override is complete with environment fallback preserved.

## 6. Phase 3: Gateway MVP

Tasks:

1. Build Gin app, router, recovery, and request id support.
2. Initialize SQLite(no cgo) + GORM using `github.com/glebarez/sqlite`.
3. Add models: Session, Message, RunEvent, Workspace, PermissionRequest, RunRecord, ToolCall.
4. Add Repository / Service / Controller layers.
5. Add Runtime subprocess client and stdio JSON-RPC client.
6. Add WebSocket `/api/v1/ws`.
7. Add run, permission, agent status, and subagent lifecycle WebSocket methods.
8. Add workspace tree/file/diff HTTP APIs.
9. Add run/tool/permission projection APIs.
10. Add WebSocket session de-duplication by `root_run_id/root_seq`.
11. Aggregate assistant/subagent message deltas before writing SQLite history.
12. Support Gateway root-run `runtime_mode=per_run_process`: `run.start` options may request a dedicated `red-panda-agent` process for the root run, while `single_core` remains the default. Per-run process events continue through the same WebSocket/projection stream and the per-run Runtime client is shut down and removed after run finish.
13. Add provider profile CRUD HTTP APIs under `/api/v1/provider-profiles`.
14. Persist provider profiles with name, provider `openai_compatible`, base URL, model, default flag, active flag, masked API key state, and timestamps.
15. Accept `api_key` on provider profile create/update without returning the raw key in responses; expose only `api_key_set` and a masked key value.
16. Support `provider_profile_id` in `run.start`: Gateway resolves the profile and sends provider, model, base URL, and API key to Runtime for that run.
17. Add `GET /api/v1/runs/:id/events` for persisted run event timeline queries with `after_seq` and `limit`.

Status: MVP complete. Message delta aggregation has repository coverage. Gateway root-run `per_run_process` is complete. Provider profile backend CRUD and `run.start` profile resolution are complete. Persisted run event timeline query is complete.

## 7. Phase 4: Desktop MVP

Tasks:

1. Add Wails v3 shell.
2. Reuse red panda pixel mark.
3. Add React/Vite/lucide-react frontend.
4. Add Gateway bootstrap, HTTP client, and WebSocket client.
5. Add TopBar, Sidebar, and StatusBar.
6. Add ChatPanel, MessageBubble, and ChatComposer.
7. Add ToolCallCard.
8. Add PermissionCard.
9. Add WorkspacePanel.
10. Add SubAgentPanel with subagent `root_run_id`, backend, status, and cancel for running subagents.
11. Add RunActivityPanel using run/tool/permission projection APIs and run event timeline queries.
12. Restore session state on restart or session switch.
13. Restore global pending approvals and allow approve/deny from Activity.
14. Add RunActivityPanel filtering, search, run detail expansion with related tools and permission records, and Event Timeline display with event kind/agent filters, grouped summaries, and expandable payload inspection.
15. Add Desktop SettingsPanel for runtime and policy options: `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `tool_allowlist`, and `tool_denylist`.
16. Send SettingsPanel values as WebSocket `run.start` options to Gateway.
17. Add Desktop provider profile management UI for listing, creating, updating, deleting, selecting, and defaulting Gateway provider profiles.
18. Add WebSocket reconnect/resume in the Desktop client so active runs replay missed events from the highest known `root_seq`.

Status: MVP complete. Desktop user-visible subagent cancel is complete, including Gateway-backed browser coverage for cancelling a running subagent. RunActivityPanel Event Timeline is complete for persisted run events, including event kind/agent filters, grouped event summaries, and expandable payload inspection. Desktop SettingsPanel is complete for runtime mode, tool policy, permission mode, subagent spawning/backend, model, and tool allow/deny lists, with values sent through WebSocket `run.start` options. Desktop provider profile management UI is complete for listing, selecting, creating, updating, deleting, and defaulting Gateway provider profiles. Desktop keeps only masked provider profile API key state and sends the selected `provider_profile_id` through WebSocket `run.start` options; inactive selected profile failures are visible without losing the selected setting. Desktop WebSocket reconnect/resume is complete for active runs, merging restored run cursors with client-observed `root_seq` values and replaying missed events through `run.resume`. Browser-level fixture automation covers restore, permissions, tool cards, subagents, and Activity timeline UI paths through `npm run test:ui`. Gateway-backed Playwright e2e coverage starts the real `red-panda-gateway` / `red-panda-agent` pair, uses the Vite Gateway base URL override, sends `/read README.md`, verifies the browser message, tool card, and Activity timeline, covers inactive provider profile UI failure, covers denied permission/tool rendering, covers pending permission run cancellation, covers running subagent cancellation, and covers a permission-wait reconnect path where missed events are replayed after browser connectivity returns.

## 8. Phase 5: End-to-End Integration

Core scenarios:

1. Gateway starts and initializes Agent Runtime.
2. Desktop or an external client connects through WebSocket and sends an echo prompt.
3. `/read README.md` runs `workspace.read_file`.
4. `/list scripts` runs `workspace.list`.
5. `/grep red_panda README.md` runs `workspace.grep`.
6. `/diff README.md old text => new text` runs low-risk read-only `workspace.diff_file` and previews a unified diff for one file.
7. `/shell echo rp-smoke` triggers permission approval and runs `shell.exec`.
8. `/patch <unified patch>` triggers permission approval and runs high-risk `workspace.apply_patch`.
9. Denylist produces `tool_failed`, `error`, and `finish(status=denied)`.
10. Cancelling a pending permission closes the permission and marks the run cancelled.
11. Natural-language provider tool-call loop runs read/list/grep/diff/shell/write/edit/patch tools.
12. In-process subagent list/cancel calls query and cancel `planner` lifecycle state while events remain on the root run channel.
13. Desktop SubAgentPanel lists subagent `root_run_id`, backend, and status, then cancels running in-process subagents through `subagent.cancel`.
14. `subagent_backend=runtime_process` starts a child `red-panda-agent` process, runs the child task over internal stdio JSON-RPC, and bridges child events back as subagent events on the parent root run channel.
15. `subagent_backend=process_pool` leases a reusable child `red-panda-agent` process from the parent Runtime pool, returns it after successful completion, and closes/discards it after cancellation or failure.
16. `runtime_mode=per_run_process` on `run.start` starts a dedicated root-run `red-panda-agent` process, keeps events on the same WebSocket/projection stream, and shuts down/removes the per-run Runtime client after run finish.
17. `RED_PANDA_PROVIDER_STREAM=true` streams OpenAI-compatible provider output over provider-side `text/event-stream`, forwards content deltas through the existing WebSocket event channel, and handles streaming tool calls through the existing tool-call loop.
18. Desktop SettingsPanel sends `runtime_mode`, `tool_policy`, `permission_mode`, `spawn_subagents`, `subagent_backend`, `model`, `tool_allowlist`, `tool_denylist`, and selected `provider_profile_id` as WebSocket `run.start` options to Gateway.
19. Provider profile CRUD under `/api/v1/provider-profiles` creates, reads, updates, and deletes OpenAI-compatible profiles while masking API keys in responses.
20. Desktop SettingsPanel lists, selects, creates, updates, deletes, and defaults Gateway provider profiles while keeping only `api_key_set` and masked key values in Desktop state.
21. `provider_profile_id` on `run.start` resolves a Gateway provider profile and Runtime uses it as a per-run OpenAI-compatible provider override.
22. Session restore reads history, run, tool, permission, subagent, run event timeline, and global pending approval projections.
23. `GET /api/v1/runs/:id/events` returns persisted run event timeline entries with `after_seq` and `limit`.
24. Desktop Activity timeline supports event kind/agent filtering, grouped summaries, and expandable payload inspection for persisted run events.
25. Desktop reconnects after WebSocket loss, issues `run.resume` for active runs, and replays missed persisted events by `root_seq`.

Status: core smoke coverage exists, including `scripts/ws-smoke.ps1` validation for `read_run_events`. Runtime unit automation covers subagent success, cancel, process-pool reuse, and process subagent failure events. Desktop unit-level automation covers provider profile normalization/payloads, `run.start` option construction, and WebSocket reconnect resume cursor behavior. Browser-level Playwright fixture automation covers restore, permissions, tool cards, subagents, Activity timeline event filters, grouped summaries, and payload inspection through `npm run test:ui`. Gateway-backed Playwright e2e coverage starts the real Gateway and Agent Runtime, drives `/read README.md` from the browser over HTTP/WebSocket, verifies the rendered message, tool card, Activity timeline, inactive provider profile UI failure, denied permission/tool rendering, pending permission run cancellation, `runtime_process` subagent startup failure visibility, running subagent cancellation, reconnect/resume after a missed permission approval event stream, and process leak assertions during teardown. Protocol compatibility checks cover run lifecycle, replay ordering, malformed WebSocket payload handling, permission approve/deny resolution, failed tool events/projections, failed subagent events/timeline, provider profile selection, inactive provider profile failure handling, and persisted timeline query shape.

Current validation matrix:

| Area | Coverage | Command |
| --- | --- | --- |
| WebSocket smoke and persisted event timeline | Existing smoke path, including `read_run_events` | `powershell -ExecutionPolicy Bypass -File scripts\ws-smoke.ps1` |
| HTTP/WebSocket protocol compatibility | Lightweight compatibility checks for run lifecycle, replay ordering, malformed payload recovery, permission approve/deny resolution, failed tool events/projections, failed subagent events/timeline, provider profile selection, inactive provider profile failures, persisted timeline query shape, and session fork/compact APIs | `powershell -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1` |
| Desktop unit reducers/components | Provider profile normalization/payloads, `run.start` option construction, and reconnect resume cursor behavior | `npm test` in `modules\desktop\frontend` |
| Desktop Playwright UI fixtures | Restore, permission approve/deny states, failed/completed tool cards, subagents, Activity timeline event kind/agent filters, grouped summaries, and expandable payload inspection | `npm run test:ui` in `modules\desktop\frontend` |
| Gateway-backed browser e2e | Starts real `bin\red-panda-gateway.exe` / `bin\red-panda-agent.exe` on an isolated test port, points Vite at Gateway through the dev proxy, sends `/read README.md`, verifies message/tool/timeline, verifies inactive provider profile UI failure, verifies denied permission/tool rendering, verifies pending permission cancel, verifies `runtime_process` subagent startup failure visibility, verifies running subagent cancel, verifies reconnect/resume after a missed permission approval stream, and asserts no newly created Gateway/Agent processes remain after teardown | `npm run test:ui -- --grep @gateway-backed` in `modules\desktop\frontend` |

## 9. Post-MVP Enhancements

P1:

1. Implement memory/history backend from `docs/14-memory-history-design.md`.
2. Do not implement MCP before memory/history backend/UI is complete or explicitly deferred.
3. Keep fixture-level UI automation current for restore, permissions, tool cards, subagents, and Activity timeline behavior as those surfaces evolve.

P2:

1. MCP stdio tools.
2. Context compaction.
3. Memory/history improvements.
4. Session fork/compact.

P3:

1. External WebSocket token scopes.
2. Plugin/skill/hook support.
3. Auto update.
4. Crash reports.
5. Remote access mode.
6. Multi-workspace windows.
7. Full packaging and signing.

## 10. Risks and Controls

| Risk | Control |
| --- | --- |
| WebSocket protocol expands too early | Keep MVP methods small and envelope extensible |
| Agent stdout gets polluted by logs | stdout is JSON-RPC only; logs go to stderr |
| SQLite write contention | WAL + busy timeout; add single-writer queue if needed |
| replay/live duplicate events | De-duplicate by `root_run_id/root_seq` |
| Desktop state drift | Gateway is authoritative; Desktop stores UI state and last seen seq |
| Subagent event mixing | Require `root_seq`, `agent_seq`, and stream metadata |
| TypeScript accidentally introduced | Desktop remains JavaScript-only |
| cgo SQLite accidentally introduced | Use `github.com/glebarez/sqlite` and verify `CGO_ENABLED=0` |
