# Session Fork and Context Summary Design

Updated: 2026-07-12

This is the M2 design document for session fork and compaction. Backend implementation must not start until this document is accepted as the working contract.

## 1. Goals

Session fork and compact must prepare `red_panda` for longer histories and future memory without making session restore ambiguous.

Required outcomes:

1. A user can fork a session from a known point and continue in the new branch.
2. A session can keep its full visible history while older model context is represented by an auditable summary snapshot.
3. Summary updates never create a new session and never replace or delete original messages.
4. Every fork or summary snapshot keeps traceable source metadata.
5. Desktop restore continues to use the existing history/runs/tools/permissions/event APIs with minimal branching logic.

Non-goals for M2:

- Long-term memory storage.
- MCP tool support.
- Cross-session summary merging.
- Cross-workspace session merging.
- Semantic vector retrieval.

## 2. Current Baseline

Existing persistence:

- `sessions`: session metadata, including `parent_id`.
- `messages`: ordered session messages with `seq` and `run_id`.
- `run_records`: root run summaries keyed by run id and session id.
- `run_events`: persisted event stream keyed by `root_run_id` and `root_seq`.
- `tool_calls`: tool projections keyed by root run and session.
- `permission_requests`: permission projections keyed by run and session.

Existing Desktop restore reads:

- `GET /api/v1/sessions`
- `GET /api/v1/sessions/:id/history`
- `GET /api/v1/sessions/:id/runs`
- `GET /api/v1/sessions/:id/tools`
- `GET /api/v1/sessions/:id/permissions`
- `GET /api/v1/runs/:id/events`

Design constraint: forked sessions use the restore flow above; context summaries are loaded separately and do not alter restored history.

## 3. Domain Terms

| Term | Meaning |
| --- | --- |
| Source session | The existing session being forked or summarized. |
| Fork session | A new session created from part of a source session. |
| Fork point | The last message/run boundary included in the fork. |
| Context summary | A model-facing summary snapshot stored separately from the session messages. |
| Compaction record | Persistent audit record describing the summarized input range and structured summary. |
| Lineage | Parent-child relationship created by forks. Context summaries do not create lineage. |

## 4. Design Choice

Use physical-copy sessions only for forks. Apply context summaries in place.

For a fork, copy messages up to the fork point into a new session. For a summary, keep every original message in the same session and store the structured summary in `session_compactions`. A new run receives the latest summary as system context plus original messages after the summary range.

Why:

- Existing Desktop restore keeps showing the complete original history.
- Summary updates do not create sidebar entries or synthetic chat messages.
- Queries remain simple SQLite/GORM queries by `session_id`.
- Repeated summaries supersede the previous active snapshot without deleting its audit record.

Tradeoff: model context and visible history are now intentionally different projections, so the Gateway must assemble model context explicitly.

## 5. Persistence Model

### 5.1 Extend `sessions`

Keep current fields and use `parent_id` for the direct source session.

Recommended additional fields:

| Field | Type | Meaning |
| --- | --- | --- |
| `kind` | string | `normal`, `fork`, or `compact`. Default `normal`. |
| `fork_point_seq` | uint64 | Last source message seq copied into this session. |
| `fork_point_run_id` | string | Run boundary used for fork, when available. |
| `source_session_id` | string | Same as parent for M2, explicit for query clarity. |

If schema churn should be smaller, keep only `parent_id` in `sessions` and put all extra metadata in `session_lineage`.

### 5.2 Add `session_lineage`

```text
id
source_session_id
target_session_id
operation        fork | compact
fork_point_seq
fork_point_run_id
source_start_seq
source_end_seq
created_at
metadata_json
```

Rules:

- One lineage row per fork/compact operation.
- `target_session_id` points to the newly created session.
- `source_start_seq/source_end_seq` describe the source message range used.

### 5.3 Add `session_compactions`

```text
id
source_session_id
target_session_id
status           preview | applied | failed
source_start_seq
source_end_seq
summary_message_id
summary_json
error
created_at
updated_at
```

`summary_json` should include:

```json
{
  "summary": "human readable session summary",
  "decisions": [],
  "open_tasks": [],
  "workspace_context": [],
  "risks": []
}
```

M2 can generate this summary through the existing echo/provider path later, but the persistence contract should not depend on a specific provider.

### 5.4 Message Copy Metadata

Recommended future-safe `messages` additions:

| Field | Type | Meaning |
| --- | --- | --- |
| `source_message_id` | string | Original message id copied from source session. |
| `metadata_json` | string | Optional message metadata, including `synthetic=true` for compaction summary. |

M2 backend can start without these if it records lineage and compaction tables. Add them if implementation complexity stays low.

## 6. API Contract

All APIs use the existing HTTP JSON envelope:

```json
{
  "ok": true,
  "data": {}
}
```

### 6.1 Fork Session

```text
POST /api/v1/sessions/:id/fork
```

Request:

```json
{
  "name": "Forked session name",
  "fork_point": {
    "message_seq": 12,
    "run_id": "run_...",
    "root_seq": 18
  },
  "include_recent_run_events": false
}
```

Response:

```json
{
  "session": {
    "id": "session_...",
    "name": "Forked session name",
    "workspace_root": "D:\\codes\\issueye\\ai_agents",
    "parent_id": "session_source",
    "kind": "fork"
  },
  "lineage": {
    "id": "lineage_...",
    "source_session_id": "session_source",
    "target_session_id": "session_...",
    "operation": "fork",
    "fork_point_seq": 12,
    "fork_point_run_id": "run_..."
  },
  "copied_messages": 12
}
```

Rules:

