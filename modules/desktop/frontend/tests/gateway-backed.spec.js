import { expect, test } from '@playwright/test';
import { createServer } from 'node:http';
import path from 'node:path';
import { startGateway } from './helpers/gatewayProcess.js';

test.describe.configure({ mode: 'serial' });

async function chooseMenuOption(trigger, optionName) {
  await trigger.click();
  await trigger.page().getByRole('option', { name: optionName, exact: true }).click();
}

test('Gateway-backed desktop run renders chat tools and timeline @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('read file README.md');
    await page.getByTestId('chat-composer-send').click();

    await expect(page.getByTestId('message-row').filter({ hasText: 'read file README.md' })).toBeVisible();
    const readTool = page.getByTestId('tool-card').filter({ hasText: 'workspace.read_file' });
    await expect(readTool).toHaveClass(/tool-completed/, { timeout: 20000 });
    await readTool.getByTestId('tool-card-toggle').click();
    await readTool.locator('.tool-detail-toggle').filter({ hasText: '输出' }).click();
    await expect(readTool.getByTestId('tool-output')).toContainText(/# red_panda|Go-based local AI Agent/i, { timeout: 20000 });

    await page.getByTestId('right-tab-activity').click();
    const activity = page.getByTestId('activity-panel');
    await expect(activity).toBeVisible();
    const runRow = activity.getByTestId('activity-run').filter({ hasText: 'read file README.md' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已完成', { timeout: 10000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await expect(runRow.getByTestId('activity-run-toggle')).toBeVisible();
      await runRow.getByTestId('activity-run-toggle').click();
    }
    await expect(runRow.getByTestId('activity-event-timeline')).toBeVisible();
    await expect(runRow.getByTestId('activity-event-count')).toHaveText(/^\d\/[4-6]$/, { timeout: 15000 });

    await chooseMenuOption(runRow.getByTestId('activity-event-kind-filter'), '工具');
    await expect(runRow.getByTestId('activity-event-count')).toHaveText(/^[1-3]\/[4-6]$/);
    await expect(runRow.getByTestId('activity-event-timeline')).toContainText('workspace.read_file');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop renders denied tool failure path @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'single_core',
        toolPolicy: 'allow_all',
        permissionMode: 'strict',
        providerProfileId: '',
        spawnSubAgents: false,
        subAgentBackend: 'in_process',
        model: '',
        toolAllowlist: '',
        toolDenylist: 'shell.exec',
      }));
    });
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('run shell echo denied-e2e');
    await page.getByTestId('chat-composer-send').click();

    const shellTool = page.getByTestId('tool-card').filter({ hasText: 'shell.exec' });
    await expect(shellTool).toHaveClass(/tool-denied/, { timeout: 20000 });
    await expect(shellTool).toContainText('tool is denied by tool_denylist', { timeout: 20000 });
    await expect(page.getByTestId('message-row').filter({ hasText: 'tool is denied by tool_denylist' })).toBeVisible({ timeout: 20000 });

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: 'run shell echo denied-e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已完成', { timeout: 15000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    await expect(runRow.getByTestId('activity-event-timeline')).toContainText('tool_failed', { timeout: 15000 });
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop forks and summarizes a session in place @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('read file README.md');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('tool-card').filter({ hasText: 'workspace.read_file' })).toHaveClass(/tool-completed/, { timeout: 20000 });

    await page.getByTestId('session-fork').click();
    const forkedSession = page.getByTestId('session-item').filter({ hasText: '的分叉' });
    await expect(forkedSession).toBeVisible({ timeout: 15000 });
    await expect(forkedSession).toContainText('分叉');
    await expect(page.getByTestId('message-row').filter({ hasText: 'read file README.md' }).first()).toBeVisible({ timeout: 15000 });

    const sessionsBefore = await page.getByTestId('session-item').count();
    const applied = page.waitForResponse((response) => (
      response.request().method() === 'POST'
      && /\/api\/v1\/sessions\/[^/]+\/compact$/.test(new URL(response.url()).pathname)
    ));
    await page.getByTestId('session-compact').click();
    await applied;
    await expect(page.getByTestId('session-item')).toHaveCount(sessionsBefore);
    await expect(page.getByTestId('message-row').filter({ hasText: 'read file README.md' }).first()).toBeVisible();
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop manages memory and shows injection event @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('right-tab-memory').click();
    await expect(page.getByTestId('memory-panel')).toBeVisible();
    await chooseMenuOption(page.getByLabel('记忆范围', { exact: true }), '会话');
    await page.getByTestId('memory-title-input').fill('Gateway memory');
    await page.getByTestId('memory-content-input').fill('Gateway-backed memory should be injected.');
    await page.getByTestId('memory-save').click();
    await expect(page.getByTestId('memory-item').filter({ hasText: 'Gateway memory' })).toBeVisible({ timeout: 15000 });

    await page.getByRole('button', { name: '预览' }).click();
    await expect(page.getByTestId('memory-preview-context')).toContainText('Gateway-backed memory should be injected.', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('memory injection e2e');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('message-row').filter({ hasText: 'memory injection e2e' }).first()).toBeVisible();

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: 'memory injection e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已完成', { timeout: 20000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    const timeline = runRow.getByTestId('activity-event-timeline');
    await expect(timeline).toContainText('memory_injected', { timeout: 15000 });
    await chooseMenuOption(runRow.getByTestId('activity-event-kind-filter'), '记忆');
    await expect(runRow.getByTestId('activity-event-count')).toContainText('1/');
    await expect(timeline).toContainText('memory_injected');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop manages MCP server configs @gateway-backed', async ({ page }) => {
  test.setTimeout(60000);
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByRole('button', { name: '设置' }).click();
    await page.getByRole('dialog', { name: '设置' }).getByRole('tab', { name: 'MCP' }).click();
    await page.getByRole('button', { name: '新建服务器' }).click();
    await page.getByPlaceholder('filesystem', { exact: true }).fill('filesystem');
    await page.getByPlaceholder('npx', { exact: true }).fill('npx');
    await page.getByPlaceholder('-y\n@modelcontextprotocol/server-filesystem\n.', { exact: true }).fill('-y\n@modelcontextprotocol/server-filesystem\n.');
    await page.getByRole('button', { name: '创建服务器' }).click();

    const list = page.getByTestId('settings-mcp-list');
    await expect(list).toContainText('filesystem', { timeout: 15000 });
    const created = await apiJson(gateway.baseURL, '/api/v1/mcp/servers');
    expect(created.servers).toHaveLength(1);
    expect(created.servers[0].args).toEqual(['-y', '@modelcontextprotocol/server-filesystem', '.']);

    const enabledToggle = page.getByLabel('停用 filesystem');
    await expect(enabledToggle).toBeChecked();
    await enabledToggle.locator('..').click();
    await expect(page.getByLabel('启用 filesystem')).not.toBeChecked({ timeout: 15000 });
    await expect.poll(async () => {
      const current = await apiJson(gateway.baseURL, `/api/v1/mcp/servers/${encodeURIComponent(created.servers[0].id)}`);
      return current.enabled;
    }, { timeout: 15000 }).toBe(false);

    await page.getByRole('button', { name: '删除 filesystem' }).click();
    await page.getByTestId('confirm-delete-mcp-ok').click();
    await expect(list).not.toContainText('filesystem');
    const afterDelete = await apiJson(gateway.baseURL, '/api/v1/mcp/servers');
    expect(afterDelete.servers).toHaveLength(0);
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop keeps selected inactive provider profile after run.start failure @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    const inactiveProfile = await createInactiveProviderProfile(gateway.baseURL);
    await page.addInitScript((profileId) => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'single_core',
        toolPolicy: 'risk_based',
        permissionMode: 'strict',
        providerProfileId: profileId,
        spawnSubAgents: false,
        subAgentBackend: 'in_process',
        model: '',
        toolAllowlist: '',
        toolDenylist: '',
      }));
    }, inactiveProfile.id);
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('provider profile should fail');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('message-row').filter({ hasText: '启动运行失败：' })).toContainText('inactive', { timeout: 20000 });

    await page.getByRole('button', { name: '设置', exact: true }).click();
    await expect(page.getByTestId('settings-providerProfileId')).toContainText('Inactive E2E Provider', { timeout: 15000 });
    await expect(page.getByTestId('settings-providerProfileId')).toContainText('Inactive E2E Provider');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop renders denied permission path @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/permission deny e2e');
    await page.getByTestId('chat-composer-send').click();

    const permissionCard = page.getByTestId('permission-card').filter({ hasText: 'protected checkpoint' });
    await expect(permissionCard).toBeVisible({ timeout: 15000 });
    await permissionCard.getByTestId('permission-deny').click();
    await expect(permissionCard.getByTestId('permission-state')).toHaveText('已拒绝', { timeout: 10000 });

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/permission deny e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已拒绝', { timeout: 15000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    await expect(runRow.getByTestId('activity-event-timeline')).toContainText('permission_required', { timeout: 15000 });
    await expect(runRow.getByTestId('activity-event-timeline')).toContainText('error', { timeout: 15000 });
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop cancels a waiting permission run @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/permission cancel e2e');
    await page.getByTestId('chat-composer-send').click();

    await expect(page.getByTestId('permission-card').filter({ hasText: 'protected checkpoint' })).toBeVisible({ timeout: 15000 });
    await page.getByTestId('chat-composer-cancel').click();
    await expect(page.getByTestId('message-row').filter({ hasText: '运行已取消。' })).toBeVisible({ timeout: 20000 });

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/permission cancel e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已取消', { timeout: 15000 });
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop renders runtime_process subagent failure @gateway-backed', async ({ page }) => {
  const missingSubAgent = path.join(process.cwd(), '..', '..', '..', 'bin', 'missing-red-panda-subagent.exe');
  const gateway = await startGateway({
    env: {
      RED_PANDA_SUBAGENT_COMMAND: missingSubAgent,
    },
  });
  try {
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'single_core',
        toolPolicy: 'risk_based',
        permissionMode: 'strict',
        providerProfileId: '',
        spawnSubAgents: true,
        subAgentBackend: 'runtime_process',
        model: '',
        toolAllowlist: '',
        toolDenylist: '',
      }));
    });
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/subagent failure e2e');
    await page.getByTestId('chat-composer-send').click();

    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    const failedSubAgent = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
    await expect(failedSubAgent).toContainText('失败', { timeout: 20000 });
    await expect(failedSubAgent).toContainText('runtime_process');
    await expect(failedSubAgent).toContainText('runtime_process planner subagent failed');

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/subagent failure e2e' });
    await expect(runRow).toBeVisible({ timeout: 15000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    const timeline = runRow.getByTestId('activity-event-timeline');
    await expect(timeline).toContainText('subagent_update', { timeout: 15000 });
    await expect(timeline).toContainText('missing-red-panda-subagent');
    await expect(timeline).toContainText('子代理 · planner');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop cancels a running subagent @gateway-backed', async ({ page }) => {
  const gateway = await startGateway({
    env: {
      RED_PANDA_PLANNER_DRAFT_DELAY_MS: '15000',
    },
  });
  try {
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'single_core',
        toolPolicy: 'risk_based',
        permissionMode: 'strict',
        providerProfileId: '',
        spawnSubAgents: true,
        subAgentBackend: 'in_process',
        model: '',
        toolAllowlist: '',
        toolDenylist: '',
      }));
    });
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/subagent cancel child e2e');
    await page.getByTestId('chat-composer-send').click();

    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    const planner = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
    await expect(planner).toContainText('运行中', { timeout: 15000 });
    await expect(planner.getByTestId('subagent-cancel')).toBeEnabled();
    await planner.getByTestId('subagent-cancel').click();
    await expect(planner).toContainText('已取消', { timeout: 15000 });
    await expect(planner).toContainText('planner subagent cancelled');

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/subagent cancel child e2e' });
    await expect(runRow).toBeVisible({ timeout: 15000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    const timeline = runRow.getByTestId('activity-event-timeline');
    await expect(timeline).toContainText('subagent_update', { timeout: 15000 });
    await expect(timeline).toContainText('planner subagent cancelled');
    await expect(timeline).toContainText('子代理 · planner');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop shows queued worker before execution @gateway-backed', async ({ page }) => {
  test.setTimeout(90000);
  const provider = await startDelayedWorkerProvider();
  const gateway = await startGateway({
    env: {
      RED_PANDA_SUBAGENT_POOL_SIZE: '1',
      RED_PANDA_PROVIDER: 'openai_compatible',
      RED_PANDA_PROVIDER_BASE_URL: provider.baseURL,
      RED_PANDA_PROVIDER_API_KEY: 'queue-e2e',
      RED_PANDA_PROVIDER_MODEL: 'queue-e2e',
      RED_PANDA_PROVIDER_STREAM: 'false',
    },
  });
  try {
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'single_core',
        toolPolicy: 'allow_all',
        permissionMode: 'trusted',
        providerProfileId: '',
        spawnSubAgents: true,
        subAgentBackend: 'process_pool',
        model: '',
        toolAllowlist: '',
        toolDenylist: '',
      }));
    });
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    const firstSessionTitle = (await page.locator('.tree-session-row.active [data-testid="session-item"] strong').textContent()).trim();
    await page.getByTestId('chat-composer-input').fill('/subagent queue holder');
    await page.getByTestId('chat-composer-send').click();
    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    await expect(page.getByTestId('subagent-item').filter({ hasText: 'planner' })).toContainText('运行中', { timeout: 15000 });

    await page.getByRole('button', { name: '新建会话', exact: true }).click();
    await page.getByTestId('chat-composer-input').fill('/subagent queue waiter');
    await page.getByTestId('chat-composer-send').click();
    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    const queued = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
    await expect(queued).toContainText('排队中', { timeout: 15000 });

    await page.getByTestId('session-item').filter({ hasText: firstSessionTitle }).click();
    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    const holder = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
    await holder.getByTestId('subagent-cancel').click();
    await expect(holder).toContainText('已取消', { timeout: 15000 });

    await page.getByTestId('session-item').filter({ hasText: '新会话' }).first().click();
    await page.getByRole('tab', { name: '子代理', exact: true }).click();
    const waiter = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
    await expect(waiter).toContainText('运行中', { timeout: 15000 });
    await waiter.getByTestId('subagent-cancel').click();
    await expect(waiter).toContainText('已取消', { timeout: 15000 });

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/subagent queue waiter' });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    const timeline = runRow.getByTestId('activity-event-timeline');
    await expect(timeline).toContainText('queued', { timeout: 15000 });
    await expect(timeline).toContainText('starting');
    await expect(timeline).toContainText('running');
    await expect(timeline).toContainText('cancelled');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
    await provider.stop();
  }
});

