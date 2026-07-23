# MCP stdio Tools Design

Updated: 2026-07-23

Status: implemented for config CRUD, discovery, tools/call, process reuse, discovery cache, protocol cancel, and Desktop try-call; see §2.1.

This document defines the MCP stdio tool boundary for a later implementation slice. It preserves the v0.1.0 architecture:

```text
Desktop(Wails v3 + React) <-> Gateway(Gin/GORM/SQLite no-cgo) <-> Agent Runtime(stdio JSON-RPC)
```

## 1. Decision

MCP stdio processes are owned by Agent Runtime.

Gateway owns only durable MCP configuration, validation, API exposure, and handoff of enabled configuration to Runtime. Gateway must not start MCP server processes, execute MCP tool calls, parse MCP stdout, or translate MCP tool results into tool outputs itself.

The Runtime remains the only layer that owns provider calls, tool registry, tool execution, permission checks, cancellation, and child process IO isolation. MCP tools must enter the same Runtime execution path as built-in tools and memory tools.

Design consequence:

- Gateway persists and passes `mcp_servers` config.
- Runtime lazily starts enabled MCP stdio servers when their tools are needed or when discovery is requested.
- Runtime exposes discovered MCP tools to providers through the existing tool definition flow.
- Runtime emits existing `tool_started`, `tool_output`, `tool_finished`, and `tool_failed` events.
- Gateway receives ordinary Runtime `agent.event` notifications and updates existing projections.

## 2. Scope

v0.1.1 scope:

1. Define the config contract for MCP stdio servers.
2. Define Runtime process lifecycle, initialization, tool discovery, calls, shutdown, crash handling, timeout, and cancellation.
3. Define how MCP tools are registered, named, and converted to provider-facing tool names.
4. Define how MCP tools reuse existing permission policy and events.
5. Define stdout/stderr isolation rules and fake server test coverage.
6. Define implementation order for later slices.

## 2.1 Implementation status (2026-07-20)

The design below is now partially realized in code. See [docs/10-development-status.md](10-development-status.md) §MCP for the authoritative truth.

**Implemented (v0.2.0):**

- Gateway config CRUD + validation + masked env + `/api/v1/mcp/servers/:id/discover` endpoint.
- `run.start` injects enabled server configs into `RunExecuteOptions.MCPServers` with plaintext secrets (Gateway-side only).
- Runtime lazily spawns the stdio child process, performs `initialize` + notifications/initialized, paginates `tools/list`, and registers discovered tools under canonical names (`mcp__server__tool`) into the provider-facing tool table for the run.
- **Process reuse:** one long-lived stdio session per server config identity (name/command/args/cwd/env/workspace) is shared across `tools/list` and `tools/call` for the Runtime process lifetime; per-server mutex serializes concurrent calls; transport failures drop the session and the next call rebuilds it.
- **Discovery cache:** successful `tools/list` results are cached per config identity for the Manager lifetime so subsequent runs skip re-list when config is unchanged.
- `tools/call` uses the pooled session with start/initialize/call timeouts; multi-content text concatenation; 64KB max output truncation; bounded (8KB) stderr drain; secret redaction in diagnostics.
- MCP tools default to `RiskHigh` and flow through the existing ToolRunner policy / permission / event pipeline (`tool_started` / `tool_output` / `tool_finished` / `tool_failed`). Per-server allowlists and risk overrides apply.
- `core.shutdown` and Runtime `Close` close all MCP child processes; end-to-end integration tests (`mcp_tools_integration_test.go`) cover success / timeout / `isError` / reuse / discovery-cache paths.
- Crash/restart budget (§7.7): Manager tracks startup-phase failures per server and auto-disables a server after the configured threshold. `tools/call` business failures are not counted.

**Implemented (2026-07-23):** protocol-level cancellation — when a call context is cancelled (run cancel or call timeout), Runtime sends `notifications/cancelled` with the in-flight `requestId` on the MCP stdio session, and still unblocks via context cancel if the server ignores the notification.

