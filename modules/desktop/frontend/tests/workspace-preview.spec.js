import { expect, test } from '@playwright/test';

test('workspace renders Markdown files and keeps other content as source text', async ({ page }) => {
  await page.goto('/workspace-fixture.html');

  await page.getByRole('button', { name: 'report.md' }).click();
  const preview = page.locator('.workspace-markdown-preview');
  await expect(preview.getByRole('heading', { name: 'Workspace report' })).toBeVisible();
  await expect(preview.locator('strong')).toHaveText('formatted Markdown');
  await expect(preview.locator('table')).toContainText('Ready');
  await expect(preview.locator('pre code')).toContainText('const rendered = true;');
  await expect(preview.locator('input[type="checkbox"]')).toHaveCount(2);
  await expect(preview.locator('script')).toHaveCount(0);
  await expect(preview).toContainText('<script>window.fixtureUnsafeHtml = true</script>');

  await page.getByRole('button', { name: '差异' }).click();
  await expect(page.locator('.workspace-markdown-preview')).toHaveCount(0);
  await expect(page.locator('.workspace-preview-source')).toContainText('+# New title');

  await page.getByRole('button', { name: 'notes.txt' }).click();
  await expect(page.locator('.workspace-markdown-preview')).toHaveCount(0);
  await expect(page.locator('.workspace-preview-source')).toHaveText('# Plain text');
});
