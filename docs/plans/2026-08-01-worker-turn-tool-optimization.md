# Worker Turn and Tool Optimization Plan

**Goal:** Make delegated Worker execution bounded, observable, and predictable so `max_turns` cannot be mistaken for a tool-call limit and one assignment cannot silently consume a second full budget.

**Status:** Completed on 2026-08-01. Backend and frontend regression suites passed, Windows binaries were rebuilt, and the deployment database was preserved.

## Root Causes

- `max_turns` limited provider loop iterations, while one response could request many tools.
- A text-only final synthesis request occurred outside the loop and was not reported separately.
- Worker capture concatenated progress narration from every provider response.
- Any failed delegated assignment was automatically retried with a fresh `max_turns` budget.
- Windows Workers were not told to use PowerShell and retried failed Unix commands.
- Desktop metrics exposed tool cards but not model requests or loop budgets.

## Development Plan

1. Add separate counters for loop turns, provider requests, requested tools, and executed tools.
2. Limit one provider response to eight executed tools and total executed tools to `max_turns * 4`.
3. Preserve rejected calls as synthetic failed tool results in model history without emitting tool cards.
4. Keep the final text-only synthesis request, count it, and expose distinct max-turn and tool-budget termination flags.
5. Capture only text produced after the last tool call as the Worker report.
6. Stop retrying model-level assignment failures with a second complete budget; retain process-start recovery.
7. Add Worker prompt rules for no progress narration, workspace batch reads, and Windows PowerShell syntax.
8. Project execution statistics through Worker snapshots, protocol DTOs, events, and Desktop UI.
9. Add runtime, capture, protocol, and frontend regression tests.
10. Run full tests, build Windows binaries, and deploy them without replacing `red_panda.db`.

## Termination Semantics

| Reason | Run status | Meaning |
|---|---|---|
| `no_tools` | `completed` | The model returned a natural final response. |
| `max_turns` | `completed` | The loop budget ended and one text-only synthesis was attempted. |
| `tool_budget_reached` | `completed` | The executed-tool budget ended and one text-only synthesis was attempted. |
| `failed` | `failed` | Provider or runtime execution failed. |
| `cancelled` | `cancelled` | The user or parent run cancelled execution. |

## Success Criteria

- At most eight tools execute from one provider response.
- At most `max_turns * 4` tools execute in one assignment attempt.
- A final synthesis is visible as `provider_requests = loop_turns + 1` when it occurs.
- Assignment output contains the final report, not intermediate narration.
- Model-level failures consume one assignment budget, not two.
- The Worker panel displays loop turns, provider requests, executed tools, and the reached budget.

## Phase 2: Report Transport and Budget Relaxation

The first implementation made execution bounded, but production runs showed that the generic 8 KiB model-facing tool-round budget could truncate successful Worker reports. The root model then created replacement Assignments to recover text that already existed in the original Assignment.

### Changes

1. Raise delegated Worker defaults from 16 to 32 loop turns.
2. Raise analysis/report reserve from 8 to 16 turns, making a 31-file scope recommend 47 turns instead of 39.
3. Keep the root cap at 48 while raising the delegated Worker cap to 128.
4. Accept `file_count` and `path` in `worker.delegate` so runtime budget derivation matches `workspace.stats` guidance.
5. Remove the generic 64 KiB truncation from the direct `worker.delegate` return path.
6. Give `worker.delegate` and `worker.result` a separate 256 KiB per-round model budget, with up to 96 KiB for one report.
7. Add `worker.result` to retrieve a retained Assignment report in UTF-8-safe 24 KiB chunks using `offset` and `next_offset`.
8. Require the root orchestrator to recover transport-truncated text from the same Assignment instead of launching a replacement Worker.
9. Ask Workers to keep normal final reports within 24 KiB and reserve time for a final response.

### Additional Success Criteria

- A Worker report larger than the old 8 KiB budget reaches the root model without truncation when it fits the dedicated report budget.
- Oversized reports can be reconstructed byte-for-byte through `worker.result` chunks.
- No chunk advertises a `next_offset` beyond content that can fit the model-facing budget.
- Transport truncation alone never justifies a follow-up Assignment.
