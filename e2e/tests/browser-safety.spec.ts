import { test, expect } from '@playwright/test';

test.describe('EgoLite browser boundary', () => {
  test('legacy browser process and configuration routes are unavailable', async ({ request }) => {
    for (const path of [
      '/api/browsers',
      '/api/config/browsers',
      '/api/accounts/1/browser/start',
      '/api/browsers/stop-batch',
    ]) {
      const response = path.endsWith('/start') || path.endsWith('/stop-batch')
        ? await request.post(path, { data: {} })
        : await request.get(path);
      expect(response.status(), path).toBe(404);
    }
  });

  test('keeps Ego runtime details behind the compact account configuration', async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-ego').click();

    await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible();
    await expect(page.getByText('并发校准')).toHaveCount(0);
    await page.getByRole('button', { name: '账号配置' }).click();
    await expect(page.getByRole('dialog', { name: '账号配置' }).getByText('并发校准')).toBeVisible();
    await expect(page.getByText(/Chrome|Edge|Playwright|fingerprint-chromium|CDP/i)).toHaveCount(0);
  });

  test('returns an empty Space collection as a JSON array', async ({ request }) => {
    const response = await request.get('/api/ego/spaces');
    expect(response.ok()).toBeTruthy();
    expect(await response.json()).toEqual([]);
  });
});
