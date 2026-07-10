import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await page.getByRole('button', { name: '设置' }).click();
});

test('settings exposes four keyboard-accessible management modules', async ({ page }) => {
  const dialog = page.getByRole('dialog', { name: '设置' });
  const tabs = dialog.getByRole('tab');
  await expect(tabs).toHaveCount(4);
  await expect(dialog.getByRole('tab', { name: '供应商' })).toHaveAttribute('aria-selected', 'true');

  await dialog.getByRole('tab', { name: '供应商' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(dialog.getByRole('tab', { name: '技能' })).toBeFocused();
  await expect(dialog.getByRole('tabpanel', { name: '技能管理' })).toBeVisible();

  await page.keyboard.press('End');
  await expect(dialog.getByRole('tab', { name: '其他' })).toBeFocused();
  await expect(dialog.getByRole('tabpanel', { name: '其他设置' })).toContainText('运行与代理');
});

test('skill configs can be managed and persist locally', async ({ page }) => {
  await page.getByRole('tab', { name: '技能' }).click();
  await page.getByRole('button', { name: '新建技能' }).click();
  await page.getByLabel('名称').fill('代码审查');
  await page.getByLabel('路径').fill('C:\\skills\\review');
  await page.getByLabel('描述').fill('检查代码质量与风险');
  await page.getByRole('button', { name: '保存技能' }).click();
  await expect(page.getByTestId('settings-skill-list')).toContainText('代码审查');

  await page.getByRole('button', { name: '关闭设置' }).click();
  await page.reload();
  await page.getByRole('button', { name: '设置' }).click();
  await page.getByRole('tab', { name: '技能' }).click();
  await expect(page.getByTestId('settings-skill-list')).toContainText('代码审查');
});

test('settings management layout remains usable on compact screens', async ({ page }) => {
  await page.setViewportSize({ width: 520, height: 760 });
  await page.getByRole('tab', { name: '技能' }).click();
  await page.getByRole('button', { name: '新建技能' }).click();

  const panelBox = await page.getByRole('dialog', { name: '设置' }).boundingBox();
  expect(panelBox.x).toBe(0);
  expect(panelBox.width).toBe(520);
  expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false);
  await expect(page.getByRole('button', { name: '保存技能' })).toBeVisible();
});
