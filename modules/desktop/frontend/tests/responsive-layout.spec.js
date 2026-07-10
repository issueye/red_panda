import { expect, test } from '@playwright/test';

test('narrow app layout keeps right panel tools reachable without horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 980, height: 760 });
  await page.goto('/');

  const railBox = await page.locator('.right-panel-rail').boundingBox();
  const messageBox = await page.locator('.message-bubble').boundingBox();
  expect(railBox.y + railBox.height).toBeLessThanOrEqual(messageBox.y);

  await expect(page.getByTestId('right-tab-activity-rail')).toBeVisible();
  await page.getByTestId('right-tab-activity-rail').click();
  await expect(page.locator('.right-panel.drawer-open')).toBeVisible();
  await expect(page.getByTestId('activity-panel')).toBeVisible();
  await expect(page.getByRole('button', { name: '关闭辅助面板' })).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(page.locator('.right-panel')).toHaveAttribute('aria-hidden', 'true');
  await expect(page.locator('.right-panel')).toHaveAttribute('inert', '');
  await expect(page.getByTestId('right-tab-activity-rail')).toBeFocused();

  await page.setViewportSize({ width: 520, height: 760 });
  const hasHorizontalOverflow = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  expect(hasHorizontalOverflow).toBe(false);
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
  await page.setViewportSize({ width: 390, height: 760 });
  await page.goto('/activity-fixture.html');
  await expect(page.getByTestId('activity-panel')).toBeVisible();
  await expect(page.getByTestId('activity-event-timeline')).toBeVisible();

  await page.goto('/memory-fixture.html');
  await expect(page.getByTestId('memory-panel')).toBeVisible();
  await expect(page.getByTestId('memory-item')).toHaveCount(2);
});
