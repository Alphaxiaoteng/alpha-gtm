import { expect, test, type Page } from '@playwright/test';
import path from 'node:path';

const verifiedModel = 'opencode/test-live';
const unreachableModel = 'opencode/test-unreachable';

function providerState(models: string[] = [], validatingModel = '', validationError = '') {
  return {
    providers: [
      { id: 'codex', label: 'Codex', capabilities: ['chat'] },
      { id: 'opencode', label: 'OpenCode', capabilities: ['chat'] },
      { id: 'antigravity', label: 'Antigravity CLI', capabilities: ['chat'] },
    ],
    probes: [
      { provider_id: 'codex', state: 'ready', available: true, ready: true, version: 'test' },
      {
        provider_id: 'opencode',
        state: 'ready',
        available: true,
        ready: true,
        version: 'test',
        models,
        model_candidates: [unreachableModel, verifiedModel],
        validating_model: validatingModel || undefined,
        model_validation_error: validationError || undefined,
      },
      {
        provider_id: 'antigravity',
        state: 'incompatible',
        available: true,
        ready: false,
        version: '1.1.6',
        reason: '测试环境保持不兼容',
      },
    ],
  };
}

async function stubModelValidation(page: Page) {
  const verified: string[] = [];
  let activeModel = '';
  let pollCount = 0;
  let validationError = '';
  let batchFinished = false;

  await page.route('**/api/agent/providers', async (route) => {
    if (batchFinished) {
      const state = providerState(verified, '', 'AGENT_MODEL_UNREACHABLE');
      Object.assign(state.probes[1], {
        model_validation_total: 2,
        model_validation_completed: 2,
        model_validation_passed: 1,
        model_validation_failures: [{ model_id: unreachableModel, error_code: 'AGENT_MODEL_UNREACHABLE' }],
      });
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify(state),
      });
      return;
    }
    if (activeModel) {
      pollCount += 1;
      if (pollCount === 1) {
        await route.fulfill({
          contentType: 'application/json',
          body: JSON.stringify(providerState(verified, activeModel)),
        });
        return;
      }
      const completedModel = activeModel;
      activeModel = '';
      if (completedModel === unreachableModel) {
        validationError = 'AGENT_MODEL_UNREACHABLE';
      } else {
        validationError = '';
        verified.push(completedModel);
      }
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify(providerState(verified, '', validationError)),
    });
  });

  await page.route('**/api/agent/providers/opencode/models/validate', async (route) => {
    const body = route.request().postDataJSON() as { model_id?: string };
    activeModel = body.model_id || '';
    pollCount = 0;
    validationError = '';
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ provider_id: 'opencode', model_id: activeModel, status: 'validating' }),
    });
  });

  await page.route('**/api/agent/providers/opencode/models/validate-batch', async (route) => {
    const body = route.request().postDataJSON() as { model_ids?: string[] };
    expect(body.model_ids).toEqual([unreachableModel, verifiedModel]);
    if (!verified.includes(verifiedModel)) verified.push(verifiedModel);
    batchFinished = true;
    await route.fulfill({
      status: 202,
      contentType: 'application/json',
      body: JSON.stringify({ provider_id: 'opencode', total: 2, status: 'validating' }),
    });
  });

  await page.route('**/api/agent/providers/opencode/selection', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        provider_id: 'opencode',
        model_id: (route.request().postDataJSON() as { model_id?: string }).model_id || '',
        draft: '',
      }),
    });
  });

  await page.route('**/api/agent/provider-catalog', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        providers: [
          {
            id: 'claude',
            label: 'Claude Code',
            description: 'Anthropic 官方本地 CLI',
            supported: true,
            executable_configurable: true,
          },
          {
            id: 'kimi',
            label: 'Kimi Code',
            description: 'Kimi 本地 CLI',
            supported: true,
            executable_configurable: true,
          },
          {
            id: 'codebuddy',
            label: 'CodeBuddy Code',
            description: '腾讯 CodeBuddy 本地 CLI',
            supported: true,
            executable_configurable: true,
          },
        ],
      }),
    });
  });

  await page.route('**/api/agent/bootstrap**', async (route) => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ...providerState(verified),
        selection: { provider_id: 'opencode', model_id: '', draft: '' },
        conversations: [],
        pending_approvals: [],
      }),
    });
  });
}

