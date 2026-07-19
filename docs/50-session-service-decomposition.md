# Session Service Decomposition

| Field | Value |
| --- | --- |
| **Date** | 2026-07-19 |
| **Status** | complete (Wave B Task B1/B2/B3) |
| **Related** | [45](45-session-system-abstraction-design.md)、[46](46-session-store-development-plan.md)、[47](47-complexity-redundancy-development-plan.md)、[48](48-session-correctness-optimization-plan.md)、[49](49-session-hard-delete-jsonl-archive.md)、[plans/2026-07-19-convergence-wave](plans/2026-07-19-convergence-wave.md) |
| **BREAKING** | none. Public API of `SessionService` (controller-facing method signatures) is unchanged. Internal collaborators renamed/relocated only. |

## 1. 背景与目标

### 1.1 现状问题

Before this wave, `SessionService` was a 850-line struct holding 12 public methods spanning five responsibilities: CRUD/Fork/History, Compact, Archive+Hard-Delete, Bootstrap, ContextState. Two structural smells were present:

1. **"False separation"**: `session_compact_summary.go` (585 lines) lived in a separate file but contained `SessionService` methods, not free functions — the split was physical only, with no actual decoupling.
2. **Cross-service coupling masquerading as a private field**: `sessionLifecycle` was a private field of `SessionService`, yet `WorkspaceService.Remove` already consumed it via `NewWorkspaceService(repos, lifecycle)` (workspace.go:17). It was de-facto shared but lacked a public name.
3. **Cross-service direct call**: `buildModelConversation` was a package-private free function called from `RunService.prepareRun` (run_start.go:78) — RunService implicitly reached into SessionService's domain.

### 1.2 目标

- Extract three cohesive collaborators: **SessionContextPacker**, **SessionCompactor**, **PurgeService**.
- Preserve all external API signatures (controllers and existing tests stay unchanged in behavior).
- Move `compactGate` ownership from SessionService to SessionCompactor — the SessionService-by-value-copy problem disappears because the gate is now inside a pointer-shared struct.
- Make the Run↔Session shared dependency on context packing an explicit injected collaborator.

### 1.3 非目标

- No DTO relocation (DTOs remain in `session.go`; ~180 lines of pure data did not justify a separate file in this wave).
- No change to compact concurrency contract, archive format, or hard-delete cascade.
- No refactoring of `worker.Pool` or `RunStateStore` (those are addressed in [plans/2026-07-19-convergence-wave](plans/2026-07-19-convergence-wave.md) Wave C / known-limitations).

## 2. 拆分边界

### 2.1 SessionContextPacker (Task B1)

**Owns:** model-context packing — the assembly of the provider-facing transcript (optional in-place compaction system message + message tail trimmed by token budget).

| Aspect | Detail |
|---|---|
| Type | `*SessionContextPacker` (pointer, shared instance) |
| File | `session_context_packer.go` (48 lines) |
| Fields | `repos repository.Set` |
| Public methods | `BuildModelConversation(sessionID)`, `LoadForAssembly(sessionID)`, `ModelContext(sessionID)` |
| Internal helpers | `loadModelContextForAssembly`, `assembleModelConversation`, `selectModelContextMessages` (kept as package-private for testability of the pure token-budget trim) |
| Consumers | `RunService.prepareRun`, `SessionService.ContextState` |
| Construction | `NewSessionContextPacker(repos)` — called once in `Set.NewSet`, injected into Run and Session |

**Decision:** the helpers stay package-private free functions because `selectModelContextMessages` is a pure function over `[]model.Message` with no state — wrapping it as a method would only add ceremony. The Packer type is the single public entry point.

### 2.2 SessionCompactor (Task B2)

**Owns:** the compact critical section, plan/summary logic, pause/resume orchestration, and the LLM/local summary builders.

| Aspect | Detail |
|---|---|
| Type | `*SessionCompactor` (pointer, shared instance) |
| File | `session_compactor.go` (358 lines) + `session_compact_summary.go` (585 lines, planner/LLM/local logic) |
| Fields | `repos`, `store` (shared with SessionService), `runtime *runtimeclient.Client`, `hub`, `gate *sessionCompactGate` |
| Public methods | `Preview`, `Apply`, `State`, `Summaries` |
| Internal helpers | `beginSessionCompact`, `pauseSessionForCompact`, `resumeRunsAfterCompact`, `resumeSessionAfterCompact`, `ensureOpenTasks`, `planCompaction`, `planCompactionByMessageCount`, `buildCompactSummary`, `summarizeMessagesWithLLM`, `resolveCompactProvider` |
| Consumers | `SessionService.CompactPreview/Compact/CompactionState/Summaries` (thin delegation) |
| Construction | `NewSessionCompactor(repos, store, runtime, hub)` — built inside `NewSessionServiceWithPacker` so the same `sessionStore` instance is shared |

**Decision:** SessionService keeps the four compact method signatures as one-line delegators. This preserves the public API (controllers and existing tests do not need to learn a new type), while the actual logic now lives in the Compactor. The `compactGate` is no longer a SessionService field — SessionService value copies no longer need to share a mutex.

