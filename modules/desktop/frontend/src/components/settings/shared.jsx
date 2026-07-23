import { useState } from 'react';
import {
  Blocks,
  Bot,
  Building2,
  PlugZap,
  ScrollText,
  Search,
  SlidersHorizontal,
  Trash2,
} from 'lucide-react';
import { Badge } from '../ui/badge.jsx';
import { Button, IconButton } from '../ui/button.jsx';
import { EmptyState, ErrorMessage } from '../ui/feedback.jsx';
import { Field } from '../ui/field.jsx';
import { SelectMenu } from '../ui/select.jsx';
import { HelpTooltip } from '../ui/tooltip.jsx';

export const RUNTIME_MODES = [
  ['per_run_process', '每次运行独立进程（推荐）'],
  ['single_core', '单核心（共享进程）'],
];

export const TOOL_POLICIES = [
  ['risk_based', '按风险处理'],
  ['allow_all', '全部允许'],
  ['ask_all', '全部询问'],
  ['deny_all', '全部拒绝'],
];

export const PERMISSION_MODES = [
  ['strict', '严格'],
  ['permissive', '宽松'],
  ['allow_all', '全部允许'],
  ['deny_all', '全部拒绝'],
];

export const WEB_SEARCH_PROVIDERS = [
  ['auto', '自动（有 Tavily Key 优先用 Tavily）'],
  ['tavily', 'Tavily（推荐，需 API Key）'],
  ['duckduckgo', 'DuckDuckGo（免费，可能被墙）'],
];

export const SETTINGS_TABS = [
  { id: 'providers', label: '供应商管理', shortLabel: '供应商', icon: Building2 },
  { id: 'workers', label: 'Worker 配置', shortLabel: 'Worker', icon: Bot },
  { id: 'skills', label: '技能管理', shortLabel: '技能', icon: Blocks },
  { id: 'mcp', label: 'MCP 管理', shortLabel: 'MCP', icon: PlugZap },
  { id: 'logs', label: '日志审计', shortLabel: '日志', icon: ScrollText },
  { id: 'other', label: '其他设置', shortLabel: '其他', icon: SlidersHorizontal },
];

// Capability tags for Worker Profiles (API field is still `phase`, docs/41 W3-4).
export const AGENT_CAPABILITY_OPTIONS = [
  ['research', '调研'],
  ['strategy', '策略'],
  ['build', '实施'],
  ['review', '验证'],
  ['assess', '评估'],
  ['general', '通用'],
  ['custom', '自定义'],
  // Legacy pipeline-era labels kept for custom profiles already using them.
  ['analyze', '分析（旧）'],
  ['plan', '规划（旧）'],
  ['execute', '执行（旧）'],
  ['verify', '验证（旧）'],
  ['evaluate', '终评（旧）'],
];

/** @deprecated use AGENT_CAPABILITY_OPTIONS */
export const AGENT_PHASE_OPTIONS = AGENT_CAPABILITY_OPTIONS;

export const emptySkillDraft = {
  name: '',
  description: '',
  instructions: '',
  path: '',
  isNew: true,
};

export const emptyMcpDraft = {
  id: '',
  name: '',
  command: '',
  args: '',
  cwd: '',
  enabled: true,
};

export const focusableSelector = [
  'button:not([disabled])',
  'input:not([disabled])',
  'textarea:not([disabled])',
  '[href]',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

export function localID(prefix) {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `${prefix}_${crypto.randomUUID()}`;
  }
  return `${prefix}_${Date.now()}_${Math.random().toString(16).slice(2)}`;
}

export function SettingRow({ children, label, tooltip }) {
  return (
    <Field className="settings-row" label={label} tooltip={tooltip}>
      {children}
    </Field>
  );
}

export function SettingSelect({ label, options, settings, settingKey, onUpdate, tooltip }) {
  return (
    <SettingRow label={label} tooltip={tooltip}>
      <SelectMenu
        ariaLabel={label}
        testId={`settings-${settingKey}`}
        options={options}
        value={settings?.[settingKey] ?? options[0]?.[0] ?? ''}
        onChange={(nextValue) => onUpdate(settingKey, nextValue)}
      />
    </SettingRow>
  );
}

