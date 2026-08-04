import { expect, test } from '@playwright/test';

test('workspace opens as a dedicated modal without auxiliary tabs', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await page.getByTestId('left-tab-workspace').click();
  const toggle = page.getByTestId('workspace-panel-layout-toggle');
  await expect(toggle).toHaveAccessibleName('独立查看工作区');
  const sidebarBox = await page.locator('.sidebar').boundingBox();
  expect(sidebarBox.width).toBe(280);
  await page.getByTestId('left-tab-sessions').click();
  const sessionsBox = await page.locator('.sidebar').boundingBox();
  expect(sessionsBox.width).toBe(280);
  await page.getByTestId('left-tab-workspace').click();

  await toggle.click();
  const panel = page.getByRole('dialog', { name: '工作区' });
  await expect(panel).toBeVisible();
  await expect(toggle).toHaveAccessibleName('关闭工作区');
  await expect(panel.locator('.left-panel-tabs')).toHaveCount(0);
  await expect(panel.getByRole('tab')).toHaveCount(0);
  const expandedBox = await panel.boundingBox();
  expect(expandedBox.x).toBeGreaterThan(0);
  expect(expandedBox.x + expandedBox.width).toBeLessThan(1440);
  expect(expandedBox.width).toBeGreaterThan(1200);
  const columns = await page.locator('.workspace-panel-grid').evaluate((element) => (
    getComputedStyle(element).gridTemplateColumns
  ));
  expect(columns.split(' ')).toHaveLength(2);

  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog', { name: '工作区' })).toHaveCount(0);
  await expect(toggle).toHaveAccessibleName('独立查看工作区');
  const collapsedBox = await page.locator('.sidebar').boundingBox();
  expect(collapsedBox.width).toBe(280);

  await toggle.click();
  await page.getByRole('button', { name: '返回侧栏' }).click({ position: { x: 2, y: 2 } });
  await expect(page.getByRole('dialog', { name: '工作区' })).toHaveCount(0);
});

test('narrow app layout keeps right panel tools reachable without horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 980, height: 760 });
  await page.goto('/');

  const workspaceBox = await page.locator('.workspace').boundingBox();
  const chatBox = await page.locator('.chat-panel').boundingBox();
  const railBox = await page.locator('.right-panel-rail').boundingBox();
  const messageBox = await page.locator('.message-bubble').boundingBox();
  expect(chatBox.y).toBe(workspaceBox.y);
  await expect(page.locator('.right-panel-rail')).toHaveCSS('position', 'absolute');
  expect(railBox.y + railBox.height).toBeLessThanOrEqual(messageBox.y);

  await expect(page.getByTestId('right-tab-activity-rail')).toBeVisible();
  await page.getByTestId('right-tab-activity-rail').click();
  const drawer = page.locator('.right-panel.drawer-open');
  await expect(drawer).toBeVisible();
  await expect(drawer.locator('.right-panel-tabs')).toHaveCount(0);
  await expect(drawer.getByRole('tablist')).toHaveCount(0);
  await expect(drawer.locator('.right-panel-mobile-header strong')).toHaveText('活动');
  await expect(page.getByRole('button', { name: '展开辅助面板' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '收起辅助面板' })).toHaveCount(0);
  await expect(page.getByTestId('activity-panel')).toBeVisible();
  await expect(page.getByRole('button', { name: '关闭辅助面板' })).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(page.locator('.right-panel')).toHaveAttribute('aria-hidden', 'true');
  await expect(page.locator('.right-panel')).toHaveAttribute('inert', '');
  await expect(page.getByTestId('right-tab-activity-rail')).toBeFocused();

  await page.setViewportSize({ width: 520, height: 760 });
  const hasHorizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  expect(hasHorizontalOverflow).toBe(false);
  const compactRailBox = await page.locator('.right-panel-rail').boundingBox();
  expect(compactRailBox.width).toBeLessThan(260);
});

