import { expect, test } from '@playwright/test';

test('Restored workflow panels render permissions tools and Worker assignments', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  await expect(page.getByTestId('message-row')).toHaveCount(2);
  await expect(page.getByText('Restore previous session state')).toBeVisible();
  await expect(page.getByText('Restored messages, tools, permissions, and Worker assignments.')).toBeVisible();
  await expect(page.getByText('Worker worker-01 · entry', { exact: true })).toBeVisible();

  await expect(page.getByTestId('tool-card')).toHaveCount(2);
  const readTool = page.getByTestId('tool-card').filter({ hasText: 'Read file' });
  await expect(readTool).toHaveClass(/tool-completed/);
  await readTool.getByTestId('tool-card-toggle').click();
  await readTool.getByRole('button', { name: /^输出/ }).click();
  await expect(readTool.getByTestId('tool-output')).toHaveText('README.md loaded');
  const failedTool = page.getByTestId('tool-card').filter({ hasText: 'shell.exec' });
  await expect(failedTool).toContainText('Shell command');
  await expect(failedTool).toHaveClass(/tool-failed/);
  await expect(failedTool.getByTestId('tool-error')).toHaveText('exit status 1');
  await expect(failedTool.getByTestId('tool-output')).toHaveText('go: cannot find main module; see go help modules');

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
  await denyPermission.getByTestId('permission-deny').click();
  await expect(denyPermission.getByTestId('permission-state')).toHaveText('已拒绝');
  await expect(denyPermission.getByTestId('permission-deny')).toHaveCount(0);
  await expect(page.getByTestId('permission-result')).toHaveText('perm_deny_restore:deny');

  await expect(page.getByTestId('worker-workspace')).toHaveCount(0);
  await expect(page.getByTestId('worker-item')).toHaveCount(0);
  await expect(page.getByTestId('worker-assignment')).toHaveCount(8);
  const planner = page.getByTestId('worker-assignment').filter({ hasText: 'worker-02' });
  const archivist = page.getByTestId('worker-assignment').filter({ hasText: 'worker-01' });
  await expect(planner).toContainText('worker-02');
  await expect(planner.getByTestId('worker-assignment-cancel')).toBeEnabled();
  await expect(archivist.getByTestId('worker-assignment-cancel')).toBeDisabled();
  await planner.getByTestId('worker-assignment-cancel').click();
  await expect(page.getByTestId('worker-result')).toHaveText('assignment-planner:running');

  await expect(page.getByTestId('chat-tab-main')).toBeVisible();
  await expect(page.getByTestId('todo-composer-strip')).toBeVisible();
  await expect(page.getByTestId('goal-composer-strip')).toHaveCount(0);

  await planner.getByTestId('worker-assignment-open').click();
  await expect(page.getByTestId('chat-tab-worker')).toContainText('目标规划');
  const workerConversation = page.getByTestId('chat-conversation');
  await expect(workerConversation.getByText('Reading restored context', { exact: true })).toBeVisible();
  await expect(workerConversation.getByText('任务输入', { exact: true })).toBeVisible();
  await expect(page.getByText('Private planner analysis.')).toBeVisible();
  await expect(page.getByText('Restore previous session state')).toHaveCount(0);
  await expect(page.getByTestId('tool-card')).toHaveCount(1);
  await expect(page.getByTestId('permission-card')).toHaveCount(1);
  await expect(page.getByTestId('permission-approve')).toHaveCount(0);
  await expect(page.getByTestId('permission-deny')).toHaveCount(0);
  await expect(page.getByTestId('conversation-readonly-hint')).toBeVisible();

  await page.getByTestId('chat-tab-main').click();
  await page.getByTestId('fixture-goal-mode').click();
  await expect(page.getByTestId('goal-composer-strip')).toBeVisible();
  await expect(page.getByTestId('todo-composer-strip')).toHaveCount(0);
  await expect(page.getByTestId('goal-criteria')).toContainText('2/3 已满足');
  await expect(page.getByTestId('goal-actions')).toContainText('行动 1/3');
  await expect(page.getByTestId('goal-assessment')).toContainText('最近评估：有进展');

  await page.getByTestId('chat-tab-worker').click();
  await page.getByTestId('chat-tab-close').click();
  await expect(page.getByTestId('chat-tab-worker')).toHaveCount(0);
  await expect(page.getByTestId('chat-tab-main')).toHaveAttribute('aria-selected', 'true');
});

