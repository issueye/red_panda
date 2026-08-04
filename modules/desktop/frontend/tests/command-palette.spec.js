import { expect, test } from '@playwright/test';

test('command button opens the command palette without a slash trigger', async ({ page }) => {
  await page.goto('/');

  const input = page.getByTestId('chat-composer-input');
  await input.fill('/');

  await expect(page.getByTestId('composer-command-panel')).toHaveCount(0);
  await page.getByTestId('composer-command-trigger').click();

  const panel = page.getByTestId('composer-command-panel');
  await expect(panel).toBeVisible();
  await expect(panel).toHaveCSS('position', 'fixed');
  expect(await panel.evaluate((element) => element.parentElement === document.body)).toBe(true);
  await expect(panel).toContainText('/help');
  await panel.getByTestId('composer-command-item').first().click();
  await expect(panel).toHaveCount(0);
});
