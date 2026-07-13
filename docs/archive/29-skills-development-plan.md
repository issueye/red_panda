# Skills Discovery and Management Development Plan

Updated: 2026-07-11

Status: complete (Slices 1–4), verified 2026-07-11.

This plan closes the gap between the existing managed-skill Runtime tools (`skill.create` / `skill.update` / `skill.run`) and a complete, inspectable skill surface across Protocol, Runtime, Gateway, and Desktop.

## 1. Goal

Make workspace managed skills discoverable, inspectable, deletable, and manageable from Desktop, while keeping the existing isolated `skill.run` execution model.

Truth source remains the workspace filesystem:

```text
<workspace>/.codex/skills/<name>/SKILL.md
```

## 2. Baseline (already implemented)

| Capability | Status |
| --- | --- |
| `skill.create` | Done — high-risk, create-only |
| `skill.update` | Done — high-risk, update-only |
| `skill.run` | Done — isolated `runtime_process` subagent |
| Permission / policy / audit events | Done |
| Subagent context isolation | Done |
| Protocol constants `agent.skills` / `agent.skill.load` | Constants only |
| Gateway `/api/v1/skills` | Not implemented |
| Desktop Skills settings | LocalStorage-only shell, not wired |

## 3. Non-Goals (this plan)

Do **not** include in this plan:

1. Automatic skill selection / silent prompt injection without an explicit tool call.
2. Plugin marketplace or remote skill registry.
3. Skill packages outside `.codex/skills`.
4. MCP-backed skills.
5. Desktop redesign beyond rebinding the existing Skills tab.

Automatic selection can be a later optional slice after list/load/delete and Desktop management are solid.

## 4. Priority-Ordered Slices

| Order | Slice | Scope | Done when |
| --- | --- | --- | --- |
| 1 | Protocol + Runtime list/load | `protocol`, `agent` | `agent.skills` / `agent.skill.load` work; `skill.list` tool returns summaries; tests pass |
| 2 | Runtime `skill.delete` | `agent` | High-risk delete tool removes managed skill; permission/policy covered |
| 3 | Gateway HTTP APIs | `gateway` | `GET/DELETE` skills under current workspace; proxies Runtime; protocol-compat coverage |
| 4 | Desktop rebind | `desktop/frontend` | Settings Skills tab lists/creates/updates/deletes via Gateway; localStorage fake skills removed from product path |
| 5 | Optional auto-select (blocked) | later | Only after 1–4; not started in this plan |

## 5. Architecture

```text
Desktop Settings (Skills tab)
        │ HTTP
        ▼
Gateway /api/v1/skills[/:name]
        │ stdio JSON-RPC
        ▼
Runtime agent.skills / agent.skill.load
        │
        ▼
Workspace .codex/skills/*/SKILL.md
```

Provider tool path remains independent and Runtime-owned:

```text
Provider → skill.list / skill.create / skill.update / skill.delete / skill.run
```

Gateway never writes skill files itself during create/update; those remain Runtime tools during a run. Desktop create/update for management may either:

- **Preferred for this plan:** call Gateway write APIs that invoke Runtime write helpers, **or**
- Use a thin Gateway filesystem write that mirrors Runtime validation.

Decision for Slice 3: Gateway list/get/delete proxy Runtime JSON-RPC methods. For Desktop create/update without an active chat run, Gateway exposes:

- `POST /api/v1/skills` → Runtime-equivalent create validation + write
- `PUT /api/v1/skills/:name` → update
- `DELETE /api/v1/skills/:name` → delete
- `GET /api/v1/skills` / `GET /api/v1/skills/:name` → list/load

To avoid a long-lived “skills write” JSON-RPC surface if not needed, Gateway may implement the same managed-skill filesystem helpers in a Gateway service package **only if** they share the exact validation rules. Prefer shared logic by having Gateway call Runtime methods:

| Method | Purpose |
| --- | --- |
| `agent.skills` | List summaries for a workspace root |
| `agent.skill.load` | Load one skill detail (optional full body) |
| `agent.skill.delete` | Delete one managed skill (new) |

Create/update already exist as tools; for management without a provider turn, add:

| Method | Purpose |
| --- | --- |
| `agent.skill.create` | Create managed skill (management path) |
| `agent.skill.update` | Update managed skill (management path) |

