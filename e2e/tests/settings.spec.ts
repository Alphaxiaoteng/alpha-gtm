import { test, expect } from '@playwright/test';

test.describe('EgoLite settings workspace', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-settings').click();
    await expect(page.getByTestId('ego-settings')).toBeVisible();
  });

  test('shows only the Ego runtime, Spaces, and concurrency diagnostics', async ({ page }) => {
    const settings = page.getByTestId('ego-settings');
    await expect(settings).toContainText('EgoLite 浏览器运行时');
    await expect(settings).toContainText('ego-browser CLI');
    await expect(settings).toContainText('Space 并发');
    await expect(settings).toContainText('运行本机校准');
    await expect(settings).toContainText('alpha-nexus-egolite ego doctor');
    await expect(settings).not.toContainText('浏览器路径');
    await expect(settings).not.toContainText('CDP 端口');
    await expect(settings.getByTestId('settings-section-agent')).toBeVisible();
  });

  test('documents takeover, credential, and one-shot submit boundaries', async ({ page }) => {
    const settings = page.getByTestId('ego-settings');
    await expect(settings).toContainText('验证码、扫码和 2FA');
    await expect(settings).toContainText('不读取、导出或记录平台登录存储与密码');
    await expect(settings).toContainText('冻结批次的一次性明确授权');
    await expect(settings).toContainText('result_unknown，禁止自动重试');
  });

  test('keeps direct LLM configuration inside Alpha Agent and identifies every runtime', async ({ page }) => {
    await page.route('**/api/agent/providers', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          providers: [
            { id: 'alpha', label: 'Alpha Agent', capabilities: ['Pi runtime'] },
            { id: 'opencode', label: 'OpenCode', capabilities: ['streaming chat'] },
            { id: 'codex', label: 'Codex', capabilities: ['streaming chat'] },
            { id: 'custom', label: '自定义 LLM', capabilities: ['OpenCode runtime'] },
          ],
          probes: [
            { provider_id: 'alpha', state: 'ready', available: true, ready: true, models: ['pi/fast'] },
            { provider_id: 'opencode', state: 'ready', available: true, ready: true, models: ['opencode/works'] },
            { provider_id: 'codex', state: 'ready', available: true, ready: true, models: ['codex/works'] },
            { provider_id: 'custom', state: 'ready', available: true, ready: true, models: ['alpha-nexus-llm/deepseek-chat'] },
          ],
        }),
      });
    });
    await page.route('**/api/agent/models', async (route) => {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          provider_type: 'custom',
          provider_name: '我的网关',
          base_url: 'https://api.example.test/v1',
          protocol: 'chat_completions',
          api_configured: true,
          default_model: 'deepseek-chat',
          default_effort: 'medium',
          models: [{ id: 'deepseek-chat', label: 'deepseek-chat', enabled: true }],
        }),
      });
    });

    await page.goto('/');
    await page.getByTestId('nav-settings').click();
    await page.getByTestId('settings-section-agent').click();

    const agent = page.getByTestId('alpha-agent-settings');
    await expect(agent.getByTestId('alpha-agent-llm-config')).toBeVisible();
    await expect(agent.getByTestId('settings-llm-verify')).toBeVisible();
    for (const providerID of ['alpha', 'opencode', 'codex', 'custom']) {
      await expect(agent.getByTestId(`alpha-agent-provider-${providerID}-mark`)).toBeVisible();
    }
    await expect(agent.getByTestId('alpha-agent-provider-opencode-mark').locator('img')).toHaveAttribute('src', '/brand/opencode-logo.png');
    await expect(agent.getByTestId('alpha-agent-provider-codex-mark').locator('img')).toHaveAttribute('src', '/brand/codex-logo.png');
  });
});
