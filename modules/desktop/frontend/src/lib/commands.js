/**
 * Composer slash-command system.
 *
 * Commands are parsed client-side so Desktop can actively trigger help /
 * permission without waiting for the model.
 */

/** @typedef {'run'|'help'|'error'} CommandAction */

/**
 * @typedef {object} ParsedCommand
 * @property {CommandAction} action
 * @property {string} name          canonical command name (help, …)
 * @property {string} raw           original trimmed input
 * @property {string} displayText   text shown in the chat bubble
 * @property {string} [inputText]   payload for run.start input.text
 * @property {boolean} [requirePermission]
 * @property {string} [message]     help / error body
 * @property {boolean} isCommand    true when input started with /
 */

const HELP_TEXT = [
  '可用指令（以 / 开头）：',
  '',
  '  /help                显示本帮助',
  '',
  '其它标志（可夹在普通消息中）：',
  '  /permission          强制对本 run 走权限确认',
].join('\n');

/**
 * True when the composer text is a slash command (first non-space is `/`).
 * @param {string} text
 */
export function looksLikeCommand(text) {
  return /^\s*\//.test(String(text || ''));
}

/**
 * Parse composer text into a structured command action.
 * Non-command text becomes a normal `run` action with flag detection.
 * @param {string} text
 * @returns {ParsedCommand}
 */
export function parseCommand(text) {
  const raw = String(text || '').trim();
  if (!raw) {
    return {
      action: 'error',
      name: '',
      raw,
      displayText: '',
      message: '请输入内容',
      isCommand: false,
    };
  }

  const requirePermission = /(^|\s)\/permission(\s|$)/i.test(raw);

  if (!looksLikeCommand(raw)) {
    return {
      action: 'run',
      name: 'run',
      raw,
      displayText: raw,
      inputText: raw,
      requirePermission,
      isCommand: false,
    };
  }

  // First token is /cmd; rest is args (may contain newlines).
  const match = raw.match(/^\/([^\s]+)(?:\s+([\s\S]*))?$/);
  if (!match) {
    return {
      action: 'error',
      name: '',
      raw,
      displayText: raw,
      message: '无法解析指令',
      isCommand: true,
    };
  }

  const name = match[1].toLowerCase();
  const args = (match[2] || '').trim();

  switch (name) {
    case 'help':
    case '?':
    case 'h':
      return {
        action: 'help',
        name: 'help',
        raw,
        displayText: raw,
        message: HELP_TEXT,
      isCommand: true,
    };

    case 'permission':
      // Standalone /permission still starts a run (used by e2e fixtures).
      return {
        action: 'run',
        name: 'permission',
        raw,
        displayText: raw,
        inputText: raw,
        requirePermission: true,
        isCommand: true,
      };

    default:
      return {
        action: 'error',
        name,
        raw,
        displayText: raw,
        message: `未知指令 /${name}。输入 /help 查看可用指令。`,
        isCommand: true,
      };
  }
}

/**
 * Catalog for UI hints / slash-command palette.
 * `insert` is what gets written into the composer on pick.
 * @returns {Array<{
 *   id: string,
 *   name: string,
 *   usage: string,
 *   description: string,
 *   insert: string,
 *   keywords?: string[],
 * }>}
 */
export function listCommands() {
  return [
    {
      id: 'help',
      name: 'help',
      usage: '/help',
      description: '显示指令帮助',
      insert: '/help',
      keywords: ['帮助', '?', 'h'],
    },
    {
      id: 'permission',
      name: 'permission',
      usage: '/permission',
      description: '强制权限确认（可夹在消息中）',
      insert: '/permission ',
      keywords: ['权限', 'perm'],
    },
  ];
}

/**
 * @param {string} text
 * @param {number} max
 */
function truncateText(text, max) {
  const chars = Array.from(String(text || ''));
  if (chars.length <= max) return chars.join('');
  return `${chars.slice(0, max).join('')}…`;
}

export { HELP_TEXT };