- `message_seq` is the canonical fork boundary.
- If no fork point is supplied, fork through the latest message.
- Runs/tools/permissions are not copied in M2; the copied message history is enough for continuing the new branch.
- New runs in the fork use the new `session_id`.

### 6.2 Compact Preview

```text
POST /api/v1/sessions/:id/compact/preview
```

Request:

```json
{
  "source_range": {
    "start_seq": 1,
    "end_seq": 120
  },
  "keep_tail_messages": 8,
  "mode": "summary"
}
```

Response:

```json
{
  "preview": {
    "source_session_id": "session_source",
    "source_start_seq": 1,
    "source_end_seq": 120,
    "keep_tail_messages": 8,
    "summary": {
      "summary": "...",
      "decisions": [],
      "open_tasks": [],
      "workspace_context": [],
      "risks": []
    }
  }
}
```

Rules:

- Preview does not write a new session.
- Preview may be provider-backed later; for first backend implementation, a deterministic local summary is acceptable.
- Preview must include enough metadata for the Desktop to show what will be replaced.

### 6.3 Apply Compaction

```text
POST /api/v1/sessions/:id/compact
```

Request:

```json
{
  "name": "Compacted session name",
  "source_range": {
    "start_seq": 1,
    "end_seq": 120
  },
  "keep_tail_messages": 8,
  "summary": {
    "summary": "...",
    "decisions": [],
    "open_tasks": [],
    "workspace_context": [],
    "risks": []
  }
}
```

Response:

```json
{
  "session": {
    "id": "session_source",
    "name": "Original session name",
    "workspace_root": "D:\\codes\\issueye\\ai_agents",
    "kind": "normal"
  },
  "compaction": {
    "id": "compact_...",
    "source_session_id": "session_source",
    "target_session_id": "session_source",
    "status": "applied",
    "source_start_seq": 1,
    "source_end_seq": 120
  },
  "keep_tail_turns": 3
}
```

Rules:

- No target session or synthetic chat message is created.
- `GET /sessions/:id/history` continues returning the original messages.
- `GET /sessions/:id/compact` returns the active context-summary snapshot.
- New runs receive the summary as system context followed by original messages after `source_end_seq`.

### 6.4 Lineage Query

```text
GET /api/v1/sessions/:id/lineage
```

Response:

```json
{
  "parents": [],
  "children": [],
  "compactions": []
}
```

This can be added after fork/compact if needed by Desktop. It is not required for the first backend slice unless the Desktop design needs branch visualization immediately.

## 7. Desktop Behavior

### Session Sidebar

M2 Desktop should show:

- normal session title,
- fork/compact marker,
- parent/source hint,
- creation/update time.

Avoid a full branch graph in M2. A small marker is enough.

### Fork Action

Minimum UX:

1. User selects a session.
2. User chooses fork from current/latest point.
3. Desktop calls `POST /sessions/:id/fork`.
4. Desktop selects the returned session.
5. Existing restore flow loads copied history.

Later UX:

- Fork from a selected message/run in Activity.

### Compact Action

Minimum UX:

1. User opens a compact preview for the current session.
2. Desktop generates and applies a structured summary snapshot.
3. The current session remains selected and its complete history remains visible.
4. The token estimate switches to summary plus uncovered recent messages.
5. Future runs use that model-facing context projection.

The UI must not create or select a derived session for summary updates.

## 8. Runtime Contract

M2 fork does not require Runtime changes.

M2 compaction can start with a Gateway-local deterministic summary. When provider-backed compaction is added:

- Gateway starts a normal run or a dedicated compaction request.
- Compaction output must be persisted as `session_compactions.summary_json`.
- Any provider/tool events used to create the summary must be auditable, either through a run record or a compaction event table.

No memory injection should be implemented before this contract exists in code.

## 9. Verification Plan

### Backend Unit Tests

- Fork copies messages up to `message_seq`.
- Fork preserves source session unchanged.
- Fork sets parent/source lineage.
- Compact preview does not write sessions/messages.
- Compact apply creates target session, summary message, tail messages, lineage, and compaction record.
- Invalid fork/compact source returns a structured error.

### Protocol/API Compatibility

Add to `scripts/protocol-compat.ps1` after backend exists:

- create source session,
- create messages through runs,
- fork through latest message,
- verify new session history,
- compact preview,
- compact apply,
- verify compact session history and lineage.

### Desktop Tests

- Fixture-level session sidebar shows fork/compact markers.
- Gateway-backed fork selects new session and restores copied history.
- Gateway-backed compact selects new session and restores summary plus tail.

## 10. Implementation Order

1. Add models and migrations:
   - `session_lineage`
   - `session_compactions`
   - optional message source metadata.
2. Add repository methods:
   - copy messages through seq,
   - create lineage,
   - create compaction record.
3. Add service methods:
   - `ForkSession`
   - `PreviewCompaction`
   - `ApplyCompaction`
4. Add controllers/routes.
5. Add backend tests.
6. Add Desktop controls and restore handling.
7. Add Gateway-backed E2E.
8. Update protocol compatibility script.

## 11. Open Questions

1. Should M2 copy run records/tools/permissions into forked sessions, or keep only message history?
   - Recommendation: do not copy them in M2. Keep lineage to source for audit.
2. Should summary apply create a new session or mutate message history?
   - Resolved: neither. Store an in-place context snapshot linked to the same session; do not mutate messages.
3. Should compaction use provider summaries immediately?
   - Recommendation: start deterministic; add provider-backed summaries later with audit events.
4. Should fork point be message seq or run id?
   - Recommendation: message seq is canonical; run id is optional metadata.
