const missReasonLabels = {
  unsupported: "当前接口不支持缓存",
  below_minimum: "输入未达到缓存门槛",
  epoch_changed: "缓存版本已变更",
  prefix_changed: "提示词前缀已变更",
  provider_miss: "服务端未命中",
  unknown: "首次调用或服务端未返回原因",
};

function numberValue(value) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function aggregateCacheMetrics(events) {
  const calls = (Array.isArray(events) ? events : [])
    .filter((event) => event?.type === "usage")
    .sort((left, right) => (Number(left.runSeq) || 0) - (Number(right.runSeq) || 0));
  const totals = calls.reduce(
    (result, event) => {
      const payload = event.payload || {};
      result.inputTokens += numberValue(payload.input_tokens);
      result.cacheReadTokens += numberValue(payload.cache_read_tokens);
      result.cacheWriteTokens += numberValue(payload.cache_write_tokens);
      if (numberValue(payload.cache_read_tokens) > 0) result.hitCalls += 1;
      return result;
    },
    { inputTokens: 0, cacheReadTokens: 0, cacheWriteTokens: 0, hitCalls: 0 },
  );
  const latest = calls.at(-1)?.payload || {};
  return {
    ...totals,
    calls: calls.length,
    hitRatio: totals.inputTokens > 0
      ? Math.min(1, totals.cacheReadTokens / totals.inputTokens)
      : 0,
    latestMissReason: numberValue(latest.cache_read_tokens) > 0
      ? ""
      : String(latest.cache_miss_reason || ""),
    cacheMode: String(latest.cache_mode || ""),
    cacheEpoch: String(latest.cache_epoch || ""),
  };
}

export function formatCacheRatio(value) {
  const ratio = Math.max(0, Math.min(1, Number(value) || 0));
  return `${(ratio * 100).toFixed(1)}%`;
}

export function formatTokenCount(value) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 0 }).format(numberValue(value));
}

export function displayCacheMissReason(value) {
  return missReasonLabels[value] || value || "";
}
