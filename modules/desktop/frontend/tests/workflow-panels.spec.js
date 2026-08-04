import { expect, test } from '@playwright/test';

test('Restored workflow panels render permissions tools and Worker assignments', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  expect(await page.evaluate(() => {
    const styles = getComputedStyle(document.documentElement);
    return {
      background: styles.getPropertyValue('--color-bg').trim(),
      mutedSurface: styles.getPropertyValue('--color-surface-muted').trim(),
      primaryText: styles.getPropertyValue('--color-text').trim(),
      conversationText: styles.getPropertyValue('--color-conversation-text').trim(),
    };
  })).toEqual({
    background: '#fafbfc',
    mutedSurface: '#f7f8fa',
    primaryText: '#070b12',
    conversationText: '#030712',
  });

  // Main tab only shows root-facing rows (user + reasoning + root assistant + root tool/permission).
  await expect(page.getByTestId('message-row')).toHaveCount(2);
  await expect(page.getByText('Restore previous session state')).toBeVisible();
  await expect(page.getByText('Restored messages, tools, permissions, and Worker assignments.')).toBeVisible();
  await expect(page.getByText('助手', { exact: true })).toBeVisible();

  const rootToolGroup = page.getByTestId('tool-execution-group');
  await expect(rootToolGroup).toHaveCount(1);
  await expect(page.getByTestId('tool-card')).toHaveCount(0);
  await rootToolGroup.getByTestId('tool-execution-group-toggle').click();
  await expect(rootToolGroup.getByTestId('tool-card')).toHaveCount(2);
  const readTool = page.getByTestId('tool-card').filter({ hasText: 'Read file' });
  await expect(readTool).toHaveClass(/tool-completed/);
  await readTool.getByTestId('tool-card-toggle').click();
  await readTool.getByRole('button', { name: /^输出/ }).click();
  await expect(readTool.getByTestId('tool-output')).toHaveText('README.md loaded');

  await expect(page.getByTestId('permission-card')).toHaveCount(2);
  const permissionBoxes = await page.getByTestId('permission-card').evaluateAll((items) => (
    items.map((item) => {
      const box = item.getBoundingClientRect();
      return { top: box.top, bottom: box.bottom };
    })
  ));
  expect(permissionBoxes.every((box, index) => (
    index === 0 || box.top >= permissionBoxes[index - 1].bottom
  ))).toBe(true);
  expect(await page.locator('.conversation > [data-timeline-type]').evaluateAll((items) => (
    items.map((item) => item.dataset.timelineType)
  ))).toEqual([
    'message',
    'tool',
    'message',
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

  await planner.getByTestId('worker-assignment-open').click();
  await expect(page.getByTestId('chat-tab-worker')).toContainText('planner');
  const workerConversation = page.getByTestId('chat-conversation');
  // Task input row + collaborative Worker message both contain the same task text.
  await expect(workerConversation.getByText('Reading restored context', { exact: true })).toHaveCount(2);
  await expect(workerConversation.getByText('任务输入', { exact: true })).toBeVisible();
  await expect(page.getByText('Private planner analysis.')).toBeVisible();
  await expect(page.getByText('Restore previous session state')).toHaveCount(0);
  await expect(page.getByTestId('tool-card')).toHaveCount(1);
  const failedTool = page.getByTestId('tool-card').filter({ hasText: 'shell.exec' });
  await expect(failedTool).toContainText('Shell command');
  await expect(failedTool).toHaveClass(/tool-failed/);
  await failedTool.getByTestId('tool-card-toggle').click();
  await expect(failedTool.getByTestId('tool-error')).toHaveText('exit status 1');
  await expect(failedTool.getByTestId('tool-output')).toHaveText('go: cannot find main module; see go help modules');
  // Worker conversation is read-only: no decision buttons, even if cards are shown.
  await expect(page.getByTestId('permission-approve')).toHaveCount(0);
  await expect(page.getByTestId('permission-deny')).toHaveCount(0);
  await expect(page.getByTestId('conversation-readonly-hint')).toBeVisible();

  await page.getByTestId('chat-tab-worker').click();
  await page.getByTestId('chat-tab-close').click();
  await expect(page.getByTestId('chat-tab-worker')).toHaveCount(0);
  await expect(page.getByTestId('chat-tab-main')).toHaveAttribute('aria-selected', 'true');
});

test('All tool calls stay compact until the user expands them', async ({ page }) => {
  await page.goto('/tool-card-fixture.html');

  const readTool = page.getByTestId('tool-card').filter({ hasText: 'Read file' });
  const runningTool = page.getByTestId('tool-card').filter({ hasText: 'Grep workspace' });
  const failedTool = page.getByTestId('tool-card').filter({ hasText: 'Search workspace' });

  await expect(readTool).toHaveClass(/is-collapsed/);
  await expect(readTool.getByTestId('tool-card-body')).toHaveCount(0);
  await expect(page.getByTestId('tool-call-index')).toHaveCount(0);
  expect((await readTool.boundingBox()).height).toBeLessThanOrEqual(30);

  // Running and failed tools also stay one-line until the user asks for details.
  await expect(runningTool).toHaveClass(/is-collapsed/);
  await expect(runningTool).toHaveClass(/is-live/);
  await expect(runningTool).toHaveAttribute('aria-busy', 'true');
  await expect(runningTool.getByTestId('tool-status-label')).toContainText('进行中');
  expect(await runningTool.evaluate((element) => (
    getComputedStyle(element, '::after').animationName
  ))).toBe('tool-progress-scan');
  await expect(runningTool.getByTestId('tool-card-body')).toHaveCount(0);
  await expect(failedTool).toHaveClass(/is-collapsed/);
  await expect(failedTool.getByTestId('tool-card-body')).toHaveCount(0);

  await failedTool.getByTestId('tool-card-toggle').click();
  await expect(failedTool).toHaveClass(/is-expanded/);
  await expect(failedTool.getByTestId('tool-error')).toBeVisible();

  const cards = page.getByTestId('tool-card');
  const cardBoxes = await cards.evaluateAll((items) => items.map((item) => {
    const box = item.getBoundingClientRect();
    return { top: box.top, bottom: box.bottom };
  }));
  expect(cardBoxes[1].top - cardBoxes[0].bottom).toBeGreaterThanOrEqual(4);
  expect(cardBoxes[1].top - cardBoxes[0].bottom).toBeLessThanOrEqual(8);

  const columnsDoNotOverlap = await page.locator('.tool-card-content').evaluateAll((items) => (
    items.every((item) => {
      const title = item.querySelector('.tool-card-title')?.getBoundingClientRect();
      const summary = item.querySelector('.tool-card-summary')?.getBoundingClientRect();
      const meta = item.querySelector('.tool-card-meta')?.getBoundingClientRect();
      return title && summary && meta && title.right <= summary.left && summary.right <= meta.left;
    })
  ));
  expect(columnsDoNotOverlap).toBe(true);

  await readTool.getByTestId('tool-card-toggle').click();
  await expect(readTool).toHaveClass(/is-expanded/);
  await expect(readTool.getByTestId('tool-card-body')).toBeVisible();

  // Manual collapse is sticky even if status stays completed.
  await readTool.getByTestId('tool-card-toggle').click();
  await expect(readTool).toHaveClass(/is-collapsed/);

  await page.setViewportSize({ width: 390, height: 720 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
  expect(await page.locator('.tool-card-name').evaluateAll((items) => (
    items.every((item) => getComputedStyle(item).display === 'none')
  ))).toBe(true);
});

test('Chat messages keep the user avatar and omit the assistant avatar', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  const userBubble = page.locator('.message-row.role-user .message-bubble');
  const assistantBubble = page.locator('.message-row.role-assistant .message-bubble');
  const userAvatar = page.locator('.message-row.role-user .message-avatar');
  const assistantAvatar = page.locator('.message-row.role-assistant .message-avatar');
  const reasoning = page.getByTestId('reasoning-block');
  await expect(userAvatar).toBeVisible();
  await expect(assistantAvatar).toHaveCount(0);
  await expect(reasoning.locator('.message-thinking-action-open')).toBeVisible();
  await expect(reasoning.locator('.message-thinking-action-close')).toBeHidden();
  await reasoning.locator('summary').click();
  await expect(reasoning.locator('.message-thinking-action-open')).toBeHidden();
  await expect(reasoning.locator('.message-thinking-action-close')).toBeVisible();
  const thinkingColors = await Promise.all([
    reasoning.locator('summary strong').evaluate((element) => getComputedStyle(element).color),
    reasoning.locator('.message-thinking-preview').evaluate((element) => getComputedStyle(element).color),
    reasoning.locator('.message-thinking-content').evaluate((element) => getComputedStyle(element).color),
  ]);
  expect(thinkingColors).toEqual([
    'rgb(11, 17, 27)',
    'rgb(102, 112, 133)',
    'rgb(3, 7, 18)',
  ]);
  const appearance = await Promise.all([
    userBubble.evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      borderStyle: getComputedStyle(element).borderStyle,
      boxShadow: getComputedStyle(element).boxShadow,
    })),
    assistantBubble.evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      borderStyle: getComputedStyle(element).borderStyle,
      boxShadow: getComputedStyle(element).boxShadow,
    })),
    reasoning.evaluate((element) => ({
      background: getComputedStyle(element).backgroundColor,
      borderStyle: getComputedStyle(element).borderStyle,
    })),
  ]);

  expect(appearance[0]).toEqual({
    background: 'rgba(0, 0, 0, 0)',
    borderStyle: 'none',
    boxShadow: 'none',
  });
  expect(appearance[1]).toEqual(appearance[0]);
  expect(appearance[2]).toEqual({
    background: 'rgba(0, 0, 0, 0)',
    borderStyle: 'none',
  });
  expect(await page.locator('.message-plain').evaluateAll((items) => (
    items.every((item) => getComputedStyle(item).color === 'rgb(3, 7, 18)')
  ))).toBe(true);

  const toolGroup = page.getByTestId('tool-execution-group');
  await toolGroup.getByTestId('tool-execution-group-toggle').click();
  const [reasoningRail, toolRail] = await Promise.all([
    reasoning.locator('.message-thinking-content').boundingBox(),
    toolGroup.getByTestId('tool-execution-group-body').boundingBox(),
  ]);
  expect(Math.abs(reasoningRail.x - toolRail.x)).toBeLessThanOrEqual(1);
  expect(await reasoning.locator('.message-thinking-content').evaluate((element) => (
    getComputedStyle(element).paddingLeft
  ))).toBe('18px');
});

