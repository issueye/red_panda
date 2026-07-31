# Prompt Cache Protocol

## Purpose

Red Panda uses provider-side prompt caching. It does not cache final answers in the application. The request pipeline keeps repeated prompt bytes stable and exposes cache effectiveness without logging credentials or prompt text.

The protocol schema version is `prompt-envelope-v1`. Increment `protocol.PromptSchemaVersion` whenever a change can alter cacheable request bytes.

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

Missing values migrate at read time. Anthropic defaults to `explicit`; OpenAI-compatible Chat and Responses default to `implicit`. Generic Chat requests never receive non-standard cache fields. Responses sends `prompt_cache_key` and retention only when the profile declares cache-key support. `disabled` suppresses Anthropic cache breakpoints.

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

Desktop Activity aggregates these call records for each Run. Hit ratio is `sum(cache_read_tokens) / sum(input_tokens)` and is capped at 100%.

## Real Provider Validation

On 2026-07-31, three consecutive requests were sent to a StepFun OpenAI-compatible endpoint using `step-3.7-flash`. The system prefix was identical and only the final user round marker changed.

| Round | Input tokens | Cached tokens | Hit ratio | Elapsed |
| --- | ---: | ---: | ---: | ---: |
| 1 | 15,028 | 0 | 0% | 2,886 ms |
| 2 | 15,028 | 14,912 | 99.23% | 1,291 ms |
| 3 | 15,028 | 14,912 | 99.23% | 1,165 ms |

The response returned cached tokens in both `usage.cached_tokens` and `usage.prompt_tokens_details.cached_tokens`. The compatible adapter accepts either location and uses the larger value when both are present.

## Validation

Run the following after changing prompt composition or provider adapters:

```powershell
go test ./modules/agent/... ./modules/gateway/... ./modules/protocol/...
cd modules/desktop/frontend
npm test
npm run test:ui -- --grep "Activity timeline"
```

Any prefix-affecting change requires a schema version bump and an explicit review of the request-body golden tests.
