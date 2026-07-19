import { useCallback, useEffect, useRef, useState } from 'react';
import { rightPanelTabs } from '../lib/panelLayout.js';
import { nextRightPanelTabIndex } from '../lib/rightPanelNav.js';

/**
 * Right-panel tab selection, compact drawer, and keyboard roving (docs/47 D2).
 */
export function useRightPanelChrome({ compactLayout = false } = {}) {
  const [rightPanelTab, setRightPanelTab] = useState('activity');
  const [rightPanelDrawerOpen, setRightPanelDrawerOpen] = useState(false);
  const rightPanelCloseRef = useRef(null);
  const rightPanelReturnFocusRef = useRef(null);

  useEffect(() => {
    if (!compactLayout || !rightPanelDrawerOpen) return undefined;
    const frame = window.requestAnimationFrame(() => rightPanelCloseRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [compactLayout, rightPanelDrawerOpen]);

  const selectRightPanelTab = useCallback((tab) => {
    setRightPanelTab(tab);
    if (compactLayout) {
      if (!rightPanelDrawerOpen) {
        rightPanelReturnFocusRef.current = document.activeElement;
      }
      setRightPanelDrawerOpen(true);
    }
  }, [compactLayout, rightPanelDrawerOpen]);

  const closeRightPanelDrawer = useCallback(() => {
    setRightPanelDrawerOpen(false);
    const returnTarget = rightPanelReturnFocusRef.current;
    window.requestAnimationFrame(() => {
      if (returnTarget && typeof returnTarget.focus === 'function' && document.contains(returnTarget)) {
        returnTarget.focus();
      }
    });
  }, []);

  const handleRightPanelTabsKeyDown = useCallback((event) => {
    const currentIndex = rightPanelTabs.findIndex((tab) => tab.id === rightPanelTab);
    const nextIndex = nextRightPanelTabIndex(currentIndex, event.key, rightPanelTabs.length);
    if (nextIndex == null) return;

    event.preventDefault();
    const tabList = event.currentTarget;
    const nextTab = rightPanelTabs[nextIndex];
    setRightPanelTab(nextTab.id);
    window.requestAnimationFrame(() => {
      tabList.querySelector(`[data-right-panel-tab="${nextTab.id}"]`)?.focus();
    });
  }, [rightPanelTab]);

  return {
    rightPanelTab,
    setRightPanelTab,
    rightPanelDrawerOpen,
    setRightPanelDrawerOpen,
    rightPanelCloseRef,
    selectRightPanelTab,
    closeRightPanelDrawer,
    handleRightPanelTabsKeyDown,
  };
}