test('Running sessions animate while attention and idle sessions stay stable', async ({ page }) => {
  await page.goto('/sidebar-state-fixture.html');

  const runningStatus = page.getByTestId('session-run-status').filter({ hasText: '运行中' });
  const runningRow = page.locator('.tree-session-row.is-running');
  const permissionStatus = page.getByTestId('session-run-status').filter({ hasText: '授权' });
  await expect(runningStatus).toHaveAccessibleName('会话运行中');
  await expect(permissionStatus).toHaveAccessibleName('等待授权');
  await expect(page.locator('.tree-session-row').filter({ hasText: '已完成的会话' }))
    .not.toHaveClass(/is-running/);
  expect(await runningStatus.locator('.session-run-spinner').evaluate((element) => (
    getComputedStyle(element).animationName
  ))).toBe('session-spinner');
  expect(await runningRow.locator('.tree-session-main').evaluate((element) => (
    getComputedStyle(element, '::after').animationName
  ))).toBe('session-progress-scan');

  await page.emulateMedia({ reducedMotion: 'reduce' });
  expect(await runningStatus.locator('.session-run-spinner').evaluate((element) => (
    getComputedStyle(element).animationName
  ))).toBe('none');
});

test('Worker thinking indicator follows the selected Assignment status', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  await expect(page.getByTestId('running-panda-row')).toHaveCount(0);

  const runningAssignment = page.getByTestId('worker-assignment').filter({ hasText: 'worker-02' });
  await runningAssignment.getByTestId('worker-assignment-open').click();
  await expect(page.getByTestId('chat-tab-worker')).toContainText('运行中');
  await expect(page.getByTestId('running-panda-row')).toBeVisible();

  const completedAssignment = page.getByTestId('worker-assignment').filter({ hasText: 'worker-01' });
  await completedAssignment.getByTestId('worker-assignment-open').click();
  await expect(page.getByTestId('chat-tab-worker').filter({ hasText: 'archivist' })).toContainText('已完成');
  await expect(page.getByTestId('running-panda-row')).toHaveCount(0);
  await expect(page.getByText('暂无 Worker 输出')).toBeVisible();
});