Alternatively, reuse filesystem helpers from Runtime packages only through JSON-RPC to keep a single writer. This plan chooses **JSON-RPC management methods** so Desktop never touches the filesystem directly and Gateway never duplicates skill path rules.

### 5.1 Protocol DTOs

```text
SkillSummary {
  name
  description
  path            // workspace-relative .codex/skills/<name>/SKILL.md
  has_instructions
}

SkillDetail {
  name
  description
  instructions    // body after frontmatter
  path
  size_bytes
}

SkillsListParams { workspace_root }
SkillsListResult { items: SkillSummary[] }

SkillLoadParams { workspace_root, name, include_instructions? }
SkillLoadResult { skill: SkillDetail }

SkillMutateParams { workspace_root, name, description, instructions }
SkillMutateResult { action, name, path }

SkillDeleteParams { workspace_root, name }
SkillDeleteResult { action, name, deleted }
```

### 5.2 Runtime tools

| Tool | Risk | Notes |
| --- | --- | --- |
| `skill.list` | Low | Returns JSON summaries; no instructions body |
| `skill.create` | High | Existing |
| `skill.update` | High | Existing |
| `skill.delete` | High | New |
| `skill.run` | High | Existing |

Update `skillSubagentDenylist` to include `skill.delete` and `skill.list` remains allowed for child read-only discovery if useful; prefer denylisting `skill.list` only if it confuses isolation. Decision: child keeps `skill.list` optional; **denylist create/update/delete/run**.

### 5.3 Gateway HTTP

```text
GET    /api/v1/skills?workspace_root=
GET    /api/v1/skills/:name?workspace_root=&include_instructions=1
POST   /api/v1/skills
PUT    /api/v1/skills/:name
DELETE /api/v1/skills/:name?workspace_root=
```

Request bodies for POST/PUT:

```json
{
  "workspace_root": "...",
  "name": "code-review",
  "description": "...",
  "instructions": "..."
}
```

### 5.4 Desktop

Replace localStorage `settings.skills` product path with Gateway-backed state:

1. On open Skills tab (or Settings open): `GET /api/v1/skills?workspace_root=`
2. Create / edit form writes description + instructions (not free-form external path)
3. Delete calls Gateway DELETE
4. Remove `skills` from `run.start` options concerns (already not sent)
5. Keep empty-state when no workspace is open

## 6. Implementation Tasks

### Slice 1 — Protocol + Runtime list/load + `skill.list`

**Files:**

- Modify: `modules/protocol/methods/methods.go`
- Create: `modules/protocol/skills/skills.go` (optional package; may live in methods)
- Modify: `modules/agent/internal/runtime/skill_tools.go`
- Create or modify: `modules/agent/internal/runtime/skill_discovery.go`
- Modify: `modules/agent/internal/runtime/runtime.go` (dispatch + capabilities)
- Modify: `modules/agent/internal/runtime/tools.go`
- Test: `modules/agent/internal/runtime/skill_tools_test.go` / new discovery tests

**Steps:**

1. Add DTOs and keep existing method constants; add create/update/delete method constants if needed for later slices.
2. Parse SKILL.md frontmatter (`name`, `description` JSON string) and body.
3. Implement `listManagedSkills(workspaceRoot)`.
4. Implement `loadManagedSkillDetail(workspaceRoot, name, includeInstructions)`.
5. Wire `handleAgentSkills` / `handleAgentSkillLoad`.
6. Register capability names on initialize.
7. Add low-risk `skill.list` tool.
8. Tests: empty workspace, one skill, invalid markdown skipped/sanitized, path escape rejected, tool output omits full instructions.

### Slice 2 — `skill.delete`

**Files:**

- Modify: `modules/agent/internal/runtime/skill_tools.go`
- Modify: `modules/agent/internal/runtime/tools.go`
- Modify: `modules/agent/internal/runtime/skill_subagent.go` denylist
- Test: skill delete unit + permission denial

**Steps:**

1. Delete only managed skill directory under `.codex/skills/<name>` after validation.
2. Refuse symlink escape and non-managed paths.
3. High-risk permission + policy.
4. Tests for success, missing skill, escape, permission deny.

### Slice 3 — Gateway APIs

**Files:**

