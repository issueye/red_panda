import { expect, test } from '@playwright/test';

test('Restored workflow panels render permissions tools and subagent actions', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  await expect(page.getByTestId('message-row')).toHaveCount(2);
  await expect(page.getByText('Restore previous session state')).toBeVisible();
  await expect(page.getByText('Restored messages, tools, permissions, and subagents.')).toBeVisible();

  await expect(page.getByTestId('tool-card')).toHaveCount(2);
  const readTool = page.getByTestId('tool-card').filter({ hasText: 'Read file' });
  await expect(readTool).toContainText('已完成');
  await expect(readTool).toContainText('README.md loaded');
  const failedTool = page.getByTestId('tool-card').filter({ hasText: 'shell.exec' });
  await expect(failedTool).toContainText('Shell command');
  await expect(failedTool).toContainText('失败');
  await expect(failedTool).toContainText('exit status 1');

  await expect(page.getByTestId('permission-card')).toHaveCount(2);
  expect(await page.locator('.conversation > [data-timeline-type]').evaluateAll((items) => (
    items.map((item) => item.dataset.timelineType)
  ))).toEqual([
    'message',
    'tool',
    'tool',
    'message',
    'permission',
    'permission',
  ]);
  const approvePermission = page.getByTestId('permission-card').filter({ hasText: 'Allow shell read' });
  await expect(approvePermission).toContainText('中风险');
  await expect(approvePermission).toContainText('shell.exec');
  await expect(approvePermission).toContainText('cat README.md');
  await approvePermission.getByTestId('permission-approve').click();
  await expect(approvePermission.getByTestId('permission-state')).toHaveText('已允许');
  await expect(approvePermission.getByTestId('permission-approve')).toHaveCount(0);
  await expect(page.getByTestId('permission-result')).toHaveText('perm_approve_restore:approve');

  const denyPermission = page.getByTestId('permission-card').filter({ hasText: 'Allow destructive shell command' });
  await expect(denyPermission).toContainText('高风险');
  await expect(denyPermission).toContainText('子代理 planner');
  await denyPermission.getByTestId('permission-deny').click();
  await expect(denyPermission.getByTestId('permission-state')).toHaveText('已拒绝');
  await expect(denyPermission.getByTestId('permission-deny')).toHaveCount(0);
  await expect(page.getByTestId('permission-result')).toHaveText('perm_deny_restore:deny');

  await expect(page.getByTestId('subagent-item')).toHaveCount(3);
  const planner = page.getByTestId('subagent-item').filter({ hasText: 'planner' });
  const archivist = page.getByTestId('subagent-item').filter({ hasText: 'archivist' });
  await expect(planner).toContainText('进程池');
  await expect(archivist).toContainText('运行时进程');
  await expect(planner.getByTestId('subagent-cancel')).toBeEnabled();
  await expect(archivist.getByTestId('subagent-cancel')).toBeDisabled();
  await planner.getByTestId('subagent-cancel').click();
  await expect(page.getByTestId('subagent-result')).toHaveText('planner:running');
});

test('Conversation pauses following after manual scroll and can return to latest', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  const conversation = page.getByTestId('chat-conversation');

  await expect.poll(() => conversation.evaluate((element) => (
    element.scrollHeight - element.scrollTop - element.clientHeight
  ))).toBeLessThan(2);

  await conversation.evaluate((element) => {
    element.scrollTop = 0;
    element.dispatchEvent(new Event('scroll'));
  });
  await expect(page.getByTestId('conversation-latest')).toBeVisible();
  await page.getByTestId('conversation-latest').click();
  await expect.poll(() => conversation.evaluate((element) => (
    element.scrollHeight - element.scrollTop - element.clientHeight
  ))).toBeLessThan(2);
});
