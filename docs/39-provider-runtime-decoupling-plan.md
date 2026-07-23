# Provider-Runtime Decoupling Plan

Updated: 2026-07-23
Status: Wave 1–2 complete; Wave 3 planned
Baseline commit: `3544c7a` plus the verified, uncommitted Wave 1 changes recorded in doc 38

## 1. Objective

Reduce the coupling between the Agent Runtime and model providers so that provider implementations can be replaced, tested, and extended without changing the Runtime tool loop or exposing unrelated Runtime state.

Target direction:

```text
Runtime orchestration -> provider-neutral request/stream contract -> provider adapter
```

The Runtime continues to own tool turns, permissions, events, cancellation, Goal segments, and final-answer recovery. Provider adapters own vendor configuration, request encoding, response decoding, and transport retries.

## 2. Current Coupling

| Coupling | Current evidence | Cost |
| --- | --- | --- |
| Construction | `runtime.New` calls `provider.NewFromEnv` directly | tests and alternate hosts cannot inject a provider cleanly |
| File ownership | `provider.go` contains interface, Echo behavior, HTTP transport, stream decoding, prompt policies, and message mapping | unrelated provider changes collide in one file |
| Request DTO | `ProviderRequest` embeds protocol `ReplySession`, `ReplyInput`, and `ReplyOptions` | provider sees more Runtime/Gateway state than transport needs |
| Strategy | Goal/Todo/Worker policies are assembled inside the provider package | agent behavior and vendor transport evolve together |
| Tool result formatting | provider imports `internal/tools` for model-facing output | provider depends on a concrete Agent implementation package |
| Selection | environment defaults and per-run overrides are resolved in different execution paths | provider selection behavior is difficult to reason about |

## 3. Guardrails

- Preserve all JSON-RPC, HTTP, event, tool, and persisted data contracts.
- Preserve EchoProvider behavior used by tests and local development.
- Preserve OpenAI-compatible request bodies, stream parsing, retry conditions, and per-run profile overrides.
- Do not move the Runtime tool loop into provider adapters.
- Do not add a generic dependency-injection framework.
- Do not mix Gateway, Desktop, CSS, Goal behavior, or tool-registry changes into this plan.
- Each wave must compile and pass focused tests before later semantic decoupling starts.

## 4. Wave 1 - Structural Boundary

Wave 1 is behavior-preserving and can be developed in parallel.

### W1-A Runtime provider injection

Primary files:

- `modules/agent/internal/runtime/runtime.go`
- focused Runtime tests

Implementation:

1. Introduce a small Runtime dependency/options structure containing a `provider.Provider`.
2. Add a constructor that accepts dependencies.
3. Keep the existing `New` signature as the environment-backed compatibility wrapper.
4. Default a missing provider through the existing environment factory.

Acceptance:

- production construction behavior is unchanged;
- tests and alternate hosts can inject a provider without mutating Runtime internals;
- Runtime tests prove injected and default provider selection.

### W1-B Provider file and responsibility split

Primary files:

- `modules/agent/internal/provider/*.go`
- existing provider tests

Target files:

```text
provider.go          interface and neutral shared types
factory.go           environment defaults and per-run provider resolution
echo.go              deterministic local/test provider
openai_http.go       HTTP request, retry, and non-stream decoding
openai_stream.go     SSE stream decoding
messages.go          OpenAI-compatible message/tool mapping
policies.go          existing agent prompt policy constants
```

Implementation constraints:

- start with zero-behavior-diff moves;
- keep private helper names where practical;
- do not redesign request DTOs in the same change;
- leave LLM request logging in `llm_log.go`.

Acceptance:

- no production provider source file exceeds roughly 500 lines unless it is mostly declarative policy text;
- Provider interface and observable provider behavior remain unchanged;
- provider full tests pass.

### W1-C Provider boundary and compatibility tests

Primary files:

- new provider/runtime focused test files

Implementation:

1. Lock the Provider interface and per-run override behavior through black-box-style tests where possible.
2. Add an import-boundary test proving the provider package does not import Runtime.
3. Cover injected Runtime provider use without requiring HTTP.
4. Lock stream/non-stream tool-call normalization and provider-name reporting where existing coverage is weak.

Acceptance:

- tests fail on accidental provider -> Runtime imports;
- tests protect constructor injection and existing override semantics;
- tests do not depend on network access.

## 5. Wave 2 - Data and Strategy Decoupling

Start after Wave 1 is integrated and green.

