# Parallel Development Plan

## 1. Goal

Split `red_panda` development into independent work streams so Gateway, Agent Runtime, Desktop, Integration, and Docs can move in parallel.

Rules:

1. Stabilize protocol first, then implement layers in parallel.
2. `modules/protocol` is the only shared contract. Other modules must not depend on each other's `internal` packages.
3. Gateway and Agent Runtime communicate through newline-delimited stdio JSON-RPC.
4. Desktop and Gateway communicate through `/api/v1/ws` WebSocket. SSE is not used.
5. Root agent and subagents share the same root run channel. `root_seq` gives global order; `agent_seq` and stream metadata identify the source.
6. Each stream needs tests, mocks, stubs, or smoke scripts that allow independent validation.

## 2. Work Streams

| Stream | Scope | Blocked By |
| --- | --- | --- |
| A. Protocol | JSON-RPC, WebSocket, Agent event, permission DTO, tool DTO | Starts first |
| B. Agent Runtime | stdio server, event mux, provider, tools, permission, subagent, cancel | Protocol |
| C. Gateway | Gin, GORM, SQLite(no cgo), runtime client, WebSocket hub, MVC, projection APIs | Protocol |
| D. Desktop | Wails v3, React JS, shadcn/ui-style components, WebSocket client, restore, Activity panel, global pending approval entry, run search/filter/detail | WebSocket envelope |
| E. Integration | smoke scripts, replay restore, permission loop, cancel loop, builds | B/C/D minimal loop |
| F. Docs | README, development plan, parallel plan, status | Current implementation evidence |

## 3. Worker Ownership

| Worker | Owner Area | Output |
| --- | --- | --- |
| Worker A | Gateway/Runtime reliability | History aggregation, permission/tool tests, cancellation boundaries |
| Worker B | Desktop | RunActivityPanel, restored state, UI reducers |
| Worker C | Docs | README and docs sync without overstating unfinished features |
| Worker D | Integration | smoke, builds, Wails packaging, regression checks |

Docs workers must only report completed features when current implementation and verification prove them.

## 4. Milestones

### M0: Protocol Minimal Set

Deliverables:

- JSON-RPC envelope.
- Agent event envelope.
- WebSocket envelope.
- Tool and permission DTOs.

Status: complete.

### M1: Three Layers Start Independently

Agent Runtime:

- `red-panda-agent` reads JSON-RPC from stdin.
- Supports `core.initialize` and `core.ping`.

Gateway:

- `red-panda-gateway` starts Gin.
- Supports `/healthz`, `/readyz`, and `/api/v1/ws`.
- Starts or connects to Runtime.

Desktop:

- Wails v3 shell starts.
- React UI shows logo, connection state, and empty chat.
- Connects to Gateway WebSocket.

Status: complete.

### M2: Echo Run Loop

Goal: Desktop sends a message, Gateway calls Agent over stdio, Agent returns echo, Desktop displays output.

Status: complete.

### M3: Tool and Permission Loop

Goal: high-risk tools trigger permission approval, and approval returns to Runtime.

Status: complete.

### M4: Subagent Loop

Goal: root agent creates an in-process subagent and subagent events display inside the same root run channel.

Status: MVP complete. `subagent_cancel`, `runtime_process`, and process pools are future work.

### M5: State Restore Loop

Goal: Desktop restores user-visible state after restart, session switch, or WebSocket reconnect.

Completion criteria:

- `run.resume` replays by `root_seq`.
- HTTP returns session history, run status, tool audit, permission records, and global pending permissions.
- Desktop restores messages, tool cards, pending permissions, active run, Activity summary, latest `root_seq`, and global pending approval queue.
- Activity panel can approve or deny global pending permissions.
- Activity panel supports status filtering, run search, and expandable run details with related tools and permission records.
- WebSocket session avoids replay/live duplicates.

Status: complete.

## 5. Current Parallel Results

Merged:

1. Worker A: message delta history aggregation; only assistant/subagent are appendable.
2. Worker B: RunActivityPanel for runs, tools, permissions, global pending approvals, filtering, search, and detail expansion.
3. Worker C: docs direction; mainline docs are ASCII to avoid encoding corruption and avoid overstating unfinished features.

## 6. Mock and Test Strategy

Agent Runtime mock:

- Gateway can use a fake runtime client to simulate `agent.event`.

Gateway mock:

- Desktop can use a mock WebSocket server to simulate `run.event`.

Provider mock:

- Agent Runtime echo provider is the no-external-dependency baseline.

Tool smoke:

- `/read README.md` validates low-risk tools.
- `/shell echo rp-smoke` validates high-risk tools, permission approval, and output.
- Denylist validates policy denial.
- Cancel validates pending permission closure and run status update.

## 7. Integration Gates

Every merge should satisfy:

1. Formatting.
2. Relevant module tests.
3. `CGO_ENABLED=0` build remains valid for Gateway and Agent Runtime.
4. No TypeScript introduced.
5. No cgo SQLite introduced.
6. Agent Runtime stdout remains JSON-RPC only.
7. No SSE/EventSource as Desktop realtime channel.
8. run/tool/permission projections are visible before replay events are exposed.
9. Docs distinguish complete features from MVP limitations and future work.
