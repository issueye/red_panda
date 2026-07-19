/**
 * Right-panel tab keyboard navigation helpers (docs/47 Wave D2).
 */

/**
 * @param {number} currentIndex
 * @param {string} key
 * @param {number} tabCount
 * @returns {number|null} next index, or null if key is not a nav key
 */
export function nextRightPanelTabIndex(currentIndex, key, tabCount) {
  if (currentIndex < 0 || tabCount <= 0) return null;
  const lastIndex = tabCount - 1;
  if (key === 'ArrowRight' || key === 'ArrowDown') {
    return currentIndex === lastIndex ? 0 : currentIndex + 1;
  }
  if (key === 'ArrowLeft' || key === 'ArrowUp') {
    return currentIndex === 0 ? lastIndex : currentIndex - 1;
  }
  if (key === 'Home') return 0;
  if (key === 'End') return lastIndex;
  return null;
}