test('Expanded task panel stays narrow and translucent', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  await page.getByTestId('todo-composer-toggle').click();
  const list = page.getByTestId('todo-composer-list');
  await expect(list).toBeVisible();
  const appearance = await list.evaluate((element) => ({
    backdropFilter: getComputedStyle(element).backdropFilter,
    backgroundColor: getComputedStyle(element).backgroundColor,
    width: element.getBoundingClientRect().width,
  }));
  expect(appearance.width).toBeLessThanOrEqual(660);
  expect(appearance.backdropFilter).not.toBe('none');
  expect(appearance.backgroundColor).not.toBe('rgb(255, 255, 255)');
});

test('Consecutive tools render as a collapsed execution group', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  const group = page.getByTestId('tool-execution-group');
  await expect(group).toHaveCount(1);
  expect(await group.evaluate((element) => ({
    background: getComputedStyle(element).backgroundColor,
    borderStyle: getComputedStyle(element).borderStyle,
    borderRadius: getComputedStyle(element).borderRadius,
    boxShadow: getComputedStyle(element).boxShadow,
  }))).toEqual({
    background: 'rgba(0, 0, 0, 0)',
    borderStyle: 'none',
    borderRadius: '0px',
    boxShadow: 'none',
  });
  await expect(group.getByTestId('tool-execution-group-toggle')).toHaveAttribute('aria-expanded', 'false');
  await expect(group).toContainText('已运行');
  await expect(group).toContainText('Read file · Workspace stats');
  expect((await group.getByTestId('tool-execution-group-toggle').boundingBox()).height)
    .toBeLessThanOrEqual(32);
  await expect(group.getByTestId('tool-execution-group-body')).toHaveCount(0);
  await expect(group.getByTestId('tool-card')).toHaveCount(0);

  await group.getByTestId('tool-execution-group-toggle').click();
  await expect(group.getByTestId('tool-execution-group-body')).toBeVisible();
  await expect(group.getByTestId('tool-card')).toHaveCount(2);
  await expect(group.getByTestId('tool-call-index')).toHaveCount(0);
  await expect(group.getByTestId('tool-card').nth(0)).toContainText('Read file');
  await expect(group.getByTestId('tool-card').nth(1)).toContainText('Workspace stats');
  expect(await group.getByTestId('tool-card').evaluateAll((items) => items.every((item) => {
    const style = getComputedStyle(item);
    return style.backgroundColor === 'rgba(0, 0, 0, 0)' && style.borderRadius === '0px';
  }))).toBe(true);
});