test('Worker panel only shows assignments in a scrollable region', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  const panel = page.getByTestId('worker-panel-content');
  const assignmentRegion = page.getByTestId('worker-assignment-region');
  const assignmentScroll = page.getByTestId('worker-assignment-scroll');
  const sectionLabel = page.getByTestId('worker-assignment-section-label');
  await expect(panel).toBeVisible();
  await expect(page.getByTestId('worker-workspace')).toHaveCount(0);
  await expect(sectionLabel).toHaveText('工作分配');

  const layout = await Promise.all([
    panel.evaluate((element) => {
      const style = window.getComputedStyle(element);
      return { overflowY: style.overflowY, clientHeight: element.clientHeight };
    }),
    assignmentScroll.evaluate((element) => {
      const style = window.getComputedStyle(element);
      return {
        overflowY: style.overflowY,
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      };
    }),
    sectionLabel.evaluate((element) => ({
      clientWidth: element.clientWidth,
      scrollWidth: element.scrollWidth,
      lineHeight: window.getComputedStyle(element).lineHeight,
    })),
  ]);

  expect(layout[0].overflowY).toBe('hidden');
  expect(layout[0].clientHeight).toBeGreaterThan(0);
  expect(layout[1].overflowY).toBe('auto');
  expect(layout[1].scrollHeight).toBeGreaterThan(layout[1].clientHeight);
  expect(layout[2].scrollWidth).toBeLessThanOrEqual(layout[2].clientWidth);

  const before = await assignmentRegion.boundingBox();
  await assignmentScroll.evaluate((element) => { element.scrollTop = element.scrollHeight; });
  await expect.poll(() => assignmentScroll.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  const after = await assignmentRegion.boundingBox();
  expect(after.y).toBe(before.y);
  expect(after.height).toBe(before.height);
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

test('Goal information can be dragged, restored, and reset', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  await page.getByTestId('fixture-goal-mode').click();

  const strip = page.getByTestId('goal-composer-strip');
  const handle = page.getByTestId('goal-drag-handle');
  await expect(strip).toBeVisible();
  await expect(handle).toHaveAttribute('aria-label', '拖动目标信息');

  const initial = await strip.boundingBox();
  const grip = await handle.boundingBox();
  expect(initial).not.toBeNull();
  expect(grip).not.toBeNull();

  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2 + 72, grip.y + grip.height / 2 - 72, { steps: 8 });
  await page.mouse.up();

  const moved = await strip.boundingBox();
  expect(moved.x).toBeGreaterThan(initial.x + 20);
  expect(moved.y).toBeLessThan(initial.y - 20);
  expect(moved.x).toBeGreaterThanOrEqual(8);
  expect(moved.x + moved.width).toBeLessThanOrEqual(1280 - 8);
  await expect.poll(() => page.evaluate(() => (
    window.localStorage.getItem('red_panda_goal_strip_position_v1')
  ))).not.toBeNull();

  await page.reload();
  await page.getByTestId('fixture-goal-mode').click();
  const restored = await strip.boundingBox();
  expect(restored.x).toBeGreaterThan(initial.x + 20);
  expect(restored.y).toBeLessThan(initial.y - 20);

  await handle.dblclick();
  await expect.poll(async () => (await strip.boundingBox()).x).toBeLessThan(initial.x + 8);
  await expect.poll(async () => (await strip.boundingBox()).y).toBeGreaterThan(initial.y - 8);
});

test('Collapsed task strip can be dragged, restored, and reset', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  const strip = page.getByTestId('todo-composer-strip');
  const handle = page.getByTestId('todo-drag-handle');
  await expect(strip).toBeVisible();
  await expect(handle).toHaveAttribute('aria-label', '拖动任务信息');

  const initial = await strip.boundingBox();
  const grip = await handle.boundingBox();
  expect(initial).not.toBeNull();
  expect(grip).not.toBeNull();

  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
  await page.mouse.down();
  await page.mouse.move(grip.x + grip.width / 2 + 72, grip.y + grip.height / 2 - 56, { steps: 8 });
  await page.mouse.up();

  const moved = await strip.boundingBox();
  expect(moved.x).toBeGreaterThan(initial.x + 20);
  expect(moved.y).toBeLessThan(initial.y - 20);
  expect(moved.x).toBeGreaterThanOrEqual(8);
  expect(moved.x + moved.width).toBeLessThanOrEqual(1280 - 8);
  await expect.poll(() => page.evaluate(() => (
    window.localStorage.getItem('red_panda_todo_strip_position_v1')
  ))).not.toBeNull();

  await page.reload();
  const restored = await strip.boundingBox();
  expect(restored.x).toBeGreaterThan(initial.x + 20);
  expect(restored.y).toBeLessThan(initial.y - 20);

  await handle.dblclick();
  await expect.poll(async () => (await strip.boundingBox()).x).toBeLessThan(initial.x + 8);
  await expect.poll(async () => (await strip.boundingBox()).y).toBeGreaterThan(initial.y - 8);
});
