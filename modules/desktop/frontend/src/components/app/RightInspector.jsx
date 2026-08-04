import { useRef, useState } from 'react';
import { PanelRightClose, PanelRightOpen, X } from 'lucide-react';
import { rightPanelTabs } from '../../lib/panelLayout.js';
import { IconButton } from '../ui/button.jsx';
import { TabButton } from '../ui/tabs.jsx';
import { RightPanelResizer } from './PanelResizer.jsx';

function InspectorTabs({
  activeWorkerCount,
  pendingPermissionBadge,
  rightPanelTab,
  selectRightPanelTab,
  testIdSuffix = '',
}) {
  return rightPanelTabs.map((tab) => (
    <TabButton
      active={rightPanelTab === tab.id}
      badge={tab.id === 'workers'
        ? activeWorkerCount
        : tab.id === 'activity'
          ? pendingPermissionBadge
          : null}
      badgeTone={tab.id === 'activity' ? 'warning' : 'info'}
      data-right-panel-tab={tab.id}
      data-testid={tab.testId ? `${tab.testId}${testIdSuffix}` : undefined}
      key={tab.id}
      onClick={() => selectRightPanelTab(tab.id)}
      panelId="right-panel-content"
    >
      {tab.label}
    </TabButton>
  ));
}

export function RightInspector({
  activeWorkerCount,
  closeRightPanelDrawer,
  compactLayout,
  content,
  handleRightPanelTabsKeyDown,
  pendingPermissionBadge,
  rightPanelCloseRef,
  rightPanelDrawerOpen,
  rightPanelTab,
  rightPanelWidth,
  selectRightPanelTab,
  setRightPanelWidth,
  startRightPanelResize,
}) {
  const [desktopCollapsed, setDesktopCollapsed] = useState(false);
  const desktopCollapseRef = useRef(null);
  const desktopExpandRef = useRef(null);
  const panelCollapsed = !compactLayout && desktopCollapsed;
  const panelHidden = compactLayout ? !rightPanelDrawerOpen : panelCollapsed;

  function collapseDesktopPanel() {
    setDesktopCollapsed(true);
    window.requestAnimationFrame(() => desktopExpandRef.current?.focus());
  }

  function expandDesktopPanel() {
    setDesktopCollapsed(false);
    window.requestAnimationFrame(() => desktopCollapseRef.current?.focus());
  }

  return (
    <>
      <div
        aria-label="辅助面板"
        className="right-panel-rail"
        onKeyDown={handleRightPanelTabsKeyDown}
        role="tablist"
      >
        <InspectorTabs
          activeWorkerCount={activeWorkerCount}
          pendingPermissionBadge={pendingPermissionBadge}
          rightPanelTab={rightPanelTab}
          selectRightPanelTab={selectRightPanelTab}
          testIdSuffix="-rail"
        />
      </div>
      {panelCollapsed ? (
        <IconButton
          className="right-panel-expand"
          label="展开辅助面板"
          onClick={expandDesktopPanel}
          ref={desktopExpandRef}
          variant="soft"
        >
          <PanelRightOpen size={17} />
        </IconButton>
      ) : null}
      {rightPanelDrawerOpen ? (
        <button
          aria-hidden="true"
          aria-label="关闭辅助面板"
          className="right-panel-backdrop"
          onClick={closeRightPanelDrawer}
          tabIndex={-1}
          type="button"
        />
      ) : null}
      <aside
        aria-hidden={panelHidden ? 'true' : undefined}
        aria-label="辅助面板"
        className={[
          'right-panel',
          rightPanelDrawerOpen ? 'drawer-open' : '',
          panelCollapsed ? 'is-collapsed' : '',
        ].filter(Boolean).join(' ')}
        inert={panelHidden ? '' : undefined}
        onKeyDown={(event) => {
          if (compactLayout && event.key === 'Escape') closeRightPanelDrawer();
        }}
      >
        {!compactLayout ? (
          <RightPanelResizer
            onChange={setRightPanelWidth}
            onPointerDown={startRightPanelResize}
            value={rightPanelWidth}
          />
        ) : null}
        <div className="right-panel-mobile-header">
          <strong>{rightPanelTabs.find((tab) => tab.id === rightPanelTab)?.label || '辅助面板'}</strong>
          <IconButton label="关闭辅助面板" onClick={closeRightPanelDrawer} ref={rightPanelCloseRef}>
            <X size={17} />
          </IconButton>
        </div>
        {!compactLayout ? (
          <div
            aria-label="辅助面板"
            className="right-panel-tabs"
            onKeyDown={handleRightPanelTabsKeyDown}
            role="tablist"
          >
            <InspectorTabs
              activeWorkerCount={activeWorkerCount}
              pendingPermissionBadge={pendingPermissionBadge}
              rightPanelTab={rightPanelTab}
              selectRightPanelTab={selectRightPanelTab}
            />
            <IconButton
              className="right-panel-collapse"
              label="收起辅助面板"
              onClick={collapseDesktopPanel}
              ref={desktopCollapseRef}
            >
              <PanelRightClose size={16} />
            </IconButton>
          </div>
        ) : null}
        <div className="right-panel-content" id="right-panel-content" role="tabpanel">
          {content}
        </div>
      </aside>
    </>
  );
}
