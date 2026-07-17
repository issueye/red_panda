import { useCallback, useEffect, useState } from 'react';
import { Clock3, Minus, RefreshCw, Settings2, Square, X } from 'lucide-react';
import mark from '../assets/red-panda-mark.svg';
import {
  windowClose,
  windowIsMaximised,
  windowMinimise,
  windowToggleMaximise,
} from '../lib/desktopShell.js';
import { classNames } from '../lib/format.js';
import { IconButton } from './ui/button.jsx';
import { StatusBadge } from './ui/badge.jsx';

function RestoreIcon() {
  return <span aria-hidden className="window-restore-icon" />;
}

/**
 * Global app header: brand, connection controls, and custom window chrome
 * (replaces the native Wails / OS title bar when the window is frameless).
 */
export function TopBar({ status, gatewayBase, onReconnect, onSettings, onSchedules, busy = false }) {
  const [maximised, setMaximised] = useState(false);

  const refreshMaximised = useCallback(async () => {
    setMaximised(await windowIsMaximised());
  }, []);

  useEffect(() => {
    refreshMaximised();
    const onResize = () => {
      refreshMaximised();
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [refreshMaximised]);

  function handleTitleDoubleClick(event) {
    // Ignore double-clicks that originate from interactive controls.
    if (event.target.closest('[data-no-drag], button, a, input, select, textarea')) {
      return;
    }
    windowToggleMaximise().then(refreshMaximised);
  }

  return (
    <header className="topbar" onDoubleClick={handleTitleDoubleClick}>
      <div className={classNames('topbar-drag brand', busy && 'is-busy')}>
        <span className="brand-mark-wrap" aria-hidden="true">
          <img alt="" className="brand-mark" draggable={false} src={mark} />
        </span>
        <div>
          <strong>red_panda</strong>
        </div>
      </div>

      <div className="topbar-drag topbar-spacer" aria-hidden="true" />

      <div className="topbar-actions" data-no-drag>
        <StatusBadge
          className={classNames('connection-pill', `state-${status}`)}
          data-testid="gateway-status"
          status={status}
          title={`连接地址：${gatewayBase}`}
        />
        <IconButton label="重新连接" onClick={onReconnect}>
          <RefreshCw size={15} />
        </IconButton>
        {onSchedules ? (
          <IconButton data-testid="open-schedules" label="定时任务" onClick={onSchedules}>
            <Clock3 size={16} />
          </IconButton>
        ) : null}
        <IconButton label="设置" onClick={onSettings}>
          <Settings2 size={16} />
        </IconButton>

        <div className="window-controls" role="group" aria-label="窗口控制">
          <button
            className="window-control window-control-min"
            onClick={() => windowMinimise()}
            title="最小化"
            type="button"
          >
            <Minus size={14} strokeWidth={2} />
          </button>
          <button
            className="window-control window-control-max"
            onClick={() => windowToggleMaximise().then(refreshMaximised)}
            title={maximised ? '还原' : '最大化'}
            type="button"
          >
            {maximised ? <RestoreIcon /> : <Square size={12} strokeWidth={2} />}
          </button>
          <button
            className="window-control window-control-close"
            onClick={() => windowClose()}
            title="关闭"
            type="button"
          >
            <X size={15} strokeWidth={2} />
          </button>
        </div>
      </div>
    </header>
  );
}