test('Gateway-backed desktop terminalizes a run after Runtime crash @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/permission runtime crash e2e');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('permission-card').filter({ hasText: 'protected checkpoint' })).toBeVisible({ timeout: 15000 });
    await gateway.killRuntimeProcesses();

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/permission runtime crash e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('失败', { timeout: 20000 });
    if (await runRow.getByTestId('activity-event-timeline').count() === 0) {
      await runRow.getByTestId('activity-run-toggle').click();
    }
    await expect(runRow.getByTestId('activity-event-timeline')).toContainText('agent runtime process exited unexpectedly', { timeout: 15000 });
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接');
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

test('Gateway-backed desktop enforces Agent definitions in worker Runtime @gateway-backed', async ({ page }) => {
  test.setTimeout(90000);
  const provider = await startAgentDefinitionProvider();
  const gateway = await startGateway({
    env: {
      RED_PANDA_PROVIDER: 'openai_compatible',
      RED_PANDA_PROVIDER_BASE_URL: provider.baseURL,
      RED_PANDA_PROVIDER_API_KEY: 'agent-definition-e2e',
      RED_PANDA_PROVIDER_MODEL: 'agent-definition-e2e',
      RED_PANDA_PROVIDER_STREAM: 'false',
    },
  });
  try {
    const definition = await apiJson(gateway.baseURL, '/api/v1/agents', {
      method: 'POST',
      body: JSON.stringify({
        key: 'reviewer-e2e',
        name: 'Reviewer E2E',
        system_prompt: 'CUSTOM_REVIEWER_PROMPT',
        tool_allowlist: ['workspace.read_file'],
        tool_denylist: ['shell.exec'],
        default_max_turns: 3,
        enabled: true,
      }),
    });
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem('red_panda_run_settings', JSON.stringify({
        runtimeMode: 'per_run_process',
        toolPolicy: 'allow_all',
        permissionMode: 'trusted',
        providerProfileId: '',
        spawnSubAgents: true,
        subAgentBackend: 'runtime_process',
        model: '',
        toolAllowlist: '',
        toolDenylist: '',
      }));
    });
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });
    await page.getByTestId('chat-composer-input').fill('invoke reviewer agent');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('message-row').filter({ hasText: 'custom reviewer report' })).toBeVisible({ timeout: 30000 });

    await expect.poll(() => provider.childRequests.length, { timeout: 15000 }).toBeGreaterThan(0);
    const childRequest = provider.childRequests[0];
    const childMessages = childRequest.messages.map((item) => String(item.content || '')).join('\n');
    expect(childMessages).toContain('CUSTOM_REVIEWER_PROMPT');
    expect(childMessages).toContain('Tool-turn budget=3');
    expect(childRequest.tools.map((item) => item.function.name)).toEqual(['workspace__read_file']);

    await apiJson(gateway.baseURL, `/api/v1/agents/${encodeURIComponent(definition.id)}`, {
      method: 'PUT',
      body: JSON.stringify({ enabled: false }),
    });
    await page.getByTestId('chat-composer-input').fill('invoke reviewer agent disabled');
    await page.getByTestId('chat-composer-send').click();
    await expect(page.getByTestId('message-row').filter({ hasText: 'reviewer-e2e is disabled' })).toBeVisible({ timeout: 30000 });
  } finally {
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
    await provider.stop();
  }
});

