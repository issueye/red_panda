const statusLabel = {
  active: '启用',
  approve: '已允许',
  cancelled: '已取消',
  cancelling: '取消中',
  closed: '已关闭',
  completed: '已完成',
  connected: '已连接',
  connecting: '连接中',
  deleted: '已删除',
  denied: '已拒绝',
  deny: '已拒绝',
  disabled: '已停用',
  disconnected: '已断开',
  error: '错误',
  failed: '失败',
  idle: '空闲',
  pending: '待处理',
  reset: '已重置',
  resolved: '已处理',
  running: '运行中',
  unknown: '未知',
  waiting_permission: '等待授权',
};

const riskLabel = {
  critical: '严重',
  high: '高',
  low: '低',
  medium: '中',
};

const memoryScopeLabel = {
  all: '全部',
  project: '项目',
  session: '会话',
};

const memoryKindLabel = {
  decision: '决策',
  fact: '事实',
  preference: '偏好',
  summary: '摘要',
  task: '任务',
  warning: '提醒',
};

const confidenceLabel = {
  high: '高',
  low: '低',
  medium: '中',
};

const runtimeModeLabel = {
  per_run_process: '每次运行独立进程',
  single_core: '单核心',
  mixed: '混合模式',
};

const sessionKindLabel = {
  compact: '压缩',
  fork: '分叉',
};

const subAgentBackendLabel = {
  in_process: '进程内',
  process_pool: '进程池',
  runtime_process: '运行时进程',
};

const eventKindLabel = {
  done: '完成',
  error: '错误',
  event: '事件',
  memory: '记忆',
  message: '消息',
  permission: '授权',
  tool: '工具',
};

function displayFrom(map, value, fallback = '未知') {
  return map[value] || value || fallback;
}

export function displayStatus(value) {
  return displayFrom(statusLabel, value);
}

export function displayRisk(value) {
  return displayFrom(riskLabel, value);
}

export function displayMemoryScope(value) {
  return displayFrom(memoryScopeLabel, value);
}

export function displayMemoryKind(value) {
  return displayFrom(memoryKindLabel, value);
}

export function displayConfidence(value) {
  return displayFrom(confidenceLabel, value);
}

export function displayRuntimeMode(value) {
  return displayFrom(runtimeModeLabel, value);
}

export function displaySessionKind(value) {
  return displayFrom(sessionKindLabel, value, '');
}

export function displaySubAgentBackend(value) {
  return displayFrom(subAgentBackendLabel, value);
}

export function displayEventKind(value) {
  return displayFrom(eventKindLabel, value);
}
