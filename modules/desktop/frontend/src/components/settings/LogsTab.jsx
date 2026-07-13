// Split from SettingsPanel.jsx (checklist R7c)
import { ModuleHeader, SettingCheck } from './shared.jsx';
import { Button } from '../ui/button.jsx';
import { EmptyState } from '../ui/feedback.jsx';
import { Badge } from '../ui/badge.jsx';
import { clearDiagnosticLogs, defaultLlmLogDirHint } from '../../lib/diagnosticLog.js';


export function LogsTab({
  diagnosticLogs,
  setDiagnosticLogs,
  settings,
  updateSetting,
}) {
  return (
    <>
          <ModuleHeader
            badge={`${diagnosticLogs.length} 条`}
            title="日志审计"
          >
            <Button
              data-testid="settings-clear-diagnostic-logs"
              onClick={() => clearDiagnosticLogs()}
              type="button"
              variant="soft"
            >
              清空客户端日志
            </Button>
          </ModuleHeader>
    
          <section className="settings-section" data-testid="settings-diagnostics">
            <h3>LLM 请求记录</h3>
            <SettingCheck
              checked={Boolean(settings?.logLlmRequests)}
              label="记录发往 LLM 的请求数据"
              onChange={(next) => updateSetting('logLlmRequests', next)}
              tooltip={
                '开启后，Agent Runtime 会把每次向模型发送的 messages/tools 请求体写入本机日志目录，便于排查上下文与工具调用问题。\n'
                + '不会写入 API Key。修改后重新发送任务生效。\n'
                + `默认目录：${defaultLlmLogDirHint()}（可用环境变量 RED_PANDA_LOG_DIR 覆盖）。`
              }
            />
            <p className="settings-hint">
              LLM 请求日志目录：
              <code data-testid="settings-llm-log-dir">{defaultLlmLogDirHint()}</code>
            </p>
            <p className="settings-hint">
              开关状态会随设置保存；关闭后新的运行不再落盘。已有 JSON 文件需手动清理。
            </p>
          </section>
    
          <section className="settings-section">
            <h3>桌面端运行日志</h3>
            <div className="settings-log-viewer settings-log-viewer-tall" data-testid="settings-diagnostic-logs">
              {diagnosticLogs.length === 0 ? (
                <EmptyState title="暂无日志">运行任务或发生错误后，客户端诊断信息会出现在这里。</EmptyState>
              ) : (
                <ul className="settings-log-list">
                  {[...diagnosticLogs].reverse().slice(0, 120).map((entry) => (
                    <li className={`settings-log-item is-${entry.level}`} key={entry.id}>
                      <div className="settings-log-meta">
                        <Badge tone={entry.level === 'error' ? 'danger' : entry.level === 'warn' ? 'warning' : 'neutral'}>
                          {entry.level}
                        </Badge>
                        <small>{entry.ts}</small>
                        <small>{entry.source}</small>
                      </div>
                      <strong>{entry.message}</strong>
                      {entry.detail ? <pre>{entry.detail}</pre> : null}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          </section>
        </>
  );
}
