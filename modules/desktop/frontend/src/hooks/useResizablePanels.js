import { useEffect, useRef, useState } from 'react';
import {
  clampLeftPanelWidth,
  clampRightPanelWidth,
  loadLeftPanelWidth,
  loadRightPanelWidth,
  persistLeftPanelWidth,
  persistRightPanelWidth,
} from '../lib/panelLayout.js';

/**
 * Left/right shell panel widths + pointer-driven resize (docs/47 Wave D).
 */
export function useResizablePanels({ compactLayout = false } = {}) {
  const [leftPanelWidth, setLeftPanelWidth] = useState(loadLeftPanelWidth);
  const [leftPanelResizing, setLeftPanelResizing] = useState(false);
  const [rightPanelWidth, setRightPanelWidth] = useState(loadRightPanelWidth);
  const [rightPanelResizing, setRightPanelResizing] = useState(false);
  const leftPanelResizeRef = useRef(null);
  const rightPanelResizeRef = useRef(null);

  useEffect(() => {
    persistLeftPanelWidth(leftPanelWidth);
  }, [leftPanelWidth]);

  useEffect(() => {
    persistRightPanelWidth(rightPanelWidth);
  }, [rightPanelWidth]);

  useEffect(() => {
    if (!leftPanelResizing) return undefined;

    function onPointerMove(event) {
      const start = leftPanelResizeRef.current;
      if (!start) return;
      setLeftPanelWidth(clampLeftPanelWidth(start.startWidth + (event.clientX - start.startX)));
    }

    function onPointerUp() {
      setLeftPanelResizing(false);
      leftPanelResizeRef.current = null;
    }

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerUp);
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerUp);
    };
  }, [leftPanelResizing]);

  useEffect(() => {
    if (!rightPanelResizing) return undefined;

    function onPointerMove(event) {
      const start = rightPanelResizeRef.current;
      if (!start) return;
      // Drag left = widen right panel.
      const next = clampRightPanelWidth(start.startWidth + (start.startX - event.clientX));
      setRightPanelWidth(next);
    }

    function onPointerUp() {
      setRightPanelResizing(false);
      rightPanelResizeRef.current = null;
    }

    window.addEventListener('pointermove', onPointerMove);
    window.addEventListener('pointerup', onPointerUp);
    window.addEventListener('pointercancel', onPointerUp);
    return () => {
      window.removeEventListener('pointermove', onPointerMove);
      window.removeEventListener('pointerup', onPointerUp);
      window.removeEventListener('pointercancel', onPointerUp);
    };
  }, [rightPanelResizing]);

  function startRightPanelResize(event) {
    if (compactLayout) return;
    event.preventDefault();
    rightPanelResizeRef.current = {
      startX: event.clientX,
      startWidth: rightPanelWidth,
    };
    setRightPanelResizing(true);
    try {
      event.currentTarget.setPointerCapture?.(event.pointerId);
    } catch {
      // Pointer capture is optional.
    }
  }

  function startLeftPanelResize(event) {
    if (compactLayout) return;
    event.preventDefault();
    leftPanelResizeRef.current = {
      startX: event.clientX,
      startWidth: leftPanelWidth,
    };
    setLeftPanelResizing(true);
    try {
      event.currentTarget.setPointerCapture?.(event.pointerId);
    } catch {
      // Pointer capture is optional.
    }
  }

  return {
    leftPanelWidth,
    setLeftPanelWidth,
    leftPanelResizing,
    rightPanelWidth,
    setRightPanelWidth,
    rightPanelResizing,
    startLeftPanelResize,
    startRightPanelResize,
  };
}