**Implemented (2026-07-23):** Desktop Settings try-call UI — discovery panel exposes per-tool「试调用」with JSON arguments; Gateway `POST /api/v1/mcp/servers/:id/call` → Runtime `mcp.call` → `Manager.CallTool` (no chat run).

## 3. Non-goals

v0.1.1 does not implement MCP.

Explicit non-goals:

- No full MCP stdio runtime implementation.
- No HTTP MCP transport.
- No plugin marketplace.
- No broad Desktop UI redesign.
- No Gateway-owned MCP process execution.
- No ToolRunner rewrite before this design is accepted.
- No remote access hardening.
- No claim that MCP stdio support is complete.

## 4. Current Boundaries

### 4.1 Runtime stdio JSON-RPC

Gateway and Runtime communicate through newline-delimited JSON-RPC 2.0 over Runtime stdin/stdout. Runtime stdout must contain only JSON-RPC responses, requests, or notifications. Runtime emits run events as `agent.event` notifications whose params are `events.Envelope`.

Current Runtime methods include:

- `core.initialize`
- `core.ping`
- `core.shutdown`
- `agent.reply`
- `agent.cancel`
- `agent.tools`
- `agent.subagents`
- `agent.subagent.cancel`
- `permission.resolve`
- Runtime-to-Gateway request: `memory.tool.execute`

MCP must not add non-JSON lines to Runtime stdout. Runtime stderr remains for Runtime diagnostics only.

### 4.2 ToolRunner

Current tools are represented by:

- `tools.Definition`
- `tools.Call`
- `tools.Result`
- `ToolRunner.AvailableTools()`
- `ToolRunner.InvocationFromCall(...)`
- `ToolRunner.RunWithContext(...)`

Built-in tools include workspace, shell, and memory tools. Memory tools already show the intended split: Runtime owns the model-visible tool and permission flow, while Gateway performs persistence through an internal JSON-RPC request.

MCP tools should be modeled as another tool source behind a registry. The later implementation should avoid making provider code know whether a tool is built-in, memory-backed, or MCP-backed.

### 4.3 Subagent process and process pool

Runtime already has process-owned subagents for `runtime_process` and `process_pool`. These provide useful process-management patterns:

- `exec.CommandContext`
- stdin/stdout/stderr pipes
- JSON-RPC request/response correlation
- stdout reader goroutine
- stderr drain goroutine
- process wait cleanup
- context cancellation
- fixed request timeout

MCP stdio process management should use similar isolation rules, but it is a separate client because MCP protocol methods and lifecycle differ from child Runtime protocol methods.

### 4.4 Gateway projections

Gateway currently persists and projects Runtime events:

- `run_events` stores `events.Envelope` by `(root_run_id, root_seq)`.
- `tool_calls` is projected from `tool_started`, `tool_output`, `tool_finished`, and `tool_failed`.
- `permission_requests` is projected from `permission_required`.
- Activity timeline reads the persisted event stream and projections.

MCP tools must reuse this surface. Gateway should not need a separate MCP tool audit stream for v0.1.1 design.

## 5. MCP Config Contract

Gateway persists MCP server configs and passes enabled configs to Runtime during run start or Runtime initialization.

Recommended protocol DTO names for a later slice:

```go
type MCPServerConfig struct {
    Name          string            `json:"name"`
    Command       string            `json:"command"`
    Args          []string          `json:"args,omitempty"`
    Env           map[string]string `json:"env,omitempty"`
    CWD           string            `json:"cwd,omitempty"`
    Enabled       bool              `json:"enabled"`
    Timeouts      MCPTimeouts       `json:"timeouts,omitempty"`
    ToolAllowlist []string          `json:"tool_allowlist,omitempty"`
    RiskOverrides map[string]string `json:"risk_overrides,omitempty"`
}

type MCPTimeouts struct {
    StartMS      int `json:"start_ms,omitempty"`
    InitializeMS int `json:"initialize_ms,omitempty"`
    ListMS       int `json:"list_ms,omitempty"`
    CallMS       int `json:"call_ms,omitempty"`
    ShutdownMS   int `json:"shutdown_ms,omitempty"`
}
```