test('Composer switches provider models and reasoning effort', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  const thinking = page.getByTestId('composer-thinking-toggle');
  await expect(thinking).toHaveAttribute('aria-checked', 'false');
  await thinking.click();
  await expect(thinking).toHaveAttribute('aria-checked', 'true');

  const reasoning = page.getByTestId('reasoning-block');
  await expect(reasoning.locator('.message-thinking-preview')).toContainText('First inspect the restored run state');
  await expect(reasoning).not.toHaveAttribute('open', '');
  await reasoning.locator('summary').click();
  await expect(reasoning).toContainText('First inspect the restored run state');

  const modelMenu = page.getByTestId('composer-model-menu');
  await expect(modelMenu).toContainText('Fast');
  await modelMenu.click();
  await page.getByTestId('composer-model-menu-models').click();
  const desktopSubmenu = page.getByTestId('composer-model-models-submenu');
  await expect(desktopSubmenu).toBeVisible();
  expect(await desktopSubmenu.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return box.left >= 0 && box.right <= window.innerWidth;
  })).toBe(true);
  await page.getByRole('menuitemradio', { name: 'Deep', exact: true }).click();
  await expect(modelMenu).toContainText('Deep');

  await modelMenu.click();
  await page.getByTestId('composer-model-menu-reasoning').click();
  await page.getByRole('menuitemradio', { name: '高', exact: true }).click();
  await expect(modelMenu).toContainText('高');

  await page.setViewportSize({ width: 390, height: 720 });
  await modelMenu.click();
  await page.getByTestId('composer-model-menu-models').click();
  const mobileSubmenu = page.getByTestId('composer-model-models-submenu');
  expect(await mobileSubmenu.evaluate((element) => {
    const box = element.getBoundingClientRect();
    return box.left >= 0 && box.right <= window.innerWidth;
  })).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(390);
});

test('Task progress is docked above the composer without covering the timeline', async ({ page }) => {
  await page.goto('/workflow-fixture.html');

  const strip = page.getByTestId('todo-composer-strip');
  const composer = page.locator('.chat-composer');
  const layout = await Promise.all([
    page.locator('.composer-strips').evaluate((element) => getComputedStyle(element).position),
    strip.boundingBox(),
    composer.boundingBox(),
  ]);
  expect(layout[0]).toBe('relative');
  expect(layout[1].y + layout[1].height).toBeLessThanOrEqual(layout[2].y + 1);
  await expect(strip).toContainText('Verify restored workflow');
  await expect(strip).toContainText('已完成 1');
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

  await conversation.evaluate((element) => {
    const spacer = document.createElement('div');
    spacer.style.height = '1000px';
    spacer.dataset.testid = 'scroll-spacer';
    element.prepend(spacer);
    element.scrollTop = element.scrollHeight;
  });

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

test('Expanded reasoning content scrolls independently when it is long', async ({ page }) => {
  await page.goto('/workflow-fixture.html');
  const reasoning = page.getByTestId('reasoning-block');
  await reasoning.locator('summary').click();
  const content = reasoning.locator('.message-thinking-content');
  await content.evaluate((element) => {
    element.innerHTML = Array.from({ length: 80 }, (_, index) => `<p>Reasoning line ${index + 1}</p>`).join('');
  });

  const dimensions = await content.evaluate((element) => ({
    clientHeight: element.clientHeight,
    overflowY: getComputedStyle(element).overflowY,
    scrollHeight: element.scrollHeight,
  }));
  expect(dimensions.overflowY).toBe('auto');
  expect(dimensions.scrollHeight).toBeGreaterThan(dimensions.clientHeight);

  await content.hover();
  await page.mouse.wheel(0, 240);
  await expect.poll(() => content.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
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
