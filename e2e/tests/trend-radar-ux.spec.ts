import { expect, test } from '@playwright/test';

const now = '2026-07-26T09:00:00Z';

function signal(overrides: Record<string, unknown> = {}) {
  return {
    niche_id: 'ai',
    niche_name: 'AI赛道',
    search_term: 'AI工具',
    platform: 'douyin',
    title: '测试信号',
    content_text: '测试正文',
    content_source: 'provider_text',
    creator: '测试博主',
    observed_at: now,
    first_seen_at: now,
    occurrences: 1,
    is_new: true,
    play: 0,
    likes: 0,
    collects: 0,
    shares: 0,
    ...overrides,
  };
}

function summary(overrides: Record<string, unknown> = {}) {
  return {
    generated_at: now,
    last_synced_at: now,
    snapshot_state: 'ready',
    storage: 'local',
    search_runs: 1,
    signals: [],
    video_lookups: [],
    niches: [{
      id: 'ai',
      name: 'AI赛道',
      description: 'AI 工具',
      enabled: true,
      search_terms: ['AI工具'],
      platforms: ['douyin', 'x'],
      state: 'ready',
      result_count: 0,
      search_runs: 1,
      last_synced_at: now,
    }],
    creators: [],
    sources: [],
    ...overrides,
  };
}

async function openRadar(page: import('@playwright/test').Page, payload: Record<string, unknown>) {
  await page.route('**/api/trend-radar/summary', (route) => route.fulfill({
    contentType: 'application/json',
    body: JSON.stringify(payload),
  }));
  await page.goto('/');
  await page.getByTestId('nav-dashboard').click();
  await expect(page.getByTestId('trend-radar-dashboard')).toBeVisible();
}

test.describe('trend radar decision quality', () => {
  test('shows creator growth and new-post metrics in the creator folder', async ({ page }) => {
    await openRadar(page, summary({
      creators: [{
        platform: 'douyin',
        user_id: 'creator-1',
        name: 'AI 效率研究所',
        folder_path: '/tmp/radar/creators/creator-1',
        lookback_days: 30,
        page_size: 20,
        max_pages: 3,
        sort_type: 0,
        snapshot_count: 3,
        last_pages: 2,
        profile_checked_at: now,
        followers: 125000,
        followers_delta: 2300,
        total_likes: 980000,
        total_likes_delta: 7200,
        last_check: now,
        last_count: 12,
        new_posts: 4,
        total_engagement: 8500,
        engagement_delta: 1700,
        latest_posts: [],
        state: 'ready',
        message: '',
      }],
    }));

    await page.getByRole('tab', { name: '博主监控' }).click();
    const folder = page.getByTestId('creator-folder-douyin-creator-1');
    await expect(folder).toContainText('粉丝 12.5万');
    await expect(folder).toContainText('较上次 +2,300');
    await expect(folder).toContainText('新增作品 4');
    await expect(folder).toContainText('互动 +1,700');
  });

  test('labels metadata-only LLM output and its unsupported evidence boundary', async ({ page }) => {
    const video = signal({
      niche_id: 'video_lookup',
      niche_name: '视频搜索',
      search_term: '视频详情',
      platform: 'douyin',
      title: '只有标题和公开指标的视频',
      video_id: '7666283984318131889',
      call_id: 88,
      analysis_status: 'succeeded',
      analysis: {
        schema_version: 1,
        video_id: '7666283984318131889',
        source_call_id: 88,
        status: 'succeeded',
        route: 'agent',
        provider: 'agent',
        model: 'test-model',
        source_sha256: 'fixture',
        started_at: now,
        finished_at: now,
        duration_ms: 120,
        result: {
          summary: '基于标题生成的推断',
          viral_potential: '可能传播',
          hook: '未验证',
          evidence: ['点赞 10'],
          structure: ['未验证'],
          audience: '未验证',
          emotional_drivers: ['未验证'],
          topic_suggestions: ['选题一'],
          analysis_prompts: [],
          limitations: ['没有逐字稿'],
        },
      },
    });
    await openRadar(page, summary({ video_lookups: [video] }));

    await page.getByRole('tab', { name: '视频搜索' }).click();
    await expect(page.getByText('LLM 元数据分析', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: /查看视频拆解/ }).click();
    await expect(page.getByRole('heading', { name: '未含逐字稿：只可分析元数据' })).toBeVisible();
    await expect(page.getByText(/行业基准、口播、画面与内容结构均未验证/)).toBeVisible();
  });

  test('sorts cross-platform engagement by relative performance inside each platform', async ({ page }) => {
    await openRadar(page, summary({
      signals: [
        signal({ call_id: 1, platform: 'douyin', title: '抖音平台低位', likes: 100, observed_at: '2026-07-26T06:00:00Z' }),
        signal({ call_id: 2, platform: 'douyin', title: '抖音平台高位', likes: 10000, observed_at: '2026-07-26T07:00:00Z' }),
        signal({ call_id: 3, platform: 'x', title: 'X 平台低位', likes: 50, observed_at: '2026-07-26T08:00:00Z' }),
        signal({ call_id: 4, platform: 'x', title: 'X 平台高位', likes: 5000, observed_at: '2026-07-26T09:00:00Z' }),
      ],
    }));

    await page.getByRole('button', { name: '高互动', exact: true }).click();
    const rows = page.locator('[data-testid^="trend-signal-"]');
    await expect(rows).toHaveCount(4);
    await expect(rows.nth(0)).toContainText('X 平台高位');
    await expect(page.getByText('按平台内相对互动排序')).toBeVisible();
  });

  test('rejects lookalike creator domains before saving a monitor', async ({ page }) => {
    let saves = 0;
    await page.route('**/api/trend-radar/creators', async (route) => {
      if (route.request().method() === 'POST') saves += 1;
      await route.fulfill({ contentType: 'application/json', body: '{}' });
    });
    await openRadar(page, summary());

    await page.getByRole('tab', { name: '抓取' }).click();
    await page.getByRole('button', { name: '添加监控博主' }).click();
    await page.getByPlaceholder('粘贴主页链接或 sec_user_id').fill(
      'https://douyin.com.evil.example/user/pretend-creator',
    );
    await page.getByPlaceholder('例如：AI 效率研究所').fill('伪造域名');
    await page.getByRole('button', { name: '加入名单' }).click();

    const alert = page.getByRole('alert');
    await expect(alert).toContainText('请输入有效的抖音或小红书主页链接 / ID');
    await page.waitForTimeout(3_200);
    await expect(alert).toBeVisible();
    await alert.getByRole('button', { name: '关闭通知' }).click();
    await expect(alert).toHaveCount(0);
    expect(saves).toBe(0);
  });
});
