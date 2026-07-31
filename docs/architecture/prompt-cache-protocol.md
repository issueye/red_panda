# Prompt Cache Protocol

## Purpose

Red Panda uses provider-side prompt caching. It does not cache final answers in the application. The request pipeline keeps repeated prompt bytes stable and exposes cache effectiveness without logging credentials or prompt text.

The protocol schema version is `prompt-envelope-v2`. Increment `protocol.PromptSchemaVersion` whenever a change can alter cacheable request bytes.

## Prompt Envelope

Every provider request contains one authoritative `PromptEnvelope`:

1. `StablePrefix`: application policy and stable tool-use instructions.
2. `SessionPrefix`: session summary and other session-stable context.
3. `History`: persisted conversation history in original order.
4. `TurnTail`: current input, runtime context, Todo state, time, permissions, and attachments.

Provider adapters flatten these segments in exactly this order. During a tool loop, the envelope and canonical tool definitions are frozen. Assistant tool calls and tool results are appended after `TurnTail`; earlier segments are not recomposed.

`Request.Messages` and message-level `CacheControl` were removed. Hooks must read and transform the four named prompt segments.

## Cache Epoch

`cache_epoch` is a SHA-256 digest over:

- prompt schema version;
- provider and model;
- canonical tool definitions;
- stable prefix;
- session prefix.

It intentionally excludes credentials, raw cache keys, history, current input, Todo state, current time, tool results, and attachments. Session compaction persists `summary_digest`, `prompt_schema_version`, and `cache_epoch`; a changed summary causes one intentional epoch change.

## Provider Profiles

Provider profiles support these fields:

| Field | Meaning |
| --- | --- |
| `cache_mode` | `implicit`, `explicit`, or `disabled` |
| `cache_key_supported` | Endpoint accepts an explicit cache identity |
| `cache_retention` | Provider-specific retention value |
| `min_cache_tokens` | Diagnostic eligibility threshold |

Missing values migrate at read time. Anthropic defaults to `explicit`; OpenAI-compatible Chat and Responses default to `implicit`. Generic Chat requests never receive non-standard cache fields. Responses enables cache keys by default and sends a SHA-256 identity scoped to `session_id + cache_epoch`; an explicit profile value can disable it. `disabled` suppresses Anthropic cache breakpoints.

Model-facing tool results have an 8 KiB aggregate budget per tool round. Legacy text is represented once, and oversized structured data is omitted before text is truncated. Full tool output remains available in events and UI cards.

The migration is one-way at the protocol level: older clients can omit the fields, while updated services return normalized capability values. Clients and provider fakes that construct the removed `Messages` field must migrate to `PromptEnvelope`.

## Usage And Diagnostics

Streaming usage fragments are merged with the maximum value reported for each counter, then emitted once per provider call:

- `input_tokens`
- `output_tokens`
- `cache_read_tokens`
- `cache_write_tokens`
- `cache_hit_ratio`
- `cache_epoch`
- `prefix_hash`
- `cache_miss_reason`

The runtime records only hashes for prefix comparison. Miss reasons are:

| Reason | Meaning |
| --- | --- |
| `unsupported` | Cache mode is disabled or unavailable |
| `below_minimum` | Provider input tokens are below the configured threshold |
| `epoch_changed` | Stable cache identity changed |
| `prefix_changed` | The previous request is no longer an exact prefix |
| `provider_miss` | Eligible stable prefix was sent but provider returned no cache read |
| `unknown` | Cold call or insufficient provider usage data |

Desktop Activity aggregates these call records for each Run. Hit ratio is `sum(cache_read_tokens) / sum(input_tokens)` and is capped at 100%. Anthropic-compatible usage is normalized so `input_tokens` includes uncached input, cache creation, and cache reads before this ratio is calculated.

## Real Provider Validation

On 2026-07-31, two consecutive requests were sent to the StepFun Anthropic-compatible Messages endpoint using `step-3.7-flash`. The cacheable system prefix was identical and only the final user message changed.

| Round | Total input tokens | Cache read tokens | Hit ratio |
| --- | ---: | ---: | ---: |
| 1 | 35,021 | 0 | 0% |
| 2 | 35,021 | 34,944 | 99.78% |

The second response reported `input_tokens=77` and `cache_read_input_tokens=34,944`; total input is their sum. This validates the Anthropic cache breakpoint request shape and the normalized usage denominator used by Runtime diagnostics.

## Validation

Run the following after changing prompt composition or provider adapters:

```powershell
go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...
cd modules/desktop/frontend
npm test
npm run test:ui -- --grep "Activity timeline"
```

Any prefix-affecting change requires a schema version bump and an explicit review of the request-body golden tests.
