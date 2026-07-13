import { displayStatus } from './ui/badge.jsx';

export function StatusBar({ status, runtimeStatus, runSeq }) {
  return (
    <footer className="statusbar">
      <span aria-live="polite">连接: {displayStatus(status)}</span>
      <span aria-live="polite">运行: {runtimeStatus}</span>
      <span>事件: {runSeq}</span>
    </footer>
  );
}
