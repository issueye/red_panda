import { expect, test } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await page.getByRole('button', { name: '设置' }).click();
});

test('settings exposes five keyboard-accessible management modules', async ({ page }) => {
  const dialog = page.getByRole('dialog', { name: '设置' });
  const tabs = dialog.getByRole('tab');
  await expect(tabs).toHaveCount(5);
  await expect(dialog.getByRole('tab', { name: '供应商' })).toHaveAttribute('aria-selected', 'true');

  await dialog.getByRole('tab', { name: '供应商' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(dialog.getByRole('tab', { name: '技能' })).toBeFocused();
  await expect(dialog.getByRole('tabpanel', { name: '技能管理' })).toBeVisible();

  await dialog.getByRole('tab', { name: '日志' }).click();
  await expect(dialog.getByRole('tabpanel', { name: '日志审计' })).toContainText('LLM 请求记录');
  await expect(dialog.getByTestId('settings-diagnostics')).toBeVisible();

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

test('MCP discovery shows read-only server health and tools', async ({ page }) => {
  await page.route('**/api/v1/mcp/servers', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          servers: [{
            id: 'mcp_srv_fixture',
            name: 'filesystem',
            command: 'fixture-mcp',
            args: [],
            enabled: true,
          }],
        },
      }),
    });
  });
  await page.route('**/api/v1/mcp/servers/mcp_srv_fixture/discover', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ok: true,
        data: {
          servers: [{
            name: 'filesystem',
            status: 'connected',
            server_info: { name: 'Filesystem MCP', version: '1.2.0' },
            tools: [{ name: 'read_file', description: '读取工作区文件' }],
            duration_ms: 24,
            env: { SECRET_TOKEN: 'must-not-render' },
          }],
        },
      }),
    });
  });

  await page.reload();
  await page.getByRole('button', { name: '设置' }).click();
  await page.getByRole('tab', { name: 'MCP' }).click();
  const discover = page.getByRole('button', { name: '发现 filesystem 工具' });
  await expect(discover).toHaveAttribute('title', '发现 filesystem 工具');
  await discover.click();

  const result = page.getByTestId('mcp-discovery-result');
  await expect(result).toContainText('健康');
  await expect(result).toContainText('Filesystem MCP 1.2.0');
  await expect(result).toContainText('read_file');
  await expect(result).toContainText('读取工作区文件');
  await expect(result).not.toContainText('SECRET_TOKEN');
});
