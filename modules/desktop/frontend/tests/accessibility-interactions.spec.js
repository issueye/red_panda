import { expect, test } from '@playwright/test';

test('main workspace copy avoids protocol labels', async ({ page }) => {
  await page.goto('/');

  const body = page.locator('body');
  await expect(body).not.toContainText('root_seq');
  await expect(body).not.toContainText('WebSocket');
  await expect(body).not.toContainText('JSON-RPC');
  await expect(body).not.toContainText('Agent Runtime');
});

test('left and right panel tabs support keyboard navigation', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');

  await page.getByTestId('left-tab-sessions').focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByTestId('left-tab-workspace')).toBeFocused();
  await expect(page.getByTestId('left-tab-workspace')).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('Home');
  await expect(page.getByTestId('left-tab-sessions')).toBeFocused();
  await expect(page.getByTestId('left-tab-sessions')).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('End');
  await expect(page.getByTestId('left-tab-workspace')).toBeFocused();
  await expect(page.getByTestId('left-tab-workspace')).toHaveAttribute('aria-selected', 'true');

  await page.getByTestId('right-tab-workers').click();
  await page.getByTestId('right-tab-workers').focus();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByTestId('right-tab-activity')).toBeFocused();
  await expect(page.getByTestId('right-tab-activity')).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('End');
  await expect(page.getByTestId('right-tab-memory')).toBeFocused();
  await expect(page.getByTestId('right-tab-memory')).toHaveAttribute('aria-selected', 'true');

  await page.keyboard.press('Home');
  await expect(page.getByTestId('right-tab-workers')).toBeFocused();
  await expect(page.getByTestId('right-tab-workers')).toHaveAttribute('aria-selected', 'true');
});

test('settings dialog manages focus and closes with Escape', async ({ page }) => {
  await page.goto('/');

  const settingsButton = page.getByRole('button', { name: '设置' });
  await settingsButton.click();

  const dialog = page.getByRole('dialog', { name: '设置' });
  await expect(dialog).toBeVisible();
  await expect(page.getByRole('button', { name: '关闭设置' })).toBeFocused();

  await page.keyboard.press('Shift+Tab');
  await expect(dialog.getByRole('button', { name: '关闭', exact: true })).toBeFocused();

  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: '关闭设置' })).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(settingsButton).toBeFocused();
});
