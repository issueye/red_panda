import { RefreshCw, Settings2 } from 'lucide-react';
import mark from '../assets/red-panda-mark.svg';
import { classNames } from '../lib/format.js';
import { IconButton } from './ui/button.jsx';
import { StatusBadge } from './ui/badge.jsx';

export function TopBar({ status, gatewayBase, onReconnect, onSettings }) {
  return (
    <header className="topbar">
      <div className="brand">
        <img alt="red_panda" src={mark} />
        <div>
          <strong>red_panda</strong>
        </div>
      </div>

      <div className="topbar-actions">
        <StatusBadge
          className={classNames('connection-pill', `state-${status}`)}
          data-testid="gateway-status"
          status={status}
          title={`连接地址：${gatewayBase}`}
        />
        <IconButton label="重新连接" onClick={onReconnect}>
          <RefreshCw size={15} />
        </IconButton>
        <IconButton label="设置" onClick={onSettings}>
          <Settings2 size={16} />
        </IconButton>
      </div>
    </header>
  );
}