export function SettingTextInput({ label, placeholder, settings, settingKey, onUpdate, tooltip }) {
  return (
    <SettingRow label={label} tooltip={tooltip}>
      <input
        type="text"
        value={settings?.[settingKey] ?? ''}
        placeholder={placeholder}
        onChange={(event) => onUpdate(settingKey, event.target.value)}
      />
    </SettingRow>
  );
}

export function SettingCheck({ checked, label, onChange, tooltip }) {
  return (
    <label className="settings-check">
      <input
        checked={Boolean(checked)}
        onChange={(event) => onChange(event.target.checked)}
        type="checkbox"
      />
      <span className="settings-check-label">
        <span>{label}</span>
        {tooltip ? <HelpTooltip content={tooltip} /> : null}
      </span>
    </label>
  );
}

export function ModuleHeader({ badge, children, title }) {
  return (
    <div className="settings-module-header">
      <div>
        <h2>{title}</h2>
        {badge ? <Badge>{badge}</Badge> : null}
      </div>
      <div className="settings-module-actions">{children}</div>
    </div>
  );
}

export function ManagerItem({ active, children, disabled = false, enabled, icon: Icon, meta, name, onDelete, onSelect, onToggle }) {
  const className = [
    'settings-manager-item',
    active ? 'active' : '',
    children ? 'has-extra-action' : '',
  ].filter(Boolean).join(' ');
  return (
    <div className={className}>
      <button className="settings-manager-select" disabled={disabled} onClick={onSelect} type="button">
        <Icon aria-hidden="true" size={16} />
        <span>
          <strong>{name}</strong>
          <small>{meta}</small>
        </span>
      </button>
      {children}
      <label className="settings-switch" title={enabled ? '停用' : '启用'}>
        <input
          aria-label={`${enabled ? '停用' : '启用'} ${name}`}
          checked={enabled}
          disabled={disabled}
          onChange={onToggle}
          type="checkbox"
        />
        <span aria-hidden="true" />
      </label>
      <IconButton disabled={disabled} label={`删除 ${name}`} onClick={onDelete}>
        <Trash2 size={14} />
      </IconButton>
    </div>
  );
}

export function McpDiscoveryReadOnlyNotice() {
  return (
    <small className="mcp-discovery-notice" data-testid="mcp-discovery-readonly-notice">
      发现：已列出服务器信息与可用工具。可在下方「试调用」直接执行一次 tools/call（不经对话 run）；对话中调用仍受工具策略与权限模式约束。
    </small>
  );
}

export function McpDiscoveryPanel({ state, onCallTool, callBusy = false }) {
  if (!state) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-empty">
        <span className="mcp-discovery-heading">可用工具</span>
        <McpDiscoveryReadOnlyNotice />
        <small>使用列表中的发现按钮读取服务器信息和工具清单。</small>
      </section>
    );
  }
  if (state.loading) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-loading">
        <span className="mcp-discovery-heading">正在发现</span>
        <small>正在连接服务器并读取工具清单（不会调用工具）。</small>
      </section>
    );
  }
  if (state.error) {
    return (
      <section className="mcp-discovery" data-testid="mcp-discovery-error">
        <span className="mcp-discovery-heading">发现失败</span>
        <ErrorMessage>{state.error}</ErrorMessage>
      </section>
    );
  }

  const servers = state.result?.servers || [];
  return (
    <section className="mcp-discovery" data-testid="mcp-discovery-result">
      <span className="mcp-discovery-heading">可用工具</span>
      <McpDiscoveryReadOnlyNotice />
      {servers.length === 0 ? <small>服务器未返回发现结果。</small> : servers.map((server, index) => {
        const healthy = ['connected', 'healthy', 'ready', 'ok', 'success'].includes(server.status);
        const info = [server.serverInfo?.name, server.serverInfo?.version].filter(Boolean).join(' ');
        return (
          <div className="mcp-discovery-server" key={`${server.name}-${index}`}>
            <div className="mcp-discovery-summary">
              <strong>{server.name || info || 'MCP 服务器'}</strong>
              <Badge tone={healthy ? 'success' : server.error ? 'danger' : 'neutral'}>
                {healthy ? '健康' : server.status}
              </Badge>
              {server.durationMs > 0 ? <small>{server.durationMs} ms</small> : null}
            </div>
            {info ? <small>{info}</small> : null}
            {server.error ? <ErrorMessage>{server.error}</ErrorMessage> : null}
            {server.stderrSummary ? <small className="mcp-discovery-stderr">{server.stderrSummary}</small> : null}
            <div className="mcp-tool-list">
              {server.tools.length === 0 ? <small>未发现工具。</small> : server.tools.map((tool) => (
                <McpToolTryCall
                  busy={callBusy}
                  key={tool.name}
                  onCallTool={onCallTool}
                  tool={tool}
                />
              ))}
            </div>
          </div>
        );
      })}
    </section>
  );
}

