import { expect, test } from '@playwright/test';

test.describe('Social media workspace', () => {
  test('stores an article locally with only a title and body', async ({ page }) => {
    const title = '本地文章内容';
    await page.goto('/');
    await page.getByTestId('nav-social').click();
    await page.getByTestId('social-media-section-article').click();
    await page.getByRole('button', { name: '添加内容' }).click();
    await page.getByTestId('local-content-title').fill(title);
    await page.getByTestId('local-content-body').fill('仅用于本地内容库回归验证的正文。');
    await page.getByTestId('local-content-save').click();
    await expect(page.getByText('文章已保存到内容库')).toBeVisible();

    await page.reload();
    await page.getByTestId('nav-social').click();
    await page.getByTestId('social-media-section-article').click();
    await expect(page.getByText(title, { exact: true })).toBeVisible();
  });

  test('separates video and graph-text content into their own boards', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-social').click();

    await expect(page.getByTestId('local-content-video')).toBeVisible();
    await expect(page.getByRole('heading', { name: '视频看板' })).toBeVisible();
    await page.getByRole('button', { name: '添加内容' }).click();
    await expect(page.getByTestId('local-content-title')).toBeVisible();
    await expect(page.getByTestId('local-content-video-file')).toHaveCount(1);
    await page.getByRole('button', { name: '关闭内容表单' }).click();
    await page.getByTestId('social-media-section-graph_text').click();
    await expect(page.getByTestId('local-content-graph_text')).toBeVisible();
    await expect(page.getByRole('heading', { name: '图文看板' })).toBeVisible();
    await page.getByRole('button', { name: '添加内容' }).click();
    await expect(page.getByTestId('local-content-image-files')).toHaveCount(1);
    await expect(page.getByText('从素材库选择图片')).toBeVisible();
    await expect(page.getByTestId('social-media-workspace').getByText('素材与脚本', { exact: true })).toHaveCount(0);
    await expect(page.getByTestId('social-media-workspace').getByText('九宫格', { exact: true })).toHaveCount(0);
  });
});
