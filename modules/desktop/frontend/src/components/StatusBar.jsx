import { displayStatus } from './ui/badge.jsx';

export function StatusBar({ status, runtimeStatus, rootSeq }) {
  return (
    <footer className="statusbar">
      <span aria-live="polite">连接: {displayStatus(status)}</span>
      <span aria-live="polite">运行: {runtimeStatus}</span>
      <span>事件: {rootSeq}</span>
    </footer>
  );
}
