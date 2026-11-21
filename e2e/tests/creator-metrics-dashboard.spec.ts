import { test, expect } from '@playwright/test';

test('shows one browser all-platform snapshots and only collects after explicit click', async ({ page }) => {
  const account = (id: number, identityId: number, platformCode: string, name: string, loginState = 'logged_in') => ({
    id,
    identityId,
    platformCode,
    kind: 'browser',
    name,
    avatar: '',
    accountIdentifier: `${platformCode}-${id}`,
    loginState,
    remark: '',
    accountGroup: '',
    enabled: true,
    browserRunning: false,
    fingerprintSeed: 0,
    fingerprintConfig: '',
    tags: [],
  });
  const accounts = [
    account(11, 1, 'douyin', '浏览器1抖音'),
    account(12, 1, 'wechat_video', '浏览器1视频号'),
    account(13, 1, 'kuaishou', '浏览器1快手'),
    account(14, 1, 'xhs', '浏览器1小红书', 'logged_out'),
	account(15, 1, 'tiktok', '浏览器1 TikTok'),
	account(16, 1, 'youtube', '浏览器1 YouTube'),
	account(17, 1, 'meta', '浏览器1 Meta'),
	account(18, 1, 'wechat_mp', '浏览器1公众号'),
	account(19, 1, 'csdn', '墨衍 MoGrow'),
    account(21, 2, 'douyin', '浏览器2不应显示'),
  ];
  const latest = [
    { id: 101, account_id: 11, account_name: '浏览器1抖音', platform_code: 'douyin', snapshot_date: '2026-08-01', window_days: 7, range_verified: true, followers: 0, followers_verified: false, views: 12000, likes: 800, comments: 70, shares: 40, favorites: 50, posts: 4, evidence_code: 'REVIEWED_ANALYTICS_DOM', collected_at: '2026-08-01T01:00:00Z' },
    { id: 102, account_id: 12, account_name: '浏览器1视频号', platform_code: 'wechat_video', snapshot_date: '2026-08-01', window_days: 7, range_verified: true, followers: 0, followers_verified: false, views: 8000, likes: 500, comments: 40, shares: 30, favorites: 20, posts: 3, evidence_code: 'REVIEWED_ANALYTICS_DOM', collected_at: '2026-08-01T01:01:00Z' },
    { id: 103, account_id: 13, account_name: '浏览器1快手', platform_code: 'kuaishou', snapshot_date: '2026-08-01', window_days: 7, range_verified: true, followers: 0, followers_verified: false, views: 6000, likes: 400, comments: 30, shares: 20, favorites: 10, posts: 2, evidence_code: 'REVIEWED_ANALYTICS_DOM', collected_at: '2026-08-01T01:02:00Z' },
    { id: 199, account_id: 21, account_name: '浏览器2不应显示', platform_code: 'douyin', snapshot_date: '2026-08-01', window_days: 7, range_verified: true, followers: 999, followers_verified: true, views: 999999, likes: 999, comments: 999, shares: 999, favorites: 999, posts: 99, evidence_code: 'REVIEWED_ANALYTICS_RESOURCE', collected_at: '2026-08-01T01:03:00Z' },
  ];
  const collections = accounts.map((item) => ({
    account_id: item.id,
    account_name: item.name,
    platform_code: item.platformCode,
    state: item.id === 14 ? 'skipped' : item.id <= 13 || item.id === 19 || item.id === 21 ? 'observed' : 'unknown',
    error_code: item.id === 14 ? 'ACCOUNT_NOT_LOGGED_IN' : item.id >= 15 && item.id <= 19 ? 'METRICS_DOM_UNRECOGNIZED' : '',
    evidence_code: item.id <= 13 || item.id === 19 || item.id === 21 ? 'REVIEWED_ANALYTICS_DOM' : '',
    range_verified: item.id <= 13 || item.id === 19 || item.id === 21,
    post_count: item.id <= 13 || item.id === 19 || item.id === 21 ? 2 : 0,
    collected_at: '2026-08-01T01:05:00Z',
    updated_at: '2026-08-01T01:05:00Z',
  }));
  const toolCalls: Array<{ name: string; args?: Record<string, unknown> }> = [];
  const modelCalls: Array<Record<string, unknown>> = [];

  await page.route('**/api/accounts', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(accounts),
  }));
  await page.route('**/api/ego/identities', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify([
      { id: 1, name: '浏览器1', enabled: true, sort: 1, created_at: '', updated_at: '' },
      { id: 2, name: '浏览器2', enabled: true, sort: 2, created_at: '', updated_at: '' },
    ]),
  }));
  await page.route('**/api/v1/tools/call', async (route) => {
    const request = route.request().postDataJSON() as { name: string; args?: Record<string, unknown> };
    toolCalls.push(request);
    if (request.name === 'social.workflow.catalog') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 1,
          request_id: 'workflow-catalog',
          result: {
            ok: true,
            tool: request.name,
            data: {
              platforms: [
                { platform_code: 'douyin', platform_name: '抖音', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'wechat_video', platform_name: '视频号', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'kuaishou', platform_name: '快手', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'xhs', platform_name: '小红书', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'tiktok', platform_name: 'TikTok', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'prepare_then_submit_approval', publish_steps: [], submit_verified: false },
                { platform_code: 'youtube', platform_name: 'YouTube', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'prepare_then_submit_approval', publish_steps: [], submit_verified: false },
                { platform_code: 'meta', platform_name: 'Meta', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'wechat_mp', platform_name: '公众号', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
                { platform_code: 'csdn', platform_name: 'CSDN', data_workflow: 'automated_snapshot', data_steps: [], publish_workflow: 'freeze_then_manual_handoff', publish_steps: [], submit_verified: false },
              ],
            },
          },
        }),
      });
      return;
    }
    if (request.name === 'social.metrics.collect-batch') {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          schema_version: 1,
          request_id: 'metrics-collect',
          result: {
            ok: true,
            tool: request.name,
            data: {
              all_observed: false,
              accounts: [
                { account_id: 11, platform_code: 'douyin', ok: true, state: 'observed', range_verified: true },
                { account_id: 12, platform_code: 'wechat_video', ok: true, state: 'observed', range_verified: true },
                { account_id: 13, platform_code: 'kuaishou', ok: true, state: 'observed', range_verified: true },
                { account_id: 14, platform_code: 'xhs', ok: true, state: 'skipped', error_code: 'ACCOUNT_NOT_LOGGED_IN' },
				{ account_id: 15, platform_code: 'tiktok', ok: false, state: 'unknown', error_code: 'METRICS_DOM_UNRECOGNIZED' },
				{ account_id: 16, platform_code: 'youtube', ok: false, state: 'unknown', error_code: 'METRICS_DOM_UNRECOGNIZED' },
				{ account_id: 17, platform_code: 'meta', ok: false, state: 'unknown', error_code: 'METRICS_DOM_UNRECOGNIZED' },
				{ account_id: 18, platform_code: 'wechat_mp', ok: false, state: 'unknown', error_code: 'METRICS_DOM_UNRECOGNIZED' },
				{ account_id: 19, platform_code: 'csdn', ok: false, state: 'unknown', error_code: 'METRICS_DOM_UNRECOGNIZED' },
              ],
            },
          },
        }),
      });
      return;
    }
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        schema_version: 1,
        request_id: 'metrics-overview',
        result: {
          ok: true,
          tool: 'social.metrics.overview',
          data: {
            overview: {
              window_days: 7,
              from_date: '2026-07-26',
              to_date: '2026-08-01',
              latest,
              daily: [],
              account_daily: [
                { account_id: 11, platform_code: 'douyin', date: '2026-07-31', views: 3000, likes: 200, comments: 20, shares: 10, favorites: 15, posts: 1 },
                { account_id: 12, platform_code: 'wechat_video', date: '2026-07-31', views: 2000, likes: 100, comments: 10, shares: 8, favorites: 7, posts: 1 },
                { account_id: 11, platform_code: 'douyin', date: '2026-08-01', views: 12000, likes: 800, comments: 70, shares: 40, favorites: 50, posts: 4 },
                { account_id: 21, platform_code: 'douyin', date: '2026-08-01', views: 999999, likes: 999, comments: 999, shares: 999, favorites: 999, posts: 99 },
              ],
              platforms: [],
              top_posts: [
                { id: 301, snapshot_id: 101, account_id: 11, platform_code: 'douyin', external_post_id: 'post-11', title: '浏览器1最佳作品', published_at: '2026-07-31T00:00:00Z', post_url: '', views: 10000, likes: 700, comments: 60, shares: 30, favorites: 40 },
                { id: 399, snapshot_id: 199, account_id: 21, platform_code: 'douyin', external_post_id: 'post-21', title: '浏览器2作品不应显示', published_at: '2026-07-31T00:00:00Z', post_url: '', views: 999999, likes: 999, comments: 999, shares: 999, favorites: 999 },
              ],
              collections,
              insights: [],
            },
          },
        },
      }),
    });
  });
  await page.route('**/api/content-assist', async (route) => {
    const request = route.request().postDataJSON() as Record<string, unknown>;
    modelCalls.push(request);
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        result: '证据：浏览器1抖音近 7 天浏览 12000。\n局限：小红书未登录，未纳入推断。',
        source: 'configured_model',
        external: true,
        citations: [],
      }),
    });
  });

  await page.goto('/');
  await page.getByTestId('nav-analytics').click();
  await expect(page.getByTestId('creator-metrics-dashboard')).toBeVisible();
  await expect(page.getByTestId('creator-account-douyin')).toContainText('浏览器1抖音');
  await expect(page.getByTestId('creator-account-wechat_video')).toContainText('浏览器1视频号');
  await expect(page.getByTestId('creator-account-kuaishou')).toContainText('浏览器1快手');
  await expect(page.getByTestId('creator-account-xhs')).toContainText('未登录，已跳过');
  await expect(page.getByTestId('creator-account-tiktok')).toContainText('页面结构待适配');
  await expect(page.getByTestId('creator-account-csdn')).toContainText('已有历史快照，但今天尚未重新采集');
  await expect(page.getByTestId('creator-account-douyin').getByTestId('creator-platform-workflow')).toContainText('数据：自动快照');
  await expect(page.getByTestId('creator-account-tiktok').getByTestId('creator-platform-workflow')).toContainText('发布：审批后提交');
  await expect(page.getByTestId('creator-metrics-coverage')).toContainText('近 7 天已验证 3/9');
  await expect(page.getByTestId('creator-metrics-status-summary')).toContainText('已验证 3');
  await expect(page.getByTestId('creator-metrics-status-summary')).toContainText('证据不足 4');
  await expect(page.getByTestId('creator-metrics-status-summary')).toContainText('数据过期 1');
  await expect(page.getByTestId('creator-metrics-status-summary')).toContainText('未登录 1');
  await expect(page.getByTestId('creator-metrics-status-summary')).toContainText('未采集 0');
  await expect(page.getByTestId('creator-metrics-followers-total')).toContainText('—');
  await expect(page.getByTestId('creator-metrics-trend')).toContainText('1.2万');
  await expect(page.getByTestId('creator-metrics-trend').locator('[title*="999999"]')).toHaveCount(0);
  await expect(page.getByText('浏览器1最佳作品', { exact: true })).toBeVisible();
  await expect(page.getByText(/抖音近 7 天浏览最高/)).toBeVisible();
  await expect(page.getByText('浏览器2不应显示')).toHaveCount(0);
  await expect(page.getByText('浏览器2作品不应显示')).toHaveCount(0);

  expect(toolCalls.filter((call) => call.name === 'social.metrics.collect-batch')).toHaveLength(0);
  expect(modelCalls).toHaveLength(0);
  await page.getByTestId('review-selected-browser-metrics').click();
  await expect(page.getByTestId('creator-metrics-model-review')).toContainText('浏览器1抖音近 7 天浏览 12000');
  expect(modelCalls).toHaveLength(1);
  const reviewCall = modelCalls[0];
  expect(reviewCall.action).toBe('metrics_review');
  expect(reviewCall.allow_external).toBe(true);
  expect(reviewCall.use_knowledge).toBe(false);
  const reviewBody = JSON.parse(String(reviewCall.body));
  expect(reviewBody.identity).toBe('浏览器1');
  expect(reviewBody.snapshots).toHaveLength(3);
  expect(reviewBody.snapshots.every((snapshot: { followers: number | null }) => snapshot.followers === null)).toBe(true);
  expect(reviewBody.top_posts).toHaveLength(1);
  expect(reviewBody.top_posts[0].title).toBe('浏览器1最佳作品');
  expect(String(reviewCall.body)).not.toContain('浏览器2');
  expect(String(reviewCall.body)).not.toContain('999999');

  await page.getByTestId('collect-selected-browser-metrics').click();
  await expect.poll(() => toolCalls.filter((call) => call.name === 'social.metrics.collect-batch').length).toBe(1);
  const collectCall = toolCalls.find((call) => call.name === 'social.metrics.collect-batch');
  expect(collectCall?.args).toEqual({ account_ids: [11, 12, 13, 14, 15, 16, 17, 18, 19] });
});
