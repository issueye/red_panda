import { Terminal } from 'lucide-react';
import { useLayoutEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { classNames } from '../../lib/format.js';

/**
 * Slash-command picker shown above the chat composer.
 */
export function CommandPalette({
  items = [],
  activeIndex = 0,
  onSelect,
  onHover,
  visible = false,
  anchorRef,
}) {
  const [position, setPosition] = useState(null);

  useLayoutEffect(() => {
    if (!visible || !anchorRef?.current) {
      setPosition(null);
      return undefined;
    }

    const updatePosition = () => {
      const rect = anchorRef.current?.getBoundingClientRect();
      if (!rect) return;
      const viewportPadding = 12;
      const gap = 8;
      const width = Math.min(rect.width, window.innerWidth - viewportPadding * 2);
      const left = Math.min(
        Math.max(viewportPadding, rect.left),
        Math.max(viewportPadding, window.innerWidth - viewportPadding - width),
      );
      const availableAbove = Math.max(140, rect.top - viewportPadding - gap);
      setPosition({
        bottom: Math.max(viewportPadding, window.innerHeight - rect.top + gap),
        left,
        maxHeight: Math.min(280, availableAbove),
        width,
      });
    };

    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [anchorRef, visible]);

  if (!visible || items.length === 0 || typeof document === 'undefined') {
    return null;
  }

  const panel = (
    <div
      aria-label="指令面板"
      className="composer-command-panel"
      data-testid="composer-command-panel"
      id="composer-command-panel"
      role="listbox"
      style={position || { visibility: 'hidden' }}
    >
      <div className="composer-command-panel-header">
        <Terminal aria-hidden size={12} />
        <span>指令</span>
        <em className="composer-command-panel-hint">↑↓ 选择 · Tab/Enter 填入 · Esc 关闭</em>
      </div>
      <ul className="composer-command-list" role="presentation">
        {items.map((item, index) => {
          const active = index === activeIndex;
          return (
            <li key={item.id || item.name} role="presentation">
              <button
                aria-selected={active}
                className={classNames('composer-command-item', active && 'is-active')}
                data-testid="composer-command-item"
                data-command-id={item.id}
                onMouseDown={(event) => {
                  // Prevent textarea blur before click applies.
                  event.preventDefault();
                  onSelect?.(item);
                }}
                onMouseEnter={() => onHover?.(index)}
                role="option"
                type="button"
              >
                <span className="composer-command-usage">{item.usage}</span>
                <span className="composer-command-desc">{item.description}</span>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
  );

  return createPortal(panel, document.body);
}