test('desktop side panels resize by dragging their separators', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  const sidebar = page.locator('.sidebar');
  const leftHandle = page.getByTestId('left-panel-resizer');
  const leftBefore = await sidebar.boundingBox();
  const leftHandleBox = await leftHandle.boundingBox();
  await page.mouse.move(leftHandleBox.x + leftHandleBox.width / 2, leftHandleBox.y + 120);
  await page.mouse.down();
  await page.mouse.move(leftHandleBox.x + leftHandleBox.width / 2 + 48, leftHandleBox.y + 120);
  await page.mouse.up();
  const leftAfter = await sidebar.boundingBox();
  expect(leftAfter.width).toBeGreaterThan(leftBefore.width + 40);

  const rightPanel = page.locator('.right-panel');
  await expect(rightPanel.locator('.right-panel-tabs')).toBeVisible();
  await expect(rightPanel.getByRole('tab')).toHaveCount(3);
  const chatPanel = page.locator('.chat-panel');
  const chatBeforeCollapse = await chatPanel.boundingBox();
  const rightBeforeCollapse = await rightPanel.boundingBox();
  const collapsePanel = page.getByRole('button', { name: '收起辅助面板' });
  await collapsePanel.click();
  await expect(rightPanel).toHaveClass(/is-collapsed/);
  await expect(rightPanel).toHaveAttribute('aria-hidden', 'true');
  const expandPanel = page.getByRole('button', { name: '展开辅助面板' });
  await expect(expandPanel).toBeFocused();
  const chatCollapsed = await chatPanel.boundingBox();
  expect(chatCollapsed.width).toBeGreaterThan(chatBeforeCollapse.width + rightBeforeCollapse.width - 4);
  await expandPanel.click();
  await expect(rightPanel).not.toHaveClass(/is-collapsed/);
  await expect(rightPanel).not.toHaveAttribute('aria-hidden', 'true');
  await expect(collapsePanel).toBeFocused();
  const rightHandle = page.getByTestId('right-panel-resizer');
  const rightBefore = await rightPanel.boundingBox();
  const rightHandleBox = await rightHandle.boundingBox();
  await page.mouse.move(rightHandleBox.x + rightHandleBox.width / 2, rightHandleBox.y + 120);
  await page.mouse.down();
  await page.mouse.move(rightHandleBox.x + rightHandleBox.width / 2 - 48, rightHandleBox.y + 120);
  await page.mouse.up();
  const rightAfter = await rightPanel.boundingBox();
  expect(rightAfter.width).toBeGreaterThan(rightBefore.width + 40);
});

test('settings uses the full viewport at the compact breakpoint', async ({ page }) => {
  await page.setViewportSize({ width: 520, height: 760 });
  await page.goto('/');
  await page.getByRole('button', { name: '设置' }).click();

  const panelBox = await page.getByRole('dialog', { name: '设置' }).boundingBox();
  expect(panelBox.x).toBe(0);
  expect(panelBox.width).toBe(520);
});

test('panel fixtures stay visible in narrow review viewports', async ({ page }) => {
  await page.setViewportSize({ width: 980, height: 820 });
  await page.goto('/workflow-fixture.html');
  const toolbarBounds = await page.locator('.composer-toolbar').evaluate((element) => {
    const toolbar = element.getBoundingClientRect();
    const children = Array.from(element.children).map((child) => {
      const box = child.getBoundingClientRect();
      return { left: box.left, right: box.right };
    });
    return {
      left: toolbar.left,
      right: toolbar.right,
      children,
      pageOverflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    };
  });
  expect(toolbarBounds.pageOverflow).toBe(0);
  expect(toolbarBounds.children.every((child) => (
    child.left >= toolbarBounds.left && child.right <= toolbarBounds.right
  ))).toBe(true);

  await page.setViewportSize({ width: 390, height: 760 });
  await page.goto('/activity-fixture.html');
  await expect(page.getByTestId('activity-panel')).toBeVisible();
  await page.getByTestId('activity-run-toggle').click();
  await expect(page.getByTestId('activity-event-timeline')).toBeVisible();

  await page.goto('/memory-fixture.html');
  await expect(page.getByTestId('memory-panel')).toBeVisible();
  await expect(page.getByTestId('memory-item')).toHaveCount(2);
});
