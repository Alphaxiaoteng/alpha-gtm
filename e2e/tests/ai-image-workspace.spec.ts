import { test, expect } from '@playwright/test';

test.describe('AI image feature gate', () => {
  test('keeps AI image navigation and settings hidden by default', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByTestId('nav-ai-image')).toHaveCount(0);
    await expect(page.getByRole('button', { name: /AI.*生图|生图/i })).toHaveCount(0);

    await page.getByTestId('nav-settings').click();
    await expect(page.getByText(/AI.*生图|图片生成模型/i)).toHaveCount(0);
  });
});