Field rules:

| Field | Owner | Rule |
| --- | --- | --- |
| `name` | Gateway validates, Runtime consumes | Required stable identifier. Lowercase ASCII recommended. Must not contain `__`, whitespace, path separators, or shell metacharacters. |
| `command` | Gateway persists, Runtime executes | Required executable path or command name. Runtime must execute without shell interpolation. |
| `args` | Gateway persists, Runtime executes | Optional argv list. Runtime passes as argv, not a joined shell string. |
| `env` | Gateway persists, Runtime merges | Optional extra environment. Runtime should merge onto inherited safe env or configured base env. Secrets must not be emitted in events. |
| `cwd` | Gateway persists, Runtime validates/uses | Optional working directory. Empty means run workspace root or Runtime default. |
| `enabled` | Gateway persists and filters | Disabled servers are not sent as active tools unless explicit diagnostics require it. |
| `timeouts` | Gateway persists, Runtime enforces | Optional per-phase limits with Runtime defaults. |
| `tool_allowlist` | Gateway persists, Runtime enforces | Optional allowlist of raw MCP tool names for that server before registration. |
| `risk_overrides` | Gateway persists, Runtime applies | Optional map from raw MCP tool name to `low`, `medium`, or `high`. |

Default timeout recommendations:

| Timeout | Default |
| --- | --- |
| `start_ms` | 10000 |
| `initialize_ms` | 10000 |
| `list_ms` | 10000 |
| `call_ms` | 30000 |
| `shutdown_ms` | 3000 |

Gateway API shape is intentionally deferred, but persistence should support create, update, delete, list, and enable/disable without requiring Desktop MCP UI in the first implementation slice.

## 6. Runtime Handoff Contract

Runtime must receive MCP configs as protocol data, not by reading Gateway database files.

Preferred later protocol extension:

