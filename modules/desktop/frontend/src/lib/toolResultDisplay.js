const TOOL_RESULT_SCHEMA = 'red_panda.tool_result.v1';

export function parseToolResultEnvelope(output) {
  if (!output || typeof output !== 'string') return null;
  const trimmed = output.trim();
  if (!trimmed.startsWith('{')) return null;
  try {
    const parsed = JSON.parse(trimmed);
    return parsed?.schema === TOOL_RESULT_SCHEMA ? parsed : null;
  } catch {
    return null;
  }
}

export function displayToolOutput(output, error = '') {
  const parsed = parseToolResultEnvelope(output);
  if (!parsed) return output || '';

  const text = String(parsed.text || '').trim();
  const parsedError = String(parsed.error || '').trim();
  if (parsed.ok === false && (text === parsedError || text === String(error || '').trim())) {
    const raw = String(parsed.data?.raw || '').trim();
    return raw && raw !== parsedError ? raw : '';
  }
  if (text) return text;
  if (parsed.data != null && Object.keys(parsed.data || {}).length > 0) {
    return JSON.stringify(parsed.data, null, 2);
  }
  return '';
}

export function buildToolOutputSummary(output) {
  const parsed = parseToolResultEnvelope(output);
  if (!parsed) return '';
  const text = String(parsed.text || parsed.error || '').replace(/\s+/g, ' ').trim();
  if (text) return text.length > 96 ? `${text.slice(0, 96)}…` : text;
  if (parsed.meta?.truncated) return '结果已截断（完整内容见展开区）';
  return '';
}

export function isWorkerToolFallback(message) {
  if (!message || message.role === 'user' || message.visibility === 'worker_private') return false;
  if (!message.assignmentId && !message.workerId) return false;
  const text = String(message.text || '').trim();
  return text.startsWith('根据工具执行结果整理如下：')
    || text.includes('[完整内容见工具卡片，未删除]');
}
