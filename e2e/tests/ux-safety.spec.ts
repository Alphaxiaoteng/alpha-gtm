import { expect, test } from '@playwright/test';

const contentFixture = {
  id: 991,
  type: 'video',
  title: '本地内容不应被账号搜索隐藏',
  caption: '搜索状态必须按工作区隔离',
  body: '',
  tags_json: '[]',
  status: 'draft',
  publish_count: 0,
};

test.describe('workspace safety and responsive layout', () => {
  test('keeps the Ego account workspace isolated from the content library', async ({ page }) => {
    await page.route('**/api/contents', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([contentFixture]),
    }));

    await page.goto('/');
    await page.getByTestId('nav-ego').click();
    await expect(page.getByTestId('ego-workspace')).toBeVisible();
    await page.getByTestId('nav-social').click();

    await expect(page.getByTestId('local-content-item-991')).toBeVisible();
    await expect(page.getByTestId('local-content-item-991')).toContainText('本地内容不应被账号搜索隐藏');
    await expect(page.getByTestId('local-content-item-991')).not.toHaveCSS('user-select', 'none');
    await expect(page.getByText('还没有内容，添加第一条')).toHaveCount(0);
  });

  test('asks before discarding an edited content draft', async ({ page }) => {
    await page.route('**/api/contents', (route) => route.fulfill({
      contentType: 'application/json',
      body: '[]',
    }));

    await page.goto('/');
    await page.getByTestId('nav-social').click();
    await page.getByTestId('social-media-section-article').click();
    await page.getByRole('button', { name: '添加内容' }).click();
    await page.getByTestId('local-content-title').fill('不能静默丢失的草稿');

    const dismissed = page.waitForEvent('dialog').then(async (dialog) => {
      expect(dialog.type()).toBe('confirm');
      await dialog.dismiss();
    });
    await page.getByRole('button', { name: '关闭内容表单' }).click();
    await dismissed;
    await expect(page.getByTestId('local-content-add-modal')).toBeVisible();
    await expect(page.getByTestId('local-content-title')).toHaveValue('不能静默丢失的草稿');

    const accepted = page.waitForEvent('dialog').then(async (dialog) => {
      expect(dialog.type()).toBe('confirm');
      await dialog.accept();
    });
    await page.getByRole('button', { name: '关闭内容表单' }).click();
    await accepted;
    await expect(page.getByTestId('local-content-add-modal')).toHaveCount(0);
  });

  test('keeps Agent as a right rail at desktop minimum width', async ({ page }) => {
    await page.setViewportSize({ width: 1230, height: 768 });
    await page.goto('/');
    await page.getByTestId('nav-ego').click();
    const mainBefore = await page.getByTestId('app-main').boundingBox();

    await page.getByTestId('agent-drawer-toggle').click();
    const mainAfter = await page.getByTestId('app-main').boundingBox();
    const drawer = page.getByTestId('agent-drawer');

    expect(mainBefore).not.toBeNull();
    expect(mainAfter).not.toBeNull();
    expect(mainAfter!.width).toBeLessThan(mainBefore!.width);
    await expect(drawer).toHaveAttribute('data-layout', 'docked');
    await expect(page.getByTestId('ego-workspace')).toBeVisible();
  });
});
