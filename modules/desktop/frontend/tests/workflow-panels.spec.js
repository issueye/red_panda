import { expect, test } from '@playwright/test';

test('Restored workflow panels render permissions tools and subagent actions', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  await expect(page.getByTestId('message-row')).toHaveCount(2);
  await expect(page.getByText('Restore previous session state')).toBeVisible();
  await expect(page.getByText('Restored messages, tools, permissions, and subagents.')).toBeVisible();

  await expect(page.getByTestId('tool-card')).toHaveCount(2);
  const readTool = page.getByTestId('tool-card').filter({ hasText: 'Read file' });
  await expect(readTool).toContainText('completed');
  await expect(readTool).toContainText('README.md loaded');
  const failedTool = page.getByTestId('tool-card').filter({ hasText: 'shell.exec' });
  await expect(failedTool).toContainText('Shell command');
  await expect(failedTool).toContainText('failed');
  await expect(failedTool).toContainText('exit status 1');

  await expect(page.getByTestId('permission-card')).toHaveCount(2);
  const approvePermission = page.getByTestId('permission-card').filter({ hasText: 'Allow shell read' });
  await approvePermission.getByTestId('permission-approve').click();
  await expect(approvePermission.getByTestId('permission-state')).toHaveText('Approved');
  await expect(approvePermission.getByTestId('permission-approve')).toHaveCount(0);
  await expect(page.getByTestId('permission-result')).toHaveText('perm_approve_restore:approve');

  const denyPermission = page.getByTestId('permission-card').filter({ hasText: 'Allow destructive shell command' });
  await denyPermission.getByTestId('permission-deny').click();
  await expect(denyPermission.getByTestId('permission-state')).toHaveText('Denied');
  await expect(denyPermission.getByTestId('permission-deny')).toHaveCount(0);
  await expect(page.getByTestId('permission-result')).toHaveText('perm_deny_restore:deny');

  await expect(page.getByTestId('subagent-item')).toHaveCount(3);
  const planner = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
  const archivist = page.getByTestId('subagent-item').filter({ hasText: 'archivist' });
  await expect(planner).toContainText('process_pool');
  await expect(archivist).toContainText('runtime_process');
  await expect(planner.getByTestId('subagent-cancel')).toBeEnabled();
  await expect(archivist.getByTestId('subagent-cancel')).toBeDisabled();
  await planner.getByTestId('subagent-cancel').click();
  await expect(page.getByTestId('subagent-result')).toHaveText('planner:running');
});
