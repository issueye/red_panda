import { expect, test } from '@playwright/test';

test('Activity timeline filters groups and payload inspection work in browser', async ({ page }) => {
  await page.goto('/activity-fixture.html');

  const timeline = page.getByTestId('activity-event-timeline');
  await expect(timeline).toBeVisible();
  await expect(page.getByTestId('activity-event-count')).toHaveText('4/4');
  await expect(page.getByTestId('activity-event-group')).toHaveCount(4);

  await page.getByTestId('activity-event-kind-filter').selectOption('tool');
  await expect(page.getByTestId('activity-event-count')).toHaveText('1/4');
  await expect(timeline.getByText('workspace.read_file')).toBeVisible();
  await expect(timeline.getByText('planner saw the file')).toHaveCount(0);

  await page.getByTestId('activity-event-kind-filter').selectOption('all');
  await page.getByTestId('activity-event-agent-filter').selectOption('subagent:planner');
  await expect(page.getByTestId('activity-event-count')).toHaveText('1/4');
  await expect(timeline.getByText('planner saw the file')).toBeVisible();
  await expect(timeline.getByText('workspace.read_file')).toHaveCount(0);

  await page.getByTestId('activity-event-agent-filter').selectOption('all');
  await page.getByTestId('activity-event-kind-filter').selectOption('permission');
  await expect(page.getByTestId('activity-event-count')).toHaveText('1/4');
  await page.getByTestId('activity-event-payload-toggle').click();
  await expect(page.getByTestId('activity-event-payload')).toContainText('"permission_id": "perm_fixture"');
  await expect(page.getByTestId('activity-event-payload')).toContainText('"tool_name": "shell.exec"');
});
