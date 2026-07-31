import assert from "node:assert/strict";
import test from "node:test";
import {
  aggregateCacheMetrics,
  displayCacheMissReason,
  formatCacheRatio,
  formatTokenCount,
} from "./cacheMetrics.js";

test("aggregateCacheMetrics sums provider calls and uses cached/input ratio", () => {
  const metrics = aggregateCacheMetrics([
    { type: "usage", runSeq: 1, payload: { input_tokens: 15028, cache_read_tokens: 0, cache_miss_reason: "unknown" } },
    { type: "usage", runSeq: 2, payload: { input_tokens: 15028, cache_read_tokens: 14912, cache_write_tokens: 4, cache_mode: "implicit" } },
  ]);
  assert.equal(metrics.calls, 2);
  assert.equal(metrics.hitCalls, 1);
  assert.equal(metrics.inputTokens, 30056);
  assert.equal(metrics.cacheReadTokens, 14912);
  assert.equal(metrics.cacheWriteTokens, 4);
  assert.equal(formatCacheRatio(metrics.hitRatio), "49.6%");
  assert.equal(metrics.latestMissReason, "");
});

test("latest cache miss reason is translated without exposing cache identities", () => {
  const metrics = aggregateCacheMetrics([
    { type: "usage", runSeq: 3, payload: { input_tokens: 128, cache_read_tokens: 0, cache_miss_reason: "below_minimum", cache_epoch: "opaque" } },
  ]);
  assert.equal(metrics.latestMissReason, "below_minimum");
  assert.equal(displayCacheMissReason(metrics.latestMissReason), "输入未达到缓存门槛");
  assert.equal(formatTokenCount(14912), "14,912");
});