test('Gateway-backed desktop resumes missed events after reconnect @gateway-backed', async ({ page }) => {
  const gateway = await startGateway();
  try {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto('/');
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 15000 });

    await page.getByTestId('chat-composer-input').fill('/permission reconnect e2e');
    await page.getByTestId('chat-composer-send').click();

    const permissionCard = page.getByTestId('permission-card').filter({ hasText: 'protected checkpoint' });
    await expect(permissionCard).toBeVisible({ timeout: 15000 });

    const pending = await waitForPendingPermission(gateway.baseURL);
    await page.context().setOffline(true);
    await resolvePermissionOverWebSocket(gateway.baseURL, {
      permission_id: pending.id,
      run_id: pending.run_id,
      decision: 'approve',
    });

    await page.context().setOffline(false);
    await expect(page.getByTestId('gateway-status')).toHaveText('已连接', { timeout: 20000 });
    await expect(page.getByTestId('message-row').filter({ hasText: 'permission approved' })).toBeVisible({ timeout: 20000 });

    await page.getByTestId('right-tab-activity').click();
    const runRow = page.getByTestId('activity-run').filter({ hasText: '/permission reconnect e2e' });
    await expect(runRow.getByTestId('activity-run-status')).toContainText(/已完成|运行中/, { timeout: 10000 });
    await expect(runRow.getByTestId('activity-run-status')).toContainText('已完成', { timeout: 20000 });
  } finally {
    await page.context().setOffline(false).catch(() => {});
    await page.close().catch(() => {});
    await new Promise((resolve) => setTimeout(resolve, 300));
    await gateway.stop();
  }
});

