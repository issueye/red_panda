import { expect, test } from '@playwright/test';

test('Memory panel manages records and preview in browser', async ({ page }) => {
  await page.goto('/memory-fixture.html');

  await expect(page.getByTestId('memory-panel')).toBeVisible();
  await expect(page.getByTestId('memory-item')).toHaveCount(2);
  await expect(page.getByText('Project command')).toBeVisible();
  await expect(page.getByText('Session decision')).toBeVisible();

  await page.getByTestId('memory-item').filter({ hasText: 'Session decision' }).click();
  await expect(page.getByTestId('memory-title-input')).toHaveValue('Session decision');
  await page.getByTestId('memory-content-input').fill('Keep the Memory panel compact and inspectable.');
  await page.getByTestId('memory-save').click();
  await expect(page.getByTestId('memory-item').filter({ hasText: 'Session decision' })).toContainText('active');

  await page.getByRole('button', { name: 'New' }).click();
  await page.getByTestId('memory-title-input').fill('Created memory');
  await page.getByTestId('memory-content-input').fill('Created from the Memory panel.');
  await page.getByTestId('memory-save').click();
  await expect(page.getByText('Created memory')).toBeVisible();

  await page.getByText('Created memory').click();
  await page.getByRole('button', { name: 'Disable' }).click();
  await expect(page.getByTestId('memory-list')).not.toContainText('Created memory');

  await page.getByRole('button', { name: 'Preview' }).click();
  await expect(page.getByTestId('memory-preview-context')).toContainText('Session decision');
  await expect(page.getByTestId('memory-preview-context')).toContainText('Project command');
  await expect(page.getByTestId('memory-preview-context')).not.toContainText('Disabled note');

  await page.getByTestId('memory-item').filter({ hasText: 'Project command' }).click();
  await page.getByLabel('Delete memory').click();
  await expect(page.getByTestId('memory-list')).not.toContainText('Project command');
});
