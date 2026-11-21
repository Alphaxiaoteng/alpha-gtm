import { expect, test } from '@playwright/test';

test('keeps the Agent usable when creating a conversation fails', async ({ page }) => {
  const providerState = {
    providers: [{
      id: 'opencode',
      label: 'OpenCode',
      capabilities: ['chat'],
    }],
    probes: [{
      provider_id: 'opencode',
      state: 'ready',
      available: true,
      ready: true,
      models: ['verified-model'],
    }],
  };
  await page.route('**/api/agent/providers', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(providerState),
  }));
  await page.route('**/api/agent/bootstrap**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      ...providerState,
      selection: { provider_id: 'opencode', model_id: 'verified-model' },
      conversations: [],
      pending_approvals: [],
    }),
  }));
  await page.route('**/api/agent/providers/opencode/selection', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({ provider_id: 'opencode', model_id: 'verified-model' }),
  }));
  await page.route('**/api/agent/conversations**', (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: '会话服务暂不可用' }),
      });
    }
    return route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ conversations: [] }),
    });
  });

  await page.goto('/');
  await page.getByTestId('agent-drawer-toggle').click();
  await expect(page.getByTestId('agent-bootstrap-loading')).toHaveCount(0);
  // The inline new-session button was removed from the stage header; sending a
  // message without an active conversation is the create-conversation path.
  await page.getByPlaceholder('输入 @ 引用资料或趋势，/ 使用 Skill…').fill('hello');
  await page.getByRole('button', { name: '发送' }).click();

  const alert = page.getByRole('alert');
  await expect(alert).toContainText('本次运行没有完成');
  await expect(page.getByTestId('agent-drawer')).toBeVisible();
  await expect(page.getByRole('button', { name: '发送' })).toBeEnabled();
  await alert.getByRole('button', { name: '关闭错误提示' }).click();
  await expect(alert).toHaveCount(0);
});
