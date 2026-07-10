import { RefreshCw, Settings2 } from 'lucide-react';
import mark from '../assets/red-panda-mark.svg';
import { classNames } from '../lib/format.js';
import { Button, IconButton } from './ui/button.jsx';
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
        <Button icon={<RefreshCw size={15} />} onClick={onReconnect} variant="soft">
          重新连接
        </Button>
        <IconButton label="设置" onClick={onSettings}>
          <Settings2 size={17} />
        </IconButton>
      </div>
    </header>
  );
}
