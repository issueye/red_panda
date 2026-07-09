# Memory Runtime Tools Design

Updated: 2026-07-09

This document defines the optional `memory.*` Runtime tool slice. It intentionally stays smaller than MCP and must preserve the existing Gateway-owned memory persistence model.

## 1. Decision

Implement `memory.*` tools before MCP stdio tools, but do not let Agent Runtime write memory storage directly.

Reason:

- Gateway already owns memory persistence, validation, filtering, soft delete, and Desktop inspection.
- Runtime tools already have permission and tool event flows, but Runtime has no repository access.
- Calling Gateway HTTP from Runtime would create a circular local network dependency and leak auth/process configuration into Runtime.
- A Gateway-mediated internal JSON-RPC call keeps the existing stdio discipline: Runtime stdout remains JSON-RPC only, and Gateway remains the state owner.

## 2. Goals

Required outcomes:

1. Provider/tool loop can call `memory.list`, `memory.create`, `memory.update`, and `memory.delete`.
2. Memory writes go through Gateway service validation and persistence.
3. High-risk memory mutations use the existing permission path before execution.
4. Tool events remain normal `tool_started`, `tool_finished`, and `tool_failed`.
5. Desktop can inspect resulting memory records through the existing Memory tab.

Non-goals:

- MCP integration.
- Vector search.
- Hidden automatic memory writes.
- Runtime direct SQLite access.
- Runtime direct Gateway HTTP access.

## 3. Tool Contract

| Tool | Risk | Purpose | Permission |
| --- | --- | --- | --- |
| `memory.list` | medium | Read active/filtered memory records visible to the current run. | Requires permission in strict mode; allowed in permissive mode. |
| `memory.create` | high | Create a user/agent-sourced memory record. | Requires permission unless explicitly allowlisted. |
| `memory.update` | high | Update title/content/status/confidence/kind. | Requires permission unless explicitly allowlisted. |
| `memory.delete` | high | Soft-delete a memory record. | Requires permission unless explicitly allowlisted. |

Initial arguments:

```text
memory.list
  scope          optional project|session
  status         optional active|disabled|deleted|all
  limit          optional integer

memory.create
  scope          required project|session
  kind           optional fact|preference|decision|task|summary|warning
  title          optional
  content        required
  confidence     optional low|medium|high

memory.update
  id             required
  kind           optional
  status         optional active|disabled
  title          optional
  content        optional
  confidence     optional low|medium|high

memory.delete
  id             required
```

Scope rules:

- `project` uses the run workspace root.
- `session` uses the run session id.
- `user` and `system` are not exposed through Runtime tools in the first slice.
- Agent-created records must include `run_id` and `source = agent`.

## 4. Gateway-Mediated Runtime Call

Add a new internal stdio JSON-RPC method from Runtime to Gateway:

```text
memory.tool.execute
```

Runtime sends this as a request over the same Runtime stdout JSON-RPC stream it already uses for `agent.event`.

Request:

```json
{
  "run_id": "run_...",
  "session_id": "session_...",
  "workspace_root": "D:\\workspace",
  "tool_call_id": "tool_...",
  "tool_name": "memory.create",
  "arguments": {}
}
```

Response:

```json
{
  "status": "completed",
  "output": "{...human readable or compact JSON...}",
  "record_id": "mem_..."
}
```

Error responses use normal JSON-RPC errors; Runtime converts them to `tool_failed`.

Why this direction:

- Runtime remains responsible for provider loop, tool policy, permission gating, and tool events.
- Gateway remains responsible for memory repository/service rules.
- No extra HTTP listener/token/base URL has to be injected into Runtime.
- The existing Runtime client already reads Runtime stdout and can dispatch non-event requests.

## 5. Event and Audit Behavior

Runtime keeps existing event sequence:

1. `tool_started`
2. optional `permission_required`
3. `tool_finished` or `tool_failed`

Gateway memory service writes the record. Runtime tool result output should include:

- action,
- memory id,
- scope,
- status,
- title,
- compact content preview.

Optional explicit memory events can be added later:

- `memory_created`
- `memory_updated`
- `memory_deleted`

Do not add them in the first tool slice unless a test requires a distinct Activity filter beyond tool audit and Memory tab inspection.

## 6. Implementation Order

1. Protocol:
   - add method constant `MemoryToolExecute`,
   - add request/result DTOs in `modules/protocol/methods`.
2. Gateway runtime client:
   - dispatch Runtime-originated JSON-RPC requests,
   - implement handler for `memory.tool.execute`,
   - call `MemoryService`.
3. Runtime ToolRunner:
   - add definitions for `memory.list/create/update/delete`,
   - mark risks low/high,
   - call a `MemoryToolExecutor` callback instead of local filesystem logic.
4. Runtime:
   - carry the callback into `ToolRunner`,
   - preserve existing permission and tool event flow.
5. Tests:
   - Runtime tool definitions and policy risk,
   - Gateway memory tool handler service validation,
   - end-to-end Runtime/Gateway unit or protocol script coverage.
6. Docs/status update.

## 7. Verification

Required before claiming complete:

```powershell
go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1
```

Add focused tests for:

- `memory.list` returns only records visible to the current session/workspace.
- `memory.create` requires high-risk permission under `risk_based`.
- denied memory mutation produces `tool_failed` and no memory record.
- approved/allowlisted memory mutation creates/updates/deletes via Gateway memory service.

## 8. Stop Rules

- Do not let Runtime import Gateway repositories.
- Do not call Gateway HTTP from Runtime.
- Do not let memory writes bypass permission policy.
- Do not hide tool-created memory from the Desktop Memory tab.
- Do not start MCP implementation in the same slice.
