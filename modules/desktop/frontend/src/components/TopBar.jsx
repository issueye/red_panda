import { RefreshCw, Settings2 } from 'lucide-react';
import mark from '../assets/red-panda-mark.svg';
import { classNames } from '../lib/format.js';
import { Button, IconButton } from './ui/button.jsx';

export function TopBar({ status, gatewayBase, onReconnect, onSettings }) {
  return (
    <header className="topbar">
      <div className="brand">
        <img alt="red_panda" src={mark} />
        <div>
          <strong>red_panda</strong>
          <span>Local AI Agent</span>
        </div>
      </div>

      <div className="topbar-actions">
        <span className={classNames('connection-pill', `state-${status}`)} data-testid="gateway-status">
          {status}
        </span>
        <span className="gateway-address">{gatewayBase}</span>
        <Button icon={<RefreshCw size={15} />} onClick={onReconnect} variant="soft">
          Reconnect
        </Button>
        <IconButton label="Settings" onClick={onSettings}>
          <Settings2 size={17} />
        </IconButton>
      </div>
    </header>
  );
}
