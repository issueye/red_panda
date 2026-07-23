import { displayStatus } from './ui/badge.jsx';

export function StatusBar({ status, runtimeStatus, runSeq, toolCount = 0 }) {
  const tools = Number(toolCount);
  const hasTools = Number.isFinite(tools) && tools > 0;
  return (
    <footer className="statusbar">
      <span aria-live="polite">连接: {displayStatus(status)}</span>
      <span aria-live="polite">运行: {runtimeStatus}</span>
      <span>事件: {runSeq}</span>
      <span aria-live="polite" data-testid="status-tool-count">
        工具: {hasTools ? tools : 0}
      </span>
    </footer>
  );
}
