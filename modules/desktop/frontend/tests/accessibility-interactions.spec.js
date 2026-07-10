import { expect, test } from '@playwright/test';

test('main workspace copy avoids protocol labels', async ({ page }) => {
  await page.goto('/');

  const body = page.locator('body');
  await expect(body).not.toContainText('root_seq');
  await expect(body).not.toContainText('WebSocket');
  await expect(body).not.toContainText('JSON-RPC');
  await expect(body).not.toContainText('Agent Runtime');
});

test('right panel tabs support keyboard navigation', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await page.getByRole('tab', { name: '工作区' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: '子代理' })).toBeFocused();
  await expect(page.getByRole('tab', { name: '子代理' })).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('End');
  await expect(page.getByRole('tab', { name: '记忆' })).toBeFocused();
  await expect(page.getByRole('tab', { name: '记忆' })).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('Home');
  await expect(page.getByRole('tab', { name: '工作区' })).toBeFocused();
  await expect(page.getByRole('tab', { name: '工作区' })).toHaveAttribute('aria-selected', 'true');
});

test('settings dialog manages focus and closes with Escape', async ({ page }) => {
  await page.goto('/');

  const settingsButton = page.getByRole('button', { name: '设置' });
  await settingsButton.click();

  const dialog = page.getByRole('dialog', { name: '设置' });
  await expect(dialog).toBeVisible();
  await expect(page.getByRole('button', { name: '关闭设置' })).toBeFocused();

  await page.keyboard.press('Shift+Tab');
  await expect(page.getByRole('button', { name: '关闭', exact: true })).toBeFocused();

  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: '关闭设置' })).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(settingsButton).toBeFocused();
});