function McpToolTryCall({ tool, onCallTool, busy }) {
  const [open, setOpen] = useState(false);
  const [argsText, setArgsText] = useState('{}');
  const [calling, setCalling] = useState(false);
  const [result, setResult] = useState(null);
  const [error, setError] = useState('');

  async function runCall() {
    if (!onCallTool || calling || busy) {
      return;
    }
    let argumentsValue = {};
    try {
      argumentsValue = argsText.trim() ? JSON.parse(argsText) : {};
      if (!argumentsValue || typeof argumentsValue !== 'object' || Array.isArray(argumentsValue)) {
        throw new Error('arguments must be a JSON object');
      }
    } catch (parseError) {
      setError(`参数 JSON 无效：${parseError.message}`);
      setResult(null);
      return;
    }
    setCalling(true);
    setError('');
    setResult(null);
    try {
      const callResult = await onCallTool(tool.name, argumentsValue);
      setResult(callResult);
      if (callResult?.error && !callResult.ok) {
        setError(callResult.error);
      }
    } catch (callError) {
      setError(callError.message || String(callError));
    } finally {
      setCalling(false);
    }
  }

  return (
    <div className="mcp-tool-item" data-testid={`mcp-tool-${tool.name}`}>
      <div className="mcp-tool-item-head">
        <strong>{tool.name}</strong>
        {typeof onCallTool === 'function' ? (
          <Button
            data-testid={`mcp-try-call-toggle-${tool.name}`}
            disabled={busy || calling}
            onClick={() => setOpen((value) => !value)}
            variant="ghost"
          >
            {open ? '收起' : '试调用'}
          </Button>
        ) : null}
      </div>
      {tool.description ? <small>{tool.description}</small> : null}
      {open ? (
        <div className="mcp-try-call" data-testid={`mcp-try-call-${tool.name}`}>
          <label className="mcp-try-call-label">
            <span>Arguments (JSON)</span>
            <textarea
              className="mcp-try-call-args"
              data-testid={`mcp-try-call-args-${tool.name}`}
              onChange={(event) => setArgsText(event.target.value)}
              rows={4}
              spellCheck={false}
              value={argsText}
            />
          </label>
          <div className="mcp-try-call-actions">
            <Button
              data-testid={`mcp-try-call-run-${tool.name}`}
              disabled={busy || calling}
              onClick={runCall}
              variant="soft"
            >
              {calling ? '调用中…' : '执行'}
            </Button>
          </div>
          {error ? <ErrorMessage>{error}</ErrorMessage> : null}
          {result ? (
            <pre className="mcp-try-call-result" data-testid={`mcp-try-call-result-${tool.name}`}>
              {result.output || (result.ok ? '(empty output)' : result.error || '')}
              {result.durationMs > 0 ? `\n/* ${result.durationMs} ms */` : ''}
            </pre>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
