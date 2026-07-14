/**
 * Composer slash-command system.
 *
 * Commands are parsed client-side so Desktop can actively trigger Goal /
 * continue / cancel without waiting for the model to call goal.create.
 */

/** @typedef {'run'|'start_goal'|'continue_goal'|'cancel_goal'|'help'|'error'} CommandAction */

/**
 * @typedef {object} ParsedCommand
 * @property {CommandAction} action
 * @property {string} name          canonical command name (goal, help, …)
 * @property {string} raw           original trimmed input
 * @property {string} displayText   text shown in the chat bubble
 * @property {string} [inputText]   payload for run.start input.text
 * @property {string} [objective]   goal objective when starting
 * @property {string} [successCriteria]
 * @property {string} [title]
 * @property {string} [extraText]   user notes for continue
 * @property {boolean} [requirePermission]
 * @property {string} [message]     help / error body
 * @property {boolean} isCommand    true when input started with /
 */

const HELP_TEXT = [
  '可用指令（以 / 开头）：',
  '',
  '  /goal <目标描述>     创建并启动长程 Goal（分析 → 规划 → 执行 → 验证 → 报告）',
  '  /goal continue [补充]  继续当前已暂停/待启动的 Goal',
  '  /goal cancel         取消当前 Goal（若有运行中的任务会先停止）',
  '  /help                显示本帮助',
  '',
  '其它标志（可夹在普通消息中）：',
  '  /permission          强制对本 run 走权限确认',
  '',
  '示例：',
  '  /goal 为项目添加用户登录与会话管理',
  '  /goal continue 优先修测试失败',
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

    case 'goal':
    case 'g':
    case '目标':
      return parseGoalCommand(raw, args, { requirePermission });

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
 * @param {string} raw
 * @param {string} args
 * @param {{ requirePermission?: boolean }} flags
 * @returns {ParsedCommand}
 */
function parseGoalCommand(raw, args, flags = {}) {
  const subMatch = args.match(/^(continue|cancel|start|help|c|x)(?:\s+([\s\S]*))?$/i);
  const sub = subMatch ? subMatch[1].toLowerCase() : '';
  const rest = subMatch ? (subMatch[2] || '').trim() : args;

  if (sub === 'help' || (!sub && !rest)) {
    return {
      action: 'help',
      name: 'goal',
      raw,
      displayText: raw,
      message: [
        'Goal 指令：',
        '  /goal <目标描述>       新建并启动长程目标',
        '  /goal continue [补充]  继续当前目标',
        '  /goal cancel           取消当前目标',
      ].join('\n'),
      isCommand: true,
    };
  }

  if (sub === 'continue' || sub === 'c') {
    return {
      action: 'continue_goal',
      name: 'goal',
      raw,
      displayText: rest ? `继续目标：${rest}` : '继续目标',
      extraText: rest,
      requirePermission: flags.requirePermission,
      isCommand: true,
    };
  }

  if (sub === 'cancel' || sub === 'x') {
    return {
      action: 'cancel_goal',
      name: 'goal',
      raw,
      displayText: '取消目标',
      isCommand: true,
    };
  }

  // /goal start <obj> or bare /goal <obj>
  const objective = sub === 'start' ? rest : args;
  if (!objective) {
    return {
      action: 'error',
      name: 'goal',
      raw,
      displayText: raw,
      message: '请提供目标描述，例如：/goal 实现用户登录',
      isCommand: true,
    };
  }

  const title = truncateText(objective, 40);
  const successCriteria = '完成用户所述目标，并通过合理验证（测试/检查/可演示结果）。';
  const inputText = buildStartGoalInput({ title, objective, successCriteria });

  return {
    action: 'start_goal',
    name: 'goal',
    raw,
    displayText: `目标：${objective}`,
    inputText,
    objective,
    successCriteria,
    title,
    requirePermission: flags.requirePermission,
    isCommand: true,
  };
}

/**
 * Build the user-visible run input for a user-initiated Goal start.
 * @param {{ title: string, objective: string, successCriteria: string }} params
 */
export function buildStartGoalInput({ title, objective, successCriteria }) {
  const lines = [
    `[启动目标] ${title || '未命名'}`,
    `目标：${objective}`,
    `成功标准：${successCriteria}`,
    '',
    '本 Goal 已由用户创建并绑定到本次 run。请作为目标控制器推进结果：',
    '1. 对照成功标准找出当前最大的结果差距',
    '2. 用 goal.plan 选择或修订最有价值的下一批 actions',
    '3. 执行 action 后用 goal.observe 记录真实结果和证据',
    '4. 用 goal.assess 逐条评估成功标准，并决定继续、调整、阻塞或已满足',
    '5. 只有 persisted assessment=satisfied 且全部 criteria=met 时才能 goal.finish succeeded',
    '不要创建第二个 Goal，也不要把 Session TODO 当作 Goal 完成条件。',
  ];
  return lines.join('\n');
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
      id: 'goal',
      name: 'goal',
      usage: '/goal <目标>',
      description: '创建并启动长程 Goal',
      insert: '/goal ',
      keywords: ['目标', 'g', 'start'],
    },
    {
      id: 'goal-continue',
      name: 'goal continue',
      usage: '/goal continue [补充]',
      description: '继续当前 Goal',
      insert: '/goal continue ',
      keywords: ['继续', 'c', 'resume'],
    },
    {
      id: 'goal-cancel',
      name: 'goal cancel',
      usage: '/goal cancel',
      description: '取消当前 Goal',
      insert: '/goal cancel',
      keywords: ['取消', 'x', 'stop'],
    },
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
