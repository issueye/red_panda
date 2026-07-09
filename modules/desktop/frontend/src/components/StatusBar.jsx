export function StatusBar({ status, runtimeStatus, rootSeq }) {
  return (
    <footer className="statusbar">
      <span>WebSocket: {status}</span>
      <span>Runtime: {runtimeStatus}</span>
      <span>root_seq: {rootSeq}</span>
    </footer>
  );
}
