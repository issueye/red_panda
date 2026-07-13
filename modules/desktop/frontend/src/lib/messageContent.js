/**
 * Parse assistant message text that embeds raw tool-call markup so the UI can
 * render structured tool cards instead of leaking XML/tags into the bubble.
 *
 * Supported shapes (whitespace-tolerant):
 *   <tool_call>
 *     <function=workspace__read>
 *     <parameter=path>foo.md</parameter>
 *     </function>
 *   </tool_call>
 *
 *   <tool_call>
 *     <function name="workspace.read_file">
 *       <parameter name="path">foo.md</parameter>
 *     </function>
 *   </tool_call>
 */

const TOOL_CALL_BLOCK =
  /<tool_call\b[^>]*>([\s\S]*?)<\/tool_call\s*>/gi;

const TOOL_CALL_OPEN = /<tool_call\b[^>]*>/i;

const FUNCTION_EQ = /<function\s*=\s*([^\s>]+)\s*>/i;
const FUNCTION_NAME_ATTR = /<function\b[^>]*\bname\s*=\s*["']?([^"'\s>]+)["']?/i;
const FUNCTION_CLOSE = /<\/function\s*>/i;

const PARAMETER_EQ =
  /<parameter\s*=\s*([^\s>]+)\s*>([\s\S]*?)<\/parameter\s*>/gi;
const PARAMETER_NAME_ATTR =
  /<parameter\b[^>]*\bname\s*=\s*["']?([^"'\s>]+)["']?[^>]*>([\s\S]*?)<\/parameter\s*>/gi;

const TOOL_DISPLAY_NAMES = {
  'shell.exec': 'Shell command',
  'workspace.list': 'List files',
  'workspace.read': 'Read file',
  'workspace.read_file': 'Read file',
  'workspace.write_file': 'Write file',
  'workspace.stats': 'Workspace stats',
  'workspace.search': 'Search files',
  'web.search': 'Web search',
  'web.fetch': 'Web fetch',
  'worker.delegate': 'Delegate work',
  'worker.send': 'Send to Worker',
  'worker.receive': 'Receive from Worker',
  'memory.create': 'Create memory',
  'memory.search': 'Search memory',
  'skill.run': 'Run skill',
};

/**
 * @param {string} name
 * @returns {string}
 */
export function normalizeToolName(name) {
  const raw = String(name || '').trim();
  if (!raw) return '';
  // Models often emit workspace__read or workspace.read_file
  let normalized = raw.replace(/__/g, '.');
  // Common shorthand: workspace.read -> workspace.read_file
  if (normalized === 'workspace.read') {
    normalized = 'workspace.read_file';
  }
  if (normalized === 'workspace.write') {
    normalized = 'workspace.write_file';
  }
  return normalized;
}

/**
 * @param {string} name
 * @returns {string}
 */
export function toolDisplayName(name) {
  const normalized = normalizeToolName(name);
  if (TOOL_DISPLAY_NAMES[normalized]) {
    return TOOL_DISPLAY_NAMES[normalized];
  }
  const short = normalized.split('.').pop() || normalized;
  return short
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, (ch) => ch.toUpperCase());
}

/**
 * @param {string} body
 * @returns {{ name: string, arguments: Record<string, string> } | null}
 */
export function parseToolCallBody(body) {
  const text = String(body || '');
  if (!text.trim()) return null;

  let name = '';
  const eqMatch = text.match(FUNCTION_EQ);
  if (eqMatch) {
    name = eqMatch[1];
  } else {
    const attrMatch = text.match(FUNCTION_NAME_ATTR);
    if (attrMatch) name = attrMatch[1];
  }
  name = normalizeToolName(name);
  if (!name) return null;

  /** @type {Record<string, string>} */
  const args = {};

  text.replace(PARAMETER_EQ, (_full, key, value) => {
    const k = String(key || '').trim();
    if (k) args[k] = String(value || '').trim();
    return '';
  });
  text.replace(PARAMETER_NAME_ATTR, (_full, key, value) => {
    const k = String(key || '').trim();
    if (k && args[k] === undefined) args[k] = String(value || '').trim();
    return '';
  });

  return { name, arguments: args };
}

/**
 * @param {string} source
 * @returns {boolean}
 */
export function messageHasToolCallMarkup(source) {
  return TOOL_CALL_OPEN.test(String(source || ''));
}

/**
 * Split assistant message text into text / tool_call segments.
 * Incomplete trailing `<tool_call>` (still streaming) becomes a pending tool segment.
 *
 * @param {string} source
 * @returns {Array<{ type: 'text', text: string } | { type: 'tool_call', name: string, arguments: Record<string, string>, raw: string, complete: boolean }>}
 */
export function parseMessageContent(source) {
  const text = typeof source === 'string' ? source : '';
  if (!text) return [];

  /** @type {Array<{ type: 'text', text: string } | { type: 'tool_call', name: string, arguments: Record<string, string>, raw: string, complete: boolean }>} */
  const segments = [];
  let cursor = 0;
  const re = new RegExp(TOOL_CALL_BLOCK.source, 'gi');
  let match = re.exec(text);

  while (match) {
    const start = match.index;
    const end = start + match[0].length;
    if (start > cursor) {
      const before = text.slice(cursor, start);
      if (before.trim()) {
        segments.push({ type: 'text', text: before });
      }
    }

    const body = match[1] || '';
    const parsed = parseToolCallBody(body);
    if (parsed) {
      segments.push({
        type: 'tool_call',
        name: parsed.name,
        arguments: parsed.arguments,
        raw: match[0],
        complete: true,
      });
    } else {
      // Unrecognized body — keep raw text so we don't silently drop content.
      segments.push({ type: 'text', text: match[0] });
    }

    cursor = end;
    match = re.exec(text);
  }

  const rest = text.slice(cursor);
  if (rest) {
    const openIndex = rest.search(TOOL_CALL_OPEN);
    if (openIndex >= 0) {
      const before = rest.slice(0, openIndex);
      if (before.trim()) {
        segments.push({ type: 'text', text: before });
      }
      const incomplete = rest.slice(openIndex);
      const parsed = parseToolCallBody(incomplete);
      segments.push({
        type: 'tool_call',
        name: parsed?.name || 'tool',
        arguments: parsed?.arguments || {},
        raw: incomplete,
        complete: false,
      });
    } else if (rest.trim()) {
      segments.push({ type: 'text', text: rest });
    }
  }

  return segments;
}

/**
 * Build a ToolCallCard-compatible item from a parsed message tool call.
 *
 * @param {object} segment
 * @param {object} message
 * @param {number} index
 */
export function toolItemFromMessageSegment(segment, message, index) {
  const name = normalizeToolName(segment.name);
  return {
    id: `${message.id || message.messageId || 'msg'}_inline_tool_${index}`,
    name,
    displayName: toolDisplayName(name),
    arguments: segment.arguments || {},
    status: segment.complete === false ? 'running' : 'completed',
    output: '',
    error: '',
    source: 'message_text',
    runId: message.runId || '',
    assignmentId: message.assignmentId || '',
    workerId: message.workerId || '',
    profileKey: message.profileKey || '',
    runSeq: message.runSeq || 0,
  };
}
