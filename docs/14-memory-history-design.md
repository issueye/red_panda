# Memory and History Design

Updated: 2026-07-09

This is the M3 design document for memory/history. Backend implementation should follow this contract and should not expand into MCP or plugin work.

## 1. Goals

Memory/history should make long-running agent work more useful without making behavior opaque.

Required outcomes:

1. Memory records are persisted and inspectable.
2. Memory records can be created, updated, disabled, and deleted.
3. Every memory record is linked to its source session/run when it comes from agent activity.
4. Runtime context injection is explicit and auditable.
5. User-provided workspace/session context always wins over memory.
6. Desktop can show what memory exists and remove it.

Non-goals for M3:

- Vector embeddings.
- MCP-backed memory stores.
- Cross-device sync.
- Automatic hidden memory writes.
- Personalization that cannot be inspected or deleted.

## 2. Current Baseline

Current persistence already includes:

- sessions,
- messages,
- run records,
- run events,
- tools,
- permissions,
- provider profiles,
- session lineage and compaction records.

Current Runtime input can receive:

- session id/name/working dir,
- user input,
- run options,
- provider profile override,
- tool policy and subagent options.

Memory should be added through Gateway-owned persistence first, then injected into Runtime through explicit `ReplyOptions` or a dedicated context field.

## 3. Memory Types

| Type | Scope | Example | Default retention |
| --- | --- | --- | --- |
| `project` | workspace root | Build commands, project conventions, repo-specific constraints | Until deleted |
| `session` | one session lineage | Current objective, open decisions, long task state | Until session lineage is deleted |
| `user` | local user profile | Preferred response style, recurring tool choices | Until deleted |
| `system` | app-generated operational hint | Provider limits, recurring failures | Time-limited |

M3 should implement `project` and `session` first. `user` can be added once Desktop inspection is solid. `system` should be conservative and disabled by default.

## 4. Record Model

Add `memory_records`:

```text
id
scope              project | session | user | system
kind               fact | preference | decision | task | summary | warning
status             active | disabled | deleted
title
content
confidence         low | medium | high
workspace_root
session_id
run_id
source_event_id
source_message_id
source             user | agent | compaction | import | system
metadata_json
created_at
updated_at
deleted_at
```

Rules:

- `content` is the human-readable value injected into context.
- `title` is short and used in Desktop lists.
- `workspace_root` is required for `project` memory.
- `session_id` is required for `session` memory.
- `run_id`, `source_event_id`, or `source_message_id` is required for agent-created memory.
- Deletion should be soft delete first, then optional purge later.

## 5. Memory Events

Use normal persisted run events for memory writes and deletions that happen during a run.

New event types:

| Event | Purpose |
| --- | --- |
| `memory_candidate` | Runtime or Gateway proposes memory; not yet persisted as active memory. |
| `memory_created` | Memory record persisted. |
| `memory_updated` | Memory content/status changed. |
| `memory_deleted` | Memory disabled/deleted. |
| `memory_injected` | Gateway injected memory into Runtime context for a run. |

If event type expansion is deferred, use `event` with `payload.kind = "memory_*"` only temporarily. The preferred contract is explicit event types.

## 6. API Contract

All APIs use the existing HTTP JSON envelope.

### 6.1 List Memory

```text
GET /api/v1/memory?scope=&workspace_root=&session_id=&status=&limit=
```

Response:

```json
[
  {
    "id": "mem_...",
    "scope": "project",
    "kind": "fact",
    "status": "active",
    "title": "Build command",
    "content": "Use npm run build in modules/desktop/frontend.",
    "confidence": "high",
    "workspace_root": "D:\\codes\\issueye\\ai_agents\\red_panda",
    "session_id": "",
    "run_id": "run_...",
    "source": "agent",
    "created_at": "...",
    "updated_at": "..."
  }
]
```

### 6.2 Create Memory

```text
POST /api/v1/memory
```

Request:

```json
{
  "scope": "project",
  "kind": "fact",
  "title": "Build command",
  "content": "Use npm run build in modules/desktop/frontend.",
  "confidence": "high",
  "workspace_root": "D:\\codes\\issueye\\ai_agents\\red_panda",
  "session_id": "session_...",
  "run_id": "run_...",
  "source": "user"
}
```

