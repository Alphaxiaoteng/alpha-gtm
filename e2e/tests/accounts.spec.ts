import { test, expect } from '@playwright/test';

test.describe('Ego identity and account workspace', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await page.getByTestId('nav-ego').click();
    await expect(page.getByTestId('ego-workspace')).toBeVisible();
  });

  test('keeps account management focused on add account and configuration', async ({ page }) => {
    await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible();
    await expect(page.getByRole('button', { name: '添加账号' })).toBeVisible();
    await expect(page.getByRole('button', { name: '账号配置' })).toBeVisible();
    for (const hiddenHeading of ['新增浏览器', '平台配置', '平台收藏夹', '登录状态监控', '本机并发校准']) {
      await expect(page.getByRole('heading', { name: hiddenHeading })).toHaveCount(0);
    }

    await page.getByRole('button', { name: '添加账号' }).click();
    const addDialog = page.getByRole('dialog', { name: '添加账号' });
    await expect(addDialog).toBeVisible();
    await expect(addDialog.getByText('新建浏览器并选择需要登录的平台')).toBeVisible();
    await expect(addDialog.getByRole('textbox', { name: /浏览器名称/ })).toBeVisible();
    await expect(addDialog.getByRole('radio')).toHaveCount(0);
    await expect(addDialog.getByText('选择平台')).toBeVisible();
    await expect(addDialog.getByRole('button', { name: '关闭' })).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(addDialog).toHaveCount(0);

    await page.getByRole('button', { name: '账号配置' }).click();
    const settingsDialog = page.getByRole('dialog', { name: '账号配置' });
    await expect(settingsDialog).toBeVisible();
    await expect(settingsDialog.getByText('登录状态监控')).toBeVisible();
    await expect(settingsDialog.getByText('EgoLite 收藏夹')).toBeVisible();
  });

  test('opens platform management from the browser card with configuration before login', async ({ page }) => {
    const browser = page.getByTestId('ego-identity-1');
    const configure = browser.getByRole('button', { name: '配置 浏览器1 平台' });
    const login = browser.getByRole('button', { name: '登录未完成平台' });
    await expect(configure).toBeVisible();
    await expect(login).toBeVisible();
    const configureBox = await configure.boundingBox();
    const loginBox = await login.boundingBox();
    expect(configureBox?.x).toBeLessThan(loginBox?.x ?? 0);

    await configure.click();
    const platformDialog = page.getByRole('dialog', { name: '浏览器1 平台管理' });
    await expect(platformDialog).toBeVisible();
    await expect(platformDialog.getByText('选择平台')).toBeVisible();
    await expect(platformDialog.getByRole('textbox', { name: /浏览器名称/ })).toHaveCount(0);
    await platformDialog.getByRole('button', { name: '关闭' }).click();

    await browser.getByRole('button', { name: '管理 浏览器1 平台' }).click();
    await expect(page.getByRole('dialog', { name: '浏览器1 平台管理' })).toBeVisible();
  });

  test('shows a platform QR inside Electron and clears it when closed', async ({ page }) => {
    await page.route('**/api/accounts/*/login-qr', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      await route.fulfill({
        contentType: 'application/json',
        headers: { 'Cache-Control': 'no-store' },
        body: JSON.stringify({
          account_id: 1,
          platform_code: 'douyin',
          state: 'qr_ready',
          image_data_url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB',
          expires_at: '2026-07-31T12:00:00Z',
        }),
      });
    });
    const configure = await page.request.put('/api/ego/identities/1/platforms', {
      data: { platform_codes: ['douyin'] },
    });
    expect(configure.ok()).toBeTruthy();
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const douyin = page.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: '抖音' }).first();
    await expect(douyin.getByText('待登录', { exact: true })).toHaveCount(0);
    await douyin.getByRole('button', { name: '登录 抖音' }).click();
    const dialog = page.getByRole('dialog', { name: '抖音扫码登录' });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole('img', { name: '抖音登录二维码' })).toHaveAttribute('src', /^data:image\/png;base64,/);
    await expect(dialog.getByText('请使用对应平台 App 扫码')).toBeVisible();
    await dialog.getByRole('button', { name: '关闭' }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('img', { name: '抖音登录二维码' })).toHaveCount(0);
  });

  test('re-syncs a video-channel QR from the current EgoLite page without navigating again', async ({ page }) => {
    await page.route('**/api/ego/focus', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ ok: true }),
    }));
    await page.route('**/api/accounts/*/login-qr', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.continue();
        return;
      }
      const body = route.request().postDataJSON() as { refresh?: boolean; navigate?: boolean };
      const isCurrentPageResync = body.refresh === true && body.navigate === false;
      await route.fulfill({
        contentType: 'application/json',
        headers: { 'Cache-Control': 'no-store' },
        body: JSON.stringify(isCurrentPageResync ? {
          account_id: 1,
          platform_code: 'wechat_video',
          state: 'qr_ready',
          image_data_url: 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAAB',
          expires_at: '2026-07-31T12:00:00Z',
        } : {
          account_id: 1,
          platform_code: 'wechat_video',
          state: 'manual_takeover',
          reason_code: 'QR_UNAVAILABLE',
        }),
      });
    });
    const configure = await page.request.put('/api/ego/identities/1/platforms', {
      data: { platform_codes: ['wechat_video'] },
    });
    expect(configure.ok()).toBeTruthy();
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const videoChannel = page.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: '视频号' }).first();
    await videoChannel.getByRole('button', { name: '登录 视频号' }).click();
    const dialog = page.getByRole('dialog', { name: '视频号扫码登录' });
    await expect(dialog.getByText('此平台需要在 EgoLite 完成登录')).toBeVisible();
    await expect(dialog.getByText('未识别到二维码，请在 EgoLite 切换到视频号扫码页面后再同步。')).toBeVisible();
    await dialog.getByRole('button', { name: /同步二维码/ }).click();
    await expect(dialog.getByRole('img', { name: '视频号登录二维码' })).toHaveAttribute('src', /^data:image\/png;base64,/);
  });

  test('replaces login with a green logged-in state', async ({ page }) => {
    await page.route('**/api/accounts', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{
        id: 9901,
        identityId: 1,
        platformCode: 'douyin',
        kind: 'social',
        name: '抖音品牌号',
        avatar: '',
        accountIdentifier: 'dy-9901',
        loginState: 'logged_in',
        remark: '',
        accountGroup: '',
        enabled: true,
        browserRunning: false,
        fingerprintSeed: 0,
        fingerprintConfig: '',
        tags: [],
      }]),
    }));
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const account = page.getByTestId('ego-platform-account-9901');
    await expect(account.getByRole('button', { name: '登录 抖音' })).toHaveCount(0);
    await expect(account.getByText('已登录', { exact: true })).toHaveClass(/emerald/);
  });

  test('shows separate Meta asset portraits and explicit platform names', async ({ page }) => {
    const avatar = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=';
    await page.route('**/api/accounts', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{
        id: 9915,
        identityId: 1,
        platformCode: 'meta',
        kind: 'browser_login',
        name: 'Meta 登录容器',
        avatar,
        accountIdentifier: 'meta-9915',
        loginState: 'logged_in',
        remark: '',
        accountGroup: '',
        enabled: true,
        browserRunning: false,
        fingerprintSeed: 0,
        fingerprintConfig: '',
        tags: [],
      }]),
    }));
    await page.route('**/api/account-assets', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([
        { id: 1, account_id: 9915, platform_code: 'facebook', asset_type: 'facebook_page', name: 'Meta Page', external_id: 'page-1', avatar, connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
        { id: 2, account_id: 9915, platform_code: 'instagram', asset_type: 'instagram_account', name: 'Meta IG', external_id: 'ig-1', avatar, connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
      ]),
    }));
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const account = page.getByTestId('ego-platform-account-9915');
    await expect(account.getByText('Meta', { exact: true })).toBeVisible();
    await expect(account.getByTestId('meta-browser-assets-9915').getByRole('img')).toHaveCount(2);
    await expect(account.getByText('Facebook · Meta Page', { exact: true })).toBeVisible();
    await expect(account.getByText('Instagram · Meta IG', { exact: true })).toBeVisible();
  });

  test('opens an empty browser platform manager instead of add account', async ({ page }) => {
    const name = `空浏览器-${Date.now()}`;
    const response = await page.request.post('/api/ego/identities', { data: { name } });
    expect(response.ok()).toBeTruthy();
    const created = await response.json();
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const browser = page.getByTestId(`ego-identity-${created.id}`);
    await browser.getByRole('button', { name: '为此浏览器添加平台' }).click();
    const platformDialog = page.getByRole('dialog', { name: `${name} 平台管理` });
    await expect(platformDialog).toBeVisible();
    await expect(page.getByRole('dialog', { name: '添加账号' })).toHaveCount(0);
    await platformDialog.getByLabel('选择抖音', { exact: true }).check();
    await platformDialog.getByRole('button', { name: '保存平台' }).click();
    await expect(browser.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: '抖音' })).toHaveCount(1);
  });

  test('shows the Ego-only hierarchy and hides legacy browser controls', async ({ page }) => {
    await expect(page.getByRole('heading', { name: '账号管理' })).toBeVisible();
    await expect(page.getByTestId('ego-identity-1')).toContainText('浏览器1');
    await expect(page.getByRole('button', { name: '添加账号' })).toBeVisible();
    await expect(page.getByRole('button', { name: '账号配置' })).toBeVisible();
    await expect(page.getByTestId('nav-browser')).toHaveCount(0);
    await expect(page.getByText(/CDP|fingerprint|浏览器路径/i)).toHaveCount(0);
  });

  test('offers metadata-only Chromium account import without claiming login', async ({ page }) => {
    await page.route('**/api/ego/legacy-accounts', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        available: true,
        count: 2,
        items: [
          { platform_code: 'xhs', name: '旧小红书', account_identifier: 'xhs-88', historical_login_state: 'logged_in' },
          { platform_code: 'douyin', name: '旧抖音', account_identifier: 'dy-99', historical_login_state: 'logged_in' },
        ],
      }),
    }));
    await page.route('**/api/ego/legacy-accounts/import', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ imported: 2, identities: [], login_state: 'unknown', requires_egolite_login: true }),
    }));
    await page.reload();
    await page.getByTestId('nav-ego').click();
    await page.getByRole('button', { name: '账号配置' }).click();

    await expect(page.getByText('发现 2 个旧 Chromium 账号记录')).toBeVisible();
    await expect(page.getByText(/只导入名称和公开账号 ID/)).toBeVisible();
    await page.getByRole('button', { name: '导入旧账号资料' }).click();
    await expect(page.getByText('已导入 2 个账号资料，需在 EgoLite 重新登录')).toBeVisible();
  });

  test('configures the serial login monitor without creating a Space', async ({ page }) => {
    await page.getByRole('button', { name: '账号配置' }).click();
    const interval = page.getByRole('spinbutton', { name: '登录状态检查间隔' });
    const enabled = page.getByRole('switch', { name: '开启登录状态监控' });
    await expect(enabled).toHaveAttribute('aria-checked', 'true');
    await expect(interval).toBeEnabled();

    await interval.fill('90');
    await page.getByRole('button', { name: '保存检查间隔' }).click();
    await expect.poll(async () => {
      const response = await page.request.get('/api/ego/session-monitor');
      return (await response.json()).interval_minutes;
    }).toBe(90);
    await expect(enabled).toBeEnabled();

    await enabled.click({ force: true });
    await expect.poll(async () => {
      const response = await page.request.get('/api/ego/session-monitor');
      return (await response.json()).enabled;
    }).toBe(false);
    await expect(enabled).toHaveAttribute('aria-checked', 'false');
    await expect(interval).toBeDisabled();

    const settings = await page.request.get('/api/ego/session-monitor');
    expect(settings.ok()).toBeTruthy();
    expect(await settings.json()).toMatchObject({ enabled: false, interval_minutes: 90, concurrency: 1 });
    const spaces = await page.request.get('/api/ego/spaces');
    expect(await spaces.json()).toEqual([]);

    await expect(enabled).toBeEnabled();
    await enabled.click({ force: true });
    await expect.poll(async () => {
      const response = await page.request.get('/api/ego/session-monitor');
      return (await response.json()).enabled;
    }).toBe(true);
    await expect(enabled).toHaveAttribute('aria-checked', 'true');
  });

  test('renames a browser and configures multiple platform accounts without entering usernames', async ({ page }) => {
    const defaultIdentity = page.getByTestId('ego-identity-1');
    await page.getByRole('button', { name: '账号配置' }).click();
    const settingsDialog = page.getByRole('dialog', { name: '账号配置' });
    await settingsDialog.getByRole('button', { name: /编辑 .* 名称/ }).first().click();
    const nameInput = settingsDialog.getByRole('textbox', { name: '浏览器名称' });
    await nameInput.fill('品牌主号');
    const renameResponse = page.waitForResponse((response) =>
      response.request().method() === 'PATCH' && response.url().endsWith('/api/ego/identities/1'));
    await settingsDialog.getByRole('button', { name: '保存浏览器名称' }).click();
    expect((await renameResponse).status()).toBe(200);
    await expect(defaultIdentity).toContainText('品牌主号');
    await settingsDialog.getByRole('button', { name: '关闭' }).click();

    const suffix = Date.now();
    await page.getByRole('button', { name: '添加账号' }).click();
    const addDialog = page.getByRole('dialog', { name: '添加账号' });
    const identityInput = addDialog.getByRole('textbox', { name: /浏览器名称/ });
    await identityInput.fill(`海外运营-${suffix}`);
    await addDialog.getByLabel('选择小红书').check();
    await addDialog.getByLabel('选择YouTube').check();
    await addDialog.getByRole('button', { name: '保存账号' }).click();
    const user2 = page.locator('[data-testid^="ego-identity-"]').filter({ hasText: `海外运营-${suffix}` });
    await expect(user2).toBeVisible();

    const xhsAccount = user2.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: '小红书' });
    const youtubeAccount = user2.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: 'YouTube' });
    await expect(xhsAccount.getByTestId(/ego-account-avatar-/)).toHaveCount(0);
    await expect(xhsAccount.getByTestId(/ego-account-platform-/)).toBeVisible();
    await expect(youtubeAccount.getByTestId(/ego-account-avatar-/)).toHaveCount(0);
    await expect(youtubeAccount.getByTestId(/ego-account-platform-/)).toBeVisible();
  });

  test('keeps one account per platform inside a browser', async ({ page }) => {
    const suffix = Date.now();
    const identityName = `平台测试-${suffix}`;
    await page.getByRole('button', { name: '添加账号' }).click();
    const addDialog = page.getByRole('dialog', { name: '添加账号' });
    const identityInput = addDialog.getByRole('textbox', { name: /浏览器名称/ });
    await identityInput.fill(identityName);
    await addDialog.getByLabel('选择小红书').check();
    await addDialog.getByLabel('选择YouTube').check();
    await addDialog.getByRole('button', { name: '保存账号' }).click();

    const identityCard = page.locator('[data-testid^="ego-identity-"]').filter({ hasText: identityName });
    await expect(identityCard).toBeVisible();

    await expect(identityCard).toContainText('0/2 已登录');
    await expect(identityCard.locator('[data-testid^="ego-platform-account-"]').filter({ hasText: '小红书' })).toHaveCount(1);
  });

  test('renders manual takeover state per Space without blocking other accounts', async ({ page }) => {
    const identity = await page.request.post('/api/ego/identities', {
      data: { name: `接管测试-${Date.now()}` },
    });
    expect(identity.ok()).toBeTruthy();
    const createdIdentity = await identity.json();
    const accountResponse = await page.request.post('/api/accounts/add', {
      data: {
        platform: 'douyin',
        name: '需要扫码的账号',
        remark: '',
        startNow: false,
        identityId: createdIdentity.id,
      },
    });
    expect(accountResponse.ok()).toBeTruthy();
    await accountResponse.json();

    await page.route('**/api/ego/spaces', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{
        id: 1,
        identity_id: createdIdentity.id,
        space_id: 901,
        task_id: 'e2e-takeover',
        name: 'e2e-takeover',
        ownership: 'user',
        state: 'manual_takeover',
        control_epoch: 2,
        active_job_id: 0,
        last_seen_at: '',
        error_code: '',
        created_at: '',
        updated_at: '',
      }]),
    }));
    await page.reload();
    await page.getByTestId('nav-ego').click();

    const identityCard = page.getByTestId(`ego-identity-${createdIdentity.id}`);
    await expect(identityCard).toContainText('1 个待处理');
    await expect(identityCard.getByRole('button', { name: '登录 抖音' })).toBeEnabled();
  });
});