test('shows a visual restore state while Agent bootstrap is pending', async ({ page }) => {
  let releaseBootstrap = () => {};
  const bootstrapGate = new Promise<void>((resolve) => {
    releaseBootstrap = resolve;
  });

  await page.route('**/api/agent/bootstrap**', async (route) => {
    await bootstrapGate;
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        ...providerState([]),
        selection: { provider_id: 'opencode', model_id: '', draft: '' },
        conversations: [],
        pending_approvals: [],
      }),
    });
  });

  await page.goto('/');
  await page.getByTestId('agent-drawer-toggle').click();
  const loading = page.getByTestId('agent-bootstrap-loading');
  await expect(loading).toBeVisible();
  await expect(loading.locator('.local-loading-mark > span')).toHaveCount(3);
  await expect(loading).toContainText('正在恢复 Agent');
  await page.getByTestId('agent-drawer').screenshot({
    path: path.join(process.cwd(), 'artifacts', 'agent-bootstrap-loading.png'),
  });

  releaseBootstrap();
  await expect(loading).toBeHidden();
});

test('shows Agent providers as one compact row per platform by default', async ({ page }) => {
  await stubModelValidation(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  await page.getByTestId('nav-settings').click();

  const section = page.getByTestId('settings-agent-providers');
  const rows = page.getByTestId('settings-agent-provider-list').locator('article[data-testid^="settings-agent-provider-"]');
  await expect(rows).toHaveCount(6);
  await expect(rows.nth(0)).toContainText('OpenCode');
  await expect(rows.nth(1)).toContainText('Codex');
  await expect(rows.nth(2)).toContainText('Claude Code');
  await expect(rows.nth(3)).toContainText('Antigravity CLI');
  await expect(rows.nth(4)).toContainText('Kimi Code');
  await expect(rows.nth(5)).toContainText('CodeBuddy Code');
  await expect(page.getByTestId('settings-agent-provider-opencode-primary')).toContainText('当前');
  await expect(page.locator('[data-testid$="-model-panel"]')).toHaveCount(0);
  await expect(page.getByTestId('settings-agent-provider-kimi')).toContainText('本机未验证');
  await expect(page.getByTestId('settings-agent-provider-claude')).toContainText('本机未验证');
  await expect(page.getByTestId('settings-agent-provider-kimi-catalog-model')).toHaveCount(0);
  await expect(page.getByTestId('settings-agent-external-clients')).toHaveCount(0);
  await expect(page.getByTestId('settings-agent-provider-list')).not.toContainText('WorkBuddy');
  await expect(page.getByTestId('settings-agent-provider-list')).not.toContainText('ZCode');
  await expect.poll(async () => (await rows.nth(0).boundingBox())?.height || 999).toBeLessThan(90);

  await section.screenshot({ path: path.join(process.cwd(), 'artifacts', 'settings-agent-providers-1440.png') });
  await page.setViewportSize({ width: 900, height: 900 });
  await expect(section).toBeVisible();
  await expect(page.getByTestId('settings-agent-provider-opencode-model-manage')).toBeVisible();
  await expect(page.getByTestId('settings-agent-provider-opencode-connect')).toBeVisible();
  await expect.poll(() => section.evaluate((element) => element.scrollWidth <= element.clientWidth + 1)).toBe(true);
  await section.screenshot({ path: path.join(process.cwd(), 'artifacts', 'settings-agent-providers-900.png') });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.getByTestId('settings-agent-provider-opencode-model-manage').click();
  await expect(page.getByTestId('settings-agent-provider-opencode-model-panel')).toBeVisible();
  await section.screenshot({ path: path.join(process.cwd(), 'artifacts', 'settings-agent-model-panel-1440.png') });
  await page.getByTestId('settings-agent-provider-claude-catalog-manage').click();
  const claudeDialog = page.getByRole('dialog', { name: '接入 Claude Code' });
  await expect(claudeDialog).toBeVisible();
  await expect(claudeDialog.locator('img')).toHaveAttribute('src', '/agent-providers/claude-code.ico');
  await expect(page.getByTestId('settings-agent-provider-claude-catalog-model')).toBeVisible();
  await claudeDialog.getByRole('button', { name: '关闭接入窗口' }).click();
  await page.getByTestId('settings-agent-provider-kimi-catalog-manage').click();
  await expect(page.getByTestId('settings-agent-provider-kimi-catalog-model')).toBeVisible();
});

test('adds only a verified model and updates the docked Agent in real time', async ({ page }) => {
  await stubModelValidation(page);
  await page.goto('/');
  await page.getByTestId('agent-drawer-toggle').click();
  const drawer = page.getByTestId('agent-drawer');
  await expect(drawer).toHaveAttribute('data-layout', 'docked');
  await expect(drawer.getByTestId('agent-model-select').locator('option')).toHaveText(['暂无已验证模型']);

  await page.getByTestId('nav-settings').click();

  const list = page.getByTestId('settings-agent-provider-list');
  await expect(list.locator('article[data-testid^="settings-agent-provider-"]')).toHaveCount(6);
  const card = page.getByTestId('settings-agent-provider-opencode');
  const manage = page.getByTestId('settings-agent-provider-opencode-model-manage');
  await expect(manage).toHaveAttribute('aria-expanded', 'false');
  await expect(page.getByTestId('settings-agent-provider-opencode-model-panel')).toHaveCount(0);
  await manage.click();
  await expect(manage).toHaveAttribute('aria-expanded', 'true');
  const input = page.getByTestId('settings-agent-provider-opencode-model-input');
  const validate = page.getByTestId('settings-agent-provider-opencode-model-validate');

  await input.fill(unreachableModel);
  await validate.click();
  await expect(card).toContainText('未添加：模型调用失败，请检查登录状态、模型权限或网络后重试。');
  await expect(card).not.toContainText(`已验证可用（1）：${unreachableModel}`);
  await expect(drawer.getByTestId('agent-model-select')).not.toContainText(unreachableModel);

  await input.fill(verifiedModel);
  await validate.click();
  await expect(card).toContainText(verifiedModel);

  const options = drawer.getByTestId('agent-model-select').locator('option');
  await expect(options).toHaveText(['请选择已验证模型', verifiedModel]);
  await expect(drawer.getByTestId('agent-model-select')).toHaveValue(verifiedModel);
  await expect(drawer.getByTestId('agent-model-select')).not.toContainText(unreachableModel);
});

test('shows every verified model in Settings and the Agent model switcher', async ({ page }) => {
  const secondModel = 'proxy/deepseek-v4-flash-free';
  await stubModelValidation(page);
  await page.unroute('**/api/agent/providers');
  await page.unroute('**/api/agent/bootstrap**');
  await page.route('**/api/agent/providers', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(providerState([verifiedModel, secondModel])),
  }));
  await page.route('**/api/agent/bootstrap**', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify({
      ...providerState([verifiedModel, secondModel]),
      selection: { provider_id: 'opencode', model_id: verifiedModel, draft: '' },
      conversations: [],
      pending_approvals: [],
    }),
  }));

  await page.goto('/');
  await page.getByTestId('nav-settings').click();
  const settingsOptions = page.getByTestId('settings-agent-provider-opencode-model-select').locator('option');
  await expect(settingsOptions).toHaveText(['可用模型（2）', verifiedModel, secondModel]);

  await page.getByTestId('agent-drawer-toggle').click();
  const drawerOptions = page.getByTestId('agent-drawer').getByTestId('agent-model-select').locator('option');
  await expect(drawerOptions).toHaveText(['请选择已验证模型', verifiedModel, secondModel]);
  await expect(page.getByTestId('agent-drawer').getByTestId('agent-model-select')).toHaveValue(verifiedModel);
});

test('batch-validates every discovered OpenCode candidate and keeps failed models out of Agent', async ({ page }) => {
  await stubModelValidation(page);
  await page.goto('/');
  await page.getByTestId('nav-settings').click();

  const card = page.getByTestId('settings-agent-provider-opencode');
  await page.getByTestId('settings-agent-provider-opencode-model-manage').click();
  await expect(card).toContainText('2 个候选模型待验证');
  page.once('dialog', async (dialog) => {
    expect(dialog.type()).toBe('confirm');
    expect(dialog.message()).toContain('可能产生模型费用');
    await dialog.accept();
  });
  await page.getByTestId('settings-agent-provider-opencode-validate-candidates').click();
  await expect(card).toContainText(verifiedModel, { timeout: 30_000 });
  await expect(card).toContainText('后台实测进度：2/2，通过 1，失败 1');

  await page.getByTestId('agent-drawer-toggle').click();
  const models = page.getByTestId('agent-drawer').getByTestId('agent-model-select').locator('option');
  await expect(models).toHaveText(['请选择已验证模型', verifiedModel]);
  await expect(page.getByTestId('agent-drawer')).not.toContainText(unreachableModel);
});