async function waitForPendingPermission(baseURL) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const response = await fetch(`${baseURL}/api/v1/permissions/pending`);
    const body = await response.json();
    const item = body.data?.find((permission) => permission.status === 'pending');
    if (item) {
      return item;
    }
    await delay(150);
  }
  throw new Error('pending permission was not projected');
}

async function resolvePermissionOverWebSocket(baseURL, payload) {
  const wsURL = `${baseURL.replace(/^http/, 'ws')}/api/v1/ws`;
  const socket = new WebSocket(wsURL);
  const pending = new Map();
  socket.addEventListener('message', (event) => {
    const message = JSON.parse(String(event.data));
    if (!message.id || !pending.has(message.id)) {
      return;
    }
    const { resolve, reject } = pending.get(message.id);
    pending.delete(message.id);
    if (message.type === 'error' || message.ok === false) {
      reject(new Error(message.error?.message || 'websocket request failed'));
    } else {
      resolve(message.payload ?? null);
    }
  });
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true });
    socket.addEventListener('error', () => reject(new Error('external websocket failed to open')), { once: true });
  });
  try {
    await sendWebSocketRequest(socket, pending, 'auth', undefined, {
      token: '',
      client: { kind: 'playwright', name: 'red_panda_e2e', version: '0.1.0' },
    });
    await sendWebSocketRequest(socket, pending, 'request', 'permission.resolve', payload);
  } finally {
    socket.close();
  }
}

