import { displayStatus } from './ui/badge.jsx';

export function StatusBar({ status, runtimeStatus, rootSeq }) {
  return (
    <footer className="statusbar">
      <span>WebSocket: {displayStatus(status)}</span>
      <span>运行时: {runtimeStatus}</span>
      <span>root_seq: {rootSeq}</span>
    </footer>
  );
}