Rules:

- User-created memory can be active immediately.
- Agent-created memory should require either a visible event or a configured approval policy.
- Blank content is invalid.

### 6.3 Update Memory

```text
PUT /api/v1/memory/:id
```

Allowed fields:

- `scope`
- `kind`
- `status`
- `title`
- `content`
- `confidence`
- `metadata`

### 6.4 Delete Memory

```text
DELETE /api/v1/memory/:id
```

M3 delete means:

- set `status = deleted`,
- set `deleted_at`,
- exclude from injection and default lists.

### 6.5 Run Memory Preview

```text
POST /api/v1/memory/preview-run
```

Request:

```json
{
  "session_id": "session_...",
  "workspace_root": "D:\\codes\\issueye\\ai_agents\\red_panda",
  "input": "user prompt"
}
```

Response:

```json
{
  "items": [],
  "context": "Memory:\n- ..."
}
```

This lets Desktop show what would be injected before a run.

## 7. Runtime Injection Contract

Preferred first implementation:

- Gateway resolves memory records before `agent.reply`.
- Gateway sends memory text in `ReplyOptions.MemoryContext` or a new `ReplyContext.Memory`.
- Runtime prepends memory to provider request context as a separate system/context block.
- Runtime emits `memory_injected` with memory ids and count.

Injection order:

1. System/developer constraints.
2. Explicit current user input.
3. Current session recent history.
4. Workspace context selected by the user.
5. Memory context.

Memory must not override explicit user instructions. When conflict is detected, Runtime should prefer current user/session context and may emit a warning event.

Initial selection rules:

- Active `session` memory for current session.
- Active `project` memory for current workspace root.
- Limit to 10 records or 4000 characters.
- Sort by scope priority, updated time, confidence.

## 8. Desktop Behavior

Add a Memory view after backend exists:

- list project/session memories,
- filter by scope/status,
- inspect content and source run/session,
- create user/project memory manually,
- edit title/content,
- disable/delete memory,
- preview memory selected for the next run.

Do not hide memory in Settings only. It should be inspectable near Activity because it affects agent behavior.

Suggested location:

- Right panel tab: `Memory`.
- Activity run detail: show `memory_injected` events.

## 9. Tool Policy

Memory writes are state-changing and must follow permission policy.

Possible built-in tools:

| Tool | Risk | Behavior |
| --- | --- | --- |
| `memory.list` | low | Reads visible memory records. |
| `memory.create` | medium/high | Writes a memory record. Requires permission unless trusted. |
| `memory.update` | medium/high | Updates status/content. Requires permission unless trusted. |
| `memory.delete` | high | Deletes memory. Requires permission. |

M3 can start with HTTP/Desktop memory management only. Runtime tool exposure should come later, after inspection/deletion UI exists.

## 10. Verification Plan

Backend:

- create/list/update/delete memory records,
- filtering by scope/workspace/session/status,
- soft delete excludes from active list,
- run memory selection respects workspace/session/status limits,
- agent-created memory requires source run/session metadata.

Protocol compatibility:

- create memory over HTTP,
- list active memory,
- delete memory,
- verify deleted memory is not returned by default,
- run with memory preview once implemented.

Runtime:

- injected memory appears in provider request context,
- `memory_injected` event includes ids and count,
- current user prompt remains dominant over memory.

Desktop:

- memory list renders,
- delete/disable removes from active view,
- preview shows what will be injected,
- Activity shows memory injection for a run.

## 11. Implementation Order

1. Add `memory_records` model/migration/repository.
2. Add Gateway service and HTTP CRUD APIs.
3. Add backend tests and protocol compatibility coverage.
4. Add memory selection/preview API.
5. Add Runtime DTO field for memory context and `memory_injected` event.
6. Inject selected memory into provider request context.
7. Add Desktop Memory panel.
8. Add Gateway-backed E2E.
9. Only then consider `memory.*` Runtime tools.

## 12. Guardrails

- No hidden memory writes.
- No raw secrets in memory content.
- No memory injection from deleted/disabled records.
- No cross-workspace project memory leakage.
- No MCP dependency for memory.
- Memory must remain auditable through source session/run/event metadata.