1. Replace `methods.ReplySession`, `ReplyInput`, and broad `ReplyOptions` inside `ProviderRequest` with provider-owned neutral types.
2. Add a Runtime prompt composer that converts Goal, Todo, Memory, Skills, and Specialist state into ordered neutral system messages.
3. Make provider adapters serialize neutral messages without deciding Runtime policy.
4. Move model-facing tool-result conversion out of provider's dependency on `internal/tools`.
5. Introduce one provider resolver responsible for environment defaults plus per-run profile overrides.

Wave 2 completion requires golden request-body tests proving OpenAI-compatible payload equivalence before and after migration.

### Wave 2 status (2026-07-23)

| Item | Status | Evidence |
| --- | --- | --- |
| Neutral `provider.Request` / `Message` / `RequestOptions` | done | `provider/provider.go`; Runtime `prompt_composer.go` builds requests |
| Runtime prompt composer owns policy | done | `runtime/prompt_policies.go` + `prompt_composer.go`; provider has no policy assembly |
| Provider does not import `protocol/methods` | done | `boundary_test.go` AST guard |
| Model-facing tool content without `internal/tools` | done | `protocol/tools.ModelFacingContent`; provider `messages.go` uses it; AST guard bans `internal/tools` |
| Public `Resolve` for env + profile | done | `provider.Resolve` wraps shared `providerFromOptions` |
| Golden OpenAI-compatible messages | done | `TestOpenAICompatibleMessagesGolden` + `TestModelFacingContentGoldenLegacyPair` |

## 6. Wave 3 - Multi-provider Extension

- add a second adapter only after the neutral contract is stable;
- keep vendor-specific request/response types inside the adapter;
- add capability flags only for real differences such as tool calling or streaming;
- do not add provider-specific branches to the Runtime loop.

## 7. Parallel Ownership

| Worker | Scope | Must not edit |
| --- | --- | --- |
| Runtime injection | W1-A Runtime constructor and focused tests | provider production files, Gateway, Desktop |
| Provider split | W1-B provider production files and existing provider tests as needed | Runtime production files, Gateway, Desktop |
| Boundary tests | W1-C new tests and read-only architecture audit | production files owned by other workers |
| Root integrator | plan, review, conflict resolution, final verification | unrelated product behavior |

## 8. Verification

Focused gates:

```text
cd modules/agent
go test ./internal/provider/...
go test ./internal/runtime/...
```

Integration gates:

```text
go test ./modules/agent/...
powershell -ExecutionPolicy Bypass -File scripts/ci-gate.ps1
git diff --check
```

Audit requirements:

- `rg 'internal/runtime' modules/agent/internal/provider -g '*.go'` returns no production import;
- existing `New` callers continue to compile;
- provider request/response behavior remains covered by existing and new tests;
- only planned files are changed by this wave.

## 9. Completion Criteria

The requested parallel development wave is complete when W1-A, W1-B, and W1-C are integrated, their acceptance criteria are verified, and the repository integration gate passes. Wave 2 and Wave 3 remain explicit follow-up work because they change internal contracts and require payload-equivalence evidence.

## 10. Progress Log

### 2026-07-15 - Wave 1 implementation

| Workstream | Result | Verification |
| --- | --- | --- |
| W1-A Runtime injection | Added `Dependencies` and `NewWithDependencies`; existing `New` remains an environment-backed compatibility wrapper | focused Runtime tests and Agent full tests |
| W1-B Provider split | Split the former provider monolith into shared contract, factory, Echo, HTTP, stream, messages, and policies files; largest production file is 266 lines | provider tests, Agent full tests, production import audit |
| W1-C Boundary tests | Added AST import guard, Provider contract/name checks, per-run override compatibility, and stream/non-stream tool-call normalization coverage | uncached provider tests and `go vet` |

Wave 1 deliberately preserves the broad `Reply*` request DTO and provider-owned prompt composition. Those are the semantic coupling points assigned to Wave 2, where request-body equivalence tests are required before migration.

### 2026-07-23 - Wave 2 implementation

| Workstream | Result | Verification |
| --- | --- | --- |
| Model-facing content | Moved model-context tool-result formatting to `protocol/tools.ModelFacingContent`; agent `ModelFacingToolContent` delegates; provider no longer imports `internal/tools` | protocol + provider + tools tests |
| Public resolver | Exported `provider.Resolve` as the single env/profile selection entry; adapters call `Resolve` | factory/boundary tests |
| Boundary guards | Production provider files must not import `internal/runtime`, `protocol/methods`, or `internal/tools` | AST boundary test |