function sendWebSocketRequest(socket, pending, type, method, payload) {
  const id = `e2e_${Date.now()}_${Math.random().toString(16).slice(2)}`;
  const message = { id, type, payload };
  if (method) {
    message.method = method;
  }
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify(message));
  });
}

async function createInactiveProviderProfile(baseURL) {
  const created = await apiJson(baseURL, '/api/v1/provider-profiles', {
    method: 'POST',
    body: JSON.stringify({
      name: 'Inactive E2E Provider',
      provider: 'openai_compatible',
      base_url: 'http://127.0.0.1:9',
      model: 'inactive-e2e-model',
      api_key: 'inactive-e2e-secret',
      is_default: false,
    }),
  });
  return apiJson(baseURL, `/api/v1/provider-profiles/${encodeURIComponent(created.id)}`, {
    method: 'PUT',
    body: JSON.stringify({ active: false }),
  });
}

async function apiJson(baseURL, path, options = {}) {
  const response = await fetch(`${baseURL}${path}`, {
    headers: { 'content-type': 'application/json', ...(options.headers || {}) },
    ...options,
  });
  const body = await response.json();
  if (!response.ok || body.ok === false) {
    throw new Error(body.error?.message || `HTTP ${response.status}`);
  }
  return body.data;
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function startAgentDefinitionProvider() {
  const childRequests = [];
  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    const messages = Array.isArray(body.messages) ? body.messages : [];
    const combined = messages.map((item) => String(item.content || '')).join('\n');
    const toolResult = [...messages].reverse().find((item) => item.role === 'tool');
    let message;
    let finishReason = 'stop';
    if (combined.includes('CUSTOM_REVIEWER_PROMPT')) {
      childRequests.push(body);
      message = { role: 'assistant', content: 'custom reviewer report' };
    } else if (toolResult) {
      message = { role: 'assistant', content: `root result: ${toolResult.content}` };
    } else {
      finishReason = 'tool_calls';
      message = {
        role: 'assistant',
        content: null,
        tool_calls: [{
          id: `call_reviewer_${Date.now()}`,
          type: 'function',
          function: {
            name: 'subagent.run',
            arguments: JSON.stringify({ name: 'reviewer-e2e', task: 'review the assigned change' }),
          },
        }],
      };
    }
    response.writeHead(200, { 'content-type': 'application/json' });
    response.end(JSON.stringify({
      id: `chatcmpl_${Date.now()}`,
      object: 'chat.completion',
      created: Math.floor(Date.now() / 1000),
      model: 'agent-definition-e2e',
      choices: [{ index: 0, message, finish_reason: finishReason }],
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    }));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  return {
    baseURL: `http://127.0.0.1:${address.port}/v1`,
    childRequests,
    stop: () => new Promise((resolve) => server.close(resolve)),
  };
}

async function startDelayedWorkerProvider() {
  const server = createServer(async (request, response) => {
    const chunks = [];
    for await (const chunk of request) {
      chunks.push(chunk);
    }
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    const combined = (body.messages || []).map((item) => String(item.content || '')).join('\n');
    if (combined.includes('planner subagent task:')) {
      await new Promise((resolve) => {
        const timer = setTimeout(resolve, 30000);
        response.once('close', () => {
          clearTimeout(timer);
          resolve();
        });
      });
    }
    if (response.destroyed) {
      return;
    }
    response.writeHead(200, { 'content-type': 'application/json' });
    response.end(JSON.stringify({
      id: `chatcmpl_queue_${Date.now()}`,
      object: 'chat.completion',
      created: Math.floor(Date.now() / 1000),
      model: 'queue-e2e',
      choices: [{ index: 0, message: { role: 'assistant', content: 'queue provider response' }, finish_reason: 'stop' }],
      usage: { prompt_tokens: 1, completion_tokens: 1, total_tokens: 2 },
    }));
  });
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });
  const address = server.address();
  return {
    baseURL: `http://127.0.0.1:${address.port}/v1`,
    stop: () => new Promise((resolve) => server.close(resolve)),
  };
}