- Modify: `modules/gateway/internal/gateway/infra/runtimeclient/client.go`
- Create: `modules/gateway/internal/gateway/service/skill.go`
- Create: `modules/gateway/internal/gateway/controller/skill.go`
- Modify: `modules/gateway/internal/gateway/service/set.go`
- Modify: `modules/gateway/internal/gateway/controller/set.go`
- Modify: `modules/gateway/internal/gateway/app/app.go` routes
- Test: service tests + expand `scripts/protocol-compat.ps1`

**Steps:**

1. Runtime client methods for list/load/create/update/delete.
2. Service validates `workspace_root` presence.
3. Controllers return standard `{ok, data}` envelopes.
4. protocol-compat: create via API, list, load, update, delete.

**Note:** Slice 3 depends on Slice 1 list/load and on management mutate methods. If create/update management methods are not yet added in Slice 1, implement them at the start of Slice 3 together with Gateway.

### Slice 4 — Desktop

**Files:**

- Create: `modules/desktop/frontend/src/lib/skills.js` (+ unit test)
- Modify: `modules/desktop/frontend/src/hooks/useGatewayConnection.js` or App API helpers
- Modify: `modules/desktop/frontend/src/components/SettingsPanel.jsx`
- Modify: `modules/desktop/frontend/src/lib/runOptions.js` (drop unused `skills` default if fully removed)
- Test: unit + Playwright settings / gateway-backed if feasible

**Steps:**

1. DTO normalize helpers.
2. Wire list/create/update/delete callbacks like MCP servers.
3. Form fields: name, description, instructions (textarea), not arbitrary host path.
4. Show path as read-only meta from server.
5. Fixture / e2e: create skill, list appears, delete removes.

## 7. Verification Gates

### After Slice 1–2

```powershell
go test ./modules/protocol/... ./modules/agent/...
```

### After Slice 3

```powershell
go test ./modules/protocol/... ./modules/agent/... ./modules/gateway/...
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\protocol-compat.ps1
```

### After Slice 4

```powershell
cd modules\desktop\frontend
npm test
npm run test:ui
npm run build
```

## 8. Exit Criteria

This plan is complete when:

1. Runtime can list and load managed skills through JSON-RPC.
2. Provider can call `skill.list` and `skill.delete` with correct risk/policy.
3. Gateway exposes inspectable skill CRUD for the active workspace.
4. Desktop Skills tab is Gateway-backed and no longer a localStorage-only fake registry.
5. Existing `skill.run` isolation and create/update semantics remain unchanged.
6. Automatic selection is still **not** claimed.

## 9. Execution Order for This Session

1. Write this plan. ✅
2. Implement Slice 1. ✅
3. Implement Slice 2. ✅
4. Implement management mutate methods + Slice 3. ✅
5. Implement Slice 4. ✅
6. Run verification gates and update `docs/10-development-status.md` / `docs/12-current-execution-plan.md`. ✅

## 11. Verification Results (2026-07-11)

| Gate | Result |
| --- | --- |
| `go test ./modules/protocol/... ./modules/agent/... ./modules/gateway/...` | Pass |
| `scripts/protocol-compat.ps1` (skill create→list→load→update→delete) | Pass, `skill_crud` in result |
| Desktop `npm test` (32 tests, includes 4 skills tests) | Pass |
| Runtime subprocess request-context regression test | Pass |

Additional fix surfaced by Slice 3 verification: the Gateway Runtime subprocess was spawned with `exec.CommandContext(reqCtx)`, which killed the single-core Runtime when a one-shot HTTP management request (skills, MCP discovery) returned. `ensureStarted` now uses `exec.Command` and a regression test (`TestRuntimeProcessSurvivesRequestCancellation`) locks the lifecycle. Desktop `npm run build` remains blocked by a local Node 21 / rolldown native-binding toolchain limitation unrelated to this plan.

## 10. Risks

| Risk | Mitigation |
| --- | --- |
| Duplicated create/update validation | Single Runtime implementation; Gateway only proxies |
| Desktop still has localStorage skills | Remove product usage; migrate empty array |
| Listing leaks full instructions to provider | `skill.list` returns summaries only |
| Skill run child gains delete | Keep delete on denylist |
| No workspace open in Desktop | Disable Skills mutations with clear empty state |
