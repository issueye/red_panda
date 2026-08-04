import {
  clampLeftPanelWidth,
  clampRightPanelWidth,
  LEFT_PANEL_WIDTH_DEFAULT,
  LEFT_PANEL_WIDTH_MAX,
  LEFT_PANEL_WIDTH_MIN,
  RIGHT_PANEL_WIDTH_DEFAULT,
  RIGHT_PANEL_WIDTH_MAX,
  RIGHT_PANEL_WIDTH_MIN,
} from '../../lib/panelLayout.js';

function PanelResizer({
  className,
  defaultValue,
  label,
  max,
  min,
  onChange,
  onPointerDown,
  testId,
  value,
  valueForKey,
}) {
  function handleKeyDown(event) {
    const next = valueForKey(event.key, value);
    if (next === null) return;
    event.preventDefault();
    onChange(next);
  }

  return (
    <button
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemax={max}
      aria-valuemin={min}
      aria-valuenow={value}
      className={className}
      data-testid={testId}
      onDoubleClick={() => onChange(defaultValue)}
      onKeyDown={handleKeyDown}
      onPointerDown={onPointerDown}
      role="separator"
      type="button"
    />
  );
}

export function LeftPanelResizer({ onChange, onPointerDown, value }) {
  return (
    <PanelResizer
      className="left-panel-resizer"
      defaultValue={LEFT_PANEL_WIDTH_DEFAULT}
      label="拖拽调整左侧面板宽度"
      max={LEFT_PANEL_WIDTH_MAX}
      min={LEFT_PANEL_WIDTH_MIN}
      onChange={onChange}
      onPointerDown={onPointerDown}
      testId="left-panel-resizer"
      value={value}
      valueForKey={(key, current) => {
        if (key === 'ArrowLeft') return clampLeftPanelWidth(current - 16);
        if (key === 'ArrowRight') return clampLeftPanelWidth(current + 16);
        if (key === 'Home') return LEFT_PANEL_WIDTH_MIN;
        if (key === 'End') return LEFT_PANEL_WIDTH_MAX;
        return null;
      }}
    />
  );
}

export function RightPanelResizer({ onChange, onPointerDown, value }) {
  return (
    <PanelResizer
      className="right-panel-resizer"
      defaultValue={RIGHT_PANEL_WIDTH_DEFAULT}
      label="拖拽调整右侧面板宽度"
      max={RIGHT_PANEL_WIDTH_MAX}
      min={RIGHT_PANEL_WIDTH_MIN}
      onChange={onChange}
      onPointerDown={onPointerDown}
      testId="right-panel-resizer"
      value={value}
      valueForKey={(key, current) => {
        if (key === 'ArrowLeft') return clampRightPanelWidth(current + 16);
        if (key === 'ArrowRight') return clampRightPanelWidth(current - 16);
        if (key === 'Home') return RIGHT_PANEL_WIDTH_MAX;
        if (key === 'End') return RIGHT_PANEL_WIDTH_MIN;
        return null;
      }}
    />
  );
}
