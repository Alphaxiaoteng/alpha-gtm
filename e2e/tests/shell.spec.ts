import { test, expect } from '@playwright/test';

test.describe('App shell & controlled publish flow', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
    await expect(page.getByTestId('app-shell')).toBeVisible();
  });

  test('shows the compact desktop navigation and three-step publish entry', async ({ page }) => {
    await expect(page.getByTestId('app-title')).toContainText('Alpha-Nexus EgoLite');
    await expect(page.getByTestId('backend-status-text')).toHaveText('正常', { timeout: 20_000 });
    const entries = page.getByTestId('app-sidebar').locator('[data-testid^="nav-"]');
    await expect(entries).toHaveCount(7);
    await expect(entries).toHaveText(['浏览器与账号', '内容库', '知识库', '趋势雷达', '账号数据', '一键发布', '设置']);

    await page.getByTestId('nav-publish').click();
    await expect(page.getByTestId('publish-workspace')).toBeVisible();
    await expect(page.getByTestId('publish-step-accounts')).toContainText('选账号');
    await expect(page.getByTestId('publish-step-accounts')).toContainText('选择发布目标');
    await expect(page.getByTestId('publish-step-content')).toContainText('填内容');
    await expect(page.getByTestId('publish-step-review')).toContainText('发布');
    await expect(page.getByText('演练模式', { exact: true })).toHaveCount(0);
    await expect(page.getByText('尝试自动提交', { exact: true })).toHaveCount(0);
    await page.getByTestId('nav-social').click();
    await expect(page.getByTestId('social-media-workspace')).toBeVisible();
    await expect(page.getByTestId('social-media-section-video')).toHaveText('视频');
    await expect(page.getByTestId('social-media-section-graph_text')).toHaveText('图文');
    await expect(page.getByTestId('social-media-section-article')).toHaveText('文章');
    await page.getByTestId('nav-dashboard').click();
    await expect(page.getByTestId('trend-radar-dashboard')).toBeVisible();
    await page.getByTestId('agent-drawer-toggle').click();
    await expect(page.getByTestId('agent-drawer')).toBeVisible();
    await page.getByTestId('nav-settings').click();
    await expect(page.getByTestId('ego-settings')).toBeVisible();
  });

  test('keeps Agent as a non-overlapping right rail at supported desktop widths', async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 860 });
    await page.evaluate(() => {
      window.localStorage.setItem('alpha-nexus-agent-panel-width-v3', '420');
      window.name = '';
    });
    await page.reload();
    await page.getByTestId('agent-drawer-toggle').click();

    const shell = page.getByTestId('app-shell');
    const main = page.getByTestId('app-main');
    const drawer = page.getByTestId('agent-drawer');
    await expect(drawer).toHaveAttribute('data-layout', 'docked');

    for (const width of [1440, 1224, 1100]) {
      await page.setViewportSize({ width, height: 860 });
      await expect.poll(async () => Math.round((await shell.boundingBox())?.width || 0)).toBe(width);

      const [shellBox, mainBox, drawerBox] = await Promise.all([
        shell.boundingBox(),
        main.boundingBox(),
        drawer.boundingBox(),
      ]);
      expect(shellBox).not.toBeNull();
      expect(mainBox).not.toBeNull();
      expect(drawerBox).not.toBeNull();
      expect(await drawer.evaluate((element) => getComputedStyle(element).position)).toBe('relative');
      expect(Math.round(drawerBox!.x + drawerBox!.width)).toBe(Math.round(shellBox!.x + shellBox!.width));
      expect(Math.round(mainBox!.x + mainBox!.width)).toBe(Math.round(drawerBox!.x));
      expect(drawerBox!.width).toBeGreaterThanOrEqual(320);
      expect(drawerBox!.width).toBeLessThanOrEqual(width === 1440 ? 480 : 320);
    }
  });

  test('selects an article through content cards without a native menu', async ({ page }) => {
    const article = { id: 61, type: 'article', title: '文章填充测试', caption: '', body: '这是用于验证文章内容选择的本地草稿。', tags_json: '["AlphaNexus"]', status: 'draft', publish_count: 0 };
    const account = {
      id: 701, identityId: 701, name: 'CSDN 账号', platformCode: 'csdn', kind: 'browser', avatar: '', accountIdentifier: '', loginState: 'logged_in',
      loginReasonCode: 'VERIFIED_SESSION_DOM', remark: '', enabled: true, browserRunning: true, fingerprintSeed: 0, fingerprintConfig: '', tags: [],
    };

    await page.route('**/api/contents', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([article]) }));
    await page.route('**/api/contents/61/media', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
    await page.route('**/api/accounts', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([account]) }));
    await page.route('**/api/ego/spaces', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{ identity_id: 701, ownership: 'agent', state: 'ready' }]),
    }));
    await page.route('**/api/account-assets', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{ id: 71, account_id: 701, platform_code: 'csdn', asset_type: 'browser_account', name: 'CSDN 账号', external_id: '', avatar: '', connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' }]),
    }));
    await page.route('**/api/account-tags', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
    await page.route('**/api/templates', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
    await page.route('**/api/platforms', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([{ code: 'csdn', name: 'CSDN', loginUrl: '', managerUrl: '', publishMode: 'manual_handoff', canCreatePublishJob: true, canAttemptAutoSubmit: false, publishSummary: '' }]),
    }));

    await page.reload();
    await page.getByTestId('nav-publish').click();
    await page.getByTestId('publish-family-article').click();
    await expect(page.getByTestId('publish-capability-71')).toContainText('自动预填');
    await page.getByTestId('publish-target-71').check();
    await page.getByTestId('publish-next-content').click();

    const contentCard = page.getByTestId('publish-content-61');
    await expect(contentCard).toBeVisible();
    await contentCard.click();
    await expect(contentCard).toHaveAttribute('aria-pressed', 'true');
    await expect(page.getByTestId('publish-content-summary')).toContainText('文章填充测试');
    await expect(page.getByTestId('publish-platform-tags-csdn')).toHaveValue('AlphaNexus');
    await expect(page.getByTestId('publish-next-review')).toBeEnabled();
  });

  test('saves a per-platform variant, freezes, and only prepares to final review', async ({ page }) => {
    const content = { id: 51, type: 'video', title: '公共标题', caption: '公共描述', body: '公共描述', tags_json: '[]', status: 'draft', publish_count: 0 };
		const graphContent = { id: 52, type: 'graph_text', title: '图文内容', caption: '', body: '图文正文', tags_json: '[]', status: 'draft', publish_count: 0 };
    const media = { id: 9, content_id: 51, kind: 'video', file_path: '/tmp/local.mp4', original_path: '/tmp/local.mp4', mime_type: 'video/mp4', size_bytes: 10, sha256: 'a'.repeat(64), width: 1080, height: 1920, duration_ms: 6000 };
    const planCalls: Record<string, unknown>[] = [];
    const toolCalls: Record<string, unknown>[] = [];
    let approveCalls = 0;
    let prepareCalls = 0;
    let submitCalls = 0;
		const account = (id: number, name: string, platformCode: string, loginState: 'logged_in' | 'logged_out', loginReasonCode = loginState === 'logged_in' ? 'VERIFIED_PROFILE_DOM' : 'EXPLICIT_LOGIN_DOM') => ({
			id, identityId: id, name, platformCode, kind: 'browser', avatar: '', accountIdentifier: '', loginState, loginReasonCode,
			remark: '', enabled: true, browserRunning: true, fingerprintSeed: 0, fingerprintConfig: '', tags: [],
		});

		await page.route('**/api/contents', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([content, graphContent]) }));
    await page.route('**/api/contents/51', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify(content) }));
    await page.route('**/api/accounts', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([
			account(301, '小红书账号', 'xhs', 'logged_in'),
			account(302, '抖音账号', 'douyin', 'logged_in'),
			account(303, 'YouTube 账号', 'youtube', 'logged_in'),
			account(304, '未验证账号', 'kuaishou', 'logged_in', 'CDP_UNREACHABLE'),
    ]) }));
    await page.route('**/api/ego/spaces', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([
        { identity_id: 301, ownership: 'agent', state: 'ready' },
        { identity_id: 302, ownership: 'agent', state: 'ready' },
      ]),
    }));
		await page.route('**/api/contents/51/media', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([media]) }));
    await page.route('**/api/account-assets', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([
        { id: 31, account_id: 301, platform_code: 'xhs', asset_type: 'browser_account', name: '小红书账号', external_id: '', avatar: '', connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
        { id: 32, account_id: 302, platform_code: 'douyin', asset_type: 'browser_account', name: '抖音账号', external_id: '', avatar: '', connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
        { id: 33, account_id: 303, platform_code: 'youtube', asset_type: 'browser_account', name: 'YouTube 账号', external_id: '', avatar: '', connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
        { id: 34, account_id: 304, platform_code: 'kuaishou', asset_type: 'browser_account', name: '掉线账号', external_id: '', avatar: '', connected_asset_id: 0, connected_asset_name: '', enabled: true, last_seen_at: '' },
      ]),
    }));
    await page.route('**/api/account-tags', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify([{ id: 7, name: '主矩阵', color: '#22c55e', accountCount: 2 }]) }));
    await page.route('**/api/account-tags/resolve', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ count: 2, accounts: [{ id: 301, platform: 'xhs', name: '小红书账号' }, { id: 303, platform: 'youtube', name: 'YouTube 账号' }] }) }));
		await page.route('**/api/accounts/*/observe', (route) => {
			const accountId = Number(route.request().url().match(/accounts\/(\d+)\/observe/)?.[1]);
			return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ accountId, platformCode: accountId === 301 ? 'xhs' : 'douyin', authState: 'logged_in', persistedLoginState: 'logged_in', browserState: 'ready', loginMode: 'browser_profile_session', authMethod: 'unknown', reasonCode: 'VERIFIED_PROFILE_DOM', evidenceCode: 'VERIFIED_PROFILE_DOM', avatarSource: 'observed', observedAt: '2026-07-20T12:00:00Z', traceId: 'test' }) });
		});
    await page.route('**/api/templates', (route) => route.fulfill({ contentType: 'application/json', body: '[]' }));
    await page.route('**/api/platforms', (route) => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify([
        { code: 'xhs', name: '小红书', loginUrl: '', managerUrl: '', publishMode: 'manual_handoff', canCreatePublishJob: true, canAttemptAutoSubmit: false, publishSummary: '', settingsSchema: { platform_code: 'xhs', version: 1, fields: [{ key: 'visibility', label: '可见范围', type: 'enum', required: false, default: 'public', options: [{ value: 'public', label: '公开' }, { value: 'private', label: '私密' }] }, { key: 'comment_enabled', label: '允许评论', type: 'boolean', required: false, default: true }, { key: 'declare_original', label: '声明原创', type: 'boolean', required: false, default: false }] } },
        { code: 'douyin', name: '抖音', loginUrl: '', managerUrl: '', publishMode: 'manual_handoff', canCreatePublishJob: true, canAttemptAutoSubmit: false, publishSummary: '', settingsSchema: { platform_code: 'douyin', version: 1, fields: [{ key: 'visibility', label: '可见范围', type: 'enum', required: false, default: 'public', options: [{ value: 'public', label: '公开' }, { value: 'private', label: '私密' }] }, { key: 'declare_original', label: '声明原创', type: 'boolean', required: false, default: false }, { key: 'allow_download', label: '允许下载', type: 'boolean', required: false, default: true }, { key: 'allow_duet', label: '允许合拍', type: 'boolean', required: false, default: true }, { key: 'allow_stitch', label: '允许转载', type: 'boolean', required: false, default: true }] } },
      ]),
    }));
    await page.route('**/api/v1/tools/call', async (route) => {
      toolCalls.push(route.request().postDataJSON());
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ schema_version: 1, request_id: 'variant', result: { ok: true, tool: 'content.variant.upsert', data: { variant: { id: 71 } } } }) });
    });
    await page.route('**/api/publish-plans/77/approve', async (route) => {
      approveCalls += 1;
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ approvalId: 'approval-test', nonce: 'nonce-test', expiresAt: '2030-01-01T00:00:00Z' }) });
    });
    await page.route('**/api/publish-plans/77/prepare', async (route) => {
      prepareCalls += 1;
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ planId: 77, status: 'awaiting_review', trace: 'trace-test', targets: [{ accountId: 301, platformCode: 'xhs', jobId: 1, status: 'awaiting_review', step: 'ready_to_submit' }, { accountId: 302, platformCode: 'douyin', jobId: 2, status: 'awaiting_review', step: 'ready_to_submit' }] }) });
    });
    await page.route('**/api/publish-plans/77/submit', async (route) => {
      submitCalls += 1;
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ planId: 77, status: 'awaiting_verification', trace: 'trace-submit', targets: [{ accountId: 301, platformCode: 'xhs', jobId: 1, status: 'awaiting_verification', step: 'creator_backend_verification' }, { accountId: 302, platformCode: 'douyin', jobId: 2, status: 'awaiting_verification', step: 'creator_backend_verification' }] }) });
    });
    await page.route('**/api/publish-plans/77', (route) => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 77, contentId: 51, mediaMode: 'video', snapshotHash: 'f'.repeat(64), status: 'approved', concurrency: 1, snapshot: { version: 1, media_mode: 'video', concurrency: 1, media: [{ id: 9, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64), size_bytes: 10 }], content, targets: [{ account_id: 301, asset_id: 31, platform_code: 'xhs', title: '公共标题', caption: '公共描述', body: '公共描述', tags_json: '[]', settings_json: '{}' }, { account_id: 302, asset_id: 32, platform_code: 'douyin', variant_id: 71, title: '抖音单独标题', caption: '', body: '抖音单独正文', tags_json: '["抖音","视频"]', settings_json: '{}' }] } }) }));
    await page.route('**/api/publish-plans', async (route) => {
      const request = route.request().postDataJSON();
      planCalls.push(request);
      await route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 77, contentId: 51, mediaMode: 'video', snapshotHash: 'f'.repeat(64), status: 'frozen', concurrency: 1, snapshot: { version: 1, media_mode: 'video', concurrency: 1, media: [{ id: 9, kind: 'video', role: 'video', position: 0, sha256: 'a'.repeat(64), size_bytes: 10 }], content, targets: [{ account_id: 301, asset_id: 31, platform_code: 'xhs', title: '公共标题', caption: '公共描述', body: '公共描述', tags_json: '[]', settings_json: '{}' }, { account_id: 302, asset_id: 32, platform_code: 'douyin', variant_id: 71, title: '抖音单独标题', caption: '', body: '抖音单独正文', tags_json: '["抖音","视频"]', settings_json: '{}' }] } }) });
    });

    await page.reload();
    await page.getByTestId('nav-publish').click();
    await page.getByTestId('publish-account-tag-picker').locator('summary').click();
    await page.getByTestId('publish-account-tag-7').click();
    await page.getByTestId('publish-select-by-tags').click();
    await expect(page.getByTestId('publish-capability-31')).toContainText('人工完成');
    await expect(page.getByTestId('publish-capability-32')).toContainText('自动预填');
    await expect(page.getByTestId('publish-target-31')).toBeChecked();
    await expect(page.getByTestId('publish-target-32')).not.toBeChecked();
    await page.getByTestId('publish-target-32').check();
    await expect(page.getByTestId('publish-target-33')).toBeDisabled();
    await expect(page.getByTestId('publish-target-34')).toBeDisabled();
    await page.getByTestId('publish-next-content').click();
		await expect(page.getByTestId('publish-content-52')).toHaveCount(0);
    await page.getByTestId('publish-content-51').click();
		await expect(page.getByTestId('publish-content-51')).toHaveAttribute('aria-pressed', 'true');
		await expect(page.getByTestId('publish-platform-title-douyin')).toHaveCount(0);
		await page.getByTestId('publish-platform-tab-douyin').click();
		await page.getByTestId('publish-platform-title-douyin').fill('抖音单独标题');
		await page.getByTestId('publish-platform-body-douyin').fill('抖音单独正文');
		await page.getByTestId('publish-platform-tags-douyin').fill('抖音, 视频');
		await expect(page.getByTestId('publish-settings-douyin-visibility')).toHaveValue('public');
    await page.getByTestId('publish-next-review').click();

    await expect(page.getByTestId('publish-step-review')).toHaveAttribute('aria-pressed', 'true');
    expect(toolCalls).toHaveLength(2);
    expect(toolCalls.every((call) => (call as { name: string }).name === 'content.variant.upsert')).toBe(true);
		const douyinVariant = toolCalls.find((call) => (call.args as Record<string, unknown>).platform === 'douyin')?.args as Record<string, unknown>;
		expect(douyinVariant).toMatchObject({ title: '抖音单独标题', body: '抖音单独正文', caption: '', tags_json: '["抖音","视频"]' });
    expect(planCalls).toHaveLength(1);
    const targets = (planCalls[0].targets as Array<Record<string, unknown>>);
    expect(targets).toHaveLength(2);
    expect(targets[1].variant_id).toBe(71);
    expect(JSON.parse(String(targets[1].settings_json))).toEqual({
      visibility: 'public',
      declare_original: false,
      allow_download: true,
      allow_duet: true,
      allow_stitch: true,
    });
    expect(approveCalls).toBe(0);
    expect(prepareCalls).toBe(0);

    // Returning to content without changing a frozen plan must not create
    // another plan or another set of platform variants.
    await page.getByTestId('publish-step-content').click();
    await page.getByTestId('publish-next-review').click();
    await expect(page.getByTestId('publish-step-review')).toHaveAttribute('aria-pressed', 'true');
    expect(toolCalls).toHaveLength(2);
    expect(planCalls).toHaveLength(1);

    await page.getByTestId('publish-plan-prepare').click();
    await expect(page.getByTestId('publish-plan-submit')).toContainText('确认并提交一次');
    expect(approveCalls).toBe(1);
    expect(prepareCalls).toBe(1);
    expect(submitCalls).toBe(0);

    await page.getByTestId('publish-plan-submit').click();
    await expect(page.getByTestId('publish-plan-submit')).toContainText('已提交，等待核验');
    expect(approveCalls).toBe(2);
    expect(prepareCalls).toBe(1);
    expect(submitCalls).toBe(1);

    // Editing a platform field invalidates both the frozen plan and its old
    // execution result, so the next confirmation produces a fresh snapshot.
    await page.getByTestId('publish-step-content').click();
    await page.getByTestId('publish-platform-title-douyin').fill('抖音更新后的标题');
    await page.getByTestId('publish-next-review').click();
    await expect(page.getByTestId('publish-plan-prepare')).toContainText('预填并打开平台');
    expect(planCalls).toHaveLength(2);
  });
});