```go
type ReplyOptions struct {
    // existing fields...
    MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`
}
```

Alternative for long-lived Runtime initialization:

```go
type InitializeParams struct {
    // existing fields...
    MCPServers []MCPServerConfig `json:"mcp_servers,omitempty"`
}
```

Recommended rule:

- `core.initialize` may provide baseline enabled MCP configs for `single_core`.
- `agent.reply` may provide per-run MCP config snapshot.
- Runtime should treat the per-run snapshot as authoritative for that run.
- Gateway should pass only enabled configs to normal runs.

## 7. Process Lifecycle

### 7.1 Lazy start

Runtime should not eagerly start every configured MCP server at Runtime boot.

Startup triggers:

1. Tool discovery is needed for provider `tools`.
2. A provider calls a tool belonging to that server.
3. A diagnostic endpoint explicitly asks Runtime to check MCP server status.

For the first implementation, discovery before provider call is the normal trigger because providers need tool schemas.

### 7.2 Start

Runtime starts the MCP server with:

- `exec.CommandContext` or equivalent direct process launch.
- `command` as executable.
- `args` as argv.
- configured `cwd`.
- configured `env` merged with allowed base env.
- stdin pipe for MCP requests.
- stdout pipe for MCP protocol responses/notifications.
- stderr pipe drained to a bounded log buffer.

Runtime must not start MCP through `shell.exec`, PowerShell, `cmd /c`, or `sh -c`.

### 7.3 Initialize

After process start, Runtime sends MCP `initialize` over the MCP server stdin and waits for a valid JSON-RPC response within `initialize_ms`.

The exact MCP initialize payload should be implemented from the accepted MCP spec in the implementation slice. This document fixes ownership and lifecycle, not the wire version.

After a successful response, Runtime may send any required initialized notification if the MCP spec version requires it.

Failure to initialize marks the server unavailable and fails discovery or tool calls with a structured error.

### 7.4 tools/list

Runtime sends MCP `tools/list` after initialization and maps each returned tool into `tools.Definition`.

Rules:

- Apply `tool_allowlist` to raw MCP tool names before registration.
- Apply `risk_overrides` after allowlist.
- Default risk for MCP tools should be `high` unless a future trusted metadata contract is accepted.
- Preserve raw MCP schema where it is JSON Schema compatible.
- Reject or degrade invalid schemas instead of passing malformed provider tool schemas.
- Cache discovered tools per Runtime MCP server instance.

Discovery failures should not crash Runtime. The server should be marked failed/unavailable and provider-visible tools for that server should be omitted unless a call was already in progress.

### 7.5 tools/call

When a provider calls an MCP tool:

1. Runtime maps provider-facing name back to `(server_name, raw_tool_name)`.
2. Runtime builds a `tools.Call`.
3. Runtime runs `EvaluateToolPolicy`.
4. Runtime emits `tool_started`.
5. Runtime requests permission if required.
6. Runtime sends MCP `tools/call`.
7. Runtime converts MCP result into `tools.Result`.
8. Runtime emits `tool_output` and terminal `tool_finished` or `tool_failed`.

MCP tool calls must be serialized per MCP server until the implementation explicitly supports concurrent calls and proves request correlation, cancellation, and output ordering. Different MCP servers may run concurrently.

### 7.6 Shutdown

Runtime shutdown must close MCP servers:

1. Stop accepting new MCP calls.
2. Cancel active MCP call contexts.
3. Send MCP shutdown if the server is responsive.
4. Close stdin.
5. Wait up to `shutdown_ms`.
6. Kill the process if still alive.
7. Close pending requests with an error.

`core.shutdown`, Runtime process exit, and per-run Runtime cleanup must all trigger MCP cleanup for owned servers.

### 7.7 Crash and restart

If an MCP process exits unexpectedly:

- Mark server state as crashed.
- Close pending MCP calls as failed.
- Emit `tool_failed` for each affected active tool call.
- Keep Runtime alive.
- Do not write crash text to Runtime stdout.

Restart policy:

- Restart lazily on the next discovery or call.
- Use a bounded restart budget per server, for example 3 crashes in 60 seconds.
- After budget exhaustion, mark the server disabled for the Runtime process lifetime or until config changes.
- Surface failures through ordinary tool errors and optional diagnostics, not a separate required UI.

**Implementation status (2026-07-20):** the budget is enforced by `modules/agent/internal/mcp` on the Runtime-side Manager. Only **startup-phase** failures count toward the threshold — spawn / open / start-timeout / initialize failures (i.e. errors without the `tools/call failed:` prefix). Business `tools/call` failures (timeouts, `isError`, stderr) do **not** count, because they indicate a tool problem rather than a broken server. State lives on the Manager (one Runtime process); it is keyed by `MCPServerConfig.Name` and shared across runs, so a server that crashes in one run stays disabled in the next. Defaults: 3 failures within a 60s sliding window. Override via `RED_PANDA_MCP_CRASH_LIMIT` and `RED_PANDA_MCP_CRASH_WINDOW_MS`; set `RED_PANDA_MCP_CRASH_LIMIT=0` to turn the feature off entirely (escape hatch). Disabled servers are hidden from `PrepareToolsForRun` (no new spawn) and rejected at `ExecuteTool` with a `disabled by crash budget` error. The "until config changes" recovery path is not yet wired — currently the budget only resets when the Runtime process restarts.

### 7.8 Timeout and cancel

Timeouts:

- `start_ms` covers process start.
- `initialize_ms` covers MCP initialize.
- `list_ms` covers `tools/list`.
- `call_ms` covers `tools/call`.
- `shutdown_ms` covers graceful shutdown.

Cancellation:

- `run.cancel` / `agent.cancel` for a root run cancels the run context, which cancels active MCP calls for that run.
- Runtime sends MCP `notifications/cancelled` with the in-flight `requestId` and reason when the call context ends (run cancel or call timeout). Implemented in `mcpkit.Session.sendRPC` / `NotifyCancelled`.
- Runtime also cancels the local context so the caller unblocks even if the MCP server ignores cancellation.
- Transport-level failure after cancel drops the pooled session so the next call can rebuild it; ordinary business `isError` results keep the process.

Tool timeout result:

- Status: `failed`.
- Error: timeout reason with phase and configured limit.
- Event: `tool_failed`.
- Output: partial output only if it was already safely captured as MCP content, never raw protocol bytes.

## 8. Tool Registry Contract

### 8.1 Internal names

Runtime internal MCP tool names use:

```text
mcp__server__tool
```

Examples:

```text
mcp__filesystem__read_file
mcp__github__create_issue
```

`server` is the sanitized `MCPServerConfig.Name`.

`tool` is a sanitized MCP raw tool name. The registry must store a reversible mapping from the sanitized name to the raw MCP tool name. Do not rely on lossy string cleanup alone.

### 8.2 Provider-facing names

Providers may reject dots or other punctuation in tool names. Runtime already needs provider-compatible names for built-in tools. MCP naming should follow the same provider conversion layer:

| Runtime tool name | Provider-facing name |
| --- | --- |
| `workspace.read_file` | `workspace__read_file` |
| `mcp__filesystem__read_file` | `mcp__filesystem__read_file` |

Provider response handling must map provider-facing names back to Runtime tool names before registry lookup.

Contract:

- Registry key: Runtime canonical name.
- Provider name: OpenAI-compatible safe name.
- Reverse lookup: provider name to Runtime canonical name.
- MCP call target: Runtime canonical name to `(server_name, raw_tool_name)`.

Name conflicts:

- Built-in names have priority.
- Duplicate MCP canonical names are rejected at registration.
- Duplicate provider-facing names are rejected at registration.
- Rejected tools are omitted and logged in diagnostics.

### 8.3 Schema mapping

MCP `tools/list` input schema maps to `tools.Definition.Parameters`.

Rules:

- If schema is an object JSON Schema, pass it through after validation.
- If schema is missing, use an empty object schema.
- If schema is invalid, omit the tool or expose it with a no-argument schema only if that behavior is explicitly accepted in implementation review.
- Description maps to `tools.Definition.Description`.
- Display name should default to MCP raw tool name or a humanized form.
- Risk defaults to `high` unless overridden by config.

### 8.4 Registry shape

Recommended internal interface:

```go
type ToolRegistry interface {
    AvailableTools(ctx context.Context, runCtx ToolRunContext) []tools.Definition
    InvocationFromCall(runID string, index int, call tools.Call) (ToolInvocation, error)
    RunWithContext(ctx context.Context, runCtx ToolRunContext, invocation ToolInvocation) (tools.Result, string)
}
```

Implementation may wrap existing `ToolRunner` first:

- Built-in provider: current `ToolRunner`.
- MCP provider: Runtime-owned MCP manager.
- Registry: combines definitions and dispatches by canonical name.

## 9. Permission and Security

MCP tools must reuse `EvaluateToolPolicy` and the existing permission flow.

Rules:

- No MCP `tools/call` may occur before `EvaluateToolPolicy`.
- `tool_allowlist` and `tool_denylist` in `ReplyOptions` apply to canonical names such as `mcp__server__tool`.
- Per-server `tool_allowlist` applies to raw MCP tool names before Runtime registration.
- `risk_overrides` influences `tools.Call.Risk` and `tools.Definition.Risk`.
- Default MCP risk is `high`.
- Permission cards and persisted records use canonical MCP tool name.
- Permission arguments are the provider/MCP tool arguments after parsing, with secrets redacted if known.

Security boundaries:

- Gateway must not execute configured commands.
- Runtime must launch commands directly without shell interpolation.
- Runtime must not emit configured env values or secrets in events.
- MCP stderr is diagnostic only and bounded.
- MCP stdout is protocol input only and never a user-visible stream by default.
- MCP tools inherit run cancellation and workspace context.
- MCP tools must not bypass workspace/path policy if a future sandbox layer is added.

Risk override example:

```json
{
  "name": "filesystem",
  "command": "mcp-filesystem",
  "enabled": true,
  "tool_allowlist": ["read_file", "list"],
  "risk_overrides": {
    "read_file": "low",
    "list": "low"
  }
}
```

## 10. IO Isolation

### 10.1 Runtime stdout

Runtime stdout remains JSON-RPC only.

Allowed Runtime stdout lines:

- JSON-RPC response.
- JSON-RPC request to Gateway, such as `memory.tool.execute`.
- JSON-RPC notification, such as `agent.event`.

Forbidden Runtime stdout lines:

- MCP server stdout.
- MCP stderr logs.
- Tool raw output.
- Human-readable diagnostics.
- Non-JSON log lines.

### 10.2 MCP stdout

MCP stdout is consumed only by the Runtime MCP protocol reader.

Rules:

- Each MCP stdout line or frame must be parsed according to MCP transport rules.
- Valid MCP JSON-RPC responses are routed to pending MCP requests.
- Valid MCP notifications are handled only if the implementation supports them.
- Non-JSON MCP stdout is a protocol violation.
- Non-JSON MCP stdout must not be forwarded to Runtime stdout, Gateway, or Desktop as ordinary tool output.

Protocol violation handling:

- Mark server unhealthy.
- Fail active discovery or tool call.
- Capture a bounded diagnostic summary, for example first 4 KB.
- Emit `tool_failed` if a tool call was active.
- Consider process kill/restart according to restart policy.

### 10.3 MCP stderr

MCP stderr is drained continuously to avoid blocking the child process.

Rules:

- Drain to bounded ring buffer or structured Runtime diagnostics.
- Do not write MCP stderr to Runtime stdout.
- Do not include full stderr in normal tool output.
- Terminal tool failure may include a short sanitized error summary.
- Never expose environment variables or secrets from stderr without redaction.

### 10.4 Tool output

Only MCP `tools/call` result content becomes `tool_output` or `tools.Result.Output`.

Raw MCP protocol bytes are not tool output.

For multi-content MCP results, Runtime should produce a deterministic text representation for v0.1.x:

- Text content joins with newline.
- Non-text content is summarized as unsupported content unless a later design adds attachment support.
- Large output follows existing tool output truncation limits or a new documented MCP limit.

## 11. Events and Persistence

MCP tools reuse the existing `tool_*` event envelope.

### 11.1 tool_started

Payload:

```json
{
  "tool_call_id": "tool_run_1_model_1",
  "tool_name": "mcp__filesystem__read_file",
  "display_name": "filesystem.read_file",
  "risk": "high",
  "arguments": { "path": "README.md" },
  "status": "running",
  "policy": "require_permission",
  "policy_reason": "strict permission mode",
  "mcp": {
    "server": "filesystem",
    "tool": "read_file"
  }
}
```

The `mcp` object is optional metadata. Existing Gateway projections should continue to work because they already read `tool_call_id`, `tool_name`, `display_name`, `risk`, `arguments`, `status`, `policy`, and `policy_reason`.

### 11.2 permission_required

MCP tools use existing `permission.RequestPayload` fields:

- `permission_id`
- `run_id`
- `tool_call_id`
- `tool_name`
- `risk`
- `summary`
- `detail`
- `arguments`

No MCP-specific permission channel is needed.

### 11.3 tool_output

Payload:

```json
{
  "tool_call_id": "tool_run_1_model_1",
  "tool_name": "mcp__filesystem__read_file",
  "delta": "file contents..."
}
```

Stream:

```json
{
  "stream_id": "stream_tool_run_1_model_1",
  "kind": "tool_stdout",
  "seq": 1,
  "final": true
}
```

For MCP tool failures, use `tool_stderr` only for a sanitized failure message, not raw MCP stderr.

### 11.4 tool_finished and tool_failed

Payload continues to match `tools.Result`:

```json
{
  "tool_call_id": "tool_run_1_model_1",
  "tool_name": "mcp__filesystem__read_file",
  "status": "completed",
  "exit_code": 0,
  "output": "file contents...",
  "error": "",
  "duration_ms": 12
}
```

Gateway `ToolCallRepository` should project MCP tools without a schema change in the first implementation. Later diagnostics can add MCP-specific tables, but they are not required for Activity timeline or tool audit.

### 11.5 Activity timeline

Desktop Activity timeline should see MCP tools through existing run events:

- tool start
- permission request
- tool output
- tool finish/failure

No Desktop MCP settings UI is required until backend contracts and config CRUD are implemented.

## 12. Gateway Persistence Design

Gateway should add MCP config persistence in a later implementation slice, likely as a new model/table independent from run events.

Recommended durable fields:

| Column | Notes |
| --- | --- |
| `id` | Stable ID. |
| `name` | Unique server name. |
| `command` | Executable. |
| `args_json` | JSON array. |
| `env_json` | JSON object, secrets handled by the same policy as provider secrets if needed. |
| `cwd` | Optional working directory. |
| `enabled` | Boolean. |
| `timeouts_json` | JSON object. |
| `tool_allowlist_json` | JSON array of raw MCP tool names. |
| `risk_overrides_json` | JSON object. |
| `created_at` | Timestamp. |
| `updated_at` | Timestamp. |

Gateway responsibilities:

- Validate config shape.
- Persist config.
- Return masked/redacted values where needed.
- Pass enabled configs to Runtime as DTOs.
- Keep tool projections generic.

Gateway non-responsibilities:

- Starting MCP processes.
- Calling `tools/list`.
- Calling `tools/call`.
- Parsing MCP stdout.
- Retrying crashed MCP servers.

## 13. Implementation Order

MCP implementation remains gated behind this design review.

Recommended later slices:

1. Protocol DTOs
   - Add MCP config DTOs in `modules/protocol`.
   - Add `mcp_servers` to `ReplyOptions` or `InitializeParams`.
   - Add tests for JSON compatibility.

2. Gateway config CRUD
   - Add model, repository, service, and controller for MCP server configs.
   - Validate names, commands, timeouts, allowlists, and risk overrides.
   - Do not execute commands.

3. Runtime MCP stdio client skeleton
   - Start process, wire stdin/stdout/stderr, initialize, shutdown.
   - Use fake MCP servers in tests.
   - Prove Runtime stdout remains JSON-RPC only.

4. Runtime MCP discovery
   - Implement `tools/list`.
   - Map schemas to `tools.Definition`.
   - Apply per-server allowlist and risk overrides.
   - Cache and expose through registry.

5. Tool registry abstraction
   - Wrap existing built-ins.
   - Add MCP provider.
   - Implement canonical and provider-facing name mapping.
   - Preserve built-in tool behavior.

6. MCP tool calls
   - Dispatch `tools/call`.
   - Reuse `EvaluateToolPolicy`.
   - Emit existing tool events.
   - Implement timeout and cancellation.

7. Crash/restart and diagnostics
   - Add restart budget.
   - Add bounded stderr and protocol violation diagnostics.
   - Keep failures visible through tool events and status APIs.

8. Compatibility and UI
   - Update protocol compatibility script.
   - Add Gateway-backed tests.
   - Add Desktop settings only after backend contract is stable.

## 14. Tests

### 14.1 Unit tests

Protocol:

- MCP config JSON round-trip.
- Default timeout normalization.
- Invalid server names rejected.
- Invalid risk overrides rejected.

Runtime registry:

- Built-in and MCP tools combine in deterministic order.
- Duplicate canonical names rejected.
- Duplicate provider-facing names rejected.
- `mcp__server__tool` maps back to raw server/tool.
- Provider `workspace__read_file` still maps to built-in `workspace.read_file`.

Permission:

- MCP low/medium/high risk calls pass through `EvaluateToolPolicy`.
- Global `tool_allowlist` and `tool_denylist` apply to canonical MCP names.
- Per-server raw `tool_allowlist` filters discovery.
- Denied MCP tool emits `tool_started` then `tool_failed` with denied status.

### 14.2 Fake MCP server matrix

Fake server modes:

| Mode | Expected result |
| --- | --- |
| valid initialize + tools/list | Runtime discovers tools. |
| valid tools/call text result | Runtime emits `tool_output` and `tool_finished`. |
| stdout garbage before initialize | Runtime marks protocol violation and fails discovery. |
| stdout garbage during tools/call | Active tool fails, Runtime stdout stays JSON-RPC only. |
| stderr logs only | Runtime drains logs, discovery/call can still succeed. |
| large stderr | Ring buffer truncates, process does not block. |
| large tool result | Output is bounded/truncated according to tool output limits. |
| initialize timeout | Server unavailable, no process leak. |
| tools/list timeout | Discovery fails, Runtime stays alive. |
| tools/call timeout | Tool fails with timeout and emits `tool_failed`. |
| cancel during tools/call | Run cancel unblocks caller and emits cancelled/failed tool state. |
| process exits during call | Tool fails and pending request closes. |
| process exits while idle | Server marked crashed and restarts lazily within budget. |
| repeated crashes | Restart budget trips and server remains unavailable. |
| invalid schema | Tool omitted or schema degraded according to accepted implementation rule. |

### 14.3 Integration tests

Runtime:

- Start Runtime with fake MCP config and call `agent.tools`.
- Start run with provider fixture that calls an MCP tool.
- Confirm event sequence: `tool_started`, optional `permission_required`, `tool_output`, `tool_finished`.
- Confirm `agent.cancel` cancels active MCP call.
- Confirm `core.shutdown` cleans MCP child process.

Gateway:

- Config CRUD persists and redacts correctly.
- Enabled configs are passed to Runtime.
- Disabled configs are not passed to normal runs.
- MCP tool events update existing `tool_calls` projection.
- Activity timeline includes MCP tool events without schema-specific code.

End-to-end:

- Gateway-backed fake MCP run appears in Desktop tool cards and Activity timeline.
- Permission approval/denial works for MCP tools.
- Reconnect/resume replays MCP tool events by `root_seq`.

### 14.4 IO isolation tests

Required assertions:

- Runtime stdout contains only valid JSON-RPC lines while MCP server writes stderr.
- Runtime stdout contains only valid JSON-RPC lines while MCP server writes stdout garbage.
- MCP stderr never appears in `tool_output`.
- MCP stdout protocol garbage never appears in `tool_output`.
- Runtime process does not hang when MCP stderr is large.
- No MCP child processes remain after test teardown.

### 14.5 Test anti-patterns

Avoid these patterns in MCP tests:

- Do not assert exact total event counts in UI or protocol tests. MCP discovery and diagnostics may add events later without changing user-visible correctness.
- Do not rely on fixed sleeps. Wait for JSON-RPC responses, process exits, API state, WebSocket events, or UI state.
- Do not assert full error strings. Assert stable status, tool name, error code if present, and short key phrases.
- Do not test MCP protocol edge cases in Playwright. UI tests should cover user-visible tool cards, permission states, and Activity timeline only.
- Do not let fake MCP servers write real workspace files unless that specific behavior is under test.
- Do not depend on stderr/stdout ordering. Assert that stderr is drained and Runtime stdout remains JSON-RPC only.
- Do not use a real provider to trigger MCP tests. Use deterministic fake providers or direct Runtime calls.

## 15. Stop Rules

Pause implementation if any design or code path would:

- Make Gateway execute MCP tools.
- Let MCP stdout or stderr reach Runtime stdout.
- Let MCP tools bypass `EvaluateToolPolicy`.
- Bypass existing permission requests.
- Create MCP-specific tool audit that does not feed existing tool projections.
- Require broad Desktop UI changes before backend contracts are stable.
- Depend on arbitrary sleeps instead of process state, JSON-RPC responses, or event observations.

## 16. Open Questions for Implementation Review

These are intentionally deferred:

1. Exact MCP protocol version and initialize payload.
2. Whether MCP servers are shared across runs in `single_core` or scoped per run by config snapshot.
3. Whether concurrent `tools/call` per server is allowed in the first implementation.
4. How to persist and redact MCP env secrets if configs include credentials.
5. Whether non-text MCP content should become attachments in a later Desktop design.

None of these questions changes the primary decision: Agent Runtime owns MCP stdio processes; Gateway persists and passes configuration only.