### 2.3 PurgeService (Task B3)

**Owns:** cascade deletion of sessions — cancel active runs, archive to JSONL, hard-delete session and all linked rows, broadcast `session_deleted`.

| Aspect | Detail |
|---|---|
| Type | `PurgeService` (value; immutable after construction) |
| File | `session_purge_service.go` (renamed from `session_lifecycle.go`) |
| Fields | `repos`, `store`, `runtime`, `hub` |
| Public method | `PurgeSessions(sessionIDs, reason) (int64, error)` |
| Consumers | `SessionService.Delete`, `WorkspaceService.Remove` |
| Construction | `newPurgeService(repos, runtime, hub, archiveDir)` — called once in `Set.NewSet`, injected into both Session and Workspace |

**Decision:** PurgeService is exported (capital P). This makes the cross-service sharing explicit and replaces the prior unexported `sessionLifecycle` helper that was technically private but consumed across services. The constructor stays unexported because external packages should go through `Set`.

## 3. Set 装配关系

```
Set.NewSet(opts)
  │
  ├── packer   := NewSessionContextPacker(repos)         // shared instance
  ├── purge    := newPurgeService(repos, runtime, hub, archiveDir)
  ├── run      := NewRunServiceWithPacker(repos, hub, runtime, packer)
  │
  ├── Workspace  := NewWorkspaceService(repos, purge)
  │
  └── Session := NewSessionServiceWithPacker(repos, runtime, hub, archiveDir, packer)
        │   (internally builds:)
        ├── store     := newSessionStore(repos, archiveDir)
        ├── compactor := NewSessionCompactor(repos, store, runtime, hub)
        └── purge     := newPurgeService(repos, runtime, hub, archiveDir)   // for Session.Delete
```

**Note on two PurgeService instances:** Workspace and Session each construct their own PurgeService value. PurgeService is stateless (no mutex, no caches) — it only wraps repos/store/runtime/hub — so duplicate construction is harmless and avoids a wider refactor of the constructor graph. If PurgeService ever gains mutable state, this must be consolidated to a single shared instance.

## 4. 验收

| Criterion | Status |
|---|---|
| `SessionService` retains all public method signatures | ✅ |
| `SessionService` no longer holds `compactGate` field | ✅ |
| `session_compact_summary.go` no longer has `SessionService` methods | ✅ (all migrated to `*SessionCompactor`) |
| `run_start.go` no longer calls `buildModelConversation` directly | ✅ (calls `r.packer.BuildModelConversation`) |
| `WorkspaceService` depends on `PurgeService` (not an unexported helper) | ✅ |
| All pre-existing gateway tests pass | ✅ |
| `go vet ./...` no new warnings vs baseline | ✅ |
| `session.go` line count | 595 (was 850; -30%). Lower than original target ≤ 400 because ~180 lines are DTOs not in scope of this wave. |

### 4.1 Test migration

Test files were updated minimally to reflect the new ownership:

- `session_test.go`: 3 internal-helper calls moved from `service.X` to `service.compactor.X` (`pauseSessionForCompact`, `resumeSessionAfterCompact`, `beginSessionCompact`). Public-API calls (`service.Compact`, `service.CompactPreview`) are unchanged.
- `session_compact_summary_test.go`: 2 internal-helper calls moved from `service.X` to `service.compactor.X` (`buildCompactSummary`, `summarizeMessagesWithLLM`).
- `session_context_test.go`: unchanged (tests `selectModelContextMessages`, still a package-private pure function).
- `workspace_test.go`: `newSessionLifecycle` → `newPurgeService`.

## 5. 风险

1. **"False separation" fully resolved**: `session_compact_summary.go` now contains `*SessionCompactor` methods only; no more cross-file SessionService methods.
2. **Cross-service dependency made explicit**: RunService now declares `packer *SessionContextPacker` as a field rather than implicitly reaching into session domain code.
3. **Compact gate integrity**: the gate lives on `*SessionCompactor` (a pointer). SessionService value copies never need to share the gate because they delegate. Tests `TestSessionServiceRejectsConcurrentCompact`, `TestSessionServiceCompactPausesActiveRuns`, `TestSessionServiceCompactResumesWorkersWhenSummaryFails`, `TestPauseSessionForCompactPausesAndResumesDelegatedWorkers` all pass.

## 6. 进度

| Task | Status | Commit |
|---|---|---|
| B1 SessionContextPacker | ✅ done | (this wave) |
| B2 SessionCompactor | ✅ done | (this wave) |
| B3 PurgeService | ✅ done | (this wave) |

## 7. 后续可能

If `session.go` needs further shrinkage, the natural next slice is extracting session DTOs (`SessionDTO`, `MessageDTO`, `CompactSummary`, `CompactionDTO`, `CompactSessionResult`, etc., ~180 lines) into `session_dto.go`. That is a pure data relocation and is **not** included in this wave because it does not change any boundary — only file layout. It can be picked up under Wave C-style physical splits if/when desired.
